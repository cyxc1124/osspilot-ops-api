package stats

import (
	"context"
	"net/http"
	"strconv"

	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/ceph"
	"github.com/cyxc1124/osspilot-ops-api/internal/grants"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
	"github.com/cyxc1124/osspilot-ops-api/internal/project"
	"github.com/cyxc1124/osspilot-ops-api/internal/settings"
)

type Handler struct {
	store    *Store
	grants   *grants.Store
	settings *settings.Handler
	project  *project.Client
	protect  func(auth.UserHandler) http.HandlerFunc
}

func NewHandler(store *Store, grantStore *grants.Store, settingsH *settings.Handler, proj *project.Client, protect func(auth.UserHandler) http.HandlerFunc) *Handler {
	return &Handler{store: store, grants: grantStore, settings: settingsH, project: proj, protect: protect}
}

func (h *Handler) tenantUsage(ctx context.Context) *project.Usage {
	if h.project == nil {
		return nil
	}
	u, err := h.project.GetUsage(ctx)
	if err != nil {
		return nil
	}
	return u
}

func (h *Handler) tenantRequests(ctx context.Context, period string, days, limit int, sortBy string) *project.Requests {
	if h.project == nil {
		return nil
	}
	r, err := h.project.GetRequests(ctx, period, days, limit, sortBy)
	if err != nil {
		return nil
	}
	return r
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
	used, objects, trashB, trashN := int64(0), int64(0), int64(0), int64(0)
	if u := h.tenantUsage(r.Context()); u != nil {
		used, objects, trashB, trashN = u.UsedBytes, u.ObjectCount, u.TrashBytes, u.TrashCount
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"total_used_bytes": used, "total_quota_bytes": o.QuotaBytes, "total_object_count": objects,
		"total_trash_bytes": trashB, "total_trash_object_count": trashN, "tenant_count": o.TenantCount,
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
	byBucket := map[string]project.UsageBucket{}
	if u := h.tenantUsage(r.Context()); u != nil {
		for _, b := range u.Buckets {
			byBucket[b.BucketName] = b
		}
	}
	items := make([]map[string]any, 0, len(rows))
	for _, t := range rows {
		used, objects, trash := int64(0), int64(0), int64(0)
		if h.grants != nil {
			if gs, err := h.grants.List(r.Context(), t.ID); err == nil {
				for _, g := range gs {
					if b, ok := byBucket[g.BucketName]; ok {
						used += b.UsedBytes
						objects += b.ObjectCount
						trash += b.TrashBytes
					}
				}
			}
		}
		items = append(items, map[string]any{
			"tenant_id": t.ID, "name": t.Name, "display_name": t.DisplayName, "status": t.Status,
			"quota_bytes": t.QuotaBytes, "used_bytes": used, "object_count": objects, "trash_bytes": trash,
			"usage_percent": usagePercent(used, t.QuotaBytes),
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
	if reqs := h.tenantRequests(r.Context(), period, 0, 0, ""); reqs != nil {
		p := reqs.Platform
		httpx.JSON(w, http.StatusOK, map[string]any{
			"period": period, "upload_bytes": p.UploadBytes, "download_bytes": p.DownloadBytes,
			"request_count": p.RequestCount, "get_count": p.GetCount, "put_count": p.PutCount,
			"delete_count": p.DeleteCount, "error_count": p.ErrorCount, "active_users": p.ActiveUsers,
			"collected_at": p.CollectedAt,
		})
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
	if reqs := h.tenantRequests(r.Context(), "30d", days, 0, ""); reqs != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"items": reqs.Daily.Items, "collected_at": reqs.Daily.CollectedAt})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": []any{}, "collected_at": nil})
}

func (h *Handler) performance(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	period, err := parsePeriod(r.URL.Query().Get("period"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	auditErr, auditReq := int64(0), int64(0)
	if reqs := h.tenantRequests(r.Context(), period, 0, 0, ""); reqs != nil {
		auditErr, auditReq = reqs.Platform.ErrorCount, reqs.Platform.RequestCount
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
			"audit_error_count": auditErr, "audit_request_count": auditReq, "fetched_at": collected(),
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
		"audit_error_count": auditErr, "audit_request_count": auditReq, "fetched_at": collected(),
	})
}

func (h *Handler) behavior(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	period, err := parsePeriod(r.URL.Query().Get("period"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
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
	sortBy := r.URL.Query().Get("sort_by")
	if reqs := h.tenantRequests(r.Context(), period, 0, limit, sortBy); reqs != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"period": period, "items": reqs.Users.Items, "collected_at": reqs.Users.CollectedAt})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"period": period, "items": []any{}, "collected_at": nil})
}

func (h *Handler) buckets(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	period, err := parsePeriod(r.URL.Query().Get("period"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
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
	if reqs := h.tenantRequests(r.Context(), period, 0, limit, ""); reqs != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"period": period, "items": reqs.Buckets.Items, "collected_at": reqs.Buckets.CollectedAt})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"period": period, "items": []any{}, "collected_at": nil})
}

func (h *Handler) prefixes(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	period, err := parsePeriod(r.URL.Query().Get("period"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
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
	if reqs := h.tenantRequests(r.Context(), period, 0, limit, ""); reqs != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"period": period, "items": reqs.Prefixes.Items, "collected_at": reqs.Prefixes.CollectedAt})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"period": period, "items": []any{}, "collected_at": nil})
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
