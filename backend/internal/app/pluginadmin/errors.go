package pluginadmin

import "errors"

var (
	// ErrPluginNotDev 表示当前插件不是开发模式。
	ErrPluginNotDev = errors.New("仅开发模式插件支持热加载")
	// ErrPluginUnavailable 表示插件不存在或未运行。
	ErrPluginUnavailable = errors.New("插件未运行或不存在")
	// ErrPluginCapabilityUnavailable 表示指定平台没有运行中的插件声明所需能力。
	ErrPluginCapabilityUnavailable = errors.New("平台插件未提供所需能力")
)
