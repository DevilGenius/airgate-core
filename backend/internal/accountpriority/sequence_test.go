package accountpriority

import (
	"errors"
	"slices"
	"testing"
)

func TestNextSequencePriorityFillsOccupiedLevels(t *testing.T) {
	input := SequenceInput{Initial: 1000, Step: -2, GroupSize: 3, Min: Min, Max: Max, OverflowMode: OverflowError}
	occupied := map[int]int{1000: 2}
	state := SequenceState{}
	got := make([]int, 0, 3)
	for range 3 {
		priority, err := NextSequencePriority(input, occupied, &state)
		if err != nil {
			t.Fatalf("NextSequencePriority() error = %v", err)
		}
		got = append(got, priority)
	}
	if !slices.Equal(got, []int{1000, 998, 998}) {
		t.Fatalf("priorities = %v, want [1000 998 998]", got)
	}
}

func TestNextSequencePriorityRechecksSharedOccupancyOnFastPath(t *testing.T) {
	input := SequenceInput{Initial: 100, Step: -10, GroupSize: 2, Min: Min, Max: Max, OverflowMode: OverflowError}
	occupied := map[int]int{}
	first := SequenceState{}
	second := SequenceState{}
	got := make([]int, 0, 4)
	for _, state := range []*SequenceState{&first, &second, &first, &second} {
		priority, err := NextSequencePriority(input, occupied, state)
		if err != nil {
			t.Fatalf("NextSequencePriority() error = %v", err)
		}
		got = append(got, priority)
	}
	if !slices.Equal(got, []int{100, 100, 90, 90}) {
		t.Fatalf("priorities = %v, want [100 100 90 90]", got)
	}
	if occupied[100] != 2 || occupied[90] != 2 {
		t.Fatalf("occupied counts = %v, want map[100:2 90:2]", occupied)
	}
}

func TestNextSequencePriorityClampsAfterFullOccupiedBoundary(t *testing.T) {
	input := SequenceInput{Initial: 99998, Step: 1, GroupSize: 2, Min: Min, Max: Max, OverflowMode: OverflowClampAfterFull}
	occupied := map[int]int{99998: 2, 99999: 2}
	state := SequenceState{}
	got := make([]int, 0, 3)
	for range 3 {
		priority, err := NextSequencePriority(input, occupied, &state)
		if err != nil {
			t.Fatalf("NextSequencePriority() error = %v", err)
		}
		got = append(got, priority)
	}
	if !slices.Equal(got, []int{99999, 99999, 99999}) {
		t.Fatalf("priorities = %v, want all values clamped to 99999", got)
	}
}

func TestNextSequencePriorityKeepsBulkOverflowValidation(t *testing.T) {
	input := SequenceInput{Initial: Max, Step: 1, GroupSize: 1, Min: Min, Max: Max, OverflowMode: OverflowClampAfterFull}
	state := SequenceState{}
	occupied := map[int]int{}
	if _, err := NextSequencePriority(input, occupied, &state); err != nil {
		t.Fatalf("first assignment error = %v", err)
	}
	if _, err := NextSequencePriority(input, occupied, &state); !errors.Is(err, ErrSequenceOverflow) {
		t.Fatalf("second assignment error = %v, want ErrSequenceOverflow", err)
	}
}
