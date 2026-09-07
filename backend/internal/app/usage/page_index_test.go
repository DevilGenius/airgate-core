package usage

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPageIndexRankSelectMatchesSparseRecords(t *testing.T) {
	ids := []int64{math.MaxInt64, 1<<40 + 63, 1 << 40, 130, 128, 127, 65, 64, 63, 2, 1}
	random := rand.New(rand.NewPCG(12, 34))
	seen := map[int64]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	for range 15000 {
		id := random.Int64N(2_000_000) + 1000
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	index := &PageIndex{}
	for _, id := range ids {
		if err := index.AddID(id); err != nil {
			t.Fatal(err)
		}
	}
	if index.Total != int64(len(ids)) {
		t.Fatalf("total=%d", index.Total)
	}
	for _, size := range []int{1, 2, 20, 50, 100} {
		last := (len(ids) + size - 1) / size
		for _, page := range []int{1, 2, 3, last / 2, last, last + 100, math.MaxInt} {
			wantPage := max(1, min(page, last))
			start := (wantPage - 1) * size
			want := ids[start:min(start+size, len(ids))]
			got, actual := index.Page(page, size)
			if actual != wantPage || !reflect.DeepEqual(got, want) {
				t.Fatalf("size=%d page=%d actual=%d got=%v want=%v", size, page, actual, got, want)
			}
		}
	}
	for _, id := range []int64{1, 2, 64, 0, -1} {
		if err := index.AddID(id); err == nil {
			t.Fatalf("accepted non-descending ID %d", id)
		}
	}
}

func TestPageIndexRoundTripAndEmpty(t *testing.T) {
	index := &PageIndex{Token: uuid.NewString(), FilterKey: "scope", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour)}
	for id := int64(10000); id > 0; id-- {
		if id%7 != 0 {
			if err := index.AddID(id); err != nil {
				t.Fatal(err)
			}
		}
	}
	raw, err := encodePageIndex(index)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodePageIndex(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(index, decoded) {
		t.Fatal("round trip changed membership, ranks or identity")
	}
	if _, err := decodePageIndex(raw[:len(raw)-4]); err == nil {
		t.Fatal("accepted truncated gzip")
	}
	if _, err := decodePageIndex([]byte("invalid")); err == nil {
		t.Fatal("accepted invalid cache data")
	}
	bad := &PageIndex{Words: []PageWord{{Base: 1, Mask: 1}, {Base: 1, Mask: 2}}}
	raw, err = encodePageIndex(bad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodePageIndex(raw); err == nil {
		t.Fatal("accepted duplicate bitmap word")
	}
	got, page := (&PageIndex{}).Page(math.MaxInt, 100)
	if page != 1 || len(got) != 0 {
		t.Fatalf("empty page=%d ids=%v", page, got)
	}
}

func TestPaginationCacheIsolationEvictionAndPinnedVersions(t *testing.T) {
	cache := newPaginationCache(nil, nil)
	old := &PageIndex{Token: uuid.NewString(), FilterKey: "owner-1", CreatedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour)}
	current := &PageIndex{Token: uuid.NewString(), FilterKey: old.FilterKey, CreatedAt: time.Now(), ExpiresAt: old.ExpiresAt}
	cache.put(current)
	cache.put(old)
	if cache.latest[old.FilterKey] != current.Token {
		t.Fatal("old pinned view replaced latest")
	}
	if cache.load(context.Background(), old.Token, "owner-2") != nil {
		t.Fatal("cross-owner snapshot access")
	}
	if cache.load(context.Background(), old.Token, "owner-1") != old {
		t.Fatal("pinned snapshot missing")
	}
	entry := cache.entries[old.Token]
	entry.usedAt = time.Now().Add(-time.Hour)
	cache.entries[old.Token] = entry
	for i := 0; i < pageIndexMaxEntries+5; i++ {
		cache.put(&PageIndex{Token: uuid.NewString(), FilterKey: fmt.Sprint(i), CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)})
	}
	if len(cache.entries) > pageIndexMaxEntries || cache.bytes > pageIndexCacheBytes {
		t.Fatal("cache exceeded bounds")
	}
	if cache.local(old.Token) != nil {
		t.Fatal("oldest snapshot not evicted")
	}
}

func TestPaginationFiltersRejectInvalidDatesAndSeparateScopes(t *testing.T) {
	for _, filter := range []ListFilter{{StartDate: "bad"}, {EndDate: "2026-99-99"}, {StartDate: "2026-09-07", EndDate: "2026-09-06"}} {
		if _, err := normalizedListFilter(filter); !errors.Is(err, ErrInvalidListFilter) {
			t.Fatalf("filter=%+v error=%v", filter, err)
		}
	}
	u1, u2, key := int64(1), int64(2), int64(3)
	a, _ := normalizedListFilter(ListFilter{UserID: &u1, APIKeyID: &key, Page: 2, PageSize: 20, BeforeID: 55, Snapshot: "x", PageIDs: []int64{99}})
	b, _ := normalizedListFilter(ListFilter{UserID: &u1, APIKeyID: &key, Page: 999, PageSize: 100})
	if pageIndexFilterKey(a) != pageIndexFilterKey(b) {
		t.Fatal("page position changed filter identity")
	}
	otherTZ, _ := normalizedListFilter(ListFilter{UserID: &u1, APIKeyID: &key, TZ: "Asia/Kathmandu"})
	if pageIndexFilterKey(a) != pageIndexFilterKey(otherTZ) {
		t.Fatal("timezone without dates duplicated the index")
	}
	b.UserID = &u2
	if pageIndexFilterKey(a) == pageIndexFilterKey(b) {
		t.Fatal("owners share filter identity")
	}
}
