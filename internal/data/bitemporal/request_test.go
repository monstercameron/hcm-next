package bitemporal

import "testing"

func TestRequest_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestRequest_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
