package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/cyxc1124/osspilot-ops-api/internal/buckets"
	"github.com/cyxc1124/osspilot-ops-api/internal/ceph"
	"github.com/cyxc1124/osspilot-ops-api/internal/grants"
	"github.com/cyxc1124/osspilot-ops-api/internal/project"
	"github.com/cyxc1124/osspilot-ops-api/internal/settings"
	"github.com/cyxc1124/osspilot-ops-api/internal/stats"
)

type Evaluator struct {
	store    *Store
	stats    *stats.Store
	buckets  *buckets.Store
	grants   *grants.Store
	project  *project.Client
	settings *settings.Handler
	http     *http.Client
}

func (e *Evaluator) Run(ctx context.Context) (evaluated, created, resolved int, err error) {
	if e.store == nil {
		return 0, 0, 0, fmt.Errorf("store missing")
	}
	rules, err := e.store.ListEnabledRules(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	evaluated = len(rules)
	usage, _ := e.loadUsage(ctx)
	for _, rule := range rules {
		var n, r int
		switch rule.RuleType {
		case "tenant_quota_exceeded":
			n, r, err = e.evalTenantQuota(ctx, rule, usage)
		case "bucket_capacity_80", "bucket_capacity_90":
			n, r, err = e.evalBucketCapacity(ctx, rule, usage)
		case "rgw_5xx_rate":
			n, r, err = e.evalRGW5xx(ctx, rule)
		case "upload_failure_rate":
			n, r, err = e.evalAuditRate(ctx, rule, []string{"upload", "upload_object", "complete_multipart_upload"}, true)
		case "download_failure_rate":
			n, r, err = e.evalAuditRate(ctx, rule, []string{"download", "download_object", "presign_download"}, true)
		case "frequent_delete":
			n, r, err = e.evalAuditRate(ctx, rule, []string{"delete", "delete_object", "batch_delete"}, false)
		case "frequent_download":
			n, r, err = e.evalAuditRate(ctx, rule, []string{"download", "download_object", "presign_download", "batch_download"}, false)
		case "edit_save_failure":
			n, r, err = e.evalAuditRate(ctx, rule, []string{"save_text_edit", "save_office_edit"}, true)
		case "audit_write_failure":
			n, r, err = e.evalAuditRate(ctx, rule, []string{"audit_log"}, true)
		default:
			continue
		}
		if err != nil {
			return evaluated, created, resolved, err
		}
		created += n
		resolved += r
	}
	return evaluated, created, resolved, nil
}

func (e *Evaluator) loadUsage(ctx context.Context) (*project.Usage, error) {
	if e.project == nil {
		return nil, nil
	}
	return e.project.GetUsage(ctx)
}

func thresholdPercent(cfg json.RawMessage, def float64) float64 {
	var m map[string]any
	if err := json.Unmarshal(nzJSON(cfg, "{}"), &m); err != nil {
		return def
	}
	if v, ok := m["threshold_percent"]; ok {
		switch t := v.(type) {
		case float64:
			return t
		case json.Number:
			f, _ := t.Float64()
			return f
		}
	}
	return def
}

func usagePercent(used int64, quota *int64) (float64, bool) {
	if quota == nil || *quota <= 0 {
		return 0, false
	}
	return float64(used) / float64(*quota), true
}

func (e *Evaluator) fire(ctx context.Context, rule Rule, title, message string, tenantID, bucketID *int64, bucketName *string, details map[string]any) (bool, error) {
	open, err := e.store.HasOpenEvent(ctx, rule.ID, tenantID, bucketID)
	if err != nil || open {
		return false, err
	}
	raw, _ := json.Marshal(details)
	rid := rule.ID
	ev, err := e.store.InsertEvent(ctx, Event{
		RuleID: &rid, RuleType: rule.RuleType, Severity: rule.Severity,
		Title: title, Message: message, TenantID: tenantID, BucketID: bucketID, BucketName: bucketName,
		Details: raw, NotifyTenant: rule.NotifyTenant,
	})
	if err != nil {
		return false, err
	}
	e.dispatch(ctx, ev, rule)
	e.projectEvent(ctx, rule, tenantID, bucketID, bucketName, ev, false)
	return true, nil
}

func fingerprint(ruleID int64, tenantID, bucketID *int64) string {
	t, b := "0", "0"
	if tenantID != nil {
		t = strconv.FormatInt(*tenantID, 10)
	}
	if bucketID != nil {
		b = strconv.FormatInt(*bucketID, 10)
	}
	return fmt.Sprintf("r%d-t%s-b%s", ruleID, t, b)
}

func (e *Evaluator) projectEvent(ctx context.Context, rule Rule, tenantID, bucketID *int64, bucketName *string, ev *Event, resolved bool) {
	if e.project == nil || !rule.NotifyTenant || tenantID == nil || e.stats == nil {
		return
	}
	name, err := e.stats.Username(ctx, *tenantID)
	if err != nil || name == "" {
		return
	}
	body := project.AlertEvent{
		Username: name, Fingerprint: fingerprint(rule.ID, tenantID, bucketID),
		RuleType: rule.RuleType, Severity: rule.Severity, Status: "firing",
		Title: rule.Name, Message: "", BucketName: bucketName,
	}
	if ev != nil {
		body.Title, body.Message, body.Severity = ev.Title, ev.Message, ev.Severity
		if !ev.FiredAt.IsZero() {
			body.FiredAt = ev.FiredAt.UTC().Format(time.RFC3339)
		}
	}
	if resolved {
		body.Resolved, body.Status = true, "resolved"
	}
	_ = e.project.PutAlert(ctx, body)
}

func (e *Evaluator) resolveAndProject(ctx context.Context, rule Rule, tenantID, bucketID *int64) (int, error) {
	n, err := e.store.ResolveOpen(ctx, rule.ID, tenantID, bucketID)
	if err != nil {
		return n, err
	}
	if n > 0 {
		e.projectEvent(ctx, rule, tenantID, bucketID, nil, nil, true)
	}
	return n, nil
}

func (e *Evaluator) evalTenantQuota(ctx context.Context, rule Rule, usage *project.Usage) (int, int, error) {
	if e.stats == nil {
		return 0, 0, nil
	}
	th := thresholdPercent(rule.Config, 1.0)
	tenants, err := e.stats.Tenants(ctx, 1000)
	if err != nil {
		return 0, 0, err
	}
	byBucket := map[string]project.UsageBucket{}
	if usage != nil {
		for _, b := range usage.Buckets {
			byBucket[b.BucketName] = b
		}
	}
	newN, resN := 0, 0
	for _, t := range tenants {
		if t.Status != "active" {
			continue
		}
		used := int64(0)
		if e.grants != nil {
			if gs, err := e.grants.List(ctx, t.ID); err == nil {
				for _, g := range gs {
					if b, ok := byBucket[g.BucketName]; ok {
						used += b.UsedBytes
					}
				}
			}
		}
		pct, ok := usagePercent(used, t.QuotaBytes)
		if !ok {
			continue
		}
		tid := t.ID
		if pct+1e-12 >= th {
			name := t.Name
			if t.DisplayName != nil && *t.DisplayName != "" {
				name = *t.DisplayName
			}
			msg := fmt.Sprintf("账户 %s 已用 %.1f%%（%d / %d 字节）", name, pct*100, used, *t.QuotaBytes)
			created, err := e.fire(ctx, rule, rule.Name+": "+t.Name, msg, &tid, nil, nil, map[string]any{
				"usage_percent": math.Round(pct*10000) / 10000, "used_bytes": used,
			})
			if err != nil {
				return newN, resN, err
			}
			if created {
				newN++
			}
		} else {
			n, err := e.resolveAndProject(ctx, rule, &tid, nil)
			if err != nil {
				return newN, resN, err
			}
			resN += n
		}
	}
	return newN, resN, nil
}

func (e *Evaluator) evalBucketCapacity(ctx context.Context, rule Rule, usage *project.Usage) (int, int, error) {
	if e.buckets == nil {
		return 0, 0, nil
	}
	def := 0.8
	if rule.RuleType == "bucket_capacity_90" {
		def = 0.9
	}
	th := thresholdPercent(rule.Config, def)
	rows, err := e.buckets.List(ctx)
	if err != nil {
		return 0, 0, err
	}
	byName := map[string]project.UsageBucket{}
	if usage != nil {
		for _, b := range usage.Buckets {
			byName[b.BucketName] = b
		}
	}
	newN, resN := 0, 0
	for _, b := range rows {
		if b.Status != "active" {
			continue
		}
		u, ok := byName[b.BucketName]
		if !ok {
			continue
		}
		pct, ok := usagePercent(u.UsedBytes, b.QuotaBytes)
		if !ok {
			continue
		}
		bid := b.ID
		bname := b.BucketName
		if pct+1e-12 >= th {
			msg := fmt.Sprintf("存储桶 %s 已用 %.1f%%（%d / %d 字节）", b.BucketName, pct*100, u.UsedBytes, *b.QuotaBytes)
			created, err := e.fire(ctx, rule, rule.Name+": "+b.BucketName, msg, nil, &bid, &bname, map[string]any{
				"usage_percent": math.Round(pct*10000) / 10000, "used_bytes": u.UsedBytes,
			})
			if err != nil {
				return newN, resN, err
			}
			if created {
				newN++
			}
		} else {
			n, err := e.store.ResolveOpen(ctx, rule.ID, nil, &bid)
			if err != nil {
				return newN, resN, err
			}
			resN += n
		}
	}
	return newN, resN, nil
}

func (e *Evaluator) evalRGW5xx(ctx context.Context, rule Rule) (int, int, error) {
	th := configFloat(rule.Config, "error_rate", 0.05)
	if e.settings == nil {
		n, err := e.store.ResolveOpen(ctx, rule.ID, nil, nil)
		return 0, n, err
	}
	rt, err := e.settings.Runtime(ctx)
	if err != nil || rt.CephMgmtAPIURL == "" {
		n, err := e.store.ResolveOpen(ctx, rule.ID, nil, nil)
		return 0, n, err
	}
	payload, err := ceph.Fetch(ctx, rt.CephMgmtAPIURL, "/rgw/stats")
	if err != nil {
		n, err2 := e.store.ResolveOpen(ctx, rule.ID, nil, nil)
		return 0, n, err2
	}
	_, er, _, _ := ceph.ParseStats(payload)
	if er != nil && *er+1e-12 >= th {
		msg := fmt.Sprintf("RGW 5xx 错误率 %.2f%%，阈值 %.2f%%", *er*100, th*100)
		created, err := e.fire(ctx, rule, rule.Name, msg, nil, nil, nil, map[string]any{"error_rate": *er})
		if err != nil {
			return 0, 0, err
		}
		if created {
			return 1, 0, nil
		}
		return 0, 0, nil
	}
	n, err := e.store.ResolveOpen(ctx, rule.ID, nil, nil)
	return 0, n, err
}

func (e *Evaluator) evalAuditRate(ctx context.Context, rule Rule, actions []string, failureOnly bool) (int, int, error) {
	window := int(configFloat(rule.Config, "window_minutes", 60))
	if window < 1 {
		window = 60
	}
	if e.project == nil {
		return 0, 0, nil
	}
	win, err := e.project.AuditWindow(ctx, window, actions)
	if err != nil {
		return 0, 0, nil
	}
	if failureOnly {
		rate := 0.0
		if win.Total > 0 {
			rate = float64(win.Failures) / float64(win.Total)
		}
		th := configFloat(rule.Config, "failure_rate", 0.1)
		if win.Total > 0 && rate+1e-12 >= th {
			msg := fmt.Sprintf("近 %d 分钟 %v 失败率 %.1f%%（%d/%d）", window, actions, rate*100, win.Failures, win.Total)
			created, err := e.fire(ctx, rule, rule.Name, msg, nil, nil, nil, map[string]any{
				"failure_rate": rate, "total": win.Total, "failures": win.Failures,
			})
			if err != nil {
				return 0, 0, err
			}
			if created {
				return 1, 0, nil
			}
			return 0, 0, nil
		}
	} else {
		th := int64(configFloat(rule.Config, "count_threshold", 50))
		if win.Total >= th {
			msg := fmt.Sprintf("近 %d 分钟 %v 操作 %d 次，超过阈值 %d", window, actions, win.Total, th)
			created, err := e.fire(ctx, rule, rule.Name, msg, nil, nil, nil, map[string]any{
				"count": win.Total, "window_minutes": window,
			})
			if err != nil {
				return 0, 0, err
			}
			if created {
				return 1, 0, nil
			}
			return 0, 0, nil
		}
	}
	n, err := e.store.ResolveOpen(ctx, rule.ID, nil, nil)
	return 0, n, err
}

func configFloat(cfg json.RawMessage, key string, def float64) float64 {
	var m map[string]any
	if err := json.Unmarshal(nzJSON(cfg, "{}"), &m); err != nil {
		return def
	}
	v, ok := m[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case float64:
		return t
	case json.Number:
		f, _ := t.Float64()
		return f
	}
	return def
}
