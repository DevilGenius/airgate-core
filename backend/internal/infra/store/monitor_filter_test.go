package store

import (
	"reflect"
	"testing"
)

func TestParseMonitorHTTPStatusFilter(t *testing.T) {
	include, exclude := parseMonitorHTTPStatusFilter("400 4* 40? !404 !5* invalid")
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

func TestParseMonitorHTTPStatusAndErrorCodeFilter(t *testing.T) {
	includeRanges, excludeRanges, includeErrorCodes, excludeErrorCodes := parseMonitorHTTPStatusAndErrorCodeFilter("4* 402 4xx account_dead !404 !account_disabled")
	wantIncludeRanges := []monitorHTTPStatusRange{
		{min: 400, max: 499},
		{min: 402, max: 402},
	}
	wantExcludeRanges := []monitorHTTPStatusRange{{min: 404, max: 404}}
	wantIncludeErrorCodes := []string{"4xx", "account_dead"}
	wantExcludeErrorCodes := []string{"account_disabled"}
	if !reflect.DeepEqual(includeRanges, wantIncludeRanges) ||
		!reflect.DeepEqual(excludeRanges, wantExcludeRanges) ||
		!reflect.DeepEqual(includeErrorCodes, wantIncludeErrorCodes) ||
		!reflect.DeepEqual(excludeErrorCodes, wantExcludeErrorCodes) {
		t.Fatalf("parseMonitorHTTPStatusAndErrorCodeFilter() = %v, %v, %v, %v; want %v, %v, %v, %v",
			includeRanges, excludeRanges, includeErrorCodes, excludeErrorCodes,
			wantIncludeRanges, wantExcludeRanges, wantIncludeErrorCodes, wantExcludeErrorCodes)
	}
}

func TestParseMonitorHTTPStatusPattern(t *testing.T) {
	tests := []struct {
		pattern string
		want    []monitorHTTPStatusRange
		ok      bool
	}{
		{pattern: "402", want: []monitorHTTPStatusRange{{min: 402, max: 402}}, ok: true},
		{pattern: "4*", want: []monitorHTTPStatusRange{{min: 400, max: 499}}, ok: true},
		{pattern: "40?", want: []monitorHTTPStatusRange{{min: 400, max: 409}}, ok: true},
		{pattern: "?02", want: []monitorHTTPStatusRange{
			{min: 102, max: 102},
			{min: 202, max: 202},
			{min: 302, max: 302},
			{min: 402, max: 402},
			{min: 502, max: 502},
		}, ok: true},
		{pattern: "4xx", want: nil, ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got, ok := parseMonitorHTTPStatusPattern(tt.pattern)
			if ok != tt.ok || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseMonitorHTTPStatusPattern(%q) = %v, %v; want %v, %v", tt.pattern, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestSplitMonitorFilterValues(t *testing.T) {
	got := splitMonitorFilterValues("warning  info warning")
	want := []string{"warning", "info"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitMonitorFilterValues() = %v, want %v", got, want)
	}
}
