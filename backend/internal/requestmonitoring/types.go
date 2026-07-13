// Package requestmonitoring defines the low-level request monitoring boundary.
package requestmonitoring

import (
	"context"
	"time"
)

const (
	TypeAPIRequestError    = "api_request_error"
	TypePluginRouteError   = "plugin_route_error"
	TypePluginForwardRetry = "plugin_forward_retry"
	TypePluginForwardError = "plugin_forward_error"
	TypeClientRequestError = "client_request_error"
	TypeClientClosed       = "client_closed_request"

	SeverityInfo    = "info"
	SeverityWarning = "warning"

	SourceForwarder = "forwarder"
)

// EventInput is the best-effort request-level event shape accepted by the
// forwarding path. Implementations must never leak monitoring failures to callers.
type EventInput struct {
	Type     string
	Severity string
	Source   string

	Title   string
	Message string

	RequestID           string
	Fingerprint         string
	APIKeyID            *int
	APIKeyNameSnapshot  string
	UserID              *int
	UserEmailSnapshot   string
	GroupID             *int
	AccountID           *int
	AccountNameSnapshot string
	Platform            string
	PluginID            string
	Method              string
	Endpoint            string
	RequestPath         string
	Model               string
	HTTPStatus          *int
	UpstreamStatus      *int
	ErrorCode           string
	DurationMS          int64

	// HashMaterial overrides the default structured hash material.
	HashMaterial string
	// ObservedAt is primarily for tests and internally replayed events. Callers usually leave it zero.
	ObservedAt time.Time
	Detail     map[string]interface{}
}

// Recorder is intentionally best-effort and must not leak monitoring failures into callers.
type Recorder interface {
	RecordRequest(ctx context.Context, input EventInput)
}
