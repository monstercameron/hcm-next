package connectorsdk

import (
	"bytes"
	"testing"
)

func TestRenderDeterministicAndSorted(t *testing.T) {
	a := Manifest{Package: "sdk", Objects: []Object{{Name: "worker", GoType: "Worker", Version: "v1"}, {Name: "position", GoType: "Position", Version: "v2"}}}
	one, e := Render(a)
	if e != nil {
		t.Fatal(e)
	}
	two, e := Render(Manifest{Package: "sdk", Objects: []Object{a.Objects[1], a.Objects[0]}})
	if e != nil || !bytes.Equal(one, two) {
		t.Fatalf("not deterministic: %v", e)
	}
	if !bytes.Contains(one, []byte("type Position")) {
		t.Fatal("missing generated client")
	}
}
func TestRenderRejectsDuplicate(t *testing.T) {
	_, e := Render(Manifest{Package: "sdk", Objects: []Object{{Name: "worker", GoType: "W", Version: "v1"}, {Name: "worker", GoType: "W", Version: "v1"}}})
	if e == nil {
		t.Fatal("expected duplicate rejection")
	}
}
