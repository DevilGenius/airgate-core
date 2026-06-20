package i18n

import (
	"testing"
	"testing/fstest"
)

func TestTUsesLanguageThenDefaultThenKey(t *testing.T) {
	restoreTranslations := replaceTranslationsForTest(map[string]map[string]string{
		"zh": {"hello": "你好"},
		"en": {"hello": "Hello"},
	}, "zh")
	defer restoreTranslations()

	if got := T("en", "hello"); got != "Hello" {
		t.Fatalf("英文翻译 = %q，期望 Hello", got)
	}
	if got := T("fr", "hello"); got != "你好" {
		t.Fatalf("缺失语言应回退默认语言，得到 %q", got)
	}
	if got := T("en", "missing"); got != "missing" {
		t.Fatalf("缺失 key 应回退 key 本身，得到 %q", got)
	}
}

func TestLoadEmbeddedLoadsDefaultLocales(t *testing.T) {
	restoreTranslations := replaceTranslationsForTest(map[string]map[string]string{}, "zh")
	defer restoreTranslations()

	if err := LoadEmbedded(); err != nil {
		t.Fatalf("加载嵌入翻译失败: %v", err)
	}

	mu.RLock()
	defer mu.RUnlock()
	if len(translations["zh"]) == 0 || len(translations["en"]) == 0 {
		t.Fatalf("嵌入翻译未加载完整: %+v", translations)
	}
}

func TestLoadEmbeddedFromFSContinuesPastMissingAndInvalidFiles(t *testing.T) {
	restoreTranslations := replaceTranslationsForTest(map[string]map[string]string{}, "zh")
	defer restoreTranslations()

	err := loadEmbeddedFromFS(fstest.MapFS{
		"locales/zh.json": {Data: []byte(`{"hello":"你好"}`)},
		"locales/en.json": {Data: []byte(`{`)},
	})
	if err != nil {
		t.Fatalf("loadEmbeddedFromFS returned error: %v", err)
	}

	if got := T("zh", "hello"); got != "你好" {
		t.Fatalf("zh hello = %q", got)
	}
	if got := T("en", "hello"); got != "你好" {
		t.Fatalf("invalid en should not replace default fallback, got %q", got)
	}

	restoreTranslations()
	restoreTranslations = replaceTranslationsForTest(map[string]map[string]string{}, "zh")
	defer restoreTranslations()
	if err := loadEmbeddedFromFS(fstest.MapFS{}); err != nil {
		t.Fatalf("loadEmbeddedFromFS missing files returned error: %v", err)
	}
	if got := T("zh", "hello"); got != "hello" {
		t.Fatalf("missing files should leave translations empty, got %q", got)
	}
}

func replaceTranslationsForTest(next map[string]map[string]string, nextDefault string) func() {
	mu.Lock()
	oldTranslations := translations
	oldDefault := defaultLang
	translations = next
	defaultLang = nextDefault
	mu.Unlock()

	return func() {
		mu.Lock()
		translations = oldTranslations
		defaultLang = oldDefault
		mu.Unlock()
	}
}
