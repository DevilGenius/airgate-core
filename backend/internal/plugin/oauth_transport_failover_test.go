package plugin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
	"github.com/gin-gonic/gin"
)

func TestOAuthTransportDoesNotChangeCoreFailoverPolicy(t *testing.T) {
	for _, transport := range []string{"native", "basispoints"} {
		for _, kind := range []sdk.OutcomeKind{sdk.OutcomeAccountRateLimited, sdk.OutcomeAccountUnavailable, sdk.OutcomeAccountDead, sdk.OutcomeUpstreamTransient, sdk.OutcomeFamilyTransient, sdk.OutcomeClientError, sdk.OutcomeStreamAborted} {
			for _, committed := range []bool{false, true} {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				state := &forwardState{requestPath: "/v1/responses", stream: true}
				outcome := sdk.ForwardOutcome{Kind: kind, Usage: &sdk.Usage{Metadata: map[string]string{"oauth_transport": transport}}}
				if committed {
					_, _ = c.Writer.Write([]byte("data: partial\n\n"))
				}
				want := !committed && outcome.ShouldFailover()
				if got := (&Forwarder{}).canFailover(c, state, forwardExecution{outcome: outcome}); got != want {
					t.Fatalf("transport=%s kind=%s committed=%v failover=%v want=%v", transport, kind, committed, got, want)
				}
				if !committed && recorder.Body.Len() != 0 {
					t.Fatal("retry decision leaked an error to the client")
				}
			}
		}
	}
}
