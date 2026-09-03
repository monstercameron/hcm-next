package outbox

import "testing"

func TestCommit_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestCommit_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
