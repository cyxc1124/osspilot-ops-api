package stats

import (
	"net/http"
	"strconv"

	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/ceph"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
	"github.com/cyxc1124/osspilot-ops-api/internal/settings"
)

type Handler struct {
	store    *Store
	settings *settings.Handler
	protect  func(auth.UserHandler) http.HandlerFunc
}

func NewHandler(store *Store, settingsH *settings.Handler, protect func(auth.UserHandler) http.HandlerFunc) *Handler {
	return &Handler{store: store, settings: settingsH, protect: protect}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/stats/overview", h.protect(h.overview))
	mux.HandleFunc("GET /api/stats/tenants/ranking", h.protect(h.tenants))
	mux.HandleFunc("GET /api/stats/storage-classes", h.protect(h.storageClasses))
	mux.HandleFunc("GET /api/stats/traffic", h.protect(h.traffic))
	mux.HandleFunc("GET /api/stats/traffic/daily", h.protect(h.daily))
	mux.HandleFunc("GET /api/stats/performance", h.protect(h.performance))
	mux.HandleFunc("GET /api/stats/behavior/users", h.protect(h.behavior))
	mux.HandleFunc("GET /api/stats/buckets/ranking", h.protect(h.buckets))
	mux.HandleFunc("GET /api/stats/prefixes/ranking", h.protect(h.prefixes))
}

func (h *Handler) ready(w http.ResponseWriter) bool {
	if h.store == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return false
	}
	return true
}

func (h *Handler) overview(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if !h.ready(w) {
		return
	}
	o, err := h.store.Overview(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"total_used_bytes": 0, "total_quota_bytes": o.QuotaBytes, "total_object_count": 0,
		"total_trash_bytes": 0, "total_trash_object_count": 0, "tenant_count": o.TenantCount,
		"bucket_count": o.BucketCount, "collected_at": collected(),
	})
}

func (h *Handler) tenants(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if !h.ready(w) {
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			httpx.Error(w, http.StatusBadRequest, "limit must be 1-100")
			return
		}
		limit = n
	}
	rows, err := h.store.Tenants(r.Context(), limit)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, t := range rows {
		items = append(items, map[string]any{
			"tenant_id": t.ID, "name": t.Name, "display_name": t.DisplayName, "status": t.Status,
			"quota_bytes": t.QuotaBytes, "used_bytes": 0, "object_count": 0, "trash_bytes": 0,
			"usage_percent": usagePercent(0, t.QuotaBytes),
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) storageClasses(w http.ResponseWriter, _ *http.Request, _ *auth.User) {
	httpx.JSON(w, http.StatusOK, map[string]any{"items": []any{}, "collected_at": collected()})
}

func (h *Handler) traffic(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	period, err := parsePeriod(r.URL.Query().Get("period"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, zeroTraffic(period))
}

func (h *Handler) daily(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	days := 30
	if raw := r.URL.Query().Get("days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 90 {
			httpx.Error(w, http.StatusBadRequest, "days must be 1-90")
			return
		}
		days = n
	}
	_ = days
	httpx.JSON(w, http.StatusOK, map[string]any{"items": []any{}, "collected_at": collected()})
}

func (h *Handler) performance(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if _, err := parsePeriod(r.URL.Query().Get("period")); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	url := ""
	if h.settings != nil {
		rt, err := h.settings.Runtime(r.Context())
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
		url = rt.CephMgmtAPIURL
	}
	payload, err := ceph.Fetch(r.Context(), url, "/rgw/stats")
	instPayload, instErr := ceph.Fetch(r.Context(), url, "/rgw/instances")
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{
			"available": false, "error": err.Error(), "request_count": nil, "error_rate": nil,
			"p95_latency_ms": nil, "p99_latency_ms": nil, "running_instances": 0, "total_instances": 0,
			"audit_error_count": 0, "audit_request_count": 0, "fetched_at": collected(),
		})
		return
	}
	n, er, p95, p99 := parseStatsFrom(payload)
	running, total := 0, 0
	if instErr == nil {
		for _, item := range parseInst(instPayload) {
			total++
			if item == "running" {
				running++
			}
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"available": true, "error": nil, "request_count": n, "error_rate": er,
		"p95_latency_ms": p95, "p99_latency_ms": p99, "running_instances": running, "total_instances": total,
		"audit_error_count": 0, "audit_request_count": 0, "fetched_at": collected(),
	})
}

func (h *Handler) behavior(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	period, err := parsePeriod(r.URL.Query().Get("period"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"period": period, "items": []any{}, "collected_at": collected()})
}

func (h *Handler) buckets(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	period, err := parsePeriod(r.URL.Query().Get("period"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"period": period, "items": []any{}, "collected_at": collected()})
}

func (h *Handler) prefixes(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	period, err := parsePeriod(r.URL.Query().Get("period"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"period": period, "items": []any{}, "collected_at": collected()})
}

func zeroTraffic(period string) map[string]any {
	return map[string]any{
		"period": period, "upload_bytes": 0, "download_bytes": 0, "request_count": 0,
		"get_count": 0, "put_count": 0, "delete_count": 0, "error_count": 0, "active_users": 0,
		"collected_at": collected(),
	}
}

func parseStatsFrom(payload any) (*int, *float64, *float64, *float64) {
	return ceph.ParseStats(payload)
}

func parseInst(payload any) []string {
	items := ceph.ParseInstances(payload)
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Status)
	}
	return out
}
