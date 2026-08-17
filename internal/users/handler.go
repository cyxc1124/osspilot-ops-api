package users

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cyxc1124/osspilot-ops-api/internal/audit"
	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
)

type Handler struct {
	store   *Store
	audit   *audit.Store
	protect func(auth.UserHandler) http.HandlerFunc
}

func NewHandler(store *Store, auditStore *audit.Store, protect func(auth.UserHandler) http.HandlerFunc) *Handler {
	return &Handler{store: store, audit: auditStore, protect: protect}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/users", h.protect(h.list))
	mux.HandleFunc("POST /api/users", h.protect(h.create))
	mux.HandleFunc("GET /api/users/{user_id}", h.protect(h.get))
	mux.HandleFunc("PUT /api/users/{user_id}", h.protect(h.update))
	mux.HandleFunc("DELETE /api/users/{user_id}", h.protect(h.remove))
	mux.HandleFunc("POST /api/users/{user_id}/password/reset", h.protect(h.resetPassword))
}

type createRequest struct {
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	DisplayName *string  `json:"display_name"`
	Email       *string  `json:"email"`
	Phone       *string  `json:"phone"`
	OpsRoles    []string `json:"ops_roles"`
}

type updateRequest struct {
	DisplayName *string  `json:"display_name"`
	Email       *string  `json:"email"`
	Phone       *string  `json:"phone"`
	Status      *string  `json:"status"`
	OpsRoles    []string `json:"ops_roles"`
}

type passwordResetRequest struct {
	NewPassword string `json:"new_password"`
}

type response struct {
	ID          int64    `json:"id"`
	Username    string   `json:"username"`
	DisplayName *string  `json:"display_name"`
	Email       *string  `json:"email"`
	Phone       *string  `json:"phone"`
	Status      string   `json:"status"`
	OpsRoles    []string `json:"ops_roles"`
	LastLoginAt *string  `json:"last_login_at"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
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
	if len(username) > 64 {
		httpx.Error(w, http.StatusBadRequest, "username must be at most 64 characters")
		return
	}
	if len(req.Password) < 8 || len(req.Password) > 128 {
		httpx.Error(w, http.StatusBadRequest, "password must be 8-128 characters")
		return
	}
	roles, err := normalizeRoles(req.OpsRoles)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "hash error")
		return
	}
	u, err := h.store.Insert(r.Context(), username, hash, emptyToNil(req.DisplayName), emptyToNil(req.Email), emptyToNil(req.Phone), roles, time.Now())
	if errors.Is(err, ErrConflict) {
		httpx.Error(w, http.StatusConflict, "Username already exists")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	if user != nil {
		audit.Write(h.audit, r, user.ID, user.Username, "user_create", "", "success", "")
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

func (h *Handler) update(w http.ResponseWriter, r *http.Request, actor *auth.User) {
	u := h.load(w, r)
	if u == nil {
		return
	}
	var req updateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Status != nil {
		switch *req.Status {
		case "active", "disabled":
		default:
			httpx.Error(w, http.StatusBadRequest, "Invalid status; expected one of: active, disabled")
			return
		}
		if *req.Status == "disabled" && actor.ID == u.ID {
			httpx.Error(w, http.StatusBadRequest, "Cannot disable your own account")
			return
		}
		u.Status = *req.Status
	}
	var roles *[]string
	if req.OpsRoles != nil {
		names, err := normalizeRoles(req.OpsRoles)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		roles = &names
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
	u.UpdatedAt = time.Now()
	out, err := h.store.Update(r.Context(), u, roles)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	action := "user_update"
	if roles != nil {
		action = "modify_user_role"
	}
	if actor != nil {
		audit.Write(h.audit, r, actor.ID, actor.Username, action, "", "success", "")
	}
	httpx.JSON(w, http.StatusOK, toResponse(*out))
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request, actor *auth.User) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if actor.ID == id {
		httpx.Error(w, http.StatusBadRequest, "Cannot delete your own account")
		return
	}
	if h.store == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	err := h.store.Delete(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "User not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	if actor != nil {
		audit.Write(h.audit, r, actor.ID, actor.Username, "user_delete", "", "success", "")
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
	if user != nil {
		audit.Write(h.audit, r, user.ID, user.Username, "user_password_reset", "", "success", "")
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"message": "Password reset"})
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
	roles := u.Roles
	if roles == nil {
		roles = []string{}
	}
	return response{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Email:       u.Email,
		Phone:       u.Phone,
		Status:      u.Status,
		OpsRoles:    roles,
		LastLoginAt: last,
		CreatedAt:   u.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   u.UpdatedAt.UTC().Format(time.RFC3339),
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
