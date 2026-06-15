package monitor

import (
	"context"
	"time"

	"github.com/DevilGenius/airgate-core/internal/monitoring"
	"github.com/DevilGenius/airgate-core/internal/requestmonitoring"
)

const (
	defaultRetention      = 7 * 24 * time.Hour
	defaultQueueSize      = 4096
	defaultFlushInterval  = time.Second
	defaultFlushBatchSize = 500
	defaultListLimit      = 50
	maxListLimit          = 100
)

// Event is the monitor event domain model.
type Event struct {
	ID                  int
	Type                string
	Severity            string
	Status              string
	RecoveryMode        string
	Source              string
	SubjectType         string
	SubjectID           string
	Hash                string
	Title               string
	Message             string
	AccountID           *int
	AccountNameSnapshot string
	Platform            string
	PluginID            string
	TaskType            string
	ErrorCode           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ResolvedAt          *time.Time
	AutoResolveAt       *time.Time
	ExpiresAt           time.Time
	LastNotifiedAt      *time.Time
	NextNotifyAt        *time.Time
	NotifyError         string
	Detail              map[string]interface{}
}

// QueuedEvent is a normalized monitor event waiting for async persistence.
type QueuedEvent struct {
	Event
}

// RequestEvent is the request-level monitor event domain model.
type RequestEvent struct {
	ID                  int
	Type                string
	Severity            string
	Source              string
	Hash                string
	Fingerprint         string
	Title               string
	Message             string
	RequestID           string
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
	Model               string
	HTTPStatus          *int
	UpstreamStatus      *int
	ErrorCode           string
	DurationMS          int64
	CreatedAt           time.Time
	ExpiresAt           time.Time
	Detail              map[string]interface{}
}

// QueuedRequestEvent is a normalized request monitor event waiting for async persistence.
type QueuedRequestEvent struct {
	RequestEvent
}

type queuedOperationKind string

const (
	queuedOperationRecord        queuedOperationKind = "record"
	queuedOperationRecordRequest queuedOperationKind = "record_request"
	queuedOperationResolve       queuedOperationKind = "resolve"
)

type queuedOperation struct {
	Kind         queuedOperationKind
	Event        QueuedEvent
	RequestEvent QueuedRequestEvent
	Resolve      monitoring.ResolveQuery
}

// ListFilter filters monitor events.
type ListFilter struct {
	Status      string
	Severity    string
	Type        string
	Source      string
	SubjectType string
	AccountID   *int
	Platform    string
	PluginID    string
	TaskType    string
	ErrorCode   string
	From        *time.Time
	To          *time.Time
	Limit       int
	Cursor      *ListCursor
}

// ListCursor is a stable cursor for updated_at desc, id desc ordering.
type ListCursor struct {
	UpdatedAt time.Time `json:"updated_at"`
	ID        int       `json:"id"`
}

// ListResult contains one cursor page.
type ListResult struct {
	List       []Event
	HasMore    bool
	NextCursor *ListCursor
}

// RequestListFilter filters request monitor events.
type RequestListFilter struct {
	Severity       string
	Type           string
	Source         string
	APIKeyID       *int
	AccountID      *int
	Platform       string
	PluginID       string
	Method         string
	Endpoint       string
	Model          string
	HTTPStatus     *int
	UpstreamStatus *int
	ErrorCode      string
	From           *time.Time
	To             *time.Time
	Limit          int
	Cursor         *RequestListCursor
}

// RequestListCursor is a stable cursor for created_at desc, id desc ordering.
type RequestListCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        int       `json:"id"`
}

// RequestListResult contains one request cursor page.
type RequestListResult struct {
	List       []RequestEvent
	HasMore    bool
	NextCursor *RequestListCursor
}

// TypeCount is a grouped active count.
type TypeCount struct {
	Type  string
	Count int64
}

// SubjectCount is a top subject row.
type SubjectCount struct {
	ID    int
	Name  string
	Count int64
}

// Summary is the overview aggregate for monitor events.
type Summary struct {
	ActiveTotal         int64
	CriticalTotal       int64
	CriticalActiveTotal int64
	ErrorTotal          int64
	ErrorActiveTotal    int64
	WarningTotal        int64
	WarningActiveTotal  int64
	InfoTotal           int64
	InfoActiveTotal     int64
	ByType              []TypeCount
	TopAccounts         []SubjectCount
	Recent              []Event
}

// Repository defines monitor event persistence.
type Repository interface {
	InsertBatch(context.Context, []QueuedEvent) error
	InsertRequestBatch(context.Context, []QueuedRequestEvent) error
	ResolveBySubject(context.Context, monitoring.ResolveQuery) error
	Get(context.Context, int) (Event, error)
	Resolve(context.Context, int) error
	List(context.Context, ListFilter) (ListResult, error)
	ListRequests(context.Context, RequestListFilter) (RequestListResult, error)
	ClearRequestEvents(context.Context, *time.Time) (int, error)
	Summary(context.Context) (Summary, error)
	CleanupExpired(context.Context, time.Time, int) (int, error)
	CleanupExpiredRequests(context.Context, time.Time, int) (int, error)
	AutoResolveDue(context.Context, time.Time, int) (int, error)
	ListNotifyDue(context.Context, time.Time, int) ([]Event, error)
	MarkNotified(context.Context, int, time.Time, time.Time) error
	MarkNotifyFailed(context.Context, int, time.Time, string) error
}

var _ requestmonitoring.Recorder = (*Service)(nil)
