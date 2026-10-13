package plugin

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DevilGenius/airgate-core/internal/billing"
	"github.com/DevilGenius/airgate-core/internal/routegraph"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

// ForwardUsageBilling is Core's shared settlement path for internal and Host calls.
type ForwardUsageBilling struct {
	calculator *billing.Calculator
	recorder   *billing.Recorder
	scheduler  *scheduler.Scheduler
}

func NewForwardUsageBilling(calculator *billing.Calculator, recorder *billing.Recorder, scheduler *scheduler.Scheduler) *ForwardUsageBilling {
	return &ForwardUsageBilling{calculator: calculator, recorder: recorder, scheduler: scheduler}
}

// Begin admits a producer before upstream consumption and checks the paying user.
func (b *ForwardUsageBilling) Begin(userID int) (string, func(), error) {
	if b.recorder == nil {
		return "", nil, billing.ErrBillingBusy
	}
	release, err := b.recorder.Reserve()
	if err != nil {
		return "", nil, err
	}
	user, err := forwardUsageUser(userID)
	if err != nil {
		release()
		return "", nil, err
	}
	return user.Email, release, nil
}

func forwardUsageUser(userID int) (*routegraph.UserNode, error) {
	user := routegraph.User(userID)
	if user == nil {
		return nil, status.Error(codes.NotFound, "用户不存在")
	}
	if !user.Active() {
		return nil, status.Error(codes.PermissionDenied, "用户已禁用")
	}
	if user.Balance <= 0 {
		return nil, hostForwardInsufficientQuotaError()
	}
	return user, nil
}

func (b *ForwardUsageBilling) Record(ctx context.Context, identity billing.UsageRecord, usage *sdk.Usage, rates ForwardUsageRates) (int, error) {
	if usage == nil {
		return 0, nil
	}
	record := settledForwardUsageRecord(b.calculator, identity, usage, rates)
	if b.scheduler != nil {
		b.scheduler.AddWindowCost(ctx, identity.AccountID, record.AccountCost)
	}
	if b.recorder == nil {
		return 0, nil
	}
	return b.recorder.RecordSync(ctx, record)
}

func settledForwardUsageRecord(calculator *billing.Calculator, identity billing.UsageRecord, usage *sdk.Usage, rates ForwardUsageRates) billing.UsageRecord {
	settled := settleUsage(calculator, usage, rates)
	usageValues, calc, usageMetadata := settled.Usage, settled.Costs, settled.Metadata
	actualModel := usage.Model
	if actualModel == "" {
		actualModel = identity.Model
	}
	record := billing.UsageRecord{
		UserID:                identity.UserID,
		UserEmail:             identity.UserEmail,
		APIKeyID:              identity.APIKeyID,
		AccountID:             identity.AccountID,
		GroupID:               identity.GroupID,
		Platform:              identity.Platform,
		Model:                 actualModel,
		InputTokens:           usageValues.InputTokens,
		OutputTokens:          usageValues.OutputTokens,
		CachedInputTokens:     usageValues.CachedInputTokens,
		CacheCreationTokens:   usageValues.CacheCreationTokens,
		ReasoningOutputTokens: usageValues.ReasoningOutputTokens,
		InputPrice:            usageValues.InputPrice,
		OutputPrice:           usageValues.OutputPrice,
		CachedInputPrice:      usageValues.CachedInputPrice,
		CacheCreationPrice:    usageValues.CacheCreationPrice,
		InputCost:             calc.InputCost,
		OutputCost:            calc.OutputCost,
		CachedInputCost:       calc.CachedInputCost,
		CacheCreationCost:     calc.CacheCreationCost,
		TotalCost:             calc.TotalCost,
		ActualCost:            calc.ActualCost,
		BilledCost:            calc.BilledCost,
		AccountCost:           calc.AccountCost,
		RateMultiplier:        calc.RateMultiplier,
		SellRate:              calc.SellRate,
		AccountRateMultiplier: calc.AccountRateMultiplier,
		ServiceTier:           usageValues.ServiceTier,
		Endpoint:              identity.Endpoint,
		ReasoningEffort:       identity.ReasoningEffort,
		Stream:                identity.Stream,
		DurationMs:            identity.DurationMs,
		FirstEventMs:          usageValues.FirstEventMs,
		FirstTokenMs:          usageValues.FirstTokenMs,
		WSDialMs:              usageValues.WSDialMs,
		UsageMetadata:         usageMetadata,
	}

	return record
}
