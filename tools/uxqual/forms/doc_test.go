package forms

import "testing"

func TestDocPackageExists(t *testing.T) {}

func TestDocNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}
