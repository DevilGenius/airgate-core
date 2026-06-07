package monitor

import "errors"

// ErrEventNotFound indicates the target monitor event does not exist.
var ErrEventNotFound = errors.New("监控事件不存在")
