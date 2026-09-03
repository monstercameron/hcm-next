package runtimestate

import "testing"

func TestInventory_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestInventory_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
