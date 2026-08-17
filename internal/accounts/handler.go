package accounts

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cyxc1124/osspilot-ops-api/internal/audit"
	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
	"github.com/cyxc1124/osspilot-ops-api/internal/project"
	"github.com/cyxc1124/osspilot-ops-api/internal/regions"
)

type Handler struct {
	store   *Store
	regions *regions.Store
	project *project.Client
	audit   *audit.Store
	protect func(auth.UserHandler) http.HandlerFunc
}

func NewHandler(store *Store, regions *regions.Store, project *project.Client, auditStore *audit.Store, protect func(auth.UserHandler) http.HandlerFunc) *Handler {
	return &Handler{store: store, regions: regions, project: project, audit: auditStore, protect: protect}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tenant-users", h.protect(h.list))
	mux.HandleFunc("POST /api/tenant-users", h.protect(h.create))
	mux.HandleFunc("GET /api/tenant-users/{user_id}", h.protect(h.get))
	mux.HandleFunc("PUT /api/tenant-users/{user_id}", h.protect(h.update))
	mux.HandleFunc("DELETE /api/tenant-users/{user_id}", h.protect(h.remove))
	mux.HandleFunc("POST /api/tenant-users/{user_id}/password/reset", h.protect(h.resetPassword))
}

type createRequest struct {
	Username         string  `json:"username"`
	Password         string  `json:"password"`
	DisplayName      *string `json:"display_name"`
	Email            *string `json:"email"`
	Phone            *string `json:"phone"`
	QuotaBytes       *int64  `json:"quota_bytes"`
	ObjectLimit      *int64  `json:"object_limit"`
	DailyUploadBytes *int64  `json:"daily_upload_bytes"`
	BucketLimit      *int64  `json:"bucket_limit"`
	StorageRegionID  *int64  `json:"storage_region_id"`
}

type updateRequest struct {
	DisplayName      *string         `json:"display_name"`
	Email            *string         `json:"email"`
	Phone            *string         `json:"phone"`
	Status           *string         `json:"status"`
	QuotaBytes       json.RawMessage `json:"quota_bytes"`
	ObjectLimit      json.RawMessage `json:"object_limit"`
	DailyUploadBytes json.RawMessage `json:"daily_upload_bytes"`
	BucketLimit      json.RawMessage `json:"bucket_limit"`
	StorageRegionID  json.RawMessage `json:"storage_region_id"`
}

type passwordResetRequest struct {
	NewPassword string `json:"new_password"`
}

type regionJSON struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

type response struct {
	ID               int64       `json:"id"`
	Username         string      `json:"username"`
	DisplayName      *string     `json:"display_name"`
	Email            *string     `json:"email"`
	Phone            *string     `json:"phone"`
	Status           string      `json:"status"`
	QuotaBytes       *int64      `json:"quota_bytes"`
	ObjectLimit      *int64      `json:"object_limit"`
	DailyUploadBytes *int64      `json:"daily_upload_bytes"`
	BucketLimit      *int64      `json:"bucket_limit"`
	StorageRegionID  *int64      `json:"storage_region_id"`
	StorageRegion    *regionJSON `json:"storage_region"`
	Roles            []string    `json:"roles"`
	LastLoginAt      *string     `json:"last_login_at"`
	CreatedAt        string      `json:"created_at"`
	UpdatedAt        string      `json:"updated_at"`
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
	for _, u := range items {
		out = append(out, toResponse(u))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out, "total": len(out)})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, user *auth.User) {
	if h.store == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	var req createRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		httpx.Error(w, http.StatusBadRequest, "username is required")
		return
	}
	if len(req.Password) < 8 || len(req.Password) > 128 {
		httpx.Error(w, http.StatusBadRequest, "password must be 8-128 characters")
		return
	}
	if err := h.checkRegion(r, req.StorageRegionID); err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "hash error")
		return
	}
	u, err := h.store.Insert(r.Context(), username, hash, emptyToNil(req.DisplayName), emptyToNil(req.Email), emptyToNil(req.Phone), req.QuotaBytes, req.ObjectLimit, req.DailyUploadBytes, req.BucketLimit, req.StorageRegionID, time.Now())
	if errors.Is(err, ErrConflict) {
		httpx.Error(w, http.StatusConflict, "Username already exists")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	must := true
	if err := h.pushAccount(r, *u, &must); err != nil {
		httpx.Error(w, http.StatusBadGateway, "tenant projection failed")
		return
	}
	if user != nil {
		audit.Write(h.audit, r, user.ID, user.Username, "create_tenant", "", "success", "")
	}
	httpx.JSON(w, http.StatusCreated, toResponse(*u))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	u := h.load(w, r)
	if u == nil {
		return
	}
	httpx.JSON(w, http.StatusOK, toResponse(*u))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, user *auth.User) {
	u := h.load(w, r)
	if u == nil {
		return
	}
	var req updateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.DisplayName != nil {
		u.DisplayName = emptyToNil(req.DisplayName)
	}
	if req.Email != nil {
		u.Email = emptyToNil(req.Email)
	}
	if req.Phone != nil {
		u.Phone = emptyToNil(req.Phone)
	}
	if req.Status != nil {
		switch *req.Status {
		case "active", "disabled":
			u.Status = *req.Status
		default:
			httpx.Error(w, http.StatusBadRequest, "Invalid status; expected one of: active, disabled")
			return
		}
	}
	if v, set, err := optInt64(req.QuotaBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid quota_bytes")
		return
	} else if set {
		u.QuotaBytes = v
	}
	if v, set, err := optInt64(req.ObjectLimit); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid object_limit")
		return
	} else if set {
		u.ObjectLimit = v
	}
	if v, set, err := optInt64(req.DailyUploadBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid daily_upload_bytes")
		return
	} else if set {
		u.DailyUploadBytes = v
	}
	if v, set, err := optInt64(req.BucketLimit); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid bucket_limit")
		return
	} else if set {
		u.BucketLimit = v
	}
	if v, set, err := optInt64(req.StorageRegionID); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid storage_region_id")
		return
	} else if set {
		if err := h.checkRegion(r, v); err != nil {
			httpx.Error(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		u.StorageRegionID = v
	}
	u.UpdatedAt = time.Now()
	out, err := h.store.Update(r.Context(), u)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	if err := h.pushAccount(r, *out, nil); err != nil {
		httpx.Error(w, http.StatusBadGateway, "tenant projection failed")
		return
	}
	if user != nil {
		audit.Write(h.audit, r, user.ID, user.Username, "update_tenant", "", "success", "")
	}
	httpx.JSON(w, http.StatusOK, toResponse(*out))
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request, user *auth.User) {
	u := h.load(w, r)
	if u == nil {
		return
	}
	if err := h.project.DeleteAccount(r.Context(), u.Username); err != nil {
		httpx.Error(w, http.StatusBadGateway, "tenant projection failed")
		return
	}
	err := h.store.Delete(r.Context(), u.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "User not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	if user != nil {
		audit.Write(h.audit, r, user.ID, user.Username, "delete_tenant", "", "success", "")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request, user *auth.User) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if h.store == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	var req passwordResetRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(req.NewPassword) < 8 || len(req.NewPassword) > 128 {
		httpx.Error(w, http.StatusBadRequest, "new_password must be 8-128 characters")
		return
	}
	existing, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	if existing == nil {
		httpx.Error(w, http.StatusNotFound, "User not found")
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "hash error")
		return
	}
	if err := h.store.UpdatePassword(r.Context(), id, hash, time.Now()); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	must := true
	if err := h.pushAccount(r, *existing, &must); err != nil {
		httpx.Error(w, http.StatusBadGateway, "tenant projection failed")
		return
	}
	if user != nil {
		audit.Write(h.audit, r, user.ID, user.Username, "user_password_reset", "", "success", "")
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"message": "Password reset"})
}

