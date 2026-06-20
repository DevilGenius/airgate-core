package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
)

func TestSchedulabilityExtraHelpers(t *testing.T) {
	extra := map[string]interface{}{
		"string":       "value",
		"float":        1.5,
		"int":          2,
		"int64":        int64(3),
		"bool":         true,
		"bool_string":  " true ",
		"false_string": "not-bool",
		"zero":         0,
		"float_bool":   0.1,
	}

	if got := ExtraString(extra, "string"); got != "value" {
		t.Fatalf("ExtraString = %q", got)
	}
	if got := ExtraString(extra, "missing"); got != "" {
		t.Fatalf("ExtraString missing = %q", got)
	}
	if got := ExtraString(extra, "int"); got != "" {
		t.Fatalf("ExtraString non-string = %q", got)
	}

	if got := ExtraFloat64(extra, "float"); got != 1.5 {
		t.Fatalf("ExtraFloat64 float = %v", got)
	}
	if got := ExtraFloat64(extra, "int"); got != 2 {
		t.Fatalf("ExtraFloat64 int = %v", got)
	}
	if got := ExtraFloat64(extra, "int64"); got != 3 {
		t.Fatalf("ExtraFloat64 int64 = %v", got)
	}
	if got := ExtraFloat64(extra, "string"); got != 0 {
		t.Fatalf("ExtraFloat64 string = %v", got)
	}

	if got := ExtraInt(extra, "float"); got != 1 {
		t.Fatalf("ExtraInt float = %v", got)
	}
	if got := ExtraInt(extra, "int"); got != 2 {
		t.Fatalf("ExtraInt int = %v", got)
	}
	if got := ExtraInt(extra, "int64"); got != 3 {
		t.Fatalf("ExtraInt int64 = %v", got)
	}
	if got := ExtraInt(extra, "string"); got != 0 {
		t.Fatalf("ExtraInt string = %v", got)
	}

	if !ExtraBool(extra, "bool") {
		t.Fatal("ExtraBool bool = false")
	}
	if !ExtraBool(extra, "bool_string") {
		t.Fatal("ExtraBool string = false")
	}
	if ExtraBool(extra, "false_string") {
		t.Fatal("ExtraBool invalid string = true")
	}
	if ExtraBool(extra, "zero") {
		t.Fatal("ExtraBool zero int = true")
	}
	if !ExtraBool(extra, "int64") {
		t.Fatal("ExtraBool non-zero int64 = false")
	}
	if !ExtraBool(extra, "float_bool") {
		t.Fatal("ExtraBool non-zero float64 = false")
	}
	if ExtraBool(extra, "missing") {
		t.Fatal("ExtraBool missing = true")
	}
}

