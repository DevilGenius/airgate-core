// Package requestmonitoring defines the low-level request monitoring boundary.
package requestmonitoring

import (
	"context"
	"net/http"
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

// TraceInput is the raw, best-effort diagnostic payload for one request that
// ultimately failed. Body slices are immutable references owned by the caller;
// implementations should serialize them asynchronously.
type TraceInput struct {
	ObservedAt time.Time

	Method   string
	Path     string
	Platform string
	PluginID string
	Model    string
	Stream   bool

	RequestHeaders http.Header
	RequestBody    []byte
	// OriginalSize is the larger of declared Content-Length and bytes observed.
	// On unknown-length read failures it is a lower bound, not the full body size.
	RequestBodyOriginalSize int64
	RequestBodyIncomplete   bool

	PreviousResponseID          string
	RequireContinuationAffinity bool
	ContinuationRecoveryApplied bool

	Attempts []TraceAttempt
	Final    TraceFinalError
}

// TraceAttempt describes one failed plugin invocation retained until the
// overall request outcome is known.
type TraceAttempt struct {
	Number      int
	AccountID   int
	AccountName string
	AccountType string

	ClientModel     string
	SchedulingModel string
	WireModel       string
	RuleID          string
	Operation       string
	TimeoutProfile  string

	OutcomeKind   string
	FailoverScope string
	Reason        string
	PluginError   string
	DurationMS    int64

	UpstreamStatus  int
	UpstreamHeaders http.Header
	UpstreamBody    []byte

	OutboundRequests  []TraceOutboundRequest
	UpstreamErrorBody []byte
}

// TraceOutboundRequest is an internal raw snapshot, sanitized before persistence.
type TraceOutboundRequest struct {
	Transport        string
	Method           string
	URL              string
	Headers          http.Header
	Body             []byte
	StatusCode       int
	BodyOriginalSize int64
}

// TraceFinalError is the client-visible terminal error classification.
type TraceFinalError struct {
	Stage      string
	HTTPStatus int
	ErrorType  string
	ErrorCode  string
	Message    string
}

// TraceRecorder atomically queues a request event together with its raw trace.
// The bool reports whether the trace was accepted. Implementations must remain
// non-blocking and must fall back to recording the ordinary event if necessary.
type TraceRecorder interface {
	RecordRequestTrace(ctx context.Context, event EventInput, trace TraceInput) bool
}
