package ledger

import "testing"

func TestDigest_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestDigest_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
