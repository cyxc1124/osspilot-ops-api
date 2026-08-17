package buckets

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
	"github.com/cyxc1124/osspilot-ops-api/internal/project"
	"github.com/cyxc1124/osspilot-ops-api/internal/rgw"
)

func (h *Handler) discover(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	cli, ok := h.s3Client(w, r)
	if !ok {
		return
	}
	if h.store == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	listed, err := cli.ListBuckets(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "storage error")
		return
	}
	registered, err := h.store.NameSet(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	onlyUnreg := !strings.EqualFold(r.URL.Query().Get("unregistered_only"), "false")
	items := make([]map[string]any, 0, len(listed))
	for _, b := range listed {
		_, reg := registered[b.Name]
		if onlyUnreg && reg {
			continue
		}
		var created any
		if b.CreationDate != nil {
			created = b.CreationDate.UTC().Format(time.RFC3339)
		}
		items = append(items, map[string]any{
			"name": b.Name, "creation_date": created, "registered": reg,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (h *Handler) inventory(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if h.store == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	if h.project == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "tenant projection is not configured")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("bucket_id"), 10, 64)
	if err != nil || id < 1 {
		httpx.Error(w, http.StatusBadRequest, "invalid bucket_id")
		return
	}
	b, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	if b == nil {
		httpx.Error(w, http.StatusNotFound, "Bucket not found")
		return
	}
	if b.Status != "active" {
		httpx.Error(w, http.StatusConflict, "Bucket is not active")
		return
	}
	jobID, err := h.project.EnqueueInventory(r.Context(), b.BucketName)
	if err != nil {
		var he *project.HTTPError
		if errors.As(err, &he) {
			httpx.Error(w, he.Status, he.Detail)
			return
		}
		httpx.Error(w, http.StatusBadGateway, "tenant api error")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"bucket_id": b.ID, "bucket_name": b.BucketName, "job_id": jobID,
		"message": "容量统计任务已提交，请稍后刷新查看结果",
	})
}

func (h *Handler) getPolicy(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	b, cli, ok := h.loadBucketS3(w, r)
	if !ok {
		return
	}
	policy, err := cli.GetBucketPolicy(r.Context(), b.BucketName)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "storage error")
		return
	}
	tenantID, _ := h.store.FirstAccountID(r.Context(), b.ID)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"bucket_id": b.ID, "bucket_name": b.BucketName, "tenant_id": tenantID,
		"policy": policy, "has_policy": policy != nil,
	})
}

func (h *Handler) putPolicy(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	b, cli, ok := h.loadBucketS3(w, r)
	if !ok {
		return
	}
	var req struct {
		Policy map[string]any `json:"policy"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Policy == nil {
		httpx.Error(w, http.StatusBadRequest, "policy is required")
		return
	}
	if err := cli.PutBucketPolicy(r.Context(), b.BucketName, req.Policy); err != nil {
		httpx.Error(w, http.StatusBadGateway, "storage error")
		return
	}
	tenantID, _ := h.store.FirstAccountID(r.Context(), b.ID)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"bucket_id": b.ID, "bucket_name": b.BucketName, "tenant_id": tenantID,
		"policy": req.Policy, "has_policy": true,
	})
}

func (h *Handler) deletePolicy(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	b, cli, ok := h.loadBucketS3(w, r)
	if !ok {
		return
	}
	if err := cli.DeleteBucketPolicy(r.Context(), b.BucketName); err != nil {
		httpx.Error(w, http.StatusBadGateway, "storage error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) loadBucketS3(w http.ResponseWriter, r *http.Request) (*Bucket, *rgw.Client, bool) {
	if h.store == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return nil, nil, false
	}
	cli, ok := h.s3Client(w, r)
	if !ok {
		return nil, nil, false
	}
	id, err := strconv.ParseInt(r.PathValue("bucket_id"), 10, 64)
	if err != nil || id < 1 {
		httpx.Error(w, http.StatusBadRequest, "invalid bucket_id")
		return nil, nil, false
	}
	b, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return nil, nil, false
	}
	if b == nil {
		httpx.Error(w, http.StatusNotFound, "Bucket not found")
		return nil, nil, false
	}
	return b, cli, true
}

func (h *Handler) s3Client(w http.ResponseWriter, r *http.Request) (*rgw.Client, bool) {
	if h.settings == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "settings are not configured")
		return nil, false
	}
	rt, err := h.settings.Runtime(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return nil, false
	}
	cli := rgw.New(rt.S3Endpoint, rt.RGWAccessKey, rt.RGWSecretKey)
	if cli == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "S3/RGW is not configured")
		return nil, false
	}
	return cli, true
}
