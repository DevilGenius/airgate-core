package account

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DevilGenius/airgate-core/internal/plugin"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestCognitionTestPolicy(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		enabled                 bool
		pattern, text           string
		wantDegraded, wantError bool
	}{
		{"normal", true, "^42$", "42", false, false},
		{"degraded", true, "^42$", "41", true, true},
		{"empty", true, "^42$", "", true, true},
		{"upstream-error", true, "^42$", "42", true, true},
		{"disabled", false, "[", "41", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updates := 0
			runtime := newAccountGatewayRuntime(t, &accountFakeGatewayPlugin{platform: "openai",
				handle: func(_ context.Context, method, path, _ string, _ http.Header, _ []byte) (int, http.Header, []byte, error) {
					if method != "GET" || path != "accounts/cognition-test-policy" {
						t.Fatalf("unexpected policy request %s %s", method, path)
					}
					body, _ := json.Marshal(cognitionTestPolicy{Enabled: tc.enabled, Prompt: "compute", Pattern: tc.pattern})
					return 200, nil, body, nil
				},
				forward: func(_ context.Context, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
					var body struct{ Messages []struct{ Content string } }
					_ = json.Unmarshal(req.Body, &body)
					want := "hi"
					if tc.enabled {
						want = "compute"
					}
					if len(body.Messages) != 1 || body.Messages[0].Content != want {
						t.Fatalf("wrong prompt: %s", req.Body)
					}
					for _, ch := range tc.text {
						event, _ := json.Marshal(map[string]any{"type": "response.output_text.delta", "delta": string(ch)})
						_, _ = req.Writer.Write(append(append([]byte("data: "), event...), []byte("\n\n")...))
					}
					if tc.name == "upstream-error" {
						return sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient}, nil
					}
					return sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess}, nil
				},
			})
			defer runtime.cleanup()
			service := NewService(stubRepository{
				findByID: func(context.Context, int, LoadOptions) (Account, error) {
					return Account{ID: 1, Platform: "openai"}, nil
				},
				update: func(_ context.Context, _ int, input UpdateInput) (Account, error) {
					updates++
					if input.CognitionDegraded == nil || *input.CognitionDegraded != tc.wantDegraded {
						t.Fatalf("wrong flag: %+v", input)
					}
					return Account{ID: 1}, nil
				},
			}, accountGatewayCatalog{instances: map[string]*plugin.PluginInstance{"openai": runtime.instance}}, nil, nil)
			plan, err := service.PrepareConnectivityTest(t.Context(), 1, "gpt-test")
			if err != nil {
				t.Fatal(err)
			}
			wantPrompt := "hi"
			if tc.enabled {
				wantPrompt = "compute"
			}
			if plan.Prompt != wantPrompt {
				t.Fatalf("display prompt=%q, want %q", plan.Prompt, wantPrompt)
			}
			timing, err := plan.RunWithTiming(t.Context(), httptest.NewRecorder())
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v", err)
			}
			if tc.enabled {
				if updates != 1 || timing.CognitionDegraded == nil {
					t.Fatal("missing persisted result")
				}
			} else if updates != 0 || timing.CognitionDegraded != nil {
				t.Fatal("disabled test changed flag")
			}
		})
	}
}

func TestCognitionResponseCapture(t *testing.T) {
	w := &cognitionResponseWriter{ResponseWriter: httptest.NewRecorder()}
	wire := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"4\",\"reasoning_content\":\"ignore\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"2\"}}]}\n\n"
	for i := range wire {
		_, _ = w.Write([]byte{wire[i]})
	}
	if w.text() != "42" {
		t.Fatalf("text=%q", w.text())
	}
	_, _ = w.Write(make([]byte, (4<<20)+1))
	if !w.overflow || w.body.Len() > 4<<20 {
		t.Fatal("capture not bounded")
	}
}
