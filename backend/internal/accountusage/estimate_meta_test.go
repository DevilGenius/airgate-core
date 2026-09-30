package accountusage

import (
	"testing"
	"time"
)

func TestEstimateMetaTracksActualWindowPresence(t *testing.T) {
	now := time.Now()
	meta := EstimateMeta{FiveHour: WindowEstimate{ObservedAt: &now}}
	if !meta.HasFiveHourWindow() {
		t.Fatal("legacy 5h observation must remain supported")
	}
	if !meta.ObserveWindows(false, now) || meta.HasFiveHourWindow() {
		t.Fatal("latest 7d-only snapshot must override old 5h metadata")
	}
	if meta.ObserveWindows(true, now.Add(-time.Minute)) || meta.HasFiveHourWindow() {
		t.Fatal("older snapshot must not restore 5h")
	}
	cloned := Clone(meta)
	if !Equal(meta, cloned) {
		t.Fatal("clone must preserve presence")
	}
	*cloned.HasFiveHour = true
	*cloned.WindowsObservedAt = now.Add(time.Minute)
	if Equal(meta, cloned) || meta.HasFiveHourWindow() || !meta.WindowsObservedAt.Equal(now) {
		t.Fatal("clone must not share presence pointers")
	}
	if !meta.ObserveWindows(true, now.Add(time.Minute)) || !meta.HasFiveHourWindow() {
		t.Fatal("new 5h snapshot must restore support")
	}
}
