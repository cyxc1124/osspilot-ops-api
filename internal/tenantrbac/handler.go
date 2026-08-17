package tenantrbac

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cyxc1124/osspilot-ops-api/internal/accounts"
	"github.com/cyxc1124/osspilot-ops-api/internal/audit"
	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
	"github.com/cyxc1124/osspilot-ops-api/internal/project"
)

type Handler struct {
	accounts *accounts.Store
	project  *project.Client
	audit    *audit.Store
	protect  func(auth.UserHandler) http.HandlerFunc
}

func NewHandler(accounts *accounts.Store, proj *project.Client, auditStore *audit.Store, protect func(auth.UserHandler) http.HandlerFunc) *Handler {
	return &Handler{accounts: accounts, project: proj, audit: auditStore, protect: protect}
}

func (h *Handler) Register(mux *http.ServeMux) {
	// ponytail: one catch-all proxy per method; tenant owns RBAC shapes and ids.
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		mux.HandleFunc(method+" /api/tenant-users/{user_id}/rbac/{rest...}", h.protect(h.proxy))
	}
}

func (h *Handler) proxy(w http.ResponseWriter, r *http.Request, user *auth.User) {
	if h.accounts == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	if h.project == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "tenant projection is not configured")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("user_id"), 10, 64)
	if err != nil || id < 1 {
		httpx.Error(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	acct, err := h.accounts.GetByID(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	if acct == nil {
		httpx.Error(w, http.StatusNotFound, "Account not found")
		return
	}
	rest := strings.Trim(r.PathValue("rest"), "/")
	if rest == "" {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}
	path := "/internal/accounts/" + url.PathEscape(acct.Username) + "/rbac/" + rest
	if q := r.URL.RawQuery; q != "" {
		path += "?" + q
	}
	raw, err := h.project.DoRaw(r.Context(), r.Method, path, r.Body, r.Header.Get("Content-Type"))
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "tenant api error")
		return
	}
	body := raw.Body
	if raw.Status >= 200 && raw.Status < 300 {
		if remapped, err := remapAccountID(body, acct.ID); err == nil {
			body = remapped
		}
		if user != nil && r.Method != http.MethodGet {
			audit.Write(h.audit, r, user.ID, user.Username, "modify_permission", "", "success", "")
		}
	}
	if raw.ContentType != "" {
		w.Header().Set("Content-Type", raw.ContentType)
	} else if len(body) > 0 {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(raw.Status)
	_, _ = w.Write(body)
}
