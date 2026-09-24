package plugin

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"entgo.io/ent/dialect/sql/schema"

	"github.com/DevilGenius/airgate-core/internal/billing"
	"github.com/DevilGenius/airgate-core/internal/routegraph"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	pb "github.com/DevilGenius/airgate-sdk/protocol/proto"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

type closingHostStream struct {
	captureHostInvokeStream
	cancel    context.CancelFunc
	failWrite bool
}

func (s *closingHostStream) Send(frame *pb.HostStreamFrame) error {
	if frame.Event == "chunk" && strings.Contains(string(frame.Payload), "[DONE]") {
		s.cancel()
		if s.failWrite {
			return context.Canceled
		}
	}
	return s.captureHostInvokeStream.Send(frame)
}

func TestHostStreamBillsConfirmedOutcomeAfterClientCancellation(t *testing.T) {
	for _, failure := range []bool{false, true} {
		name := "complete"
		if failure {
			name = "write_error"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			t.Cleanup(routegraph.SetSnapshotForTesting(nil))
			db := testdb.OpenMemoryEnt(t, "host_stream_completion_"+name, schema.WithGlobalUniqueID(false))
			t.Cleanup(func() { _ = db.Close() })
			user := db.User.Create().SetEmail(name + "@example.com").SetPasswordHash("hash").SetBalance(10).SaveX(ctx)
			group := db.Group.Create().SetName(name).SetPlatform("openai").SaveX(ctx)
			db.Account.Create().SetName(name).SetPlatform("openai").SetType("apikey").SetRateMultiplier(1).AddGroupIDs(group.ID).SaveX(ctx)
			if err := routegraph.RefreshSync(ctx, db); err != nil {
				t.Fatal(err)
			}
			client, cleanup := newGatewayRuntimeClient(t, &pluginRuntimeGateway{id: "gateway-openai", platform: "openai", forward: func(_ context.Context, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
				req.Writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = req.Writer.Write([]byte("data: hello\n\n"))
				sdk.BeginStreamCompletion(req.Writer)
				_, _ = req.Writer.Write([]byte("data: [DONE]\n\n"))
				return sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess, Usage: &sdk.Usage{Model: "gpt-4.1", InputTokens: 10, OutputTokens: 7, InputCost: 0.25, OutputCost: 0.75}}, nil
			}})
			defer cleanup()
			manager := NewManager(t.TempDir(), "debug", "", nil)
			manager.instances["gateway-openai"] = &PluginInstance{Name: "gateway-openai", Type: "gateway", Platform: "openai", Gateway: client}
			manager.modelCache = map[string][]sdk.ModelInfo{"openai": {{ID: "gpt-4.1", Name: "GPT-4.1"}}}
			host := NewHostService(db, manager, scheduler.NewScheduler(db, nil), scheduler.NewConcurrencyManager(nil), billing.NewCalculator(), billing.NewRecorder(db, 1))
			requestCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			stream := &closingHostStream{captureHostInvokeStream: captureHostInvokeStream{ctx: requestCtx}, cancel: cancel, failWrite: failure}
			_ = host.forwardStream(requestCtx, hostForwardRequest{UserID: int64(user.ID), GroupID: int64(group.ID), Model: "gpt-4.1", Method: http.MethodPost, Path: "/v1/chat/completions", Stream: true, Body: map[string]interface{}{"model": "gpt-4.1", "stream": true}}, stream)
			if requestCtx.Err() != context.Canceled {
				t.Fatal("fixture did not cancel on completion")
			}
			rows, err := db.UsageLog.Query().All(ctx)
			if err != nil || len(rows) != 1 || rows[0].OutputTokens != 7 || rows[0].TotalCost != 1 {
				t.Fatalf("confirmed Host usage lost/duplicated: rows=%+v err=%v", rows, err)
			}
			if got := db.User.GetX(ctx, user.ID).Balance; got != 9 {
				t.Fatalf("balance=%v, want one charge", got)
			}
		})
	}
}
