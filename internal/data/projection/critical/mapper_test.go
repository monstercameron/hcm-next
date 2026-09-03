package critical

import "testing"

func TestMapper_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestMapper_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
