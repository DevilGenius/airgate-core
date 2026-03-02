package plugin

import (
	"net/http"
)

// Forwarder 请求转发器
// 负责将用户的 API 请求转发给插件处理
// 完整流程：认证 -> 调度 -> 限流 -> 转发 -> 计费
type Forwarder struct {
	manager *Manager
}

// NewForwarder 创建转发器
func NewForwarder(manager *Manager) *Forwarder {
	return &Forwarder{manager: manager}
}

// Forward 转发请求到对应插件（占位实现）
func (f *Forwarder) Forward(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "插件转发功能开发中", http.StatusNotImplemented)
}
