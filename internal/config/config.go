package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr          string
	DatabaseURL       string
	JWTSecret         string
	TokenTTL          time.Duration
	DefaultJWTUsed    bool
	S3Endpoint        string
	RGWAccessKey      string
	RGWSecretKey      string
	DownloadCDNURL    string
	PreviewCDNURL     string
	ObjectHTTPDomain  string
	ObjectHTTPSDomain string
	OfficeURL         string
	CephMgmtAPIURL    string
	TenantAPIURL      string
	ProjectionSecret  string
}

func Load() Config {
	secret := getenv("JWT_SECRET", "change-me-in-development")
	minutes := getenvInt("JWT_ACCESS_TOKEN_EXPIRE_MINUTES", 60)
	return Config{
		HTTPAddr:          getenv("HTTP_ADDR", ":8001"),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		JWTSecret:         secret,
		TokenTTL:          time.Duration(minutes) * time.Minute,
		DefaultJWTUsed:    os.Getenv("JWT_SECRET") == "",
		S3Endpoint:        os.Getenv("S3_ENDPOINT"),
		RGWAccessKey:      os.Getenv("RGW_ACCESS_KEY"),
		RGWSecretKey:      os.Getenv("RGW_SECRET_KEY"),
		DownloadCDNURL:    os.Getenv("DOWNLOAD_CDN_URL"),
		PreviewCDNURL:     os.Getenv("PREVIEW_CDN_URL"),
		ObjectHTTPDomain:  os.Getenv("OBJECT_HTTP_DOMAIN"),
		ObjectHTTPSDomain: os.Getenv("OBJECT_HTTPS_DOMAIN"),
		OfficeURL:         os.Getenv("OFFICE_URL"),
		CephMgmtAPIURL:    os.Getenv("CEPH_MGMT_API_URL"),
		TenantAPIURL:      os.Getenv("TENANT_API_URL"),
		ProjectionSecret:  os.Getenv("PROJECTION_SECRET"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}
