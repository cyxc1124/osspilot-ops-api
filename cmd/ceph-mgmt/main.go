package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
	"github.com/cyxc1124/osspilot-ops-api/internal/logx"
)

func main() {
	logx.Setup("osspilot-ceph-mgmt")
	addr := getenv("HTTP_ADDR", ":8082")
	backend := strings.ToLower(getenv("CEPH_MGMT_BACKEND", "mock"))
	var b Backend
	switch backend {
	case "ceph":
		b = NewCLI(CLIConfig{
			Bin:            getenv("CEPH_BIN", "ceph"),
			Conf:           os.Getenv("CEPH_CONF"),
			Cluster:        os.Getenv("CEPH_CLUSTER"),
			CommandTimeout: durationSec("CEPH_COMMAND_TIMEOUT_SECONDS", 15),
			RestartTimeout: durationSec("CEPH_RESTART_TIMEOUT_SECONDS", 120),
		})
	default:
		backend = "mock"
		b = Mock{}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		write(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /cluster/health", func(w http.ResponseWriter, r *http.Request) {
		out, err := b.Health(r.Context())
		reply(w, out, err)
	})
	mux.HandleFunc("GET /cluster/info", func(w http.ResponseWriter, r *http.Request) {
		out, err := b.Info(r.Context())
		reply(w, out, err)
	})
	mux.HandleFunc("GET /rgw/instances", func(w http.ResponseWriter, r *http.Request) {
		out, err := b.Instances(r.Context())
		reply(w, out, err)
	})
	mux.HandleFunc("GET /rgw/stats", func(w http.ResponseWriter, r *http.Request) {
		out, err := b.Stats(r.Context())
		reply(w, out, err)
	})
	mux.HandleFunc("POST /rgw/restart", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			InstanceID *string `json:"instance_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		id := ""
		if req.InstanceID != nil {
			id = *req.InstanceID
		}
		out, err := b.Restart(r.Context(), id)
		reply(w, out, err)
	})
	mux.HandleFunc("POST /rgw/rolling-restart", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			WaitSeconds *int `json:"wait_seconds"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		wait := 30
		if req.WaitSeconds != nil {
			wait = *req.WaitSeconds
		}
		out, err := b.RollingRestart(r.Context(), wait)
		reply(w, out, err)
	})

	slog.Info("ceph-mgmt listen", "addr", addr, "backend", backend)
	if err := http.ListenAndServe(addr, httpx.AccessLog(mux)); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}

func reply(w http.ResponseWriter, out any, err error) {
	if err != nil {
		write(w, http.StatusBadGateway, map[string]string{"detail": err.Error()})
		return
	}
	write(w, http.StatusOK, out)
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func getenv(k, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return fallback
}

func durationSec(k string, fallback int) time.Duration {
	n := fallback
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}
	return time.Duration(n) * time.Second
}
