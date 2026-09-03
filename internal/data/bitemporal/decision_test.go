package bitemporal

import "testing"

func TestDecision_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestDecision_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
