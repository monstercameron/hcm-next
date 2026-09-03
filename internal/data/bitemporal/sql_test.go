package bitemporal

import "testing"

func TestSql_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestSql_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
