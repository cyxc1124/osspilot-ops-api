package main

import "testing"

func TestRestartTarget(t *testing.T) {
	if restartTarget("") != "rgw" || restartTarget("rgw.a") != "rgw.a" || restartTarget("a") != "rgw.a" {
		t.Fatal(restartTarget("a"))
	}
}

func TestHealthSummaryChecks(t *testing.T) {
	sum := healthSummary(map[string]any{
		"status": "HEALTH_WARN",
		"checks": map[string]any{
			"OSD_DOWN": map[string]any{
				"severity": "HEALTH_WARN",
				"summary":  map[string]any{"message": "1 osd down"},
			},
			"OK": map[string]any{"severity": "HEALTH_OK", "summary": map[string]any{"message": "ignore"}},
		},
	})
	if len(sum) != 1 || sum[0] != "1 osd down" {
		t.Fatal(sum)
	}
}

func TestOrchRowAndStats(t *testing.T) {
	inst := orchRowToInstance(map[string]any{
		"daemon_id": "a", "hostname": "n1", "status_desc": "running", "ports": []any{80.0},
	})
	if inst["id"] != "a" || inst["status"] != "running" {
		t.Fatal(inst)
	}
	stats := aggregateStats([]map[string]any{{
		"rgw": map[string]any{
			"req":     map[string]any{"avgcount": 100.0},
			"failed":  map[string]any{"avgcount": 1.0},
			"get_lat": map[string]any{"avgtime": 0.02},
		},
	}})
	if stats["request_count"] != 100 {
		t.Fatal(stats)
	}
}
