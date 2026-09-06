package pgstore

import "testing"

func TestDoc_Smoke(t *testing.T) {
	if New(nil) == nil {
		t.Fatal("pgstore package constructor is unavailable")
	}
}
