package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cyxc1124/osspilot-ops-api/internal/accounts"
	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/buckets"
	"github.com/cyxc1124/osspilot-ops-api/internal/config"
	"github.com/cyxc1124/osspilot-ops-api/internal/grants"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
	"github.com/cyxc1124/osspilot-ops-api/internal/project"
	"github.com/cyxc1124/osspilot-ops-api/internal/regions"
	"github.com/cyxc1124/osspilot-ops-api/internal/settings"
	"github.com/cyxc1124/osspilot-ops-api/internal/users"
)

func newMux(authH *auth.Handler, usersH *users.Handler, regionH *regions.Handler, settingsH *settings.Handler, accountsH *accounts.Handler, bucketsH *buckets.Handler, grantsH *grants.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	if authH != nil {
		authH.Register(mux)
	}
	if usersH != nil {
		usersH.Register(mux)
	}
	if regionH != nil {
		regionH.Register(mux)
	}
	if settingsH != nil {
		settingsH.Register(mux)
	}
	if accountsH != nil {
		accountsH.Register(mux)
	}
	if bucketsH != nil {
		bucketsH.Register(mux)
	}
	if grantsH != nil {
		grantsH.Register(mux)
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
	} else {
		slog.Warn("DATABASE_URL unset; auth routes return 503")
	}

	authH := auth.NewHandler(authStore, cfg.JWTSecret, cfg.TokenTTL)
	usersH := users.NewHandler(userStore, authH.RequireAdmin)
	regionH := regions.NewHandler(regionStore, authH.RequireUser, authH.RequireAdmin)
	settingsH := settings.NewHandler(settingsStore, authH.RequireUser, authH.RequireAdmin, settings.Fallbacks{
		S3Endpoint:        cfg.S3Endpoint,
		RGWAccessKey:      cfg.RGWAccessKey,
		RGWSecretKey:      cfg.RGWSecretKey,
		DownloadCDNURL:    cfg.DownloadCDNURL,
		PreviewCDNURL:     cfg.PreviewCDNURL,
		ObjectHTTPDomain:  cfg.ObjectHTTPDomain,
		ObjectHTTPSDomain: cfg.ObjectHTTPSDomain,
		OfficeURL:         cfg.OfficeURL,
		CephMgmtAPIURL:    cfg.CephMgmtAPIURL,
	})
	accountsH := accounts.NewHandler(accountStore, regionStore, proj, authH.RequireAdmin)
	bucketsH := buckets.NewHandler(bucketStore, regionStore, authH.RequireAdmin)
	grantsH := grants.NewHandler(grantStore, accountStore, bucketStore, proj, authH.RequireAdmin)
	addr := cfg.HTTPAddr
	slog.Info("listen", "addr", addr)
	if err := http.ListenAndServe(addr, newMux(authH, usersH, regionH, settingsH, accountsH, bucketsH, grantsH)); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}
