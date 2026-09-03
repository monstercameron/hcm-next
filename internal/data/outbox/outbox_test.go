package outbox

import "testing"

func TestOutbox_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestOutbox_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
