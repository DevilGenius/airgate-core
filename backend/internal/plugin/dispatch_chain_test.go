package plugin

import (
	"testing"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestDispatchChainSelectDoesNotAdvanceStartIndex(t *testing.T) {
	t.Parallel()

	chain := newDispatchChain([]sdk.DispatchPlan{
		{SchedulingModel: "gpt-primary"},
		{SchedulingModel: "gpt-fallback"},
	})

	selected := chain.Select(1)
	if selected.SchedulingModel != "gpt-fallback" {
		t.Fatalf("selected = %+v, want fallback", selected)
	}
	if got := chain.StartIndex(); got != 0 {
		t.Fatalf("StartIndex after Select fallback = %d, want 0", got)
	}
}

func TestDispatchChainAdvanceMovesStartIndexPastFailedCandidate(t *testing.T) {
	t.Parallel()

	chain := newDispatchChain([]sdk.DispatchPlan{
		{SchedulingModel: "gpt-primary"},
		{},
		{SchedulingModel: "gpt-fallback"},
	})
	chain.Select(0)

	next, ok := chain.AdvanceOnOutcome(sdk.ForwardOutcome{
		Kind:          sdk.OutcomeClientError,
		FailoverScope: sdk.FailoverScopeDispatchCandidate,
	}, false)
	if !ok || next.Index != 2 {
		t.Fatalf("AdvanceOnOutcome next = %+v ok=%v, want index 2 true", next, ok)
	}
	if got := chain.StartIndex(); got != 2 {
		t.Fatalf("StartIndex after dispatch failover = %d, want 2", got)
	}
}

func TestDispatchChainAdvanceRequiresSelectedCandidate(t *testing.T) {
	t.Parallel()

	chain := newDispatchChain([]sdk.DispatchPlan{
		{SchedulingModel: "gpt-primary"},
		{SchedulingModel: "gpt-fallback"},
	})

	if _, ok := chain.AdvanceOnOutcome(sdk.ForwardOutcome{
		Kind:          sdk.OutcomeClientError,
		FailoverScope: sdk.FailoverScopeDispatchCandidate,
	}, false); ok {
		t.Fatal("AdvanceOnOutcome should require a selected candidate")
	}
}
