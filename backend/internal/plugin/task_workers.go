package plugin

import (
	"context"
	"sync"
)

const maxTaskWorkers = 32

type taskWorkerPool struct {
	mu            sync.Mutex
	active        map[string]int
	total, cursor int
	wake          chan struct{}
	idle          chan struct{}
	stopping      bool
	startOnce     sync.Once
	cancel        context.CancelFunc
}

func (m *Manager) taskWorkers() *taskWorkerPool {
	m.taskPoolOnce.Do(func() {
		idle := make(chan struct{})
		close(idle)
		m.taskPool = &taskWorkerPool{active: make(map[string]int), wake: make(chan struct{}, 1), idle: idle}
	})
	return m.taskPool
}
func (p *taskWorkerPool) available(plugin string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopping {
		return 0
	}
	return min(maxTaskWorkers-p.total, maxPluginConcurrency-p.active[plugin])
}
func (p *taskWorkerPool) begin(plugin string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopping || p.total >= maxTaskWorkers || p.active[plugin] >= maxPluginConcurrency {
		return false
	}
	if p.total == 0 {
		p.idle = make(chan struct{})
	}
	p.total++
	p.active[plugin]++
	return true
}
func (p *taskWorkerPool) finish(plugin string, notify bool) {
	p.mu.Lock()
	p.total--
	p.active[plugin]--
	if p.active[plugin] == 0 {
		delete(p.active, plugin)
	}
	if p.total == 0 {
		close(p.idle)
	}
	p.mu.Unlock()
	if !notify {
		return
	}
	select {
	case p.wake <- struct{}{}:
	default:
	}
}
func (p *taskWorkerPool) wait(ctx context.Context) error {
	p.mu.Lock()
	idle := p.idle
	p.mu.Unlock()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *Manager) StopTaskDispatcher(ctx context.Context) error {
	p := m.taskWorkers()
	p.mu.Lock()
	p.stopping = true
	if p.cancel != nil {
		p.cancel()
	}
	p.mu.Unlock()
	return p.wait(ctx)
}

func (m *Manager) StopTaskAdmission() {
	p := m.taskWorkers()
	p.mu.Lock()
	p.stopping = true
	p.mu.Unlock()
}
