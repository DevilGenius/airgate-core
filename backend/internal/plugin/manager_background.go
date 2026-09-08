package plugin

import (
	"context"
	"log/slog"
	"time"

	"github.com/DevilGenius/airgate-core/internal/safego"
)

// minBackgroundInterval 兜底最小间隔，避免插件声明 0 / 极小间隔时把 Core 打爆。
const minBackgroundInterval = 30 * time.Second

// taskRunTimeout 单次任务调用的硬超时；防止插件 handler 卡死把 goroutine 永远阻塞。
const taskRunTimeout = 5 * time.Minute

// startExtensionBackgroundTasks 查询 extension 插件声明的后台任务，并为每个任务
// 起一个独立的 goroutine + ticker 周期触发。所有 goroutine 共享一个 cancelable
// context，stopPlugin 时统一取消。
//
// 设计要点：
//   - gRPC 边界上 Handler 函数无法序列化（参见 ExtensionGRPCClient.BackgroundTasks
//     的注释），因此 Core 这边只拿到任务名 + 间隔，真正执行通过 RunBackgroundTask
//     RPC 反向调用插件进程内的 handler 表（见 ExtensionGRPCServer）。
//   - 启动后立即跑一次（不等第一个 tick），让重启服务后立刻清理积压的过期数据。
//   - 单次任务执行用独立 timeout，不用 ticker 的循环 ctx，避免一次慢调用阻塞下一轮。
func (m *Manager) startExtensionBackgroundTasks(inst *PluginInstance) {
	if inst == nil || inst.Extension == nil {
		return
	}
	tasks := inst.backgroundTasks
	if len(tasks) == 0 {
		return
	}

	ctx, cancel := context.WithCancel(m.runtimeCtx)
	inst.lifecycleMu.Lock()
	if inst.draining {
		inst.lifecycleMu.Unlock()
		cancel()
		return
	}
	inst.stopBackground = cancel
	inst.lifecycleMu.Unlock()

	for _, t := range tasks {
		interval := t.Interval
		if interval < minBackgroundInterval {
			slog.Warn("插件后台任务间隔过小，已抬升到最小间隔",
				"plugin", inst.Name, "task", t.Name,
				"declared", t.Interval, "applied", minBackgroundInterval)
			interval = minBackgroundInterval
		}
		taskName := t.Name
		appliedInterval := interval
		safego.Go("plugin_background_task:"+inst.Name+":"+taskName, func() {
			m.runGenerationBackgroundLoop(ctx, inst, taskName, appliedInterval)
		})
		slog.Info("已启动插件后台任务", "plugin", inst.Name, "task", t.Name, "interval", interval)
	}
}

// Tickers stop at cutover; a task already executing retains its generation and
// has its own deadline, independent of the ticker cancellation.
func (m *Manager) runGenerationBackgroundLoop(ctx context.Context, inst *PluginInstance, name string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil || !inst.acquireRequest() {
			return
		}
		callCtx, cancel := context.WithTimeout(m.runtimeCtx, taskRunTimeout)
		err := inst.Extension.RunBackgroundTask(callCtx, name)
		cancel()
		inst.releaseRequest()
		if err != nil {
			slog.Warn("plugin_background_failed", "plugin", inst.Name, "task", name, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
