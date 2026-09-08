package scheduler

import (
	"errors"
	"testing"

	"github.com/DevilGenius/airgate-core/ent"
)

func TestResponseAffinityIsScopedByUserAndKey(t *testing.T) {
	a := NewResponseAffinity(nil)
	ctx := t.Context()
	a.Bind(ctx, 7, "openai", "response", 42, 1, 11)
	for _, owner := range [][2]int{{2, 11}, {1, 12}, {0, 0}} {
		if _, found := a.Get(ctx, 7, "openai", "response", owner[0], owner[1]); found {
			t.Fatal("response affinity crossed identity")
		}
	}
	if id, found := a.Get(ctx, 7, "openai", "response", 1, 11); !found || id != 42 {
		t.Fatal("owner binding missing")
	}
	a.setMemory("ag:affinity:response:7:openai:legacy", 42)
	if _, found := a.Get(ctx, 7, "openai", "legacy", 1, 11); found {
		t.Fatal("legacy unowned binding was reused")
	}
}

func TestForeignResponseCannotFallThroughToUnscopedAccountSelection(t *testing.T) {
	s := newSelectionTestScheduler(Normal)
	account := newSelectionTestAccount(42)
	seedSelectionTestGroup(t, 7, "openai", []*ent.Account{account}, nil)
	s.BindResponseAccount(t.Context(), 7, "openai", "known-response", 42, 1, 11)
	_, err := s.SelectAccountWithOptions(t.Context(), "openai", "gpt-4.1", 2, 7, "", AccountSelectionOptions{PreviousResponseID: "known-response", APIKeyID: 11})
	if !errors.Is(err, ErrContinuationAffinityMissing) {
		t.Fatalf("foreign response was forwarded: %v", err)
	}
}
