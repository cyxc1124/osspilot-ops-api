package alerts

import (
	"encoding/json"
	"testing"
)

func TestUsagePercent(t *testing.T) {
	q := int64(100)
	pct, ok := usagePercent(80, &q)
	if !ok || pct != 0.8 {
		t.Fatalf("got %v %v", pct, ok)
	}
	if _, ok := usagePercent(1, nil); ok {
		t.Fatal("nil quota")
	}
}

func TestConfigFloat(t *testing.T) {
	raw := json.RawMessage(`{"error_rate":0.05,"window_minutes":30,"count_threshold":50}`)
	if got := configFloat(raw, "error_rate", 1); got != 0.05 {
		t.Fatalf("error_rate %v", got)
	}
	if got := configFloat(raw, "window_minutes", 60); got != 30 {
		t.Fatalf("window %v", got)
	}
	if got := configFloat(nil, "failure_rate", 0.1); got != 0.1 {
		t.Fatalf("default %v", got)
	}
}

func TestFingerprint(t *testing.T) {
	tid, bid := int64(3), int64(9)
	if got := fingerprint(1, &tid, &bid); got != "r1-t3-b9" {
		t.Fatal(got)
	}
	if got := fingerprint(2, nil, nil); got != "r2-t0-b0" {
		t.Fatal(got)
	}
}

func TestUnseenIDs(t *testing.T) {
	tid, gone := int64(1), int64(9)
	open := []Event{{ID: 1, TenantID: &tid}, {ID: 2, TenantID: &gone}, {ID: 3}}
	got := unseenIDs(open, map[int64]bool{1: true}, func(ev Event) *int64 { return ev.TenantID })
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("%v", got)
	}
}

func TestThresholdPercent(t *testing.T) {
	raw := json.RawMessage(`{"threshold_percent":0.9}`)
	if got := thresholdPercent(raw, 0.8); got != 0.9 {
		t.Fatalf("got %v", got)
	}
	if got := thresholdPercent(nil, 0.8); got != 0.8 {
		t.Fatalf("default %v", got)
	}
}
