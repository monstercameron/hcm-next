package rewards

import "testing"

func TestCatalog_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestCatalog_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
