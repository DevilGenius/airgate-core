// Package monitoring defines the low-level monitoring boundary.
package monitoring

import (
	"context"
	"time"
)

const (
	TypeSchedulerError       = "scheduler_error"
	TypeUpstreamAccountError = "upstream_account_error"
	TypePluginError          = "plugin_error"
	TypeTaskError            = "task_error"
	TypeSystemError          = "system_error"

	SeverityWarning  = "warning"
	SeverityError    = "error"
	SeverityCritical = "critical"

	StatusActive   = "active"
	StatusResolved = "resolved"
	StatusIgnored  = "ignored"

	SourceForwarder      = "forwarder"
	SourceScheduler      = "scheduler"
	SourceAccountChecker = "account_checker"
	SourceQuotaRefresh   = "quota_refresh"
	SourceTaskRunner     = "task_runner"
	SourcePluginManager  = "plugin_manager"
	SourceMonitorWorker  = "monitor_worker"

	SubjectAccount   = "account"
	SubjectTask      = "task"
	SubjectPlugin    = "plugin"
	SubjectScheduler = "scheduler"
	SubjectUser      = "user"
	SubjectSystem    = "system"
)

// EventInput is the best-effort event shape accepted by low-level packages.
type EventInput struct {
	Type        string
	Severity    string
	Source      string
	SubjectType string
	SubjectID   string

	Title   string
	Message string

	AccountID           *int
	AccountNameSnapshot string
	Platform            string
	PluginID            string
	TaskType            string
	ErrorCode           string

	// HashMaterial overrides the default structured hash material.
	HashMaterial string
	// AutoResolveAt optionally overrides the default type-based auto-resolve window.
	AutoResolveAt *time.Time
	// ObservedAt is primarily for tests and internally replayed events. Callers usually leave it zero.
	ObservedAt time.Time
	Detail     map[string]interface{}
}

// ResolveQuery identifies active events to resolve.
type ResolveQuery struct {
	Type        string
	SubjectType string
	SubjectID   string
	AccountID   *int
	PluginID    string
	TaskType    string
	ErrorCode   string
	Reason      string
}

// Recorder is intentionally best-effort and must not leak monitoring failures into callers.
type Recorder interface {
	Record(ctx context.Context, input EventInput)
	ResolveBySubject(ctx context.Context, query ResolveQuery)
}
