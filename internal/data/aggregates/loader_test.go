package aggregates

import "testing"

func TestLoader_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestLoader_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
