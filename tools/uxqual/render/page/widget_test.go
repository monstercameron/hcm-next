package page

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func stubWidget(text string) Widget {
	return func(WidgetContext) ui.Node { return html.Span(html.Props{}, ui.Text(text)) }
}

func TestRegistryRegisterAndLookup(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register("widget.a.v1", stubWidget("a")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctor, ok := reg.Lookup("widget.a.v1")
	if !ok || ctor == nil {
		t.Fatalf("Lookup(%q) = (%v, %v), want a registered constructor", "widget.a.v1", ctor, ok)
	}
	if _, ok := reg.Lookup("widget.missing.v1"); ok {
		t.Fatalf("Lookup of an unregistered ref reported ok=true")
	}
}

func TestRegistryRegisterRejectsBlankRefNilConstructorAndDuplicate(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register("  ", stubWidget("x")); err == nil {
		t.Fatal("Register with a blank ref succeeded, want an error")
	}
	if err := reg.Register("widget.a.v1", nil); err == nil {
		t.Fatal("Register with a nil constructor succeeded, want an error")
	}
	if err := reg.Register("widget.a.v1", stubWidget("a")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	err := reg.Register("widget.a.v1", stubWidget("a-again"))
	if err == nil {
		t.Fatal("Register of a duplicate ref succeeded, want an error")
	}
	if !errors.Is(err, ErrDuplicateWidget) {
		t.Fatalf("Register duplicate error = %v, want it to wrap ErrDuplicateWidget", err)
	}
}

func TestRegistryNilReceiverIsSafe(t *testing.T) {
	var reg *Registry
	if _, ok := reg.Lookup("anything"); ok {
		t.Fatal("Lookup on a nil *Registry reported ok=true")
	}
	if got := reg.Refs(); got != nil {
		t.Fatalf("Refs on a nil *Registry = %v, want nil", got)
	}
	if err := reg.Register("widget.a.v1", stubWidget("a")); err == nil {
		t.Fatal("Register on a nil *Registry succeeded, want an error")
	}
}

func TestRegistryRefsIsSortedAndComplete(t *testing.T) {
	reg := NewRegistry()
	for _, ref := range []string{"widget.c.v1", "widget.a.v1", "widget.b.v1"} {
		if err := reg.Register(ref, stubWidget(ref)); err != nil {
			t.Fatalf("Register(%q): %v", ref, err)
		}
	}
	want := []string{"widget.a.v1", "widget.b.v1", "widget.c.v1"}
	got := reg.Refs()
	if len(got) != len(want) {
		t.Fatalf("Refs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Refs() = %v, want %v", got, want)
		}
	}
}

func TestUnregisteredWidgetErrorWrapsSentinelAndNamesTheOffendingRef(t *testing.T) {
	err := &UnregisteredWidgetError{PageID: "p", RegionID: "r", SlotID: "s", WidgetRef: "widget.missing.v1"}
	if !errors.Is(err, ErrUnregisteredWidget) {
		t.Fatalf("errors.Is(err, ErrUnregisteredWidget) = false, want true")
	}
	msg := err.Error()
	for _, want := range []string{"widget.missing.v1"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q does not name %q", msg, want)
		}
	}
}
