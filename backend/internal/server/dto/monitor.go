package dto

// MonitorListQuery describes admin monitor event list filters.
type MonitorListQuery struct {
	Status          string `form:"status"`
	Severity        string `form:"severity"`
	Type            string `form:"type"`
	Source          string `form:"source"`
	SubjectType     string `form:"subject_type"`
	APIKeyID        *int   `form:"api_key_id"`
	AccountID       *int   `form:"account_id"`
	Platform        string `form:"platform"`
	PluginID        string `form:"plugin_id"`
	TaskType        string `form:"task_type"`
	Endpoint        string `form:"endpoint"`
	ErrorCode       string `form:"error_code"`
	From            string `form:"from"`
	To              string `form:"to"`
	Limit           int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Cursor          string `form:"cursor"`
	CursorUpdatedAt string `form:"cursor_updated_at"`
	CursorID        int    `form:"cursor_id"`
}

// MonitorEventResp is one monitor event response row.
type MonitorEventResp struct {
	ID                  int                    `json:"id"`
	Type                string                 `json:"type"`
	Severity            string                 `json:"severity"`
	Status              string                 `json:"status"`
	Source              string                 `json:"source"`
	SubjectType         string                 `json:"subject_type"`
	SubjectID           string                 `json:"subject_id"`
	Fingerprint         string                 `json:"fingerprint"`
	Title               string                 `json:"title"`
	Message             string                 `json:"message"`
	APIKeyID            *int                   `json:"api_key_id,omitempty"`
	APIKeyNameSnapshot  string                 `json:"api_key_name_snapshot,omitempty"`
	UserID              *int                   `json:"user_id,omitempty"`
	UserEmailSnapshot   string                 `json:"user_email_snapshot,omitempty"`
	GroupID             *int                   `json:"group_id,omitempty"`
	AccountID           *int                   `json:"account_id,omitempty"`
	AccountNameSnapshot string                 `json:"account_name_snapshot,omitempty"`
	Platform            string                 `json:"platform,omitempty"`
	PluginID            string                 `json:"plugin_id,omitempty"`
	TaskType            string                 `json:"task_type,omitempty"`
	Method              string                 `json:"method,omitempty"`
	Endpoint            string                 `json:"endpoint,omitempty"`
	Model               string                 `json:"model,omitempty"`
	HTTPStatus          *int                   `json:"http_status,omitempty"`
	UpstreamStatus      *int                   `json:"upstream_status,omitempty"`
	ErrorCode           string                 `json:"error_code,omitempty"`
	CreatedAt           string                 `json:"created_at"`
	UpdatedAt           string                 `json:"updated_at"`
	ResolvedAt          *string                `json:"resolved_at,omitempty"`
	IgnoredAt           *string                `json:"ignored_at,omitempty"`
	AutoResolveAt       *string                `json:"auto_resolve_at,omitempty"`
	ExpiresAt           string                 `json:"expires_at"`
	LastNotifiedAt      *string                `json:"last_notified_at,omitempty"`
	NextNotifyAt        *string                `json:"next_notify_at,omitempty"`
	NotifyError         string                 `json:"notify_error,omitempty"`
	Detail              map[string]interface{} `json:"detail,omitempty"`
}

// MonitorCursorResp is the cursor for updated_at desc, id desc ordering.
type MonitorCursorResp struct {
	UpdatedAt string `json:"updated_at"`
	ID        int    `json:"id"`
}

// MonitorListResp is a cursor-paged monitor event list.
type MonitorListResp struct {
	List       []MonitorEventResp `json:"list"`
	HasMore    bool               `json:"has_more"`
	NextCursor *MonitorCursorResp `json:"next_cursor,omitempty"`
}

// MonitorTypeCountResp is a grouped count row.
type MonitorTypeCountResp struct {
	Type  string `json:"type"`
	Count int64  `json:"count"`
}

// MonitorSubjectCountResp is a top API key/account row.
type MonitorSubjectCountResp struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// MonitorSummaryResp is the admin monitor overview.
type MonitorSummaryResp struct {
	ActiveTotal   int64                     `json:"active_total"`
	CriticalTotal int64                     `json:"critical_total"`
	ErrorTotal    int64                     `json:"error_total"`
	WarningTotal  int64                     `json:"warning_total"`
	ByType        []MonitorTypeCountResp    `json:"by_type"`
	TopAPIKeys    []MonitorSubjectCountResp `json:"top_api_keys"`
	TopAccounts   []MonitorSubjectCountResp `json:"top_accounts"`
	Recent        []MonitorEventResp        `json:"recent"`
}
