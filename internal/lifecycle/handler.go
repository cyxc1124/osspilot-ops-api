package lifecycle

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/buckets"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
)

type Handler struct {
	store   *Store
	buckets *buckets.Store
	protect func(auth.UserHandler) http.HandlerFunc
}

func NewHandler(store *Store, buckets *buckets.Store, protect func(auth.UserHandler) http.HandlerFunc) *Handler {
	return &Handler{store: store, buckets: buckets, protect: protect}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/lifecycle-rules", h.protect(h.list))
	mux.HandleFunc("POST /api/buckets/{bucket_id}/lifecycle-rules", h.protect(h.create))
	mux.HandleFunc("PUT /api/lifecycle-rules/{rule_id}", h.protect(h.update))
	mux.HandleFunc("DELETE /api/lifecycle-rules/{rule_id}", h.protect(h.remove))
}

type createRequest struct {
	Prefix                    string `json:"prefix"`
	Enabled                   *bool  `json:"enabled"`
	DeleteAfterDays           *int   `json:"delete_after_days"`
	CleanupTrashAfterDays     *int   `json:"cleanup_trash_after_days"`
	CleanupVersionsAfterDays  *int   `json:"cleanup_versions_after_days"`
	CleanupMultipartAfterDays *int   `json:"cleanup_multipart_after_days"`
}

type updateRequest struct {
	Prefix                    *string `json:"prefix"`
	Enabled                   *bool   `json:"enabled"`
	DeleteAfterDays           *int    `json:"delete_after_days"`
	CleanupTrashAfterDays     *int    `json:"cleanup_trash_after_days"`
	CleanupVersionsAfterDays  *int    `json:"cleanup_versions_after_days"`
	CleanupMultipartAfterDays *int    `json:"cleanup_multipart_after_days"`
}

type ruleJSON struct {
	ID                        int64  `json:"id"`
	BucketID                  int64  `json:"bucket_id"`
	BucketName                string `json:"bucket_name"`
	TenantID                  int64  `json:"tenant_id"`
	Prefix                    string `json:"prefix"`
	Enabled                   bool   `json:"enabled"`
	DeleteAfterDays           *int   `json:"delete_after_days"`
	CleanupTrashAfterDays     *int   `json:"cleanup_trash_after_days"`
	CleanupVersionsAfterDays  *int   `json:"cleanup_versions_after_days"`
	CleanupMultipartAfterDays *int   `json:"cleanup_multipart_after_days"`
	CreatedAt                 string `json:"created_at"`
	UpdatedAt                 string `json:"updated_at"`
}

func (h *Handler) ready(w http.ResponseWriter) bool {
	if h.store == nil || h.buckets == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return false
	}
	return true
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if !h.ready(w) {
		return
	}
	bucketID, ok := optQueryID(w, r, "bucket_id")
	if !ok {
		return
	}
	tenantID, ok := optQueryID(w, r, "tenant_id")
	if !ok {
		return
	}
	items, err := h.store.List(r.Context(), bucketID, tenantID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	out := make([]ruleJSON, 0, len(items))
	for _, item := range items {
		out = append(out, toJSON(item))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out, "total": len(out)})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if !h.ready(w) {
		return
	}
	bucketID, ok := pathID(w, r, "bucket_id")
	if !ok {
		return
	}
	b, err := h.buckets.GetByID(r.Context(), bucketID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	if b == nil {
		httpx.Error(w, http.StatusNotFound, "Bucket not found")
		return
	}
	var req createRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(req.Prefix) > 1024 {
		httpx.Error(w, http.StatusBadRequest, "prefix must be at most 1024 characters")
		return
	}
	if err := validDays(req.DeleteAfterDays); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validDays(req.CleanupTrashAfterDays); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validDays(req.CleanupVersionsAfterDays); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validDays(req.CleanupMultipartAfterDays); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := requireAction(req.DeleteAfterDays, req.CleanupTrashAfterDays, req.CleanupVersionsAfterDays, req.CleanupMultipartAfterDays); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	item, err := h.store.Insert(r.Context(), bucketID, req.Prefix, enabled, req.DeleteAfterDays, req.CleanupTrashAfterDays, req.CleanupVersionsAfterDays, req.CleanupMultipartAfterDays, time.Now())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	httpx.JSON(w, http.StatusCreated, toJSON(*item))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	item := h.load(w, r)
	if item == nil {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var req updateRequest
	if err := dec.Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(raw) == 0 {
		httpx.Error(w, http.StatusBadRequest, "No fields to update")
		return
	}
	if req.Prefix != nil {
		if len(*req.Prefix) > 1024 {
			httpx.Error(w, http.StatusBadRequest, "prefix must be at most 1024 characters")
			return
		}
		item.Prefix = *req.Prefix
	}
	if req.Enabled != nil {
		item.Enabled = *req.Enabled
	}
	if _, ok := raw["delete_after_days"]; ok {
		if err := validDays(req.DeleteAfterDays); err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		item.DeleteAfterDays = req.DeleteAfterDays
	}
	if _, ok := raw["cleanup_trash_after_days"]; ok {
		if err := validDays(req.CleanupTrashAfterDays); err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		item.CleanupTrashAfterDays = req.CleanupTrashAfterDays
	}
	if _, ok := raw["cleanup_versions_after_days"]; ok {
		if err := validDays(req.CleanupVersionsAfterDays); err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		item.CleanupVersionsAfterDays = req.CleanupVersionsAfterDays
	}
	if _, ok := raw["cleanup_multipart_after_days"]; ok {
		if err := validDays(req.CleanupMultipartAfterDays); err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		item.CleanupMultipartAfterDays = req.CleanupMultipartAfterDays
	}
	item.UpdatedAt = time.Now()
	out, err := h.store.Update(r.Context(), item)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	httpx.JSON(w, http.StatusOK, toJSON(*out))
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if !h.ready(w) {
		return
	}
	id, ok := pathID(w, r, "rule_id")
	if !ok {
		return
	}
	okDel, err := h.store.Delete(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	if !okDel {
		httpx.Error(w, http.StatusNotFound, "Lifecycle rule not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) load(w http.ResponseWriter, r *http.Request) *Rule {
	if !h.ready(w) {
		return nil
	}
	id, ok := pathID(w, r, "rule_id")
	if !ok {
		return nil
	}
	item, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return nil
	}
	if item == nil {
		httpx.Error(w, http.StatusNotFound, "Lifecycle rule not found")
		return nil
	}
	return item
}

func toJSON(u Rule) ruleJSON {
	return ruleJSON{
		ID: u.ID, BucketID: u.BucketID, BucketName: u.BucketName, TenantID: u.TenantID,
		Prefix: u.Prefix, Enabled: u.Enabled,
		DeleteAfterDays: u.DeleteAfterDays, CleanupTrashAfterDays: u.CleanupTrashAfterDays,
		CleanupVersionsAfterDays: u.CleanupVersionsAfterDays, CleanupMultipartAfterDays: u.CleanupMultipartAfterDays,
		CreatedAt: u.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: u.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id < 1 {
		httpx.Error(w, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return id, true
}

func optQueryID(w http.ResponseWriter, r *http.Request, name string) (*int64, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return nil, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		httpx.Error(w, http.StatusBadRequest, name+" must be a positive integer")
		return nil, false
	}
	return &id, true
}
