package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Delivery struct {
	ChannelID   int64  `json:"channel_id"`
	ChannelType string `json:"channel_type"`
	Status      string `json:"status"`
	HTTPStatus  *int   `json:"http_status,omitempty"`
	Recipients  any    `json:"recipients,omitempty"`
	Error       string `json:"error,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

func (e *Evaluator) dispatch(ctx context.Context, ev *Event, rule Rule) {
	if ev == nil || e.store == nil {
		return
	}
	ids := channelIDs(rule.ChannelIDs)
	if len(ids) == 0 {
		return
	}
	chans, err := e.store.ListEnabledChannels(ctx, ids)
	if err != nil {
		slog.Warn("alert channels", "err", err)
		return
	}
	payload := alertPayload(*ev)
	results := make([]Delivery, 0, len(chans))
	for _, ch := range chans {
		results = append(results, sendChannel(ctx, e.client(), ch, payload))
	}
	details := asObject(ev.Details)
	details["notifications"] = results
	raw, err := json.Marshal(details)
	if err != nil {
		return
	}
	if err := e.store.UpdateEventDetails(ctx, ev.ID, raw); err != nil {
		slog.Warn("alert delivery details", "err", err)
	}
}

func (e *Evaluator) client() *http.Client {
	if e.http != nil {
		return e.http
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func alertPayload(ev Event) map[string]any {
	var tenant, bucket any
	if ev.TenantID != nil {
		tenant = *ev.TenantID
	}
	if ev.BucketName != nil {
		bucket = *ev.BucketName
	}
	status := ev.Status
	if status == "" {
		status = "firing"
	}
	return map[string]any{
		"alert": ev.Title, "severity": ev.Severity, "status": status, "message": ev.Message,
		"rule_type": ev.RuleType, "tenant_id": tenant, "bucket_name": bucket,
		"fired_at": ev.FiredAt.UTC().Format(time.RFC3339),
	}
}

func channelIDs(raw json.RawMessage) []int64 {
	var ids []int64
	if json.Unmarshal(nzArray(raw), &ids) == nil {
		return ids
	}
	return nil
}

func sendChannel(ctx context.Context, cli *http.Client, ch Channel, payload map[string]any) Delivery {
	d := Delivery{ChannelID: ch.ID, ChannelType: ch.ChannelType}
	cfg := asObject(ch.Config)
	url, body, extra, done := prepareSend(ch.ChannelType, cfg, payload)
	if extra.Status != "" {
		d.Status, d.Recipients, d.Error, d.Reason = extra.Status, extra.Recipients, extra.Error, extra.Reason
	}
	if done {
		if d.Status == "queued" {
			slog.Info("alert_email_notification", "recipients", d.Recipients, "alert", payload["alert"])
		}
		return d
	}
	code, err := postJSON(ctx, cli, url, body)
	if err != nil {
		slog.Warn("alert_notification_failed", "channel_id", ch.ID, "error", err.Error())
		d.Status, d.Error = "failed", err.Error()
		return d
	}
	d.Status, d.HTTPStatus = "sent", &code
	return d
}

func prepareSend(channelType string, cfg, payload map[string]any) (url string, body any, extra Delivery, done bool) {
	switch channelType {
	case "email":
		extra.Status = "queued"
		if v, ok := cfg["recipients"]; ok {
			extra.Recipients = v
		} else {
			extra.Recipients = []any{}
		}
		return "", nil, extra, true
	case "webhook":
		url = configString(cfg, "url")
		if url == "" {
			extra.Status, extra.Error = "failed", "missing url"
			return "", nil, extra, true
		}
		return url, payload, extra, false
	case "wecom":
		url = configString(cfg, "webhook_url")
		if url == "" {
			extra.Status, extra.Error = "failed", "missing webhook_url"
			return "", nil, extra, true
		}
		return url, map[string]any{
			"msgtype": "text",
			"text":    map[string]any{"content": alertText(payload)},
		}, extra, false
	case "feishu":
		url = configString(cfg, "webhook_url")
		if url == "" {
			extra.Status, extra.Error = "failed", "missing webhook_url"
			return "", nil, extra, true
		}
		return url, map[string]any{
			"msg_type": "text",
			"content":  map[string]any{"text": alertText(payload)},
		}, extra, false
	case "alertmanager":
		url = configString(cfg, "url")
		if url == "" {
			extra.Status, extra.Error = "failed", "missing url"
			return "", nil, extra, true
		}
		return url, []map[string]any{{
			"labels":      map[string]any{"alertname": payload["rule_type"], "severity": payload["severity"]},
			"annotations": map[string]any{"summary": payload["alert"], "description": payload["message"]},
			"startsAt":    payload["fired_at"],
		}}, extra, false
	default:
		extra.Status, extra.Reason = "skipped", "unknown_channel_type"
		return "", nil, extra, true
	}
}

func alertText(payload map[string]any) string {
	return fmt.Sprintf("[%v] %v\n%v", payload["severity"], payload["alert"], payload["message"])
}

func configString(cfg map[string]any, key string) string {
	s, _ := cfg[key].(string)
	return strings.TrimSpace(s)
}

func postJSON(ctx context.Context, cli *http.Client, rawURL string, body any) (int, error) {
	if cli == nil {
		cli = &http.Client{Timeout: 10 * time.Second}
	}
	b, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(b))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := cli.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 2048))
	return res.StatusCode, nil
}
