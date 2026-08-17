package users

import (
	"reflect"
	"testing"
)

func TestNormalizeRoles(t *testing.T) {
	got, err := normalizeRoles([]string{"ops_operator", "platform_admin", "ops_operator"})
	if err != nil || !reflect.DeepEqual(got, []string{roleOperator, roleAdmin}) {
		t.Fatalf("got %#v err %v", got, err)
	}
	got, err = normalizeRoles([]string{"ops_operator"})
	if err != nil || !reflect.DeepEqual(got, []string{roleOperator}) {
		t.Fatalf("got %#v err %v", got, err)
	}
	got, err = normalizeRoles(nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %#v err %v", got, err)
	}
	if _, err := normalizeRoles([]string{"nope"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestBoolOr(t *testing.T) {
	if boolOr(nil, true) != true || boolOr(nil, false) != false {
		t.Fatal("nil uses fallback")
	}
	yes, no := true, false
	if boolOr(&yes, false) != true || boolOr(&no, true) != false {
		t.Fatal("pointer wins")
	}
}
