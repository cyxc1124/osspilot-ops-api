package audit

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
)

type Handler struct {
	store   *Store
	protect func(auth.UserHandler) http.HandlerFunc
}

func NewHandler(store *Store, protect func(auth.UserHandler) http.HandlerFunc) *Handler {
	return &Handler{store: store, protect: protect}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/audit-logs", h.protect(h.list))
	mux.HandleFunc("GET /api/audit-logs/export", h.protect(h.export))
}

func (h *Handler) ready(w http.ResponseWriter) bool {
	if h.store == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return false
	}
	return true
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if !h.ready(w) {
		return
	}
	f, page, pageSize, ok := parseFilter(w, r)
	if !ok {
		return
	}
	items, total, err := h.store.List(r.Context(), f, page, pageSize)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, e := range items {
		out = append(out, entryJSON(e))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) export(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if !h.ready(w) {
		return
	}
	f, _, _, ok := parseFilter(w, r)
	if !ok {
		return
	}
	items, err := h.store.Export(r.Context(), f)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="audit-logs.csv"`)
	_ = writeCSV(w, items)
}

func parseFilter(w http.ResponseWriter, r *http.Request) (Filter, int, int, bool) {
	q := r.URL.Query()
	f := Filter{
		TenantName: q.Get("tenant_name"), Username: q.Get("username"), BucketName: q.Get("bucket_name"),
		ObjectKey: q.Get("object_key"), Action: q.Get("action"), Status: q.Get("status"),
		SourceIP: q.Get("source_ip"), Keyword: q.Get("keyword"),
	}
	id, ok := optInt64(w, q.Get("tenant_id"), "tenant_id")
	if !ok {
		return Filter{}, 0, 0, false
	}
	f.TenantID = id
	id, ok = optInt64(w, q.Get("user_id"), "user_id")
	if !ok {
		return Filter{}, 0, 0, false
	}
	f.UserID = id
	switch q.Get("admin_only") {
	case "", "false", "0":
	case "true", "1":
		f.AdminOnly = true
	default:
		httpx.Error(w, http.StatusBadRequest, "admin_only must be a boolean")
		return Filter{}, 0, 0, false
	}
	from, ok := optTime(w, q.Get("created_from"), "created_from")
	if !ok {
		return Filter{}, 0, 0, false
	}
	to, ok := optTime(w, q.Get("created_to"), "created_to")
	if !ok {
		return Filter{}, 0, 0, false
	}
	f.From, f.To = from, to
	page, pageSize := 1, 20
	if raw := q.Get("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			httpx.Error(w, http.StatusBadRequest, "page must be >= 1")
			return Filter{}, 0, 0, false
		}
		page = n
	}
	if raw := q.Get("page_size"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			httpx.Error(w, http.StatusBadRequest, "page_size must be 1-100")
			return Filter{}, 0, 0, false
		}
		pageSize = n
	}
	return f, page, pageSize, true
}

func optInt64(w http.ResponseWriter, raw, name string) (*int64, bool) {
	if strings.TrimSpace(raw) == "" {
		return nil, true
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, name+" must be an integer")
		return nil, false
	}
	return &n, true
}

func optTime(w http.ResponseWriter, raw, name string) (*time.Time, bool) {
	if raw == "" {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, name+" must be RFC3339")
		return nil, false
	}
	return &t, true
}

func entryJSON(e Entry) map[string]any {
	return map[string]any{
		"id": e.ID, "user_id": e.UserID, "username": e.Username, "tenant_id": e.TenantID, "tenant_name": e.TenantName,
		"bucket_name": e.BucketName, "object_key": e.ObjectKey, "action": e.Action, "source_ip": e.SourceIP,
		"user_agent": e.UserAgent, "status": e.Status, "error_message": e.ErrorMessage,
		"created_at": e.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func writeCSV(w io.Writer, items []Entry) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"id", "user_id", "username", "tenant_id", "tenant_name", "bucket_name", "object_key", "action", "source_ip", "user_agent", "status", "error_message", "created_at"}); err != nil {
		return err
	}
	for _, e := range items {
		if err := cw.Write([]string{
			strconv.FormatInt(e.ID, 10), intp(e.UserID), strp(e.Username), intp(e.TenantID), strp(e.TenantName),
			strp(e.BucketName), strp(e.ObjectKey), e.Action, strp(e.SourceIP), strp(e.UserAgent), e.Status, strp(e.ErrorMessage),
			e.CreatedAt.UTC().Format(time.RFC3339),
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func strp(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func intp(n *int64) string {
	if n == nil {
		return ""
	}
	return strconv.FormatInt(*n, 10)
}

func ClientIP(r *http.Request) *string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		s := strings.TrimSpace(strings.Split(fwd, ",")[0])
		return &s
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return nil
	}
	return &host
}

func UserAgent(r *http.Request) *string {
	if v := strings.TrimSpace(r.Header.Get("User-Agent")); v != "" {
		return &v
	}
	return nil
}

func Encode(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
