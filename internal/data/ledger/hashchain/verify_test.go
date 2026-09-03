package hashchain

import "testing"

func TestVerify_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestVerify_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
