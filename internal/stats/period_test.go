package stats

import "testing"

func TestParsePeriod(t *testing.T) {
	got, err := parsePeriod("")
	if err != nil || got != "24h" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := parsePeriod("1h"); err == nil {
		t.Fatal("expected error")
	}
}

func TestUsagePercent(t *testing.T) {
	q := int64(100)
	p := usagePercent(25, &q)
	if p == nil || *p != 0.25 {
		t.Fatalf("%v", p)
	}
	if usagePercent(1, nil) != nil {
		t.Fatal("nil quota")
	}
}

func TestTopByUsed(t *testing.T) {
	rows := []tenantRank{
		{used: 10, item: map[string]any{"name": "a"}},
		{used: 50, item: map[string]any{"name": "b"}},
		{used: 20, item: map[string]any{"name": "c"}},
	}
	got := topByUsed(rows, 2)
	if len(got) != 2 || got[0]["name"] != "b" || got[1]["name"] != "c" {
		t.Fatalf("%v", got)
	}
}
