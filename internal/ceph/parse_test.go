package ceph

import "testing"

func TestParseHealth(t *testing.T) {
	st, sum := parseHealth(map[string]any{"status": "WARN", "summary": []any{"slow OSD"}})
	if st == nil || *st != "HEALTH_WARN" || len(sum) != 1 || sum[0] != "slow OSD" {
		t.Fatalf("status=%v summary=%v", st, sum)
	}
	st, _ = parseHealth(map[string]any{"health": map[string]any{"status": "HEALTH_OK"}})
	if st == nil || *st != "HEALTH_OK" {
		t.Fatalf("nested %v", st)
	}
}

func TestParseInstances(t *testing.T) {
	got := parseInstances(map[string]any{"instances": []any{
		map[string]any{"id": "rgw.a", "hostname": "n1", "status": "up", "port": float64(80)},
	}})
	if len(got) != 1 || got[0].ID != "rgw.a" || got[0].Status != "running" || got[0].Port == nil || *got[0].Port != 80 {
		t.Fatalf("%#v", got)
	}
}

func TestParseStats(t *testing.T) {
	n, er, _, _ := parseStats(map[string]any{"request_count": float64(10), "error_rate": 0.1})
	if n == nil || *n != 10 || er == nil || *er != 0.1 {
		t.Fatalf("n=%v er=%v", n, er)
	}
}
