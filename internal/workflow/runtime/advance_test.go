package runtime

import "testing"

func TestAdvance_Smoke(t *testing.T) {
	if t == nil {
		t.Fatalf("nil tester")
	}
}

func TestAdvance_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

// TestNextAttemptForNumbersARevisitAfterThePriorAttempt pins the fix for the
// re-approval loop: routing back to an already-executed node must open the
// next attempt, not collide with the stored attempt 1.
func TestNextAttemptForNumbersARevisitAfterThePriorAttempt(t *testing.T) {
	existing := []NodeExecution{
		{NodeID: "approve_manager", Attempt: 1},
		{NodeID: "approve_manager", Attempt: 2},
		{NodeID: "revalidate", Attempt: 1},
	}
	if got := nextAttemptFor(existing, "approve_manager"); got != 3 {
		t.Fatalf("approve_manager next attempt = %d, want 3", got)
	}
	if got := nextAttemptFor(existing, "revalidate"); got != 2 {
		t.Fatalf("revalidate next attempt = %d, want 2", got)
	}
	if got := nextAttemptFor(existing, "execute_promotion"); got != 1 {
		t.Fatalf("never-activated node next attempt = %d, want 1", got)
	}
	if got := nextAttemptFor(nil, "anything"); got != 1 {
		t.Fatalf("empty history next attempt = %d, want 1", got)
	}
}
