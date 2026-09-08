package billing

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	journalMagic            = "AGBILL1\n"
	maxJournalRecordBytes   = 64 << 20
	journalHighWaterRecords = 100000
	journalHighWaterBytes   = 512 << 20
)

// Journal publishes immutable, checksummed events only after fsync. Database
// commit precedes removal; a resurrected/duplicate file is safe to replay using
// the same billing_event_id. Files live on the already-persistent plugin volume.
type Journal struct {
	mu        sync.Mutex
	dir, dead string
	entries   map[string]int64
	order     []string
	cursor    int
	bytes     int64
	lastErr   error
	state     atomic.Pointer[journalStats]
}

type journalStats struct {
	count int
	bytes int64
	err   error
}

func (j *Journal) publishStatsLocked() {
	j.state.Store(&journalStats{len(j.entries), j.bytes, j.lastErr})
}

func JournalDirectory(pluginDir, override string) string {
	if override != "" {
		return override
	}
	if pluginDir == "" {
		pluginDir = "data/plugins"
	}
	return filepath.Join(pluginDir, ".billing-journal")
}

func OpenJournal(dir string) (*Journal, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	j := &Journal{dir: filepath.Join(dir, "pending"), dead: filepath.Join(dir, "dead-letter"), entries: make(map[string]int64)}
	for _, path := range []string{j.dir, j.dead} {
		if err := os.MkdirAll(path, 0700); err != nil {
			return nil, err
		}
	}
	for _, path := range []string{dir, filepath.Dir(dir)} {
		if err := syncJournalDirectory(path); err != nil {
			return nil, err
		}
	}
	if err := j.Refresh(); err != nil {
		return nil, err
	}
	return j, nil
}

func journalName(id string) string {
	hash := sha256.Sum256([]byte(id))
	return hex.EncodeToString(hash[:]) + ".event"
}
func journalFile(name string) bool {
	if len(name) != 70 || !strings.HasSuffix(name, ".event") {
		return false
	}
	_, err := hex.DecodeString(name[:64])
	return err == nil
}

func encodeJournalRecord(record UsageRecord) ([]byte, error) {
	var payload bytes.Buffer
	if err := gob.NewEncoder(&payload).Encode(record); err != nil {
		return nil, err
	}
	if payload.Len() > maxJournalRecordBytes {
		return nil, fmt.Errorf("billing event exceeds journal record limit")
	}
	sum := sha256.Sum256(payload.Bytes())
	data := make([]byte, 0, len(journalMagic)+len(sum)+payload.Len())
	data = append(data, journalMagic...)
	data = append(data, sum[:]...)
	data = append(data, payload.Bytes()...)
	return data, nil
}

func readJournalRecord(path string) (UsageRecord, error) {
	var record UsageRecord
	f, err := os.Open(path)
	if err != nil {
		return record, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxJournalRecordBytes+41))
	if err != nil {
		return record, err
	}
	if len(data) < 40 || len(data) > maxJournalRecordBytes+40 || string(data[:8]) != journalMagic {
		return record, fmt.Errorf("invalid billing journal envelope")
	}
	sum := sha256.Sum256(data[40:])
	if !bytes.Equal(sum[:], data[8:40]) {
		return record, fmt.Errorf("billing journal checksum mismatch")
	}
	if err := gob.NewDecoder(bytes.NewReader(data[40:])).Decode(&record); err != nil {
		return record, err
	}
	if record.BillingEventID == "" || journalName(record.BillingEventID) != filepath.Base(path) {
		return record, fmt.Errorf("billing journal identity mismatch")
	}
	return record, nil
}

func (j *Journal) Append(record UsageRecord) (UsageRecord, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	defer j.publishStatsLocked()
	name := journalName(record.BillingEventID)
	target := filepath.Join(j.dir, name)
	if existing, err := readJournalRecord(target); err == nil {
		// Another process may have linked this event but not synced the directory
		// yet. A duplicate acknowledgement must establish durability as well.
		if err := syncJournalDirectory(j.dir); err != nil {
			j.lastErr = err
			return record, err
		}
		return existing, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		j.lastErr = err
		return record, err
	}
	data, err := encodeJournalRecord(record)
	if err != nil {
		j.lastErr = err
		return record, err
	}
	f, err := os.CreateTemp(j.dir, ".writing-")
	if err != nil {
		j.lastErr = err
		return record, err
	}
	nameTemp := f.Name()
	defer func() { _ = os.Remove(nameTemp) }()
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		// Link is an atomic publish-if-absent on the same volume. Concurrent
		// writers cannot replace the first accepted payload for an event ID.
		err = os.Link(nameTemp, target)
		if errors.Is(err, os.ErrExist) {
			err = nil
		}
	}
	if err == nil {
		err = syncJournalDirectory(j.dir)
	}
	if err != nil {
		j.lastErr = err
		return record, err
	}
	canonical, err := readJournalRecord(target)
	if errors.Is(err, os.ErrNotExist) {
		return record, nil
	} // Another worker may already have committed it.
	if err != nil {
		j.lastErr = err
		return record, err
	}
	if _, exists := j.entries[name]; !exists {
		info, err := os.Stat(target)
		if err != nil {
			return record, err
		}
		j.entries[name] = info.Size()
		j.bytes += info.Size()
		j.order = append(j.order, name)
	}
	j.lastErr = nil
	return canonical, nil
}

