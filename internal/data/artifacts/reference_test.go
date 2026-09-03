package artifacts

import "testing"

func TestReference_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestReference_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
