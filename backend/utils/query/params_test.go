package query

import "testing"

func TestIntList(t *testing.T) {
	placeholders, args, ok := IntList([]int{1, 2, 3}, 3)
	if !ok || placeholders != "?,?,?" || len(args) != 3 {
		t.Fatalf("unexpected int list result: %q %#v %v", placeholders, args, ok)
	}
	for _, values := range [][]int{nil, {}, {0}, {-1}, {1, 2, 3, 4}} {
		if _, _, ok := IntList(values, 3); ok {
			t.Fatalf("invalid values accepted: %#v", values)
		}
	}
}
