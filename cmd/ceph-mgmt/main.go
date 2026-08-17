package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
)

func main() {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8082"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /rgw/instances", func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]any{"instances": []any{}})
	})
	mux.HandleFunc("GET /rgw/stats", func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]any{"request_count": 0, "error_rate": 0, "p95_latency_ms": 0, "p99_latency_ms": 0})
	})
	mux.HandleFunc("GET /cluster/health", func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]any{"status": "HEALTH_WARN", "summary": []string{"ceph-mgmt stub; no live cluster"}})
	})
	mux.HandleFunc("GET /cluster/info", func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]any{"ceph_version": nil, "total_bytes": nil, "used_bytes": nil, "avail_bytes": nil})
	})
	mux.HandleFunc("POST /rgw/restart", func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]any{"ok": true, "message": "stub restart", "restarted": []string{}, "error": nil})
	})
	mux.HandleFunc("POST /rgw/rolling-restart", func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]any{"ok": true, "message": "stub rolling restart", "restarted": []string{}, "error": nil})
	})
	slog.Info("ceph-mgmt stub listen", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}

func write(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
