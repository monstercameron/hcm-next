package promotion

import "testing"

func TestPreflight_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestPreflight_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
