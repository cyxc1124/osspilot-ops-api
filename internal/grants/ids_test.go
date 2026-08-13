package grants

import "testing"

func TestUniqueIDs(t *testing.T) {
	got, ok := uniqueIDs([]int64{3, 1, 3, 2})
	if !ok || len(got) != 3 || got[0] != 3 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("got %v ok %v", got, ok)
	}
	if _, ok := uniqueIDs([]int64{1, 0}); ok {
		t.Fatal("expected reject")
	}
}

func TestExceedsLimit(t *testing.T) {
	n := int64(2)
	if exceedsLimit(2, &n) || !exceedsLimit(3, &n) || exceedsLimit(9, nil) {
		t.Fatal("limit")
	}
}
