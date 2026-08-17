package audit

import (
	"time"

	"github.com/cyxc1124/osspilot-ops-api/internal/project"
)

// tenantIDShift keeps merged tenant row ids unique vs ops ids (ops-web rowKey=id).
const tenantIDShift int64 = 1_000_000_000_000

func tenantEntries(in []project.AuditEntry) []Entry {
	out := make([]Entry, 0, len(in))
	for _, e := range in {
		created, err := time.Parse(time.RFC3339, e.CreatedAt)
		if err != nil {
			created, err = time.Parse(time.RFC3339Nano, e.CreatedAt)
			if err != nil {
				continue
			}
		}
		out = append(out, Entry{
			ID: e.ID + tenantIDShift, UserID: e.UserID, Username: e.Username,
			TenantID: e.TenantID, TenantName: e.TenantName, BucketName: e.BucketName, ObjectKey: e.ObjectKey,
			Action: e.Action, SourceIP: e.SourceIP, UserAgent: e.UserAgent, Status: e.Status,
			ErrorMessage: e.ErrorMessage, CreatedAt: created,
		})
	}
	return out
}

// mergePage merges two created_at-desc lists. Ceiling: only the first need rows from each source.
func mergePage(ops, tenant []Entry, opsTotal, tenantTotal, page, pageSize int) ([]Entry, int) {
	merged := make([]Entry, 0, len(ops)+len(tenant))
	i, j := 0, 0
	for i < len(ops) || j < len(tenant) {
		switch {
		case i >= len(ops):
			merged = append(merged, tenant[j])
			j++
		case j >= len(tenant):
			merged = append(merged, ops[i])
			i++
		case after(ops[i], tenant[j]):
			merged = append(merged, ops[i])
			i++
		default:
			merged = append(merged, tenant[j])
			j++
		}
	}
	total := opsTotal + tenantTotal
	start := (page - 1) * pageSize
	if start >= len(merged) {
		return []Entry{}, total
	}
	end := start + pageSize
	if end > len(merged) {
		end = len(merged)
	}
	return merged[start:end], total
}

func after(a, b Entry) bool {
	if a.CreatedAt.Equal(b.CreatedAt) {
		return a.ID > b.ID
	}
	return a.CreatedAt.After(b.CreatedAt)
}