func TestRPMCounterNilRedisAndPureHelpers(t *testing.T) {
	ctx := context.Background()
	counter := NewRPMCounter(nil)

	if counter == nil || counter.rdb != nil {
		t.Fatalf("NewRPMCounter(nil) = %#v", counter)
	}
	if got := rpmMinuteKey(7, 123); got != "ag:rpm:7:123" {
		t.Fatalf("rpmMinuteKey = %q", got)
	}
	if got := counter.getMinuteKey(ctx, 7); got == "" {
		t.Fatal("getMinuteKey returned empty key")
	}
	if got := counter.currentMinute(ctx); got <= 0 {
		t.Fatalf("currentMinute = %d", got)
	}
	if got, err := counter.IncrementRPM(ctx, 7); err != nil || got != 0 {
		t.Fatalf("IncrementRPM nil redis = %d, %v", got, err)
	}
	if got, err := counter.GetRPM(ctx, 7); err != nil || got != 0 {
		t.Fatalf("GetRPM nil redis = %d, %v", got, err)
	}
	counter.DecrementRPM(ctx, 7)
	if ok, err := counter.TryIncrementRPM(ctx, 7, 10); err != nil || !ok {
		t.Fatalf("TryIncrementRPM nil redis = %v, %v", ok, err)
	}
	if got := counter.GetSchedulability(ctx, 7, 0); got != Normal {
		t.Fatalf("GetSchedulability unlimited = %v", got)
	}
	if got := counter.GetSchedulability(ctx, 7, 10); got != Normal {
		t.Fatalf("GetSchedulability nil redis = %v", got)
	}
	if got := counter.GetSchedulabilityBatch(ctx, []*ent.Account{{ID: 7, Extra: map[string]interface{}{"max_rpm": 10}}}); len(got) != 0 {
		t.Fatalf("GetSchedulabilityBatch nil redis = %#v", got)
	}

	for _, tt := range []struct {
		current int
		max     int
		want    Schedulability
	}{
		{current: 0, max: 0, want: Normal},
		{current: 7, max: 10, want: Normal},
		{current: 8, max: 10, want: StickyOnly},
		{current: 10, max: 10, want: NotSchedulable},
	} {
		if got := rpmSchedulability(tt.current, tt.max); got != tt.want {
			t.Fatalf("rpmSchedulability(%d, %d) = %v, want %v", tt.current, tt.max, got, tt.want)
		}
	}

	redisIntCases := []struct {
		value any
		want  int
		ok    bool
	}{
		{value: 1, want: 1, ok: true},
		{value: int64(2), want: 2, ok: true},
		{value: "3", want: 3, ok: true},
		{value: []byte("4"), want: 4, ok: true},
		{value: "bad", ok: false},
		{value: 1.2, ok: false},
	}
	for _, tt := range redisIntCases {
		got, ok := redisIntValue(tt.value)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("redisIntValue(%#v) = %d, %v; want %d, %v", tt.value, got, ok, tt.want, tt.ok)
		}
	}
}

func TestFamilyCooldownNilRedisAndPureHelpers(t *testing.T) {
	ctx := context.Background()
	fc := NewFamilyCooldown(nil)

	if got := ModelFamily("OpenAI", " GPT-IMAGE-1 "); got != "gpt-image" {
		t.Fatalf("ModelFamily image = %q", got)
	}
	if got := ModelFamily("OpenAI", " GPT-4.1 "); got != "gpt-4.1" {
		t.Fatalf("ModelFamily model = %q", got)
	}
	if got := ModelFamily(" OpenAI ", " "); got != "openai" {
		t.Fatalf("ModelFamily platform fallback = %q", got)
	}
	if got := familyCooldownIndexKey(7); got != "ag:cooldown:family:7:index" {
		t.Fatalf("familyCooldownIndexKey = %q", got)
	}
	if got := familyCooldownActiveKey(7); got != "ag:cooldown:family:7:active" {
		t.Fatalf("familyCooldownActiveKey = %q", got)
	}
	if got := familyCooldownReasonKey(7, "gpt-image"); got != "ag:cooldown:family:7:reason:gpt-image" {
		t.Fatalf("familyCooldownReasonKey = %q", got)
	}

	fc.Mark(ctx, 7, "gpt-image", time.Now().Add(time.Minute), "429")
	if until, ok := fc.Until(ctx, 7, "gpt-image"); ok || !until.IsZero() {
		t.Fatalf("Until nil redis = %v, %v", until, ok)
	}
	if got := fc.InCooldownBatch(ctx, []int{7, 8}, "gpt-image"); len(got) != 0 {
		t.Fatalf("InCooldownBatch nil redis = %#v", got)
	}
	fc.Clear(ctx, 7, "gpt-image")
	if got := fc.ClearAccount(ctx, 7); got != 0 {
		t.Fatalf("ClearAccount nil redis = %d", got)
	}
	if got := fc.List(ctx, 7); got != nil {
		t.Fatalf("List nil redis = %#v", got)
	}
	if got := fc.ListBatch(ctx, []int{7}); got != nil {
		t.Fatalf("ListBatch nil redis = %#v", got)
	}

	var nilFC *FamilyCooldown
	nilFC.Mark(ctx, 7, "gpt-image", time.Now(), "noop")
	if _, ok := nilFC.Until(ctx, 7, "gpt-image"); ok {
		t.Fatal("nil FamilyCooldown Until returned ok")
	}
	if got := nilFC.InCooldownBatch(ctx, []int{7}, "gpt-image"); len(got) != 0 {
		t.Fatalf("nil FamilyCooldown InCooldownBatch = %#v", got)
	}
	nilFC.Clear(ctx, 7, "gpt-image")
	if got := nilFC.ClearAccount(ctx, 7); got != 0 {
		t.Fatalf("nil FamilyCooldown ClearAccount = %d", got)
	}
	if got := nilFC.ListBatch(ctx, []int{7}); got != nil {
		t.Fatalf("nil FamilyCooldown ListBatch = %#v", got)
	}

	ids := uniqueAccountIDs([]int{3, -1, 2, 3, 0, 1})
	want := []int{3, 2, 1}
	if len(ids) != len(want) {
		t.Fatalf("uniqueAccountIDs length = %d, want %d", len(ids), len(want))
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("uniqueAccountIDs = %#v, want %#v", ids, want)
		}
	}
	if got := uniqueAccountIDs(nil); got != nil {
		t.Fatalf("uniqueAccountIDs(nil) = %#v", got)
	}
}

