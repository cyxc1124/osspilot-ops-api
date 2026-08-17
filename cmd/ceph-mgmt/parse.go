package main

import (
	"encoding/json"
	"strconv"
	"strings"
)

func restartTarget(instanceID string) string {
	id := strings.TrimSpace(instanceID)
	if id == "" || id == "rgw" {
		return "rgw"
	}
	if strings.HasPrefix(id, "rgw.") {
		return id
	}
	return "rgw." + id
}

func healthSummary(health map[string]any) []string {
	if health == nil {
		return []string{}
	}
	var out []string
	switch s := health["summary"].(type) {
	case []any:
		for _, item := range s {
			out = append(out, stringify(item))
		}
	case string:
		if s != "" {
			out = append(out, s)
		}
	}
	checks, _ := health["checks"].(map[string]any)
	for name, raw := range checks {
		detail, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		sev := strings.ToLower(stringify(detail["severity"]))
		if sev != "warn" && sev != "warning" && sev != "error" && sev != "health_warn" && sev != "health_err" {
			continue
		}
		msg := name
		if nested, ok := detail["summary"].(map[string]any); ok {
			if m := stringify(nested["message"]); m != "" {
				msg = m
			}
		} else if m := stringify(detail["message"]); m != "" {
			msg = m
		}
		out = append(out, msg)
	}
	if out == nil {
		return []string{}
	}
	return out
}

func orchRowToInstance(row map[string]any) map[string]any {
	id := firstString(row, "daemon_id", "name", "daemon_name")
	if id == "" {
		id = "rgw"
	}
	host := firstString(row, "hostname", "host")
	if host == "" {
		host = id
	}
	status := normalizeStatus(firstString(row, "status_desc", "status"))
	var port any
	if ports, ok := row["ports"].([]any); ok && len(ports) > 0 {
		port = asInt(ports[0])
	} else {
		port = asInt(row["port"])
	}
	var zone any
	if z := stringify(row["zone"]); z != "" {
		zone = z
	}
	return map[string]any{"id": id, "hostname": host, "status": status, "port": port, "zone": zone}
}

func instancesFromStatus(status map[string]any) []map[string]any {
	servicemap, _ := status["servicemap"].(map[string]any)
	services, _ := servicemap["services"].(map[string]any)
	rgw, _ := services["rgw"].(map[string]any)
	daemons, _ := rgw["daemons"].(map[string]any)
	var out []map[string]any
	for id, raw := range daemons {
		detail, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		host := firstString(detail, "hostname", "host")
		if host == "" {
			host = id
		}
		out = append(out, map[string]any{
			"id": id, "hostname": host,
			"status": normalizeStatus(firstString(detail, "status", "state")),
			"port":   nil, "zone": nil,
		})
	}
	if out == nil {
		return []map[string]any{}
	}
	return out
}

func normalizeStatus(v string) string {
	switch strings.ToLower(v) {
	case "running", "up", "active", "online":
		return "running"
	case "stopped", "down", "inactive", "offline":
		return "stopped"
	default:
		return "unknown"
	}
}

var requestKeys = map[string]struct{}{
	"req": {}, "reqs": {}, "requests": {}, "get": {}, "put": {}, "post": {}, "delete": {}, "head": {}, "list": {}, "copy": {}, "multipart": {},
}
var failedKeys = map[string]struct{}{
	"failed_req": {}, "failed_reqs": {}, "err": {}, "error": {}, "errors": {}, "5xx": {}, "failed": {},
}

func aggregateStats(dumps []map[string]any) map[string]any {
	var reqs, failed int
	var samples []float64
	for _, payload := range dumps {
		rgw, _ := payload["rgw"].(map[string]any)
		for name, raw := range rgw {
			val, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if _, hit := requestKeys[name]; hit {
				reqs += counter(val)
			}
			if _, hit := failedKeys[name]; hit {
				failed += counter(val)
			}
			if strings.Contains(name, "latency") || strings.Contains(name, "time") {
				if n := asFloat(val["avgtime"]); n != nil {
					samples = append(samples, *n*1000)
				}
			}
		}
	}
	out := emptyStats()
	if reqs > 0 {
		out["request_count"] = reqs
		out["error_rate"] = round6(float64(failed) / float64(reqs))
	}
	out["p95_latency_ms"] = percentile(samples, 0.95)
	out["p99_latency_ms"] = percentile(samples, 0.99)
	return out
}

func counter(val map[string]any) int {
	if n := asInt(val["avgcount"]); n != nil {
		return int(*n)
	}
	if n := asInt(val["count"]); n != nil {
		return int(*n)
	}
	return 0
}

func percentile(samples []float64, p float64) any {
	if len(samples) == 0 {
		return nil
	}
	ordered := append([]float64(nil), samples...)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j] < ordered[j-1]; j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	idx := int(float64(len(ordered))*p) - 1
	if idx < 0 {
		idx = 0
	}
	return round3(ordered[idx])
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := stringify(m[k]); s != "" {
			return s
		}
	}
	return ""
}

func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	default:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(strings.Trim(strings.TrimSpace(toRaw(v)), `"`))
	}
}

func toRaw(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func asInt(v any) *int64 {
	switch t := v.(type) {
	case float64:
		n := int64(t)
		return &n
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			return nil
		}
		return &n
	case int:
		n := int64(t)
		return &n
	case int64:
		return &t
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		if err != nil {
			return nil
		}
		return &n
	default:
		return nil
	}
}

func asFloat(v any) *float64 {
	switch t := v.(type) {
	case float64:
		return &t
	case json.Number:
		n, err := t.Float64()
		if err != nil {
			return nil
		}
		return &n
	case int:
		n := float64(t)
		return &n
	default:
		return nil
	}
}

func round6(n float64) float64 { return float64(int(n*1e6+0.5)) / 1e6 }
func round3(n float64) float64 { return float64(int(n*1e3+0.5)) / 1e3 }
