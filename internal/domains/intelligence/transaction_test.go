package intelligence

import "testing"

func TestTransaction_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestTransaction_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
