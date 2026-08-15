package accountpriority

import "errors"

// SequenceInput describes one priority sequence and its inclusive bounds.
type SequenceInput struct {
	Initial      int
	Step         int
	GroupSize    int
	Min          int
	Max          int
	OverflowMode OverflowMode
}

// OverflowMode controls what happens when a sequence cannot advance within
// its bounds. OverflowClamp matches the import DSL; OverflowClampAfterFull
// keeps the bulk-update validation behavior for ordinary sequence overflow,
// while clamping only after full occupied levels force the advance.
type OverflowMode uint8

const (
	OverflowError OverflowMode = iota
	OverflowClamp
	OverflowClampAfterFull
)

// SequenceState tracks the current level while assigning one sequence.
type SequenceState struct {
	current         int
	assigned        int
	initialized     bool
	boundaryClamped bool
}

var (
	ErrInvalidSequence  = errors.New("invalid priority sequence")
	ErrSequenceOverflow = errors.New("priority sequence out of bounds")
)

// NextSequencePriority assigns the next priority, accounting for existing
// occupancy and filling each level up to GroupSize before advancing.
func NextSequencePriority(input SequenceInput, occupied map[int]int, state *SequenceState) (int, error) {
	if state == nil || input.Min > input.Max || input.Step == 0 || input.GroupSize <= 0 ||
		input.Initial < input.Min || input.Initial > input.Max {
		return 0, ErrInvalidSequence
	}
	if occupied == nil {
		occupied = make(map[int]int)
	}
	if state.boundaryClamped {
		return assignCurrent(occupied, state, true), nil
	}
	if state.initialized && state.assigned < input.GroupSize && occupied[state.current] < input.GroupSize {
		return assignCurrent(occupied, state, false), nil
	}

	candidate := input.Initial
	if state.initialized {
		next, ok := AddOffset(state.current, input.Step)
		if !ok || next < input.Min || next > input.Max {
			if input.OverflowMode == OverflowClamp {
				return commit(boundaryForStep(input.Step, input.Min, input.Max), occupied, state, true), nil
			}
			return 0, ErrSequenceOverflow
		}
		candidate = next
	}
	for {
		if occupied[candidate] < input.GroupSize {
			return commit(candidate, occupied, state, false), nil
		}
		next, ok := AddOffset(candidate, input.Step)
		if !ok || next < input.Min || next > input.Max {
			if input.OverflowMode == OverflowError {
				return 0, ErrSequenceOverflow
			}
			return commit(boundaryForStep(input.Step, input.Min, input.Max), occupied, state, true), nil
		}
		candidate = next
	}
}

func assignCurrent(occupied map[int]int, state *SequenceState, boundaryClamped bool) int {
	occupied[state.current]++
	state.assigned++
	state.boundaryClamped = boundaryClamped
	return state.current
}

func commit(priority int, occupied map[int]int, state *SequenceState, boundaryClamped bool) int {
	occupied[priority]++
	state.current = priority
	state.assigned = occupied[priority]
	state.initialized = true
	state.boundaryClamped = boundaryClamped
	return priority
}

func boundaryForStep(step, minimum, maximum int) int {
	if step > 0 {
		return maximum
	}
	return minimum
}
