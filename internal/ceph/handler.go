package ceph

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/cyxc1124/osspilot-ops-api/internal/audit"
	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
	"github.com/cyxc1124/osspilot-ops-api/internal/settings"
)

type Handler struct {
	settings *settings.Handler
	mgmt     *client
	audit    *audit.Store
	read     func(auth.UserHandler) http.HandlerFunc
	write    func(auth.UserHandler) http.HandlerFunc
}

func NewHandler(settingsH *settings.Handler, read, write func(auth.UserHandler) http.HandlerFunc, auditStore *audit.Store) *Handler {
	return &Handler{settings: settingsH, mgmt: newClient(), read: read, write: write, audit: auditStore}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ops/rgw/instances", h.read(h.instances))
	mux.HandleFunc("GET /api/ops/rgw/stats", h.read(h.stats))
	mux.HandleFunc("GET /api/ops/cluster/health", h.read(h.health))
	mux.HandleFunc("GET /api/ops/cluster/info", h.read(h.info))
	mux.HandleFunc("POST /api/ops/s3/test", h.read(h.testS3))
	mux.HandleFunc("POST /api/ops/rgw/restart", h.write(h.restart))
	mux.HandleFunc("POST /api/ops/rgw/rolling-restart", h.write(h.rolling))
}

func (h *Handler) runtime(w http.ResponseWriter, r *http.Request) (settings.Runtime, bool) {
	if h.settings == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "settings are not configured")
		return settings.Runtime{}, false
	}
	rt, err := h.settings.Runtime(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return settings.Runtime{}, false
	}
	return rt, true
}

func (h *Handler) instances(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	rt, ok := h.runtime(w, r)
	if !ok {
		return
	}
	payload, err := h.mgmt.get(r.Context(), rt.CephMgmtAPIURL, "/rgw/instances")
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"available": false, "error": err.Error(), "instances": []Instance{}, "fetched_at": nowRFC3339()})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"available": true, "error": nil, "instances": parseInstances(payload), "fetched_at": nowRFC3339()})
}

func (h *Handler) stats(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	rt, ok := h.runtime(w, r)
	if !ok {
		return
	}
	payload, err := h.mgmt.get(r.Context(), rt.CephMgmtAPIURL, "/rgw/stats")
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{
			"available": false, "error": err.Error(), "request_count": nil, "error_rate": nil,
			"p95_latency_ms": nil, "p99_latency_ms": nil, "fetched_at": nowRFC3339(),
		})
		return
	}
	n, er, p95, p99 := parseStats(payload)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"available": true, "error": nil, "request_count": n, "error_rate": er,
		"p95_latency_ms": p95, "p99_latency_ms": p99, "fetched_at": nowRFC3339(),
	})
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	rt, ok := h.runtime(w, r)
	if !ok {
		return
	}
	payload, err := h.mgmt.get(r.Context(), rt.CephMgmtAPIURL, "/cluster/health")
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"available": false, "error": err.Error(), "status": nil, "summary": []string{}, "fetched_at": nowRFC3339()})
		return
	}
	st, sum := parseHealth(payload)
	httpx.JSON(w, http.StatusOK, map[string]any{"available": true, "error": nil, "status": st, "summary": sum, "fetched_at": nowRFC3339()})
}

func (h *Handler) info(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	rt, ok := h.runtime(w, r)
	if !ok {
		return
	}
	payload, err := h.mgmt.get(r.Context(), rt.CephMgmtAPIURL, "/cluster/info")
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{
			"available": false, "error": err.Error(), "ceph_version": nil,
			"total_bytes": nil, "used_bytes": nil, "avail_bytes": nil, "fetched_at": nowRFC3339(),
		})
		return
	}
	ver, total, used, avail := parseInfo(payload)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"available": true, "error": nil, "ceph_version": ver,
		"total_bytes": total, "used_bytes": used, "avail_bytes": avail, "fetched_at": nowRFC3339(),
	})
}

func (h *Handler) testS3(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	rt, ok := h.runtime(w, r)
	if !ok {
		return
	}
	if rt.S3Endpoint == "" || rt.RGWAccessKey == "" || rt.RGWSecretKey == "" {
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": false, "endpoint": nilStr(rt.S3Endpoint), "bucket_count": nil, "error": "S3/RGW is not configured"})
		return
	}
	n, err := listBuckets(r.Context(), rt.S3Endpoint, rt.RGWAccessKey, rt.RGWSecretKey)
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": false, "endpoint": rt.S3Endpoint, "bucket_count": nil, "error": err.Error()})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "endpoint": rt.S3Endpoint, "bucket_count": n, "error": nil})
}

func (h *Handler) restart(w http.ResponseWriter, r *http.Request, user *auth.User) {
	rt, ok := h.runtime(w, r)
	if !ok {
		return
	}
	var req struct {
		InstanceID *string `json:"instance_id"`
	}
	if r.ContentLength != 0 {
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid json")
			return
		}
	}
	payload, err := h.mgmt.post(r.Context(), rt.CephMgmtAPIURL, "/rgw/restart", map[string]any{"instance_id": req.InstanceID}, 120*time.Second)
	status, msg := "success", ""
	if err != nil {
		status, msg = "failure", err.Error()
	}
	if user != nil {
		audit.Write(h.audit, r, user.ID, user.Username, "restart_rgw", "", status, msg)
	}
	h.writeRestart(w, payload, err)
}

func (h *Handler) rolling(w http.ResponseWriter, r *http.Request, user *auth.User) {
	rt, ok := h.runtime(w, r)
	if !ok {
		return
	}
	wait := 30
	if r.ContentLength != 0 {
		var req struct {
			WaitSeconds *int `json:"wait_seconds"`
		}
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid json")
			return
		}
		if req.WaitSeconds != nil {
			if *req.WaitSeconds < 0 || *req.WaitSeconds > 300 {
				httpx.Error(w, http.StatusBadRequest, "wait_seconds must be between 0 and 300")
				return
			}
			wait = *req.WaitSeconds
		}
	}
	payload, err := h.mgmt.post(r.Context(), rt.CephMgmtAPIURL, "/rgw/rolling-restart", map[string]any{"wait_seconds": wait}, 120*time.Second)
	status, msg := "success", ""
	if err != nil {
		status, msg = "failure", err.Error()
	}
	if user != nil {
		audit.Write(h.audit, r, user.ID, user.Username, "rolling_restart_rgw", "", status, msg)
	}
	h.writeRestart(w, payload, err)
}

func (h *Handler) writeRestart(w http.ResponseWriter, payload any, err error) {
	if err != nil {
		httpx.Error(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	ok, msg, restarted, errMsg := parseRestart(payload)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": ok, "message": msg, "restarted": restarted, "error": errMsg})
}

func listBuckets(ctx context.Context, endpoint, ak, sk string) (int, error) {
	cli := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider(ak, sk, ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(strings.TrimRight(endpoint, "/"))
		o.UsePathStyle = true
	})
	out, err := cli.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return 0, err
	}
	return len(out.Buckets), nil
}

func nilStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
