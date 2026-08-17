package grants

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cyxc1124/osspilot-ops-api/internal/accounts"
	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/buckets"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
	"github.com/cyxc1124/osspilot-ops-api/internal/project"
)

type Handler struct {
	store    *Store
	accounts *accounts.Store
	buckets  *buckets.Store
	project  *project.Client
	protect  func(auth.UserHandler) http.HandlerFunc
}

func NewHandler(store *Store, accounts *accounts.Store, buckets *buckets.Store, project *project.Client, protect func(auth.UserHandler) http.HandlerFunc) *Handler {
	return &Handler{store: store, accounts: accounts, buckets: buckets, project: project, protect: protect}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tenant-users/{user_id}/buckets", h.protect(h.list))
	mux.HandleFunc("PUT /api/tenant-users/{user_id}/buckets", h.protect(h.replace))
	mux.HandleFunc("DELETE /api/tenant-users/{user_id}/buckets/{bucket_id}", h.protect(h.revoke))
}

type putRequest struct {
	BucketIDs []int64 `json:"bucket_ids"`
}

type grantJSON struct {
	BucketID    int64   `json:"bucket_id"`
	BucketName  string  `json:"bucket_name"`
	DisplayName *string `json:"display_name"`
	GrantedAt   *string `json:"granted_at"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	acct := h.loadAccount(w, r)
	if acct == nil {
		return
	}
	items, err := h.store.List(r.Context(), acct.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": toList(items), "total": len(items)})
}

func (h *Handler) replace(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	acct := h.loadAccount(w, r)
	if acct == nil {
		return
	}
	var req putRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	ids, ok := uniqueIDs(req.BucketIDs)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bucket_ids must be positive integers")
		return
	}
	if exceedsLimit(len(ids), acct.BucketLimit) {
		httpx.Error(w, http.StatusConflict, fmt.Sprintf("Account bucket limit (%d) exceeded", *acct.BucketLimit))
		return
	}
	found, err := h.buckets.GetByIDs(r.Context(), ids)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	byID := make(map[int64]buckets.Bucket, len(found))
	for _, b := range found {
		if b.Status == "active" {
			byID[b.ID] = b
		}
	}
	var missing []string
	ordered := make([]buckets.Bucket, 0, len(ids))
	for _, id := range ids {
		b, ok := byID[id]
		if !ok {
			missing = append(missing, strconv.FormatInt(id, 10))
			continue
		}
		ordered = append(ordered, b)
	}
	if len(missing) > 0 {
		httpx.Error(w, http.StatusBadRequest, "Unknown bucket ids: "+strings.Join(missing, ", "))
		return
	}
	if err := h.store.Replace(r.Context(), acct.ID, ids); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	proj := make([]project.BucketItem, 0, len(ordered))
	for _, b := range ordered {
		proj = append(proj, project.BucketItem{BucketName: b.BucketName, DisplayName: b.DisplayName})
	}
	if err := h.project.ReplaceBuckets(r.Context(), acct.Username, proj); err != nil {
		httpx.Error(w, http.StatusBadGateway, "tenant projection failed")
		return
	}
	items, err := h.store.List(r.Context(), acct.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": toList(items), "total": len(items)})
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	acct := h.loadAccount(w, r)
	if acct == nil {
		return
	}
	bucketID, err := strconv.ParseInt(r.PathValue("bucket_id"), 10, 64)
	if err != nil || bucketID < 1 {
		httpx.Error(w, http.StatusBadRequest, "invalid bucket_id")
		return
	}
	ok, err := h.store.Revoke(r.Context(), acct.ID, bucketID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	if !ok {
		httpx.Error(w, http.StatusNotFound, "Bucket grant not found")
		return
	}
	items, err := h.store.List(r.Context(), acct.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	proj := make([]project.BucketItem, 0, len(items))
	for _, g := range items {
		proj = append(proj, project.BucketItem{BucketName: g.BucketName, DisplayName: g.DisplayName})
	}
	if err := h.project.ReplaceBuckets(r.Context(), acct.Username, proj); err != nil {
		httpx.Error(w, http.StatusBadGateway, "tenant projection failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) loadAccount(w http.ResponseWriter, r *http.Request) *accounts.Record {
	id, err := strconv.ParseInt(r.PathValue("user_id"), 10, 64)
	if err != nil || id < 1 {
		httpx.Error(w, http.StatusBadRequest, "invalid user_id")
		return nil
	}
	if h.store == nil || h.accounts == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return nil
	}
	u, err := h.accounts.GetByID(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return nil
	}
	if u == nil {
		httpx.Error(w, http.StatusNotFound, "Account not found")
		return nil
	}
	return u
}

func toList(items []Grant) []grantJSON {
	out := make([]grantJSON, 0, len(items))
	for _, g := range items {
		ts := g.GrantedAt.UTC().Format(time.RFC3339)
		out = append(out, grantJSON{
			BucketID: g.BucketID, BucketName: g.BucketName, DisplayName: g.DisplayName, GrantedAt: &ts,
		})
	}
	return out
}
