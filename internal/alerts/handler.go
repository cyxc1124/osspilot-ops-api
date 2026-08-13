package alerts

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
)

type Handler struct {
	store *Store
	read  func(auth.UserHandler) http.HandlerFunc
	write func(auth.UserHandler) http.HandlerFunc
}

func NewHandler(store *Store, read, write func(auth.UserHandler) http.HandlerFunc) *Handler {
	return &Handler{store: store, read: read, write: write}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/alerts/rules", h.read(h.listRules))
	mux.HandleFunc("POST /api/alerts/rules", h.write(h.createRule))
	mux.HandleFunc("GET /api/alerts/rules/{rule_id}", h.read(h.getRule))
	mux.HandleFunc("PUT /api/alerts/rules/{rule_id}", h.write(h.updateRule))
	mux.HandleFunc("DELETE /api/alerts/rules/{rule_id}", h.write(h.deleteRule))
	mux.HandleFunc("GET /api/alerts/channels", h.read(h.listChannels))
	mux.HandleFunc("POST /api/alerts/channels", h.write(h.createChannel))
	mux.HandleFunc("PUT /api/alerts/channels/{channel_id}", h.write(h.updateChannel))
	mux.HandleFunc("DELETE /api/alerts/channels/{channel_id}", h.write(h.deleteChannel))
	mux.HandleFunc("GET /api/alerts/events", h.read(h.listEvents))
	mux.HandleFunc("GET /api/alerts/events/recent", h.read(h.recent))
	mux.HandleFunc("POST /api/alerts/events/{event_id}/acknowledge", h.read(h.ack))
	mux.HandleFunc("POST /api/alerts/events/{event_id}/resolve", h.read(h.resolve))
	mux.HandleFunc("POST /api/alerts/evaluate", h.write(h.evaluate))
}

func (h *Handler) ready(w http.ResponseWriter) bool {
	if h.store == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "database is not configured")
		return false
	}
	return true
}

type ruleReq struct {
	Name         string          `json:"name"`
	RuleType     string          `json:"rule_type"`
	Enabled      *bool           `json:"enabled"`
	Severity     string          `json:"severity"`
	Config       json.RawMessage `json:"config"`
	ChannelIDs   []int64         `json:"channel_ids"`
	NotifyTenant *bool           `json:"notify_tenant"`
	Description  *string         `json:"description"`
}

type channelReq struct {
	Name        string          `json:"name"`
	ChannelType string          `json:"channel_type"`
	Enabled     *bool           `json:"enabled"`
	Config      json.RawMessage `json:"config"`
}

func (h *Handler) listRules(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if !h.ready(w) {
		return
	}
	items, err := h.store.ListRules(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, ruleJSON(item))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) getRule(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	item := h.loadRule(w, r)
	if item == nil {
		return
	}
	httpx.JSON(w, http.StatusOK, ruleJSON(*item))
}

func (h *Handler) createRule(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if !h.ready(w) {
		return
	}
	var req ruleReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 128 {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := validRuleType(req.RuleType); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Severity == "" {
		req.Severity = "warning"
	}
	if err := validSeverity(req.Severity); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	enabled, notify := true, false
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if req.NotifyTenant != nil {
		notify = *req.NotifyTenant
	}
	ids, _ := json.Marshal(req.ChannelIDs)
	if req.ChannelIDs == nil {
		ids = []byte("[]")
	}
	item, err := h.store.InsertRule(r.Context(), Rule{
		Name: req.Name, RuleType: req.RuleType, Enabled: enabled, Severity: req.Severity,
		Config: req.Config, ChannelIDs: ids, NotifyTenant: notify, Description: req.Description,
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	httpx.JSON(w, http.StatusCreated, ruleJSON(*item))
}

func (h *Handler) updateRule(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	item := h.loadRule(w, r)
	if item == nil {
		return
	}
	var req ruleReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name != "" {
		if len(req.Name) > 128 {
			httpx.Error(w, http.StatusBadRequest, "name is required")
			return
		}
		item.Name = strings.TrimSpace(req.Name)
	}
	if req.Enabled != nil {
		item.Enabled = *req.Enabled
	}
	if req.Severity != "" {
		if err := validSeverity(req.Severity); err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		item.Severity = req.Severity
	}
	if len(req.Config) > 0 {
		item.Config = req.Config
	}
	if req.ChannelIDs != nil {
		ids, _ := json.Marshal(req.ChannelIDs)
		item.ChannelIDs = ids
	}
	if req.NotifyTenant != nil {
		item.NotifyTenant = *req.NotifyTenant
	}
	if req.Description != nil {
		item.Description = req.Description
	}
	out, err := h.store.UpdateRule(r.Context(), item)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	httpx.JSON(w, http.StatusOK, ruleJSON(*out))
}

func (h *Handler) deleteRule(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if !h.ready(w) {
		return
	}
	id, ok := pathID(w, r, "rule_id")
	if !ok {
		return
	}
	okDel, err := h.store.DeleteRule(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	if !okDel {
		httpx.Error(w, http.StatusNotFound, "Alert rule not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listChannels(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if !h.ready(w) {
		return
	}
	items, err := h.store.ListChannels(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, channelJSON(item))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) createChannel(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if !h.ready(w) {
		return
	}
	var req channelReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 128 {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := validChannel(req.ChannelType, asObject(req.Config)); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	item, err := h.store.InsertChannel(r.Context(), Channel{Name: req.Name, ChannelType: req.ChannelType, Enabled: enabled, Config: req.Config})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	httpx.JSON(w, http.StatusCreated, channelJSON(*item))
}

func (h *Handler) updateChannel(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if !h.ready(w) {
		return
	}
	id, ok := pathID(w, r, "channel_id")
	if !ok {
		return
	}
	item, err := h.store.GetChannel(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	if item == nil {
		httpx.Error(w, http.StatusNotFound, "Notification channel not found")
		return
	}
	var req channelReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name != "" {
		item.Name = strings.TrimSpace(req.Name)
	}
	if req.Enabled != nil {
		item.Enabled = *req.Enabled
	}
	if len(req.Config) > 0 {
		if err := validChannel(item.ChannelType, asObject(req.Config)); err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		item.Config = req.Config
	}
	out, err := h.store.UpdateChannel(r.Context(), item)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	httpx.JSON(w, http.StatusOK, channelJSON(*out))
}

func (h *Handler) deleteChannel(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if !h.ready(w) {
		return
	}
	id, ok := pathID(w, r, "channel_id")
	if !ok {
		return
	}
	okDel, err := h.store.DeleteChannel(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	if !okDel {
		httpx.Error(w, http.StatusNotFound, "Notification channel not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listEvents(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if !h.ready(w) {
		return
	}
	page, pageSize := 1, 20
	if raw := r.URL.Query().Get("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			httpx.Error(w, http.StatusBadRequest, "page must be >= 1")
			return
		}
		page = n
	}
	if raw := r.URL.Query().Get("page_size"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			httpx.Error(w, http.StatusBadRequest, "page_size must be 1-100")
			return
		}
		pageSize = n
	}
	var tenantID *int64
	if raw := r.URL.Query().Get("tenant_id"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1 {
			httpx.Error(w, http.StatusBadRequest, "tenant_id must be a positive integer")
			return
		}
		tenantID = &n
	}
	items, total, err := h.store.ListEvents(r.Context(), r.URL.Query().Get("status"), tenantID, page, pageSize)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, eventJSON(item))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out, "total": total})
}

func (h *Handler) recent(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if !h.ready(w) {
		return
	}
	limit := 5
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 20 {
			httpx.Error(w, http.StatusBadRequest, "limit must be 1-20")
			return
		}
		limit = n
	}
	items, err := h.store.RecentEvents(r.Context(), limit)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, eventJSON(item))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out, "total": len(out)})
}

