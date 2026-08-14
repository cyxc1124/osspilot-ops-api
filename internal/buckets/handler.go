package buckets

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
	"github.com/cyxc1124/osspilot-ops-api/internal/project"
	"github.com/cyxc1124/osspilot-ops-api/internal/regions"
	"github.com/cyxc1124/osspilot-ops-api/internal/settings"
)

type Handler struct {
	store    *Store
	regions  *regions.Store
	settings *settings.Handler
	project  *project.Client
	protect  func(auth.UserHandler) http.HandlerFunc
}

func NewHandler(store *Store, regions *regions.Store, settingsH *settings.Handler, proj *project.Client, protect func(auth.UserHandler) http.HandlerFunc) *Handler {
	return &Handler{store: store, regions: regions, settings: settingsH, project: proj, protect: protect}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/buckets", h.protect(h.list))
	mux.HandleFunc("POST /api/buckets", h.protect(h.create))
	mux.HandleFunc("POST /api/buckets/import-batch", h.protect(h.importBatch))
	mux.HandleFunc("GET /api/s3/buckets", h.protect(h.discover))
	mux.HandleFunc("POST /api/buckets/{bucket_id}/inventory", h.protect(h.inventory))
	mux.HandleFunc("GET /api/buckets/{bucket_id}/policy", h.protect(h.getPolicy))
	mux.HandleFunc("PUT /api/buckets/{bucket_id}/policy", h.protect(h.putPolicy))
	mux.HandleFunc("DELETE /api/buckets/{bucket_id}/policy", h.protect(h.deletePolicy))
}

type createRequest struct {
	BucketName      string  `json:"bucket_name"`
	DisplayName     *string `json:"display_name"`
	StorageRegionID *int64  `json:"storage_region_id"`
	QuotaBytes      *int64  `json:"quota_bytes"`
	ObjectLimit     *int64  `json:"object_limit"`
	SyncVersioning  *bool   `json:"sync_versioning"`
}

type batchRequest struct {
	BucketNames     []string `json:"bucket_names"`
	StorageRegionID *int64   `json:"storage_region_id"`
	SyncVersioning  *bool    `json:"sync_versioning"`
}

type regionJSON struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

type item struct {
	ID              int64       `json:"id"`
	BucketName      string      `json:"bucket_name"`
	DisplayName     *string     `json:"display_name"`
	StorageRegionID *int64      `json:"storage_region_id"`
	StorageRegion   *regionJSON `json:"storage_region"`
	Status          string      `json:"status"`
	UsedBytes       int64       `json:"used_bytes"`
	ObjectCount     int64       `json:"object_count"`
	CollectedAt     *string     `json:"collected_at"`
	CreatedAt       string      `json:"created_at"`
	UpdatedAt       string      `json:"updated_at"`
}

type detail struct {
	ID              int64   `json:"id"`
	BucketName      string  `json:"bucket_name"`
	DisplayName     *string `json:"display_name"`
	StorageRegionID *int64  `json:"storage_region_id"`
	QuotaBytes      *int64  `json:"quota_bytes"`
	ObjectLimit     *int64  `json:"object_limit"`
	Status          string  `json:"status"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
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
	usage := map[string]project.UsageBucket{}
	if h.project != nil {
		if u, err := h.project.GetUsage(r.Context()); err == nil {
			for _, b := range u.Buckets {
				usage[b.BucketName] = b
			}
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	out := make([]item, 0, len(items))
	for _, b := range items {
		it := toItem(b)
		if u, ok := usage[b.BucketName]; ok {
			it.UsedBytes = u.UsedBytes
			it.ObjectCount = u.ObjectCount
			it.CollectedAt = &now
		}
		out = append(out, it)
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
	b, err := h.register(r, strings.TrimSpace(req.BucketName), emptyToNil(req.DisplayName), req.StorageRegionID, req.QuotaBytes, req.ObjectLimit)
	if err != nil {
		writeRegErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toDetail(*b))
}

func (h *Handler) importBatch(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if h.store == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	var req batchRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(req.BucketNames) == 0 || len(req.BucketNames) > 50 {
		httpx.Error(w, http.StatusBadRequest, "bucket_names must contain 1-50 items")
		return
	}
	imported := make([]detail, 0, len(req.BucketNames))
	failed := make([]map[string]string, 0)
	for _, raw := range req.BucketNames {
		b, err := h.register(r, strings.TrimSpace(raw), nil, req.StorageRegionID, nil, nil)
		if err != nil {
			failed = append(failed, map[string]string{"bucket_name": strings.TrimSpace(raw), "error": err.Error()})
			continue
		}
		imported = append(imported, toDetail(*b))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"imported": imported, "failed": failed})
}

func (h *Handler) register(r *http.Request, name string, display *string, regionID, quota, objects *int64) (*Bucket, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	if err := h.checkRegion(r, regionID); err != nil {
		return nil, err
	}
	b, err := h.store.Insert(r.Context(), name, display, regionID, quota, objects, time.Now())
	if err != nil && !errors.Is(err, ErrConflict) {
		return nil, errors.New("database error")
	}
	return b, err
}

func (h *Handler) checkRegion(r *http.Request, id *int64) error {
	if id == nil {
		return nil
	}
	if h.regions == nil {
		return errRegion
	}
	reg, err := h.regions.GetByID(r.Context(), *id)
	if err != nil {
		return errors.New("database error")
	}
	if reg == nil {
		return errRegion
	}
	if reg.Status != "active" {
		return errors.New("Storage region is not active")
	}
	return nil
}

var errRegion = errors.New("Storage region not found")

func writeRegErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrConflict):
		httpx.Error(w, http.StatusConflict, "Bucket already registered")
	case err.Error() == "database error":
		httpx.Error(w, http.StatusInternalServerError, "database error")
	case errors.Is(err, errRegion) || err.Error() == "Storage region is not active":
		httpx.Error(w, http.StatusUnprocessableEntity, err.Error())
	default:
		httpx.Error(w, http.StatusBadRequest, err.Error())
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

func toItem(b Bucket) item {
	var brief *regionJSON
	if b.StorageRegion != nil {
		brief = &regionJSON{ID: b.StorageRegion.ID, Code: b.StorageRegion.Code, Name: b.StorageRegion.Name}
	}
	return item{
		ID: b.ID, BucketName: b.BucketName, DisplayName: b.DisplayName,
		StorageRegionID: b.StorageRegionID, StorageRegion: brief, Status: b.Status,
		CreatedAt: b.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: b.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toDetail(b Bucket) detail {
	return detail{
		ID: b.ID, BucketName: b.BucketName, DisplayName: b.DisplayName,
		StorageRegionID: b.StorageRegionID, QuotaBytes: b.QuotaBytes, ObjectLimit: b.ObjectLimit,
		Status:    b.Status,
		CreatedAt: b.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: b.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
