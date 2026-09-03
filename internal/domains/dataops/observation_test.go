package dataops

import "testing"

func TestObservation_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestObservation_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
