package tenantrbac

import (
	"encoding/json"
	"testing"
)

func TestRemapAccountID(t *testing.T) {
	in := []byte(`{"items":[{"id":1,"tenant_id":9,"user_id":3,"members":[]}]}`)
	out, err := remapAccountID(in, 42)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	item := got["items"].([]any)[0].(map[string]any)
	if _, ok := item["tenant_id"]; ok {
		t.Fatal("tenant_id should be gone")
	}
	if item["account_id"].(float64) != 42 {
		t.Fatalf("account_id=%v", item["account_id"])
	}
	if item["user_id"].(float64) != 3 {
		t.Fatal("user_id must stay tenant-side")
	}
}