func (j *Journal) Refresh() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	defer j.publishStatsLocked()
	files, err := os.ReadDir(j.dir)
	if err != nil {
		j.lastErr = err
		return err
	}
	entries := make(map[string]int64, len(files))
	var total int64
	for _, file := range files {
		if !journalFile(file.Name()) || file.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := file.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			j.lastErr = err
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		entries[file.Name()] = info.Size()
		total += info.Size()
	}
	for name := range entries {
		if _, exists := j.entries[name]; !exists {
			j.order = append(j.order, name)
		}
	}
	j.entries = entries
	j.bytes = total
	j.compactOrder()
	return nil
}

func (j *Journal) compactOrder() {
	if len(j.order) <= len(j.entries)*2+1024 {
		return
	}
	order := make([]string, 0, len(j.entries))
	seen := make(map[string]bool, len(j.entries))
	for _, name := range j.order {
		if _, ok := j.entries[name]; ok && !seen[name] {
			order = append(order, name)
			seen[name] = true
		}
	}
	j.order = order
	j.cursor = 0
}

func (j *Journal) Batch(limit int) ([]UsageRecord, error) {
	j.mu.Lock()
	names := make([]string, 0, limit)
	seen := make(map[string]bool, limit)
	for visited := 0; visited < len(j.order) && len(names) < limit; visited++ {
		if j.cursor >= len(j.order) {
			j.cursor = 0
		}
		name := j.order[j.cursor]
		j.cursor++
		if _, ok := j.entries[name]; ok && !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	j.mu.Unlock()
	batch := make([]UsageRecord, 0, len(names))
	for _, name := range names {
		record, err := readJournalRecord(filepath.Join(j.dir, name))
		if errors.Is(err, os.ErrNotExist) {
			j.forget(name)
			continue
		}
		if err != nil {
			slog.Error("billing_journal_corrupt_record", "file", name, "error", err)
			if moveErr := j.quarantineName(name, err.Error()); moveErr != nil {
				return nil, moveErr
			}
			continue
		}
		batch = append(batch, record)
	}
	return batch, nil
}

func (j *Journal) forget(name string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	defer j.publishStatsLocked()
	j.bytes -= j.entries[name]
	delete(j.entries, name)
	j.compactOrder()
}
func (j *Journal) Ack(id string) error {
	name := journalName(id)
	err := os.Remove(filepath.Join(j.dir, name))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	j.forget(name)
	// A crash before deletion is durable only causes an idempotent replay.
	return nil
}

func (j *Journal) quarantineName(name, reason string) error {
	if !journalFile(name) {
		return fmt.Errorf("invalid journal event name")
	}
	if err := os.Rename(filepath.Join(j.dir, name), filepath.Join(j.dead, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := syncJournalDirectory(j.dead); err != nil {
		return err
	}
	if err := syncJournalDirectory(j.dir); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(j.dead, name+".error.txt"), []byte(reason), 0600); err != nil {
		return err
	}
	j.forget(name)
	return nil
}
func (j *Journal) Quarantine(id, reason string) error {
	return j.quarantineName(journalName(id), reason)
}

// Requeue is an explicit operator action after correcting the permanent cause.
// Event identity and all original financial fields remain unchanged.
func (j *Journal) Requeue(id string) error {
	name := journalName(id)
	source := filepath.Join(j.dead, name)
	if _, err := readJournalRecord(source); err != nil {
		return err
	}
	if err := os.Link(source, filepath.Join(j.dir, name)); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if err := syncJournalDirectory(j.dir); err != nil {
		return err
	}
	if err := os.Remove(source); err != nil {
		return err
	}
	_ = os.Remove(source + ".error.txt")
	return j.Refresh()
}

func (j *Journal) Stats() (int, int64, error) {
	if state := j.state.Load(); state != nil {
		return state.count, state.bytes, state.err
	}
	return 0, 0, nil
}
