package racefixture

import "testing"

// TestRacyIncrement is expected to be flagged by `go test -race`; it is
// invoked only by TestTodo_TOOL_012 (tools/quality/race_test.go), never by
// a plain `go test ./...` sweep, because it lives under testdata.
func TestRacyIncrement(t *testing.T) {
	got := RacyCounter()
	if got != 2 {
		t.Fatalf("RacyCounter() = %d, want 2", got)
	}
}

// TestSynchronizedIncrement performs the same work with proper
// synchronization; it must pass cleanly under `go test -race`.
func TestSynchronizedIncrement(t *testing.T) {
	got := SynchronizedCounter()
	if got != 2 {
		t.Fatalf("SynchronizedCounter() = %d, want 2", got)
	}
}
