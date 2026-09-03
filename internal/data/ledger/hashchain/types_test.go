package hashchain

import "testing"

func TestTypes_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestTypes_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
