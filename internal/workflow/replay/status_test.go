package replay

import "testing"

// TestStatuses_AreDistinctAndValid pins the four outcomes a replay may report.
// They are the vocabulary a caller branches on, so an undeclared one reaching a
// caller would be an outcome nobody handles.
func TestStatuses_AreDistinctAndValid(t *testing.T) {
	all := Statuses()
	if len(all) != 4 {
		t.Fatalf("Statuses returned %d, want 4", len(all))
	}
	seen := map[Status]bool{}
	for _, s := range all {
		if s == "" {
			t.Fatalf("a declared status is empty")
		}
		if seen[s] {
			t.Fatalf("status %s is duplicated", s)
		}
		seen[s] = true
		if !s.Valid() {
			t.Fatalf("declared status %s does not validate", s)
		}
	}
	for _, want := range []Status{
		StatusComplete, StatusPausedAtFrontier, StatusOpenFrontier, StatusDiverged,
	} {
		if !seen[want] {
			t.Fatalf("Statuses omits %s", want)
		}
	}
	for _, s := range []Status{"", "COMPLETED", "PAUSED", "paused_at_frontier"} {
		if Status(s).Valid() {
			t.Fatalf("%q validated as a declared status", s)
		}
	}
}
