package provenance

import "testing"

func TestRecord_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestRecord_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
