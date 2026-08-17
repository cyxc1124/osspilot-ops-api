package stats

import (
	"fmt"
	"sort"
)

type tenantRank struct {
	item map[string]any
	used int64
}

func topByUsed(rows []tenantRank, limit int) []map[string]any {
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].used > rows[j].used })
	if limit < 0 {
		limit = 0
	}
	if limit > len(rows) {
		limit = len(rows)
	}
	out := make([]map[string]any, limit)
	for i := 0; i < limit; i++ {
		out[i] = rows[i].item
	}
	return out
}

func parsePeriod(raw string) (string, error) {
	if raw == "" {
		return "24h", nil
	}
	switch raw {
	case "24h", "7d", "30d":
		return raw, nil
	default:
		return "", fmt.Errorf("period must be 24h, 7d, or 30d")
	}
}

func usagePercent(used int64, quota *int64) *float64 {
	if quota == nil || *quota <= 0 {
		return nil
	}
	p := float64(used) / float64(*quota)
	return &p
}
