package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/cyxc1124/osspilot-ops-api/internal/buckets"
	"github.com/cyxc1124/osspilot-ops-api/internal/grants"
	"github.com/cyxc1124/osspilot-ops-api/internal/project"
	"github.com/cyxc1124/osspilot-ops-api/internal/stats"
)

type Evaluator struct {
	store   *Store
	stats   *stats.Store
	buckets *buckets.Store
	grants  *grants.Store
	project *project.Client
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
		default:
			// ponytail: audit/RGW series rules stay no-op until those series are wired.
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
	_, err = e.store.InsertEvent(ctx, Event{
		RuleID: &rid, RuleType: rule.RuleType, Severity: rule.Severity,
		Title: title, Message: message, TenantID: tenantID, BucketID: bucketID, BucketName: bucketName,
		Details: raw, NotifyTenant: rule.NotifyTenant,
	})
	return err == nil, err
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
			n, err := e.store.ResolveOpen(ctx, rule.ID, &tid, nil)
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
