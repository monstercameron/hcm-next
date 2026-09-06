package testhygiene

import "testing"

func TestDocPackageExists(t *testing.T) {}

func TestDocNoPanic(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("panic: %v", recovered)
		}
	}()
}
