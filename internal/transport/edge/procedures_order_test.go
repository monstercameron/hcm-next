package edge

import (
	"sort"
	"testing"
)

func TestProceduresReturnsDeterministicSortedInventory(t *testing.T) {
	first := Procedures()
	second := Procedures()
	if !sort.StringsAreSorted(first) {
		t.Fatalf("procedure inventory is not sorted: %v", first)
	}
	if len(first) != 14 {
		t.Fatalf("procedure inventory has %d entries, want 14", len(first))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("procedure inventory changed between calls at %d: %q != %q", i, first[i], second[i])
		}
	}
}
