package session

import "testing"

func TestDoc_Smoke(t *testing.T) {
	if Version() != 1 {
		t.Fatalf("session contract version = %d, want 1", Version())
	}
}

func TestDoc_NoPanic(t *testing.T) {
	if Explain() == "" {
		t.Fatal("session contract explanation is empty")
	}
}