func (h *Handler) pushAccount(r *http.Request, u Record, must *bool) error {
	hash, _, err := h.store.Secret(r.Context(), u.ID)
	if err != nil {
		return err
	}
	acct := project.Account{
		Username:           u.Username,
		PasswordHash:       hash,
		DisplayName:        u.DisplayName,
		Email:              u.Email,
		Phone:              u.Phone,
		Status:             u.Status,
		MustChangePassword: must,
		QuotaBytes:         u.QuotaBytes,
		ObjectLimit:        u.ObjectLimit,
		DailyUploadBytes:   u.DailyUploadBytes,
		StorageRegionID:    u.StorageRegionID,
	}
	if u.StorageRegion != nil {
		code, name, ep, reg := u.StorageRegion.Code, u.StorageRegion.Name, u.StorageRegion.S3Endpoint, u.StorageRegion.S3RegionName
		acct.StorageRegionCode = &code
		acct.StorageRegionName = &name
		acct.S3Endpoint = &ep
		acct.S3RegionName = &reg
	}
	return h.project.UpsertAccount(r.Context(), acct)
}

func (h *Handler) load(w http.ResponseWriter, r *http.Request) *Record {
	id, ok := pathID(w, r)
	if !ok {
		return nil
	}
	if h.store == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return nil
	}
	u, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return nil
	}
	if u == nil {
		httpx.Error(w, http.StatusNotFound, "User not found")
		return nil
	}
	return u
}

func (h *Handler) checkRegion(r *http.Request, id *int64) error {
	if id == nil {
		return nil
	}
	if h.regions == nil {
		return errors.New("Storage region not found")
	}
	reg, err := h.regions.GetByID(r.Context(), *id)
	if err != nil {
		return errors.New("database error")
	}
	if reg == nil {
		return errors.New("Storage region not found")
	}
	if reg.Status != "active" {
		return errors.New("Storage region is not active")
	}
	return nil
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("user_id"), 10, 64)
	if err != nil || id < 1 {
		httpx.Error(w, http.StatusBadRequest, "invalid user_id")
		return 0, false
	}
	return id, true
}

func toResponse(u Record) response {
	var last *string
	if u.LastLoginAt != nil {
		s := u.LastLoginAt.UTC().Format(time.RFC3339)
		last = &s
	}
	var brief *regionJSON
	if u.StorageRegion != nil {
		brief = &regionJSON{ID: u.StorageRegion.ID, Code: u.StorageRegion.Code, Name: u.StorageRegion.Name}
	}
	return response{
		ID:               u.ID,
		Username:         u.Username,
		DisplayName:      u.DisplayName,
		Email:            u.Email,
		Phone:            u.Phone,
		Status:           u.Status,
		QuotaBytes:       u.QuotaBytes,
		ObjectLimit:      u.ObjectLimit,
		DailyUploadBytes: u.DailyUploadBytes,
		BucketLimit:      u.BucketLimit,
		StorageRegionID:  u.StorageRegionID,
		StorageRegion:    brief,
		Roles:            []string{"tenant_admin"},
		LastLoginAt:      last,
		CreatedAt:        u.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        u.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func emptyToNil(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}

func optInt64(raw json.RawMessage) (*int64, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	if string(raw) == "null" {
		return nil, true, nil
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, false, err
	}
	return &n, true, nil
}
