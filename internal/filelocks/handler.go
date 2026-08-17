package filelocks

import (
	"errors"
	"net/http"
	"strings"

	"github.com/cyxc1124/osspilot-ops-api/internal/audit"
	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
	"github.com/cyxc1124/osspilot-ops-api/internal/project"
)

type Handler struct {
	project *project.Client
	audit   *audit.Store
	protect func(auth.UserHandler) http.HandlerFunc
}

func NewHandler(proj *project.Client, auditStore *audit.Store, protect func(auth.UserHandler) http.HandlerFunc) *Handler {
	return &Handler{project: proj, audit: auditStore, protect: protect}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/file-locks/force-unlock", h.protect(h.forceUnlock))
}

func (h *Handler) forceUnlock(w http.ResponseWriter, r *http.Request, user *auth.User) {
	if h.project == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "tenant projection is not configured")
		return
	}
	var req struct {
		BucketName string `json:"bucket_name"`
		ObjectKey  string `json:"object_key"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(req.BucketName) == "" || strings.TrimSpace(req.ObjectKey) == "" || strings.HasPrefix(req.ObjectKey, ".trash/") {
		httpx.Error(w, http.StatusBadRequest, "Invalid object key")
		return
	}
	unlocked, err := h.project.ForceUnlock(r.Context(), req.BucketName, req.ObjectKey)
	if err != nil {
		var he *project.HTTPError
		if errors.As(err, &he) && (he.Status == http.StatusBadRequest || he.Status == http.StatusNotFound) {
			httpx.Error(w, he.Status, he.Detail)
			return
		}
		httpx.Error(w, http.StatusBadGateway, "tenant api error")
		return
	}
	if h.audit != nil {
		_ = h.audit.Insert(r.Context(), audit.Entry{
			UserID:     &user.ID,
			Username:   &user.Username,
			BucketName: &req.BucketName,
			ObjectKey:  &req.ObjectKey,
			Action:     "force_unlock_file",
			SourceIP:   audit.ClientIP(r),
			UserAgent:  audit.UserAgent(r),
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"unlocked": unlocked})
}
