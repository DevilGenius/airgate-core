package plugin

import (
	"testing"
	"time"
)

func TestCorePluginContextConfigAccessors(t *testing.T) {
	t.Parallel()

	ctx := newCorePluginContext(map[string]interface{}{
		"string":   "value",
		"int":      42,
		"bool":     true,
		"float":    3.5,
		"duration": "250ms",
		"invalid":  "not-a-number",
	}, "gateway-openai")

	if ctx.Logger() == nil {
		t.Fatal("Logger() = nil")
	}
	cfg := ctx.Config()
	if got := cfg.GetString("string"); got != "value" {
		t.Fatalf("GetString() = %q, want value", got)
	}
	if got := cfg.GetString("int"); got != "42" {
		t.Fatalf("numeric values should be stringified, got %q", got)
	}
	if got := cfg.GetInt("int"); got != 42 {
		t.Fatalf("GetInt() = %d, want 42", got)
	}
	if got := cfg.GetInt("invalid"); got != 0 {
		t.Fatalf("invalid GetInt() = %d, want 0", got)
	}
	if got := cfg.GetBool("bool"); !got {
		t.Fatal("GetBool() = false, want true")
	}
	if got := cfg.GetBool("invalid"); got {
		t.Fatal("invalid GetBool() = true, want false")
	}
	if got := cfg.GetFloat64("float"); got != 3.5 {
		t.Fatalf("GetFloat64() = %v, want 3.5", got)
	}
	if got := cfg.GetFloat64("invalid"); got != 0 {
		t.Fatalf("invalid GetFloat64() = %v, want 0", got)
	}
	if got := cfg.GetDuration("duration"); got != 250*time.Millisecond {
		t.Fatalf("GetDuration() = %s, want 250ms", got)
	}
	if got := cfg.GetDuration("invalid"); got != 0 {
		t.Fatalf("invalid GetDuration() = %s, want 0", got)
	}
	all := cfg.GetAll()
	if all["string"] != "value" || all["bool"] != "true" {
		t.Fatalf("GetAll() = %#v", all)
	}
}
