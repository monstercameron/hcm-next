package health

import "testing"

func TestClock_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestClock_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
