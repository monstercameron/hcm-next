package bitemporal

import "testing"

func TestDoc_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestDoc_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