func (h *Handler) ack(w http.ResponseWriter, r *http.Request, user *auth.User) {
	ev := h.loadEvent(w, r)
	if ev == nil {
		return
	}
	if ev.Status == "resolved" {
		httpx.Error(w, http.StatusBadRequest, "Alert already resolved")
		return
	}
	out, err := h.store.Acknowledge(r.Context(), ev.ID, user.ID)
	if err != nil || out == nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	httpx.JSON(w, http.StatusOK, eventJSON(*out))
}

func (h *Handler) resolve(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ev := h.loadEvent(w, r)
	if ev == nil {
		return
	}
	out, err := h.store.Resolve(r.Context(), ev.ID)
	if err != nil || out == nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	httpx.JSON(w, http.StatusOK, eventJSON(*out))
}

func (h *Handler) evaluate(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	if !h.ready(w) {
		return
	}
	// ponytail: no usage/RGW series yet; returns enabled-rule count. Wire thresholds when stats land.
	n, err := h.store.CountEnabled(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"evaluated_rules": n, "new_events": 0, "resolved_events": 0})
}

func (h *Handler) loadRule(w http.ResponseWriter, r *http.Request) *Rule {
	if !h.ready(w) {
		return nil
	}
	id, ok := pathID(w, r, "rule_id")
	if !ok {
		return nil
	}
	item, err := h.store.GetRule(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return nil
	}
	if item == nil {
		httpx.Error(w, http.StatusNotFound, "Alert rule not found")
		return nil
	}
	return item
}

func (h *Handler) loadEvent(w http.ResponseWriter, r *http.Request) *Event {
	if !h.ready(w) {
		return nil
	}
	id, ok := pathID(w, r, "event_id")
	if !ok {
		return nil
	}
	item, err := h.store.GetEvent(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "database error")
		return nil
	}
	if item == nil {
		httpx.Error(w, http.StatusNotFound, "Alert event not found")
		return nil
	}
	return item
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	n, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || n < 1 {
		httpx.Error(w, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return n, true
}

func ruleJSON(r Rule) map[string]any {
	cfg := asObject(r.Config)
	var ids []int64
	if json.Unmarshal(r.ChannelIDs, &ids) != nil {
		ids = []int64{}
	}
	return map[string]any{
		"id": r.ID, "name": r.Name, "rule_type": r.RuleType, "enabled": r.Enabled, "severity": r.Severity,
		"config": cfg, "channel_ids": ids, "notify_tenant": r.NotifyTenant, "description": r.Description,
		"created_at": rfc(r.CreatedAt), "updated_at": rfc(r.UpdatedAt),
	}
}

func channelJSON(c Channel) map[string]any {
	return map[string]any{
		"id": c.ID, "name": c.Name, "channel_type": c.ChannelType, "enabled": c.Enabled,
		"config": asObject(c.Config), "created_at": rfc(c.CreatedAt), "updated_at": rfc(c.UpdatedAt),
	}
}

func eventJSON(e Event) map[string]any {
	return map[string]any{
		"id": e.ID, "rule_id": e.RuleID, "rule_type": e.RuleType, "severity": e.Severity, "status": e.Status,
		"title": e.Title, "message": e.Message, "tenant_id": e.TenantID, "bucket_id": e.BucketID, "bucket_name": e.BucketName,
		"details": asObject(e.Details), "notify_tenant": e.NotifyTenant, "fired_at": rfc(e.FiredAt),
		"acknowledged_at": rfcp(e.AcknowledgedAt), "acknowledged_by": e.AcknowledgedBy, "resolved_at": rfcp(e.ResolvedAt),
		"created_at": rfc(e.CreatedAt),
	}
}

func rfc(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func rfcp(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}
