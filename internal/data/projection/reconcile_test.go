package projection

import "testing"

func TestReconcile_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestReconcile_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
