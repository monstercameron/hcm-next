package pgtest

import "testing"

func TestEmbedded_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestEmbedded_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
