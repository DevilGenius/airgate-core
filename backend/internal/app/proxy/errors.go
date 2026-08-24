package proxy

import "errors"

var (
	// ErrProxyNotFound 表示目标代理不存在。
	ErrProxyNotFound = errors.New("代理不存在")
	// ErrInvalidProxyMode 表示代理模式非法。
	ErrInvalidProxyMode = errors.New("代理模式无效")
	// ErrInvalidProxySlotRange 表示代理组用户名范围非法。
	ErrInvalidProxySlotRange = errors.New("代理组用户名范围无效")
	// ErrProxyGroupHasAccounts 表示代理仍有账号绑定，不能切换单代理/代理组类型。
	ErrProxyGroupHasAccounts = errors.New("代理仍有账号绑定，不能切换类型")
	// ErrProxySlotRangeInUse 表示新的范围会使已有账号 slot 失效。
	ErrProxySlotRangeInUse = errors.New("新的代理组范围与现有账号 slot 冲突")
	// ErrProxyAssignmentRequiresGroup 表示单代理不支持 slot 分配策略。
	ErrProxyAssignmentRequiresGroup = errors.New("仅代理组支持用户名分配策略")
)
