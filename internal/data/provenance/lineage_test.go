package provenance

import "testing"

func TestLineage_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestLineage_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
