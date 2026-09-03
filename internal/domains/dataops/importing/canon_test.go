package importing

import "testing"

func TestCanon_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestCanon_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