func TestWindowCostPureHelpersAndNilPaths(t *testing.T) {
	ctx := context.Background()
	checker := NewWindowCostChecker(nil, nil)

	if checker == nil || checker.db != nil || checker.rdb != nil {
		t.Fatalf("NewWindowCostChecker(nil, nil) = %#v", checker)
	}
	if got := windowCostKey(7); got != "ag:cost:window:7" {
		t.Fatalf("windowCostKey = %q", got)
	}
	if got := checker.GetSchedulability(ctx, 7, nil); got != Normal {
		t.Fatalf("GetSchedulability no limit = %v", got)
	}
	if got := ((*WindowCostChecker)(nil)).GetSchedulabilityBatch(ctx, []*ent.Account{{ID: 7}}); len(got) != 0 {
		t.Fatalf("nil checker batch = %#v", got)
	}
	if got := checker.GetSchedulabilityBatch(ctx, nil); len(got) != 0 {
		t.Fatalf("empty batch = %#v", got)
	}
	if got := checker.GetSchedulabilityBatch(ctx, []*ent.Account{
		nil,
		{ID: 7, Extra: map[string]interface{}{"max_window_cost": 0}},
		{ID: 8, Extra: map[string]interface{}{"max_window_cost": 100}},
	}); len(got) != 0 {
		t.Fatalf("batch without redis/db result = %#v", got)
	}
	checker.AddCost(ctx, 7, 0)
	checker.AddCost(ctx, 7, -1)

	limit, ok := windowCostLimitFromExtra(map[string]interface{}{"max_window_cost": 100})
	if !ok {
		t.Fatal("windowCostLimitFromExtra default returned !ok")
	}
	if limit.MaxCost != 100 || limit.WindowHours != defaultWindowHours || limit.StickyReserve != defaultStickyReserve {
		t.Fatalf("default limit = %#v", limit)
	}
	limit, ok = windowCostLimitFromExtra(map[string]interface{}{
		"max_window_cost": 100,
		"window_hours":    2,
		"sticky_reserve":  5,
	})
	if !ok || limit.MaxCost != 100 || limit.WindowHours != 2 || limit.StickyReserve != 5 {
		t.Fatalf("custom limit = %#v, ok=%v", limit, ok)
	}
	if _, ok := windowCostLimitFromExtra(map[string]interface{}{"max_window_cost": -1}); ok {
		t.Fatal("windowCostLimitFromExtra negative max returned ok")
	}

	for _, tt := range []struct {
		cost float64
		want Schedulability
	}{
		{cost: 79, want: Normal},
		{cost: 80, want: StickyOnly},
		{cost: 109, want: StickyOnly},
		{cost: 110, want: NotSchedulable},
	} {
		if got := windowCostSchedulability(tt.cost, windowCostLimit{MaxCost: 100, StickyReserve: 10}); got != tt.want {
			t.Fatalf("windowCostSchedulability(%v) = %v, want %v", tt.cost, got, tt.want)
		}
	}

	floatCases := []struct {
		value any
		want  float64
		ok    bool
	}{
		{value: float64(1.5), want: 1.5, ok: true},
		{value: float32(2.5), want: 2.5, ok: true},
		{value: int64(3), want: 3, ok: true},
		{value: int(4), want: 4, ok: true},
		{value: "5.5", want: 5.5, ok: true},
		{value: []byte("6.5"), want: 6.5, ok: true},
		{value: "bad", ok: false},
		{value: true, ok: false},
	}
	for _, tt := range floatCases {
		got, ok := redisFloatValue(tt.value)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("redisFloatValue(%#v) = %v, %v; want %v, %v", tt.value, got, ok, tt.want, tt.ok)
		}
	}
}

