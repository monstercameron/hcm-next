package lineage

import "testing"

func TestQuery_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestQuery_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
