package settings

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
)

const (
	masked                       = "********"
	defaultUploadExpires         = 1800
	defaultDownloadExpires       = 600
	defaultMaxUploadBytes  int64 = 5 * 1024 * 1024 * 1024
	defaultLogo                  = "O"
	defaultTitle                 = "OssPilot 对象存储"
	defaultSubtitle              = "租户控制台"
	defaultMultipartStale        = 7
)

type Fallbacks struct {
	S3Endpoint        string
	RGWAccessKey      string
	RGWSecretKey      string
	DownloadCDNURL    string
	PreviewCDNURL     string
	ObjectHTTPDomain  string
	ObjectHTTPSDomain string
	OfficeURL         string
	CephMgmtAPIURL    string
}

type Handler struct {
	store     *Store
	fallbacks Fallbacks
	read      func(auth.UserHandler) http.HandlerFunc
	write     func(auth.UserHandler) http.HandlerFunc
}

func NewHandler(store *Store, read, write func(auth.UserHandler) http.HandlerFunc, fb Fallbacks) *Handler {
	return &Handler{store: store, read: read, write: write, fallbacks: fb}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings", h.read(h.get))
	mux.HandleFunc("PUT /api/settings", h.write(h.put))
}

type updateRequest struct {
	S3Endpoint                    *string `json:"s3_endpoint"`
	RGWAccessKey                  *string `json:"rgw_access_key"`
	RGWSecretKey                  *string `json:"rgw_secret_key"`
	DefaultUploadPresignExpires   *int    `json:"default_upload_presign_expires"`
	DefaultDownloadPresignExpires *int    `json:"default_download_presign_expires"`
	MaxUploadBytes                *int64  `json:"max_upload_bytes"`
	AuditEnabled                  *bool   `json:"audit_enabled"`
	OfficeURL                     *string `json:"office_url"`
	DownloadCDNURL                *string `json:"download_cdn_url"`
	PreviewCDNURL                 *string `json:"preview_cdn_url"`
	ObjectHTTPDomain              *string `json:"object_http_domain"`
	ObjectHTTPSDomain             *string `json:"object_https_domain"`
	CephMgmtAPIURL                *string `json:"ceph_mgmt_api_url"`
	TenantLoginLogoText           *string `json:"tenant_login_logo_text"`
	TenantLoginTitle              *string `json:"tenant_login_title"`
	TenantLoginSubtitle           *string `json:"tenant_login_subtitle"`
	TrashRetentionDays            *int    `json:"trash_retention_days"`
	TrashCleanupEnabled           *bool   `json:"trash_cleanup_enabled"`
	LifecycleCleanupEnabled       *bool   `json:"lifecycle_cleanup_enabled"`
	VersionRetentionDays          *int    `json:"version_retention_days"`
	VersionCleanupEnabled         *bool   `json:"version_cleanup_enabled"`
	MultipartStaleDays            *int    `json:"multipart_stale_days"`
	MultipartCleanupEnabled       *bool   `json:"multipart_cleanup_enabled"`
}

type publicSettings struct {
	S3Endpoint                    *string `json:"s3_endpoint"`
	RGWAccessKey                  *string `json:"rgw_access_key"`
	RGWSecretKey                  *string `json:"rgw_secret_key"`
	RGWAccessKeyConfigured        bool    `json:"rgw_access_key_configured"`
	RGWSecretKeyConfigured        bool    `json:"rgw_secret_key_configured"`
	DefaultUploadPresignExpires   int     `json:"default_upload_presign_expires"`
	DefaultDownloadPresignExpires int     `json:"default_download_presign_expires"`
	MaxUploadBytes                int64   `json:"max_upload_bytes"`
	AuditEnabled                  bool    `json:"audit_enabled"`
	OfficeURL                     *string `json:"office_url"`
	DownloadCDNURL                *string `json:"download_cdn_url"`
	PreviewCDNURL                 *string `json:"preview_cdn_url"`
	ObjectHTTPDomain              *string `json:"object_http_domain"`
	ObjectHTTPSDomain             *string `json:"object_https_domain"`
	CephMgmtAPIURL                *string `json:"ceph_mgmt_api_url"`
	TenantLoginLogoText           string  `json:"tenant_login_logo_text"`
	TenantLoginTitle              string  `json:"tenant_login_title"`
	TenantLoginSubtitle           string  `json:"tenant_login_subtitle"`
	TrashRetentionDays            int     `json:"trash_retention_days"`
	TrashCleanupEnabled           bool    `json:"trash_cleanup_enabled"`
	LifecycleCleanupEnabled       bool    `json:"lifecycle_cleanup_enabled"`
	VersionRetentionDays          int     `json:"version_retention_days"`
	VersionCleanupEnabled         bool    `json:"version_cleanup_enabled"`
	MultipartStaleDays            int     `json:"multipart_stale_days"`
	MultipartCleanupEnabled       bool    `json:"multipart_cleanup_enabled"`
	UpdatedAt                     *string `json:"updated_at"`
}

