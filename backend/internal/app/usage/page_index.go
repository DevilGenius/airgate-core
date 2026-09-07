package usage

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"sort"
	"time"
)

const maxPageIndexWords = 1 << 20

var (
	ErrPageIndexExpired  = errors.New("usage pagination snapshot expired")
	ErrPageIndexTooLarge = errors.New("usage pagination index exceeds memory budget")
	ErrInvalidListFilter = errors.New("invalid usage list filter")
)

// PageWord stores membership of 64 adjacent IDs. It does not assume IDs are dense.
// End is the cumulative rank in descending ID order and is reconstructed on load.
type PageWord struct {
	Base int64  `json:"b"`
	Mask uint64 `json:"m"`
	End  int64  `json:"-"`
}

// PageIndex is an immutable, exact set of IDs observed by a single database query.
// A page lookup never scans or offsets the usage_logs history.
type PageIndex struct {
	Token     string     `json:"token"`
	FilterKey string     `json:"filter_key"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	Words     []PageWord `json:"words"`
	Total     int64      `json:"-"`
}

// AddID accepts strictly descending positive IDs while building the index.
func (p *PageIndex) AddID(id int64) error {
	if id <= 0 {
		return fmt.Errorf("invalid usage id: %d", id)
	}
	base, mask := id>>6, uint64(1)<<uint(id&63)
	if n := len(p.Words); n > 0 {
		last := &p.Words[n-1]
		if base > last.Base || (base == last.Base && mask >= last.Mask&-last.Mask) {
			return errors.New("usage IDs must be strictly descending")
		}
		if base == last.Base {
			last.Mask |= mask
			p.Total++
			last.End = p.Total
			return nil
		}
	}
	if len(p.Words) >= maxPageIndexWords {
		return ErrPageIndexTooLarge
	}
	p.Total++
	p.Words = append(p.Words, PageWord{Base: base, Mask: mask, End: p.Total})
	return nil
}

func (p *PageIndex) memoryBytes() int64 { return int64(cap(p.Words))*24 + 512 }

// Page returns only the target page's IDs; new inserts cannot shift this view.
func (p *PageIndex) Page(page, size int) ([]int64, int) {
	page, size = NormalizePage(page, size)
	last := int((p.Total + int64(size) - 1) / int64(size))
	if last < 1 {
		last = 1
	}
	if page > last {
		page = last
	}
	start := int64(page-1) * int64(size)
	index := sort.Search(len(p.Words), func(i int) bool { return p.Words[i].End > start })
	if index == len(p.Words) {
		return []int64{}, page
	}
	skip := start
	if index > 0 {
		skip -= p.Words[index-1].End
	}
	ids := make([]int64, 0, size)
	for ; index < len(p.Words) && len(ids) < size; index++ {
		word := p.Words[index]
		for mask := word.Mask; mask != 0 && len(ids) < size; {
			bit := bits.Len64(mask) - 1
			mask &^= uint64(1) << uint(bit)
			if skip > 0 {
				skip--
				continue
			}
			ids = append(ids, word.Base<<6|int64(bit))
		}
	}
	return ids, page
}

func encodePageIndex(p *PageIndex) ([]byte, error) {
	var out bytes.Buffer
	writer := gzip.NewWriter(&out)
	if err := json.NewEncoder(writer).Encode(p); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func decodePageIndex(raw []byte) (*PageIndex, error) {
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	// Bound both compressed cache entries and their expanded representation.
	data, err := io.ReadAll(io.LimitReader(reader, 64<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 64<<20 {
		return nil, ErrPageIndexTooLarge
	}
	var p PageIndex
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	if len(p.Words) > maxPageIndexWords {
		return nil, ErrPageIndexTooLarge
	}
	for i := range p.Words {
		w := &p.Words[i]
		if w.Base < 0 || w.Base > int64(^uint64(0)>>7) || w.Mask == 0 || (w.Base == 0 && w.Mask&1 != 0) || (i > 0 && p.Words[i-1].Base <= w.Base) {
			return nil, errors.New("invalid usage pagination bitmap")
		}
		p.Total += int64(bits.OnesCount64(w.Mask))
		w.End = p.Total
	}
	return &p, nil
}
