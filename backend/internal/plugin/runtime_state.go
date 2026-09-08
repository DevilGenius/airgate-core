package plugin

import (
	"container/list"
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// State belongs to Core, not a plugin generation. CAS prevents an old process
// from overwriting an update made through a different process's Host connection.
const runtimeStateMaxEntries = 65536
const runtimeStateMaxBytes = 128 << 20
const runtimeStateMaxValue = 256 << 10

type runtimeStateKey struct{ plugin, key string }
type runtimeStateEntry struct {
	key            runtimeStateKey
	value, version string
	expiry         time.Time
}
type runtimeStateStore struct {
	mu       sync.Mutex
	items    map[runtimeStateKey]*list.Element
	lru      list.List
	bytes    int
	sequence uint64
}

func (s *runtimeStateStore) remove(e *list.Element) {
	v := e.Value.(*runtimeStateEntry)
	s.bytes -= len(v.value) + len(v.key.key) + len(v.key.plugin)
	delete(s.items, v.key)
	s.lru.Remove(e)
}

func (s *runtimeStateStore) invoke(ctx context.Context, plugin string, payload []byte) (map[string]interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var req sdk.RuntimeStateRequest
	if len(payload) > runtimeStateMaxValue*2 || json.Unmarshal(payload, &req) != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid runtime state request")
	}
	if req.Action == "get_many" {
		if len(req.Keys) == 0 || len(req.Keys) > 64 {
			return nil, status.Error(codes.InvalidArgument, "invalid state key count")
		}
		for _, key := range req.Keys {
			if len(key) == 0 || len(key) > 512 {
				return nil, status.Error(codes.InvalidArgument, "invalid state key")
			}
		}
	} else if len(req.Key) == 0 || len(req.Key) > 512 {
		return nil, status.Error(codes.InvalidArgument, "invalid state key")
	}
	if req.Action != "get" && req.Action != "take" && req.Action != "get_many" && req.Action != "cas" && req.Action != "delete" {
		return nil, status.Error(codes.InvalidArgument, "invalid runtime state action")
	}
	if len(req.Value) > runtimeStateMaxValue {
		return nil, status.Error(codes.ResourceExhausted, "runtime state value too large")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = make(map[runtimeStateKey]*list.Element)
	}
	if req.Action == "get_many" {
		values := make(map[string]string)
		now := time.Now()
		for _, key := range req.Keys {
			if e := s.items[runtimeStateKey{plugin, key}]; e != nil {
				entry := e.Value.(*runtimeStateEntry)
				if !entry.expiry.After(now) {
					s.remove(e)
					continue
				}
				values[key] = entry.value
				s.lru.MoveToFront(e)
			}
		}
		return map[string]interface{}{"values": values}, nil
	}
	key, now := runtimeStateKey{plugin, req.Key}, time.Now()
	e := s.items[key]
	if e != nil && !e.Value.(*runtimeStateEntry).expiry.After(now) {
		s.remove(e)
		e = nil
	}
	version := "0"
	if e != nil {
		version = e.Value.(*runtimeStateEntry).version
	}
	if req.Action == "get" || req.Action == "take" {
		if e == nil {
			return map[string]interface{}{"found": false, "version": "0", "value": ""}, nil
		}
		s.lru.MoveToFront(e)
		value := e.Value.(*runtimeStateEntry).value
		if req.Action == "take" {
			s.remove(e)
		}
		return map[string]interface{}{"found": true, "version": version, "value": value}, nil
	}
	if req.Version != version {
		return map[string]interface{}{"swapped": false}, nil
	}
	if e != nil {
		s.remove(e)
	}
	if req.Action == "delete" {
		return map[string]interface{}{"swapped": true}, nil
	}
	for len(s.items) >= runtimeStateMaxEntries || s.bytes+len(req.Value)+len(req.Key)+len(plugin) > runtimeStateMaxBytes {
		s.remove(s.lru.Back())
	}
	s.sequence++
	entry := &runtimeStateEntry{key: key, value: req.Value, version: strconv.FormatUint(s.sequence, 10), expiry: now.Add(time.Hour)}
	s.items[key] = s.lru.PushFront(entry)
	s.bytes += len(req.Value) + len(req.Key) + len(plugin)
	return map[string]interface{}{"swapped": true, "version": entry.version}, nil
}
