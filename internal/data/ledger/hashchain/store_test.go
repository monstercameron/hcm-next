package hashchain

import "testing"

func TestStore_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestStore_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
