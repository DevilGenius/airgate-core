package plugin

import (
	"strings"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

type dispatchCandidate struct {
	Index           int
	Plan            sdk.DispatchPlan
	SchedulingModel string
}

type dispatchChain struct {
	plans    []sdk.DispatchPlan
	floor    int
	current  int
	selected bool
}

func newDispatchChain(plans []sdk.DispatchPlan) dispatchChain {
	return dispatchChain{plans: plans}
}

func (c *dispatchChain) Plans() []sdk.DispatchPlan {
	if c == nil {
		return nil
	}
	return c.plans
}

func (c *dispatchChain) StartIndex() int {
	if c == nil || c.floor < 0 {
		return 0
	}
	if c.floor > len(c.plans) {
		return len(c.plans)
	}
	return c.floor
}

func (c *dispatchChain) Candidate(index int) dispatchCandidate {
	return c.candidateAt(index)
}

func (c *dispatchChain) Select(index int) dispatchCandidate {
	candidate := c.candidateAt(index)
	if candidate.SchedulingModel != "" {
		c.current = candidate.Index
		c.selected = true
	}
	return candidate
}

func (c *dispatchChain) Advance() (dispatchCandidate, bool) {
	if c == nil || !c.selected {
		return dispatchCandidate{}, false
	}
	current := c.current
	if current < c.floor {
		current = c.floor - 1
	}
	next, ok := c.next(current)
	if !ok {
		return dispatchCandidate{}, false
	}
	c.floor = next.Index
	c.current = next.Index
	c.selected = true
	return next, true
}

func (c *dispatchChain) AdvanceOnOutcome(outcome sdk.ForwardOutcome, streamCommitted bool) (dispatchCandidate, bool) {
	if streamCommitted || outcome.FailoverScope != sdk.FailoverScopeDispatchCandidate {
		return dispatchCandidate{}, false
	}
	return c.Advance()
}

func (c *dispatchChain) next(current int) (dispatchCandidate, bool) {
	if c == nil {
		return dispatchCandidate{}, false
	}
	if current < -1 {
		current = -1
	}
	for i := current + 1; i < len(c.plans); i++ {
		candidate := c.candidateAt(i)
		if candidate.SchedulingModel != "" {
			return candidate, true
		}
	}
	return dispatchCandidate{}, false
}

func (c *dispatchChain) candidateAt(index int) dispatchCandidate {
	if c == nil || index < 0 || index >= len(c.plans) {
		return dispatchCandidate{}
	}
	plan := c.plans[index]
	return dispatchCandidate{
		Index:           index,
		Plan:            plan,
		SchedulingModel: strings.TrimSpace(plan.SchedulingModel),
	}
}
