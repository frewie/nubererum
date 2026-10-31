package main

import (
	"encoding/json"
	"testing"
)

func decodeRule(t *testing.T, raw string) map[string]any {
	t.Helper()
	var rule map[string]any
	if err := json.Unmarshal([]byte(raw), &rule); err != nil {
		t.Fatal(err)
	}
	return rule
}

// The rules are shapes seen in a real history (2026-10-29).
func TestDescribeRepeatRule(t *testing.T) {
	tests := []struct{ rule, want string }{
		{`{"fu":256,"fa":1,"tp":0,"of":[{"wd":5}],"ed":64092211200}`, "Every week on Friday"},
		{`{"fu":256,"fa":2,"tp":0,"of":[{"wd":0}],"ed":64092211200}`, "Every 2 weeks on Sunday"},
		{`{"fu":16,"fa":2,"tp":1,"of":[{"dy":0}],"ed":64092211200}`, "Every 2 days after completion"},
		{`{"fu":16,"fa":1,"tp":0,"of":[{"dy":0}],"ed":64092211200}`, "Every day"},
		{`{"fu":256,"fa":4,"tp":1,"of":[{"wd":5}],"ed":64092211200}`, "Every 4 weeks after completion"},
		{`{"fu":8,"fa":1,"tp":0,"of":[{"dy":-1}],"ed":64092211200}`, "Every month on the last day"},
		{`{"fu":8,"fa":1,"tp":0,"of":[{"dy":14}],"ed":64092211200}`, "Every month on the 15th"},
		{`{"fu":8,"fa":1,"tp":0,"of":[{"dy":0}],"ed":64092211200}`, "Every month on the 1st"},
		{`{"fu":8,"fa":1,"tp":0,"of":[{"dy":21}],"ed":64092211200}`, "Every month on the 22nd"},
		{`{"fu":8,"fa":1,"tp":0,"of":[{"wd":4,"wdo":1}],"ed":64092211200}`, "Every month on the first Thursday"},
		{`{"fu":8,"fa":6,"tp":1,"of":[{"dy":3}],"ed":64092211200}`, "Every 6 months after completion"},
		{`{"fu":4,"fa":1,"tp":0,"of":[{"dy":14,"mo":9}],"ed":64092211200}`, "Every year"},
		{`{"fu":256,"fa":1,"tp":0,"of":[{"wd":1},{"wd":4}],"ed":1293753600}`, "Every week on Monday, Thursday until 2010-12-31"},
		{`{"fu":99,"fa":1}`, "Repeating"},
	}
	for _, tt := range tests {
		if got := describeRepeatRule(decodeRule(t, tt.rule)); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.rule, got, tt.want)
		}
	}
}

func TestRepeatSummaryUsesTheTemplateForInstances(t *testing.T) {
	template := map[string]any{"RepeatRule": decodeRule(t, `{"fu":256,"fa":1,"tp":0,"of":[{"wd":5}],"ed":64092211200}`)}
	tasks := map[string]map[string]any{"tpl": template}
	instance := map[string]any{"RecurrenceIDs": []any{"tpl"}}
	if got := repeatSummary(instance, tasks); got != "Every week on Friday" {
		t.Fatalf("instance = %q", got)
	}
	if got := repeatSummary(template, tasks); got != "Every week on Friday" {
		t.Fatalf("template = %q", got)
	}
	if got := repeatSummary(map[string]any{"RecurrenceIDs": []any{"gone"}}, tasks); got != "" {
		t.Fatalf("instance of a missing template = %q", got)
	}
	if got := repeatSummary(map[string]any{}, tasks); got != "" {
		t.Fatalf("plain = %q", got)
	}
}
