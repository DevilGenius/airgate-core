package monitor

import (
	"context"
	"time"

	"github.com/DevilGenius/airgate-core/internal/monitoring"
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
	Kind                string
	Severity            string
	Status              string
	Source              string
	SubjectType         string
	SubjectID           string
	Fingerprint         string
	Title               string
	Message             string
	APIKeyID            *int
	APIKeyNameSnapshot  string
	UserID              *int
	UserEmailSnapshot   string
	GroupID             *int
	AccountID           *int
	AccountNameSnapshot string
	Platform            string
	PluginID            string
	TaskType            string
	Method              string
	Endpoint            string
	Model               string
	HTTPStatus          *int
	UpstreamStatus      *int
	ErrorCode           string
	ErrorType           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ResolvedAt          *time.Time
	IgnoredAt           *time.Time
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

// ListFilter filters monitor events.
type ListFilter struct {
	Status      string
	Severity    string
	Kind        string
	Source      string
	SubjectType string
	APIKeyID    *int
	AccountID   *int
	Platform    string
	PluginID    string
	TaskType    string
	Endpoint    string
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

// KindCount is a grouped active count.
type KindCount struct {
	Kind  string
	Count int64
}

// SubjectCount is a top subject row.
type SubjectCount struct {
	ID    int
	Name  string
	Count int64
}

// Summary is the overview aggregate for active events.
type Summary struct {
	ActiveTotal   int64
	CriticalTotal int64
	ErrorTotal    int64
	WarningTotal  int64
	ByKind        []KindCount
	TopAPIKeys    []SubjectCount
	TopAccounts   []SubjectCount
	Recent        []Event
}

// Repository defines monitor event persistence.
type Repository interface {
	InsertBatch(context.Context, []QueuedEvent) error
	ResolveBySubject(context.Context, monitoring.ResolveQuery) error
	Get(context.Context, int) (Event, error)
	Resolve(context.Context, int) error
	Ignore(context.Context, int) error
	List(context.Context, ListFilter) (ListResult, error)
	Summary(context.Context) (Summary, error)
	CleanupExpired(context.Context, time.Time, int) (int, error)
	AutoResolveDue(context.Context, time.Time, int) (int, error)
	ListNotifyDue(context.Context, time.Time, int) ([]Event, error)
	MarkNotified(context.Context, int, time.Time, time.Time) error
	MarkNotifyFailed(context.Context, int, time.Time, string) error
}
