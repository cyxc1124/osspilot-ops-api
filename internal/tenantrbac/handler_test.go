package tenantrbac

import (
	"net/url"
	"testing"
)

func TestInternalRBACPath(t *testing.T) {
	username := "acme"
	rest := "user-groups/3/members/9"
	got := "/internal/accounts/" + url.PathEscape(username) + "/rbac/" + rest
	want := "/internal/accounts/acme/rbac/user-groups/3/members/9"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
