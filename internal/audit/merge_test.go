package audit

import (
	"testing"
	"time"
)

func TestMergePage(t *testing.T) {
	t1 := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(-time.Minute)
	ops := []Entry{{ID: 1, Action: "force_unlock", CreatedAt: t2}}
	tenant := []Entry{{ID: tenantIDShift + 2, Action: "upload", CreatedAt: t1}}
	got, total := mergePage(ops, tenant, 1, 1, 1, 20)
	if total != 2 || len(got) != 2 {
		t.Fatalf("total=%d n=%d", total, len(got))
	}
	if got[0].Action != "upload" || got[1].Action != "force_unlock" {
		t.Fatalf("order %s %s", got[0].Action, got[1].Action)
	}
}
