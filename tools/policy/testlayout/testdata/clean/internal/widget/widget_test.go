package widget

import "testing"

func TestName(t *testing.T) {
	if Name() != "widget" {
		t.Fatal("unexpected name")
	}
}
