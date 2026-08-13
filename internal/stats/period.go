package stats

import "fmt"

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