type Runtime struct {
	S3Endpoint     string
	RGWAccessKey   string
	RGWSecretKey   string
	CephMgmtAPIURL string
}

func (h *Handler) Runtime(ctx context.Context) (Runtime, error) {
	rows := map[string]row{}
	if h.store != nil {
		var err error
		rows, err = h.store.Load(ctx)
		if err != nil {
			return Runtime{}, err
		}
	}
	return Runtime{
		S3Endpoint:     pick(rows, "s3_endpoint", h.fallbacks.S3Endpoint),
		RGWAccessKey:   pick(rows, "rgw_access_key", h.fallbacks.RGWAccessKey),
		RGWSecretKey:   pick(rows, "rgw_secret_key", h.fallbacks.RGWSecretKey),
		CephMgmtAPIURL: pick(rows, "ceph_mgmt_api_url", h.fallbacks.CephMgmtAPIURL),
	}, nil
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	out, err := h.public(r)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if h.store == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	var req updateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	rows, err := h.store.Load(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	if err := validateUpdate(req, rows); err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	changed := 0
	set := func(key, value string, secret bool) error {
		changed++
		return h.store.Upsert(r.Context(), key, value, secret)
	}
	if req.S3Endpoint != nil {
		if err := set("s3_endpoint", strings.TrimSpace(*req.S3Endpoint), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.RGWAccessKey != nil && strings.TrimSpace(*req.RGWAccessKey) != masked {
		if err := set("rgw_access_key", strings.TrimSpace(*req.RGWAccessKey), true); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.RGWSecretKey != nil && strings.TrimSpace(*req.RGWSecretKey) != masked {
		if err := set("rgw_secret_key", strings.TrimSpace(*req.RGWSecretKey), true); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.DefaultUploadPresignExpires != nil {
		if err := set("default_upload_presign_expires", strconv.Itoa(*req.DefaultUploadPresignExpires), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.DefaultDownloadPresignExpires != nil {
		if err := set("default_download_presign_expires", strconv.Itoa(*req.DefaultDownloadPresignExpires), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.MaxUploadBytes != nil {
		if err := set("max_upload_bytes", strconv.FormatInt(*req.MaxUploadBytes, 10), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.AuditEnabled != nil {
		if err := set("audit_enabled", boolStr(*req.AuditEnabled), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.OfficeURL != nil {
		if err := set("office_url", strings.TrimSpace(*req.OfficeURL), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.DownloadCDNURL != nil {
		if err := set("download_cdn_url", strings.TrimSpace(*req.DownloadCDNURL), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.PreviewCDNURL != nil {
		if err := set("preview_cdn_url", strings.TrimSpace(*req.PreviewCDNURL), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.ObjectHTTPDomain != nil {
		if err := set("object_http_domain", strings.TrimSpace(*req.ObjectHTTPDomain), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.ObjectHTTPSDomain != nil {
		if err := set("object_https_domain", strings.TrimSpace(*req.ObjectHTTPSDomain), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.CephMgmtAPIURL != nil {
		if err := set("ceph_mgmt_api_url", strings.TrimSpace(*req.CephMgmtAPIURL), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.TenantLoginLogoText != nil {
		if err := set("tenant_login_logo_text", strings.TrimSpace(*req.TenantLoginLogoText), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.TenantLoginTitle != nil {
		if err := set("tenant_login_title", strings.TrimSpace(*req.TenantLoginTitle), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.TenantLoginSubtitle != nil {
		if err := set("tenant_login_subtitle", strings.TrimSpace(*req.TenantLoginSubtitle), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.TrashRetentionDays != nil {
		if err := set("trash_retention_days", strconv.Itoa(*req.TrashRetentionDays), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.TrashCleanupEnabled != nil {
		if err := set("trash_cleanup_enabled", boolStr(*req.TrashCleanupEnabled), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.LifecycleCleanupEnabled != nil {
		if err := set("lifecycle_cleanup_enabled", boolStr(*req.LifecycleCleanupEnabled), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.VersionRetentionDays != nil {
		if err := set("version_retention_days", strconv.Itoa(*req.VersionRetentionDays), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.VersionCleanupEnabled != nil {
		if err := set("version_cleanup_enabled", boolStr(*req.VersionCleanupEnabled), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.MultipartStaleDays != nil {
		if err := set("multipart_stale_days", strconv.Itoa(*req.MultipartStaleDays), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if req.MultipartCleanupEnabled != nil {
		if err := set("multipart_cleanup_enabled", boolStr(*req.MultipartCleanupEnabled), false); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "database error")
			return
		}
	}
	if changed == 0 {
		httpx.Error(w, http.StatusBadRequest, "No fields to update")
		return
	}
	out, err := h.public(r)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) public(r *http.Request) (publicSettings, error) {
	rows := map[string]row{}
	if h.store != nil {
		var err error
		rows, err = h.store.Load(r.Context())
		if err != nil {
			return publicSettings{}, err
		}
	}
	ak := pick(rows, "rgw_access_key", h.fallbacks.RGWAccessKey)
	sk := pick(rows, "rgw_secret_key", h.fallbacks.RGWSecretKey)
	var updated *string
	if t := latest(rows); t != nil {
		s := t.UTC().Format(time.RFC3339)
		updated = &s
	}
	return publicSettings{
		S3Endpoint:                    pickPtr(rows, "s3_endpoint", h.fallbacks.S3Endpoint),
		RGWAccessKey:                  maskAccess(ak),
		RGWSecretKey:                  maskSecret(sk),
		RGWAccessKeyConfigured:        ak != "",
		RGWSecretKeyConfigured:        sk != "",
		DefaultUploadPresignExpires:   pickInt(rows, "default_upload_presign_expires", defaultUploadExpires),
		DefaultDownloadPresignExpires: pickInt(rows, "default_download_presign_expires", defaultDownloadExpires),
		MaxUploadBytes:                pickInt64(rows, "max_upload_bytes", defaultMaxUploadBytes),
		AuditEnabled:                  pickBool(rows, "audit_enabled", true),
		OfficeURL:                     pickPtr(rows, "office_url", h.fallbacks.OfficeURL),
		DownloadCDNURL:                pickPtr(rows, "download_cdn_url", h.fallbacks.DownloadCDNURL),
		PreviewCDNURL:                 pickPtr(rows, "preview_cdn_url", h.fallbacks.PreviewCDNURL),
		ObjectHTTPDomain:              pickPtr(rows, "object_http_domain", h.fallbacks.ObjectHTTPDomain),
		ObjectHTTPSDomain:             pickPtr(rows, "object_https_domain", h.fallbacks.ObjectHTTPSDomain),
		CephMgmtAPIURL:                pickPtr(rows, "ceph_mgmt_api_url", h.fallbacks.CephMgmtAPIURL),
		TenantLoginLogoText:           pick(rows, "tenant_login_logo_text", defaultLogo),
		TenantLoginTitle:              pick(rows, "tenant_login_title", defaultTitle),
		TenantLoginSubtitle:           pick(rows, "tenant_login_subtitle", defaultSubtitle),
		TrashRetentionDays:            pickInt(rows, "trash_retention_days", 0),
		TrashCleanupEnabled:           pickBool(rows, "trash_cleanup_enabled", false),
		LifecycleCleanupEnabled:       pickBool(rows, "lifecycle_cleanup_enabled", true),
		VersionRetentionDays:          pickInt(rows, "version_retention_days", 0),
		VersionCleanupEnabled:         pickBool(rows, "version_cleanup_enabled", false),
		MultipartStaleDays:            pickInt(rows, "multipart_stale_days", defaultMultipartStale),
		MultipartCleanupEnabled:       pickBool(rows, "multipart_cleanup_enabled", false),
		UpdatedAt:                     updated,
	}, nil
}

func validateUpdate(req updateRequest, rows map[string]row) error {
	if req.DefaultUploadPresignExpires != nil && (*req.DefaultUploadPresignExpires < 600 || *req.DefaultUploadPresignExpires > 1800) {
		return errors.New("default_upload_presign_expires must be between 600 and 1800")
	}
	if req.DefaultDownloadPresignExpires != nil && (*req.DefaultDownloadPresignExpires < 300 || *req.DefaultDownloadPresignExpires > 600) {
		return errors.New("default_download_presign_expires must be between 300 and 600")
	}
	if req.MaxUploadBytes != nil && *req.MaxUploadBytes <= 0 {
		return errors.New("max_upload_bytes must be positive")
	}
	if req.TenantLoginLogoText != nil && len([]rune(strings.TrimSpace(*req.TenantLoginLogoText))) > 8 {
		return errors.New("tenant_login_logo_text must be at most 8 characters")
	}
	trashDays := pickInt(rows, "trash_retention_days", 0)
	if req.TrashRetentionDays != nil {
		if *req.TrashRetentionDays < 0 || *req.TrashRetentionDays > 3650 {
			return errors.New("trash_retention_days must be between 0 and 3650")
		}
		trashDays = *req.TrashRetentionDays
	}
	trashOn := pickBool(rows, "trash_cleanup_enabled", false)
	if req.TrashCleanupEnabled != nil {
		trashOn = *req.TrashCleanupEnabled
	}
	if trashOn && trashDays < 1 {
		return errors.New("trash_retention_days must be at least 1 when trash cleanup is enabled")
	}
	verDays := pickInt(rows, "version_retention_days", 0)
	if req.VersionRetentionDays != nil {
		if *req.VersionRetentionDays < 0 || *req.VersionRetentionDays > 3650 {
			return errors.New("version_retention_days must be between 0 and 3650")
		}
		verDays = *req.VersionRetentionDays
	}
	verOn := pickBool(rows, "version_cleanup_enabled", false)
	if req.VersionCleanupEnabled != nil {
		verOn = *req.VersionCleanupEnabled
	}
	if verOn && verDays < 1 {
		return errors.New("version_retention_days must be at least 1 when version cleanup is enabled")
	}
	mpDays := pickInt(rows, "multipart_stale_days", defaultMultipartStale)
	if req.MultipartStaleDays != nil {
		if *req.MultipartStaleDays < 0 || *req.MultipartStaleDays > 3650 {
			return errors.New("multipart_stale_days must be between 0 and 3650")
		}
		mpDays = *req.MultipartStaleDays
	}
	mpOn := pickBool(rows, "multipart_cleanup_enabled", false)
	if req.MultipartCleanupEnabled != nil {
		mpOn = *req.MultipartCleanupEnabled
	}
	if mpOn && mpDays < 1 {
		return errors.New("multipart_stale_days must be at least 1 when multipart cleanup is enabled")
	}
	return nil
}

func maskAccess(v string) *string {
	if v == "" {
		return nil
	}
	if len(v) <= 4 {
		s := masked
		return &s
	}
	s := strings.Repeat("*", len(v)-4) + v[len(v)-4:]
	return &s
}

func maskSecret(v string) *string {
	if v == "" {
		return nil
	}
	s := masked
	return &s
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
