package artifacts

import "testing"

func TestErrors_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestErrors_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
