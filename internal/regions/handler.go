package regions

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
)

type Handler struct {
	store *Store
	read  func(auth.UserHandler) http.HandlerFunc
	write func(auth.UserHandler) http.HandlerFunc
}

func NewHandler(store *Store, read, write func(auth.UserHandler) http.HandlerFunc) *Handler {
	return &Handler{store: store, read: read, write: write}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/regions", h.read(h.list))
	mux.HandleFunc("POST /api/regions", h.write(h.create))
	mux.HandleFunc("PUT /api/regions/{region_id}", h.write(h.update))
	mux.HandleFunc("DELETE /api/regions/{region_id}", h.write(h.remove))
}

type createRequest struct {
	Code         string `json:"code"`
	Name         string `json:"name"`
	S3Endpoint   string `json:"s3_endpoint"`
	S3RegionName string `json:"s3_region_name"`
	IsDefault    bool   `json:"is_default"`
	Status       string `json:"status"`
}

type updateRequest struct {
	Name         *string `json:"name"`
	S3Endpoint   *string `json:"s3_endpoint"`
	S3RegionName *string `json:"s3_region_name"`
	IsDefault    *bool   `json:"is_default"`
	Status       *string `json:"status"`
}

type response struct {
	ID           int64  `json:"id"`
	Code         string `json:"code"`
	Name         string `json:"name"`
	S3Endpoint   string `json:"s3_endpoint"`
	S3RegionName string `json:"s3_region_name"`
	IsDefault    bool   `json:"is_default"`
	Status       string `json:"status"`
	TenantCount  int64  `json:"tenant_count"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if h.store == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	items, err := h.store.List(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	out := make([]response, 0, len(items))
	for _, item := range items {
		out = append(out, toResponse(item))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out, "total": len(out)})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if h.store == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	var req createRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	code := strings.ToLower(strings.TrimSpace(req.Code))
	name := strings.TrimSpace(req.Name)
	endpoint := strings.TrimSpace(req.S3Endpoint)
	regionName := strings.TrimSpace(req.S3RegionName)
	if regionName == "" {
		regionName = "us-east-1"
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "active"
	}
	if code == "" || name == "" || endpoint == "" {
		httpx.Error(w, http.StatusBadRequest, "code, name and s3_endpoint are required")
		return
	}
	if len(code) > 64 || len(name) > 128 || len(endpoint) > 512 || len(regionName) > 64 {
		httpx.Error(w, http.StatusBadRequest, "field too long")
		return
	}
	if err := validStatus(status); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now()
	out, err := h.store.Insert(r.Context(), &Region{
		Code:         code,
		Name:         name,
		S3Endpoint:   endpoint,
		S3RegionName: regionName,
		IsDefault:    req.IsDefault,
		Status:       status,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if errors.Is(err, ErrConflict) {
		httpx.Error(w, http.StatusConflict, "Region code '"+code+"' already exists")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	httpx.JSON(w, http.StatusCreated, toResponse(*out))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if h.store == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	cur, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	if cur == nil {
		httpx.Error(w, http.StatusNotFound, "Region not found")
		return
	}
	var req updateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name == nil && req.S3Endpoint == nil && req.S3RegionName == nil && req.IsDefault == nil && req.Status == nil {
		httpx.Error(w, http.StatusBadRequest, "No fields to update")
		return
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			httpx.Error(w, http.StatusBadRequest, "name is required")
			return
		}
		cur.Name = name
	}
	if req.S3Endpoint != nil {
		endpoint := strings.TrimSpace(*req.S3Endpoint)
		if endpoint == "" {
			httpx.Error(w, http.StatusBadRequest, "s3_endpoint is required")
			return
		}
		cur.S3Endpoint = endpoint
	}
	if req.S3RegionName != nil {
		regionName := strings.TrimSpace(*req.S3RegionName)
		if regionName == "" {
			regionName = "us-east-1"
		}
		cur.S3RegionName = regionName
	}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if err := validStatus(status); err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		cur.Status = status
	}
	if req.IsDefault != nil {
		cur.IsDefault = *req.IsDefault
	}
	cur.UpdatedAt = time.Now()
	out, err := h.store.Update(r.Context(), cur, req.IsDefault)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	httpx.JSON(w, http.StatusOK, toResponse(*out))
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if h.store == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	err := h.store.Delete(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "Region not found")
		return
	}
	if errors.Is(err, ErrDefault) {
		httpx.Error(w, http.StatusConflict, "Cannot delete the default region; assign another default first")
		return
	}
	if errors.Is(err, ErrBound) {
		httpx.Error(w, http.StatusConflict, "Region has bound accounts; reassign them before deleting")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("region_id"), 10, 64)
	if err != nil || id < 1 {
		httpx.Error(w, http.StatusBadRequest, "invalid region_id")
		return 0, false
	}
	return id, true
}

func toResponse(r Region) response {
	return response{
		ID:           r.ID,
		Code:         r.Code,
		Name:         r.Name,
		S3Endpoint:   r.S3Endpoint,
		S3RegionName: r.S3RegionName,
		IsDefault:    r.IsDefault,
		Status:       r.Status,
		TenantCount:  r.TenantCount,
		CreatedAt:    r.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    r.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func validStatus(status string) error {
	switch status {
	case "active", "disabled":
		return nil
	}
	return errors.New("Invalid status; expected one of: active, disabled")
}
