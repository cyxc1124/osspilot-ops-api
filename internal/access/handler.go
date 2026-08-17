package access

import (
	"errors"
	"net/http"
	"strconv"
	"unicode/utf8"

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
	mux.HandleFunc("GET /api/tenant-api-access", h.protect(h.list))
	mux.HandleFunc("GET /api/tenant-api-access/{account_id}", h.protect(h.get))
	mux.HandleFunc("POST /api/tenant-api-access/{account_id}/approve", h.protect(h.review("approve")))
	mux.HandleFunc("POST /api/tenant-api-access/{account_id}/reject", h.protect(h.review("reject")))
	mux.HandleFunc("POST /api/tenant-api-access/{account_id}/disable", h.protect(h.review("disable")))
}

func (h *Handler) ready(w http.ResponseWriter) bool {
	if h.accounts == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return false
	}
	if h.project == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "tenant projection is not configured")
		return false
	}
	return true
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if !h.ready(w) {
		return
	}
	items, err := h.project.ListAccess(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		writeUpstream(w, err)
		return
	}
	accs, err := h.accounts.List(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	byName := make(map[string]int64, len(accs))
	for _, a := range accs {
		byName[a.Username] = a.ID
	}
	out := bindOpsIDs(items, byName)
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out, "total": len(out)})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	acct := h.loadAccount(w, r)
	if acct == nil {
		return
	}
	item, err := h.project.GetAccess(r.Context(), acct.Username)
	if err != nil {
		writeUpstream(w, err)
		return
	}
	item.AccountID = acct.ID
	item.AccountName = acct.Username
	httpx.JSON(w, http.StatusOK, item)
}

func (h *Handler) review(action string) auth.UserHandler {
	return func(w http.ResponseWriter, r *http.Request, user *auth.User) {
		acct := h.loadAccount(w, r)
		if acct == nil {
			return
		}
		var req struct {
			Note *string `json:"note"`
		}
		if r.ContentLength != 0 {
			if err := httpx.DecodeJSON(r, &req); err != nil {
				httpx.Error(w, http.StatusBadRequest, "invalid json")
				return
			}
		}
		if req.Note != nil && utf8.RuneCountInString(*req.Note) > 2000 {
			httpx.Error(w, http.StatusBadRequest, "note is too long")
			return
		}
		item, err := h.project.ReviewAccess(r.Context(), acct.Username, action, req.Note)
		if err != nil {
			writeUpstream(w, err)
			return
		}
		item.AccountID = acct.ID
		item.AccountName = acct.Username
		item.ReviewedBy = &user.ID
		if h.audit != nil {
			_ = h.audit.Insert(r.Context(), audit.Entry{
				UserID:     &user.ID,
				Username:   &user.Username,
				TenantID:   &acct.ID,
				TenantName: &acct.Username,
				Action:     action + "_tenant_api_access",
				SourceIP:   audit.ClientIP(r),
				UserAgent:  audit.UserAgent(r),
			})
		}
		httpx.JSON(w, http.StatusOK, item)
	}
}

func (h *Handler) loadAccount(w http.ResponseWriter, r *http.Request) *accounts.Record {
	if !h.ready(w) {
		return nil
	}
	id, err := strconv.ParseInt(r.PathValue("account_id"), 10, 64)
	if err != nil || id < 1 {
		httpx.Error(w, http.StatusBadRequest, "invalid account_id")
		return nil
	}
	acct, err := h.accounts.GetByID(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return nil
	}
	if acct == nil {
		httpx.Error(w, http.StatusNotFound, "Account not found")
		return nil
	}
	return acct
}

func writeUpstream(w http.ResponseWriter, err error) {
	var he *project.HTTPError
	if errors.As(err, &he) {
		switch he.Status {
		case http.StatusNotFound, http.StatusConflict, http.StatusBadRequest:
			httpx.Error(w, he.Status, he.Detail)
			return
		}
	}
	httpx.Error(w, http.StatusBadGateway, "tenant api error")
}
