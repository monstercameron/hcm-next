package outbox

import "testing"

func TestConsumer_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestConsumer_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
