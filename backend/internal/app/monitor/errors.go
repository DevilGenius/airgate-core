package monitor

import "errors"

// ErrEventNotFound indicates the target monitor event does not exist.
var ErrEventNotFound = errors.New("监控事件不存在")

// ErrRequestTraceNotFound indicates the requested raw trace is unavailable.
var ErrRequestTraceNotFound = errors.New("请求追踪不存在或已过期")

// ErrEventNotRecoverable indicates the target event cannot be manually resolved.
var ErrEventNotRecoverable = errors.New("该监控事件不支持手动恢复")
