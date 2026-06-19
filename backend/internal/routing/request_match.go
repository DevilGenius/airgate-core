package routing

import (
	"github.com/DevilGenius/airgate-core/ent"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

type Requirements struct {
	RequiredOperation string
	Status            int
	ErrorType         string
	Code              string
	Message           string
}

type GroupMatchResult struct {
	OK        bool
	Status    int
	ErrorType string
	Code      string
	Message   string
}

func RequirementsFromDispatchPlans(plans []sdk.DispatchPlan) Requirements {
	if len(plans) == 0 {
		return Requirements{}
	}
	plan := plans[0]
	return Requirements{
		RequiredOperation: plan.Gate.RequiredOperation,
		Status:            plan.Gate.Status,
		ErrorType:         plan.Gate.ErrorType,
		Code:              plan.Gate.Code,
		Message:           plan.Gate.Message,
	}
}

func GroupMatchesRequirements(g *ent.Group, requirements Requirements) GroupMatchResult {
	if g == nil {
		return GroupMatchResult{}
	}
	if requirements.RequiredOperation == "" {
		return AllowGroup()
	}
	if groupOperationEnabled(g.OperationPolicies, requirements.RequiredOperation) {
		return AllowGroup()
	}
	status := requirements.Status
	if status == 0 {
		status = 403
	}
	errType := requirements.ErrorType
	if errType == "" {
		errType = "invalid_request_error"
	}
	code := requirements.Code
	if code == "" {
		code = "operation_disabled"
	}
	message := requirements.Message
	if message == "" {
		message = "当前分组未开启该操作"
	}
	return DenyGroup(status, errType, code, message)
}

func AllowGroup() GroupMatchResult {
	return GroupMatchResult{OK: true}
}

func DenyGroup(status int, errType, code, message string) GroupMatchResult {
	return GroupMatchResult{
		Status:    status,
		ErrorType: errType,
		Code:      code,
		Message:   message,
	}
}

func groupOperationEnabled(policies map[string]bool, operation string) bool {
	if policies == nil || operation == "" {
		return false
	}
	return policies[operation]
}
