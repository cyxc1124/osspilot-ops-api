package alerts

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPrepareSendBodies(t *testing.T) {
	payload := map[string]any{
		"alert": "disk", "severity": "warning", "message": "full",
		"rule_type": "bucket_capacity_80", "fired_at": "2026-08-14T01:00:00Z",
	}
	url, body, extra, done := prepareSend("wecom", map[string]any{"webhook_url": "https://qy"}, payload)
	if done || url != "https://qy" {
		t.Fatalf("wecom %v %s", done, url)
	}
	raw, _ := json.Marshal(body)
	if string(raw) != `{"msgtype":"text","text":{"content":"[warning] disk\nfull"}}` {
		t.Fatalf("wecom body %s", raw)
	}

	url, body, extra, done = prepareSend("feishu", map[string]any{"webhook_url": "https://fs"}, payload)
	if done || url != "https://fs" {
		t.Fatalf("feishu %v %s", done, url)
	}
	raw, _ = json.Marshal(body)
	if string(raw) != `{"content":{"text":"[warning] disk\nfull"},"msg_type":"text"}` {
		t.Fatalf("feishu body %s", raw)
	}

	_, body, extra, done = prepareSend("alertmanager", map[string]any{"url": "https://am"}, payload)
	if done {
		t.Fatal("am")
	}
	raw, _ = json.Marshal(body)
	if string(raw) != `[{"annotations":{"description":"full","summary":"disk"},"labels":{"alertname":"bucket_capacity_80","severity":"warning"},"startsAt":"2026-08-14T01:00:00Z"}]` {
		t.Fatalf("am body %s", raw)
	}

	_, body, extra, done = prepareSend("webhook", map[string]any{"url": "https://hook"}, payload)
	if done {
		t.Fatal("webhook")
	}
	m := body.(map[string]any)
	if m["alert"] != "disk" {
		t.Fatalf("%v", m)
	}

	_, _, extra, done = prepareSend("email", map[string]any{"recipients": []any{"a@b"}}, payload)
	if !done || extra.Status != "queued" {
		t.Fatalf("email %+v", extra)
	}
	_, _, extra, done = prepareSend("pager", nil, payload)
	if !done || extra.Status != "skipped" {
		t.Fatalf("skip %+v", extra)
	}
	_, _, extra, done = prepareSend("webhook", map[string]any{}, payload)
	if !done || extra.Status != "failed" {
		t.Fatalf("missing %+v", extra)
	}
	_ = extra
}

func TestAlertPayloadAndChannelIDs(t *testing.T) {
	tid := int64(9)
	ev := Event{
		Title: "t", Severity: "critical", Message: "m", RuleType: "tenant_quota_exceeded",
		TenantID: &tid, FiredAt: time.Date(2026, 8, 14, 1, 2, 3, 0, time.UTC),
	}
	p := alertPayload(ev)
	if p["tenant_id"] != int64(9) || p["fired_at"] != "2026-08-14T01:02:03Z" || p["status"] != "firing" {
		t.Fatalf("%v", p)
	}
	if got := channelIDs(json.RawMessage(`[1,2]`)); len(got) != 2 || got[0] != 1 {
		t.Fatalf("%v", got)
	}
}
