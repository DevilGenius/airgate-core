package store

import (
	"reflect"
	"testing"
)

func TestParseMonitorHTTPStatusFilter(t *testing.T) {
	include, exclude := parseMonitorHTTPStatusFilter("400 4xx 40* !404 !5xx invalid")
	wantInclude := []monitorHTTPStatusRange{
		{min: 400, max: 400},
		{min: 400, max: 499},
		{min: 400, max: 409},
	}
	wantExclude := []monitorHTTPStatusRange{
		{min: 404, max: 404},
		{min: 500, max: 599},
	}
	if !reflect.DeepEqual(include, wantInclude) || !reflect.DeepEqual(exclude, wantExclude) {
		t.Fatalf("parseMonitorHTTPStatusFilter() = %v, %v; want %v, %v", include, exclude, wantInclude, wantExclude)
	}
}

func TestSplitMonitorFilterValues(t *testing.T) {
	got := splitMonitorFilterValues("warning  info warning")
	want := []string{"warning", "info"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitMonitorFilterValues() = %v, want %v", got, want)
	}
}
