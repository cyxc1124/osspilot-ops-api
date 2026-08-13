package ceph

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

var healthMap = map[string]string{
	"HEALTH_OK": "HEALTH_OK", "OK": "HEALTH_OK",
	"HEALTH_WARN": "HEALTH_WARN", "WARN": "HEALTH_WARN", "WARNING": "HEALTH_WARN",
	"HEALTH_ERR": "HEALTH_ERR", "ERR": "HEALTH_ERR", "ERROR": "HEALTH_ERR",
}

var instanceStatusMap = map[string]string{
	"running": "running", "up": "running", "active": "running",
	"stopped": "stopped", "down": "stopped", "inactive": "stopped",
}

type Instance struct {
	ID       string  `json:"id"`
	Hostname string  `json:"hostname"`
	Status   string  `json:"status"`
	Port     *int    `json:"port"`
	Zone     *string `json:"zone"`
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

func ParseInstances(payload any) []Instance { return parseInstances(payload) }

func ParseStats(payload any) (requestCount *int, errorRate, p95, p99 *float64) {
	return parseStats(payload)
}

func parseInstances(payload any) []Instance {
	raw := payload
	if m, ok := payload.(map[string]any); ok {
		raw = first(m, "instances", "items", "data")
	}
	list, ok := raw.([]any)
	if !ok {
		return []Instance{}
	}
	out := make([]Instance, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := str(first(m, "id", "name"), "rgw-"+strconv.Itoa(i+1))
		host := str(first(m, "hostname", "host", "node"), id)
		st := instanceStatusMap[strings.ToLower(str(first(m, "status", "state"), "unknown"))]
		if st == "" {
			st = "unknown"
		}
		var port *int
		if p := asInt(m["port"]); p != nil {
			port = p
		}
		var zone *string
		if z := m["zone"]; z != nil && str(z, "") != "" {
			s := str(z, "")
			zone = &s
		}
		out = append(out, Instance{ID: id, Hostname: host, Status: st, Port: port, Zone: zone})
	}
	return out
}

func parseStats(payload any) (requestCount *int, errorRate, p95, p99 *float64) {
	m, ok := payload.(map[string]any)
	if !ok {
		return
	}
	data := m
	if inner, ok := m["stats"].(map[string]any); ok {
		data = inner
	}
	requestCount = asInt(first(data, "request_count", "requests", "total_requests"))
	errorRate = asFloat(first(data, "error_rate", "error_ratio", "5xx_rate"))
	p95 = asFloat(first(data, "p95_latency_ms", "p95_latency", "latency_p95_ms"))
	p99 = asFloat(first(data, "p99_latency_ms", "p99_latency", "latency_p99_ms"))
	return
}

func parseHealth(payload any) (status *string, summary []string) {
	summary = []string{}
	m, ok := payload.(map[string]any)
	if !ok {
		return nil, summary
	}
	raw := m["status"]
	if raw == nil {
		if h, ok := m["health"].(map[string]any); ok {
			raw = h["status"]
		}
	}
	if s := healthMap[strings.ToUpper(strings.TrimSpace(str(raw, "")))]; s != "" {
		status = &s
	}
	switch v := first(m, "summary", "messages").(type) {
	case []any:
		for _, item := range v {
			summary = append(summary, str(item, ""))
		}
	case string:
		if v != "" {
			summary = []string{v}
		}
	}
	if len(summary) == 0 {
		if checks, ok := m["checks"].(map[string]any); ok {
			for name, detail := range checks {
				d, ok := detail.(map[string]any)
				if !ok {
					continue
				}
				sev := str(d["severity"], "")
				if sev == "warn" || sev == "error" {
					summary = append(summary, str(first(d, "summary", "message"), name))
				}
			}
		}
	}
	return status, summary
}

func parseInfo(payload any) (version *string, total, used, avail *int) {
	m, ok := payload.(map[string]any)
	if !ok {
		return
	}
	if v := str(m["ceph_version"], ""); v != "" {
		version = &v
	}
	total, used, avail = asInt(m["total_bytes"]), asInt(m["used_bytes"]), asInt(m["avail_bytes"])
	return
}

func parseRestart(payload any) (ok bool, message string, restarted []string, errMsg *string) {
	restarted = []string{}
	m, okm := payload.(map[string]any)
	if !okm {
		return false, "invalid response", restarted, nil
	}
	ok = asBool(m["ok"])
	message = str(m["message"], "")
	if raw, ok := m["restarted"].([]any); ok {
		for _, item := range raw {
			restarted = append(restarted, str(item, ""))
		}
	}
	if e := str(m["error"], ""); e != "" {
		errMsg = &e
	}
	return ok, message, restarted, errMsg
}

func first(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

func str(v any, fallback string) string {
	if v == nil {
		return fallback
	}
	switch t := v.(type) {
	case string:
		if t == "" {
			return fallback
		}
		return t
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fallback
		}
		s := strings.Trim(string(b), `"`)
		if s == "" || s == "null" {
			return fallback
		}
		return s
	}
}

func asInt(v any) *int {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case float64:
		n := int(t)
		return &n
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			return nil
		}
		i := int(n)
		return &i
	case int:
		return &t
	case string:
		n, err := strconv.Atoi(t)
		if err != nil {
			return nil
		}
		return &n
	}
	return nil
}

func asFloat(v any) *float64 {
	if v == nil {
		return nil
	}
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
		f := float64(t)
		return &f
	case string:
		n, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return nil
		}
		return &n
	}
	return nil
}

func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	}
	return false
}
