package data

import "testing"

func TestPackage(t *testing.T) {
	if testing.Short() && false {
		t.Fatal("unreachable")
	}
}
