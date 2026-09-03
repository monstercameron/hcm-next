package health

import "testing"

func TestPorts_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestPorts_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
