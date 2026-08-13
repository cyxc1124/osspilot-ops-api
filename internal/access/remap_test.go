package access

import (
	"testing"

	"github.com/cyxc1124/osspilot-ops-api/internal/project"
)

func TestBindOpsIDs(t *testing.T) {
	items := []project.AccessItem{
		{ID: 1, AccountID: 99, AccountName: "acme"},
		{ID: 2, AccountID: 100, AccountName: "ghost"},
		{ID: 3, AccountID: 101, AccountName: "beta"},
	}
	got := bindOpsIDs(items, map[string]int64{"acme": 7, "beta": 8})
	if len(got) != 2 {
		t.Fatalf("len %d", len(got))
	}
	if got[0].ID != 1 || got[0].AccountID != 7 {
		t.Fatalf("acme %#v", got[0])
	}
	if got[1].ID != 3 || got[1].AccountID != 8 {
		t.Fatalf("beta %#v", got[1])
	}
}
