package plugin

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DevilGenius/airgate-core/internal/safego"
)

// Poll portable source fingerprints, including deletions and embedded assets.
// Development produces an executable and then uses the production activation
// pipeline. Changes arriving during a build remain pending for the next poll.

type devWatcher struct {
	mgr      *Manager
	interval time.Duration

	mu      sync.Mutex
	plugins map[string]*devWatchEntry // canonicalName → entry

	stop      chan struct{}
	done      chan struct{}
	joined    chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
	closed    bool
}

type devWatchEntry struct {
	srcPath     string
	fingerprint string
	reloading   bool // 防止 reload 期间 polling 又触发一次
}

const devWatcherInterval = 1500 * time.Millisecond

// newDevWatcher 创建一个新的 watcher，并启动后台 polling goroutine。
func newDevWatcher(mgr *Manager) *devWatcher {
	dw := &devWatcher{
		mgr:      mgr,
		interval: devWatcherInterval,
		plugins:  make(map[string]*devWatchEntry),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		joined:   make(chan struct{}),
	}
	safego.Go("plugin_dev_watcher", dw.loop)
	return dw
}

// add 把一个 dev 插件的 srcPath 加入轮询集合。
//
// 第一次扫描会把当前 max mtime 记下作为基线；这意味着 add 之后的下一次轮询
// 不会立刻 reload（防止 LoadDev 刚结束就被自己触发）。
func (dw *devWatcher) add(name, srcPath string) {
	baseline, _ := scanSourceFingerprint(srcPath)

	dw.mu.Lock()
	if dw.closed {
		dw.mu.Unlock()
		return
	}
	if existing := dw.plugins[name]; existing != nil && existing.srcPath == srcPath {
		dw.mu.Unlock()
		return
	}
	dw.plugins[name] = &devWatchEntry{srcPath: srcPath, fingerprint: baseline}
	dw.mu.Unlock()

	slog.Info("dev 插件源码 watch 已就绪 (mtime polling)",
		"plugin", name, "src", srcPath, "interval", dw.interval, "baseline", baseline)
}

// remove 在 stopPlugin 时调用，把插件从轮询集合移除。
func (dw *devWatcher) remove(name string) {
	dw.mu.Lock()
	defer dw.mu.Unlock()
	delete(dw.plugins, name)
}

// Close 停止 watcher 轮询，并等待已触发的 reload goroutine 结束。
func (dw *devWatcher) Close() {
	_ = dw.CloseContext(context.Background())
}

func (dw *devWatcher) CloseContext(ctx context.Context) error {
	if dw == nil {
		return nil
	}
	dw.closeOnce.Do(func() {
		dw.mu.Lock()
		dw.closed = true
		dw.plugins = make(map[string]*devWatchEntry)
		dw.mu.Unlock()

		close(dw.stop)
		go func() { <-dw.done; dw.wg.Wait(); close(dw.joined) }()
	})
	select {
	case <-dw.joined:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// loop 每 interval 扫描一次所有注册插件，发现 mtime 增长就触发 reload。
func (dw *devWatcher) loop() {
	t := time.NewTicker(dw.interval)
	defer t.Stop()
	defer close(dw.done)
	for {
		select {
		case <-dw.stop:
			return
		case <-t.C:
			dw.tick()
		}
	}
}

func (dw *devWatcher) tick() {
	// 拷贝一份快照，避免长时间持锁扫描磁盘
	dw.mu.Lock()
	snapshot := make(map[string]devWatchEntry, len(dw.plugins))
	for k, v := range dw.plugins {
		if v.reloading {
			continue
		}
		snapshot[k] = *v
	}
	dw.mu.Unlock()

	for name, entry := range snapshot {
		latest, ok := scanSourceFingerprint(entry.srcPath)
		if !ok {
			continue
		}
		if latest == entry.fingerprint {
			continue
		}

		// 找到变更：更新 baseline 并触发 reload。
		dw.mu.Lock()
		current, exists := dw.plugins[name]
		if !exists || current.reloading {
			dw.mu.Unlock()
			continue
		}
		current.fingerprint = latest
		current.reloading = true
		dw.mu.Unlock()

		dw.wg.Add(1)
		reloadName := name
		reloadTrigger := latest
		safego.Go("plugin_dev_reload:"+reloadName, func() {
			defer dw.wg.Done()
			dw.doReload(reloadName, reloadTrigger)
		})
	}
}

func (dw *devWatcher) doReload(name string, trigger string) {
	defer func() {
		dw.mu.Lock()
		if e, ok := dw.plugins[name]; ok {
			e.reloading = false

		}
		dw.mu.Unlock()
	}()

	slog.Info("dev 插件源码变更，触发热重载", "plugin", name, "source_fingerprint", trigger)
	ctx, cancel := context.WithTimeout(dw.mgr.runtimeCtx, pluginBuildTimeout)
	defer cancel()
	var err error
	if dw.mgr.GetInstance(name) == nil {
		err = dw.mgr.ReloadDev(ctx, name)
	} else {
		err = dw.mgr.ReloadInstance(ctx, name)
	}
	if err != nil {
		if errors.Is(err, ErrPluginUpdateBusy) {
			dw.mu.Lock()
			if e := dw.plugins[name]; e != nil {
				e.fingerprint = ""
			}
			dw.mu.Unlock()
		}
		slog.Warn("dev 插件热重载失败", "plugin", name, "error", err)
		return
	}
	slog.Info("dev 插件热重载完成", "plugin", name)
}

// Fingerprints include names, sizes and mtimes, so deletions and edits during a
// build schedule a subsequent build. Embedded assets use the same artifact path.
func scanSourceFingerprint(root string) (string, bool) {
	hash := sha256.New()
	found := false
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := info.Name()
			if base == "vendor" || base == "node_modules" || base == "tmp" || base == ".git" || (strings.HasPrefix(base, ".") && path != root) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(info.Name(), "_test.go") {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".go") || info.Name() == "go.mod" || info.Name() == "go.sum" || strings.Contains(filepath.ToSlash(path), "/webdist/") {
			_, _ = fmt.Fprintf(hash, "%s:%d:%d\\n", path, info.Size(), info.ModTime().UnixNano())
			found = true
		}
		return nil
	})
	return fmt.Sprintf("%x", hash.Sum(nil)), found && err == nil
}
