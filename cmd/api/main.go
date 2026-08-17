package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cyxc1124/osspilot-ops-api/internal/access"
	"github.com/cyxc1124/osspilot-ops-api/internal/accounts"
	"github.com/cyxc1124/osspilot-ops-api/internal/alerts"
	"github.com/cyxc1124/osspilot-ops-api/internal/audit"
	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/buckets"
	"github.com/cyxc1124/osspilot-ops-api/internal/ceph"
	"github.com/cyxc1124/osspilot-ops-api/internal/config"
	"github.com/cyxc1124/osspilot-ops-api/internal/filelocks"
	"github.com/cyxc1124/osspilot-ops-api/internal/grants"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
	"github.com/cyxc1124/osspilot-ops-api/internal/lifecycle"
	"github.com/cyxc1124/osspilot-ops-api/internal/project"
	"github.com/cyxc1124/osspilot-ops-api/internal/regions"
	"github.com/cyxc1124/osspilot-ops-api/internal/settings"
	"github.com/cyxc1124/osspilot-ops-api/internal/stats"
	"github.com/cyxc1124/osspilot-ops-api/internal/tenantrbac"
	"github.com/cyxc1124/osspilot-ops-api/internal/users"
)

type apiHandlers struct {
	auth       *auth.Handler
	users      *users.Handler
	regions    *regions.Handler
	settings   *settings.Handler
	accounts   *accounts.Handler
	buckets    *buckets.Handler
	grants     *grants.Handler
	lifecycle  *lifecycle.Handler
	ceph       *ceph.Handler
	audit      *audit.Handler
	stats      *stats.Handler
	alerts     *alerts.Handler
	access     *access.Handler
	tenantrbac *tenantrbac.Handler
	filelocks  *filelocks.Handler
}

func newMux(h apiHandlers) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	if h.auth != nil {
		h.auth.Register(mux)
	}
	if h.users != nil {
		h.users.Register(mux)
	}
	if h.regions != nil {
		h.regions.Register(mux)
	}
	if h.settings != nil {
		h.settings.Register(mux)
	}
	if h.accounts != nil {
		h.accounts.Register(mux)
	}
	if h.buckets != nil {
		h.buckets.Register(mux)
	}
	if h.grants != nil {
		h.grants.Register(mux)
	}
	if h.lifecycle != nil {
		h.lifecycle.Register(mux)
	}
	if h.ceph != nil {
		h.ceph.Register(mux)
	}
	if h.audit != nil {
		h.audit.Register(mux)
	}
	if h.stats != nil {
		h.stats.Register(mux)
	}
	if h.alerts != nil {
		h.alerts.Register(mux)
	}
	if h.access != nil {
		h.access.Register(mux)
	}
	if h.tenantrbac != nil {
		h.tenantrbac.Register(mux)
	}
	if h.filelocks != nil {
		h.filelocks.Register(mux)
	}
	return httpx.CORS(mux)
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func main() {
	cfg := config.Load()
	if cfg.DefaultJWTUsed {
		slog.Warn("JWT_SECRET unset; using development default")
	}

	proj := project.New(cfg.TenantAPIURL, cfg.ProjectionSecret)
	if proj == nil {
		slog.Warn("TENANT_API_URL/PROJECTION_SECRET unset; tenant projection skipped")
	}

	var authStore *auth.Store
	var userStore *users.Store
	var regionStore *regions.Store
	var settingsStore *settings.Store
	var accountStore *accounts.Store
	var bucketStore *buckets.Store
	var grantStore *grants.Store
	var lifeStore *lifecycle.Store
	var auditStore *audit.Store
	var statsStore *stats.Store
	var alertStore *alerts.Store
	if cfg.DatabaseURL != "" {
		pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
		if err != nil {
			slog.Error("db pool", "err", err)
			os.Exit(1)
		}
		defer pool.Close()
		authStore = auth.NewStore(pool)
		userStore = users.NewStore(pool)
		regionStore = regions.NewStore(pool)
		settingsStore = settings.NewStore(pool)
		accountStore = accounts.NewStore(pool)
		bucketStore = buckets.NewStore(pool)
		grantStore = grants.NewStore(pool)
		lifeStore = lifecycle.NewStore(pool)
		auditStore = audit.NewStore(pool)
		statsStore = stats.NewStore(pool)
		alertStore = alerts.NewStore(pool)
	} else {
		slog.Warn("DATABASE_URL unset; auth routes return 503")
	}

	authH := auth.NewHandler(authStore, cfg.JWTSecret, cfg.TokenTTL)
	usersH := users.NewHandler(userStore, auditStore, authH.RequireAdmin)
	regionH := regions.NewHandler(regionStore, authH.RequireUser, authH.RequireAdmin, auditStore)
	settingsH := settings.NewHandler(settingsStore, regionStore, authH.RequireUser, authH.RequireAdmin, settings.Fallbacks{
		S3Endpoint:        cfg.S3Endpoint,
		RGWAccessKey:      cfg.RGWAccessKey,
		RGWSecretKey:      cfg.RGWSecretKey,
		DownloadCDNURL:    cfg.DownloadCDNURL,
		PreviewCDNURL:     cfg.PreviewCDNURL,
		ObjectHTTPDomain:  cfg.ObjectHTTPDomain,
		ObjectHTTPSDomain: cfg.ObjectHTTPSDomain,
		OfficeURL:         cfg.OfficeURL,
		CephMgmtAPIURL:    cfg.CephMgmtAPIURL,
	}, proj, auditStore)
	accountsH := accounts.NewHandler(accountStore, regionStore, proj, auditStore, authH.RequireAdmin)
	bucketsH := buckets.NewHandler(bucketStore, regionStore, settingsH, proj, auditStore, authH.RequireAdmin)
	grantsH := grants.NewHandler(grantStore, accountStore, bucketStore, proj, authH.RequireAdmin)
	lifeH := lifecycle.NewHandler(lifeStore, bucketStore, auditStore, authH.RequireAdmin)
	cephH := ceph.NewHandler(settingsH, authH.RequireUser, authH.RequireAdmin, auditStore)
	auditH := audit.NewHandler(auditStore, authH.RequireUser, proj, accountStore)
	statsH := stats.NewHandler(statsStore, grantStore, settingsH, proj, authH.RequireUser)
	alertH := alerts.NewHandler(alertStore, statsStore, bucketStore, grantStore, proj, settingsH, authH.RequireUser, authH.RequireAdmin)
	accessH := access.NewHandler(accountStore, proj, auditStore, authH.RequireAdmin)
	rbacH := tenantrbac.NewHandler(accountStore, proj, auditStore, authH.RequireAdmin)
	locksH := filelocks.NewHandler(proj, auditStore, authH.RequireAdmin)
	addr := cfg.HTTPAddr
	slog.Info("listen", "addr", addr)
	if err := http.ListenAndServe(addr, newMux(apiHandlers{
		auth: authH, users: usersH, regions: regionH, settings: settingsH,
		accounts: accountsH, buckets: bucketsH, grants: grantsH, lifecycle: lifeH, ceph: cephH,
		audit: auditH, stats: statsH, alerts: alertH, access: accessH, tenantrbac: rbacH, filelocks: locksH,
	})); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}
