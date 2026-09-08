package plugin

import (
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestResourceFailuresCannotBeRetriedByHTTPOrHost(t *testing.T) {
	if !terminalForwardFailure(sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient, FailoverScope: sdk.FailoverScopeTerminal}, errors.New("too large")) {
		t.Fatal("terminal outcome allowed retry")
	}
	if !terminalForwardFailure(sdk.ForwardOutcome{}, status.Error(codes.ResourceExhausted, "payload")) {
		t.Fatal("RPC payload error allowed retry")
	}
	if terminalForwardFailure(sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient}, errors.New("temporary")) {
		t.Fatal("ordinary transient error became terminal")
	}
}
