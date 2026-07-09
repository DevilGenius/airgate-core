package account

import "errors"

var (
	// ErrAccountNotFound 账号不存在。
	ErrAccountNotFound = errors.New("账号不存在")
	// ErrAccountEmailExists 账号邮箱已被未删除账号占用。
	ErrAccountEmailExists = errors.New("账号邮箱已存在")
	// ErrInvalidAccountEmail 账号邮箱格式非法。
	ErrInvalidAccountEmail = errors.New("账号邮箱格式无效")
	// ErrAccountEmailMismatch 顶层 email 与 credentials.email 不一致。
	ErrAccountEmailMismatch = errors.New("账号 email 与 credentials.email 不一致")
	// ErrPluginNotFound 未找到对应平台插件。
	ErrPluginNotFound = errors.New("未找到对应平台插件")
	// ErrModelRequired 缺少测试模型。
	ErrModelRequired = errors.New("请指定测试模型")
	// ErrQuotaRefreshUnsupported 当前平台不支持额度刷新。
	ErrQuotaRefreshUnsupported = errors.New("该平台不支持刷新额度")
	// ErrInvalidDateRange 日期范围参数非法。
	ErrInvalidDateRange = errors.New("日期范围无效")
	// ErrInvalidState 账号状态参数非法。
	ErrInvalidState = errors.New("账号状态无效")
	// ErrInvalidRateMultiplier 账号倍率非法。
	ErrInvalidRateMultiplier = errors.New("账号倍率必须是有限正数，范围为 0.01 到 100")
	// ErrConflictingPriorityUpdate 同时提交固定优先级和优先级偏移。
	ErrConflictingPriorityUpdate = errors.New("不能同时设置优先级和优先级偏移")
	// ErrInvalidPriorityOffset 优先级偏移后的结果超出支持范围。
	ErrInvalidPriorityOffset = errors.New("优先级偏移后的结果必须在 -99999 到 99999 范围内")
	// ErrInvalidModelPolicy 模型黑白名单策略非法。
	ErrInvalidModelPolicy = errors.New("模型策略无效")
	// ErrReauthRequired OAuth 凭证已失效，需要重新授权（refresh_token 失效且无法本地降级）。
	ErrReauthRequired = errors.New("账号凭证已失效，请重新授权")
)
