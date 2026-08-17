package buckets

import (
	"testing"

	"github.com/cyxc1124/osspilot-ops-api/internal/project"
)

func TestAttachUsageKeepsInventoryTime(t *testing.T) {
	at := "2026-08-14T07:00:00Z"
	it := item{}
	attachUsage(&it, project.UsageBucket{UsedBytes: 0, ObjectCount: 0, CollectedAt: &at})
	if it.CollectedAt == nil || *it.CollectedAt != at {
		t.Fatalf("empty inventoried bucket: %v", it.CollectedAt)
	}
	it = item{}
	attachUsage(&it, project.UsageBucket{UsedBytes: 10, ObjectCount: 1})
	if it.CollectedAt != nil {
		t.Fatalf("records without inventory stamp: %v", it.CollectedAt)
	}
}
