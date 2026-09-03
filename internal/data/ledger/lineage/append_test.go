package lineage

import "testing"

func TestAppend_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestAppend_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
