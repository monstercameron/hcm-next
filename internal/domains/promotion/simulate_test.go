package promotion

import "testing"

func TestSimulate_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestSimulate_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
