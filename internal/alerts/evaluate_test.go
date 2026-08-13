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

func TestThresholdPercent(t *testing.T) {
	raw := json.RawMessage(`{"threshold_percent":0.9}`)
	if got := thresholdPercent(raw, 0.8); got != 0.9 {
		t.Fatalf("got %v", got)
	}
	if got := thresholdPercent(nil, 0.8); got != 0.8 {
		t.Fatalf("default %v", got)
	}
}
