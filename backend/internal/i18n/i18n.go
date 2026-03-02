// Package i18n 提供国际化支持
package i18n

import (
	"encoding/json"
	"log/slog"
	"os"
	"sync"
)

var (
	translations = map[string]map[string]string{}
	mu           sync.RWMutex
	defaultLang  = "zh"
)

// Load 加载翻译文件
func Load(dir string) error {
	files := []string{"zh.json", "en.json"}
	for _, f := range files {
		path := dir + "/" + f
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("加载翻译文件失败", "path", path, "error", err)
			continue
		}
		var msgs map[string]string
		if err := json.Unmarshal(data, &msgs); err != nil {
			slog.Warn("解析翻译文件失败", "path", path, "error", err)
			continue
		}
		lang := f[:len(f)-5] // 去掉 .json
		mu.Lock()
		translations[lang] = msgs
		mu.Unlock()
		slog.Info("加载翻译文件", "lang", lang, "keys", len(msgs))
	}
	return nil
}

// T 获取翻译文本
func T(lang, key string) string {
	mu.RLock()
	defer mu.RUnlock()
	if msgs, ok := translations[lang]; ok {
		if val, ok := msgs[key]; ok {
			return val
		}
	}
	// 回退到默认语言
	if msgs, ok := translations[defaultLang]; ok {
		if val, ok := msgs[key]; ok {
			return val
		}
	}
	return key
}
