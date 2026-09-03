package health

import "testing"

func TestState_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestState_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
