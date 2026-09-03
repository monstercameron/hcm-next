package people

import "testing"

func TestAuthz_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestAuthz_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