func TestMessageQueueNilRedisAndJSONHelpers(t *testing.T) {
	ctx := context.Background()
	queue := NewMessageQueue(nil, NewRPMCounter(nil))

	if queue == nil || queue.rdb != nil || queue.rpm == nil {
		t.Fatalf("NewMessageQueue = %#v", queue)
	}
	if got := msgQueueLockKey(7); got != "ag:queue:message:{7}:lock" {
		t.Fatalf("msgQueueLockKey = %q", got)
	}
	if got := msgQueueLastKey(7); got != "ag:queue:message:{7}:last" {
		t.Fatalf("msgQueueLastKey = %q", got)
	}
	if got := waitersCounterKey(7); got != "ag:queue:message:{7}:waiters" {
		t.Fatalf("waitersCounterKey = %q", got)
	}
	if ok, err := queue.TryAcquire(ctx, 7, "req", 0); err != nil || !ok {
		t.Fatalf("TryAcquire nil redis = %v, %v", ok, err)
	}
	if ok, err := queue.WaitAcquire(ctx, 7, "req", 0, time.Second); err != nil || !ok {
		t.Fatalf("WaitAcquire nil redis = %v, %v", ok, err)
	}
	if err := queue.ForceRelease(ctx, 7); err != nil {
		t.Fatalf("ForceRelease nil redis = %v", err)
	}
	if err := queue.Release(ctx, 7, "req"); err != nil {
		t.Fatalf("Release nil redis = %v", err)
	}

	delay := queue.CalculateDelay(ctx, 7, 0)
	if delay < 170*time.Millisecond || delay > 230*time.Millisecond {
		t.Fatalf("CalculateDelay nil rpm delay = %v, want about %v", delay, defaultMinDelay)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := queue.EnforceDelay(canceled, 7, 0); err != context.Canceled {
		t.Fatalf("EnforceDelay canceled = %v", err)
	}

	jsonCases := []struct {
		name string
		body string
		want bool
	}{
		{name: "invalid json", body: `{`, want: false},
		{name: "empty messages", body: `{"messages":[]}`, want: false},
		{name: "assistant last", body: `{"messages":[{"role":"assistant","content":"ok"}]}`, want: false},
		{name: "user text", body: `{"messages":[{"role":"user","content":"hello"}]}`, want: true},
		{name: "tool result", body: `{"messages":[{"role":"user","content":[{"type":"tool_result"}]}]}`, want: false},
		{name: "tool use result", body: `{"messages":[{"role":"user","content":[{"type":"tool_use_result"}]}]}`, want: false},
		{name: "non tool blocks", body: `{"messages":[{"role":"user","content":[{"type":"text"},"raw"]}]}`, want: true},
	}
	for _, tt := range jsonCases {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRealUserMessage([]byte(tt.body)); got != tt.want {
				t.Fatalf("IsRealUserMessage = %v, want %v", got, tt.want)
			}
		})
	}
}
