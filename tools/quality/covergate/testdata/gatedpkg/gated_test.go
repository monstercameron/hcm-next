package gatedpkg

import "testing"

func TestDouble(t *testing.T) {
	if Double(21) != 42 {
		t.Fatal("Double(21) != 42")
	}
}
