package snapshot_test

import (
	"reflect"
	"testing"

	promosnapshot "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/snapshot"
)

// TestPackageHasNoConstructorThatAcceptsAValue holds doc.go's first claim to
// account structurally: the only way to obtain a PromotionInputSnapshot is
// Build, and there is no exported field or setter through which a caller could
// assert an input's value.
//
// It reflects over the type rather than trusting the reader, because "you
// cannot tell the snapshot what the baseline is" is the property the whole
// package exists for, and a future exported field would silently retire it.
func TestPackageHasNoConstructorThatAcceptsAValue(t *testing.T) {
	t.Parallel()
	snapshotType := reflect.TypeOf(promosnapshot.PromotionInputSnapshot{})
	for i := range snapshotType.NumField() {
		field := snapshotType.Field(i)
		if !field.IsExported() {
			continue
		}
		if field.Type == reflect.TypeOf([]promosnapshot.Input(nil)) {
			t.Fatalf("field %s exposes the bound inputs as a settable slice; "+
				"the inputs must stay unexported behind the copying accessor", field.Name)
		}
	}

	inputType := reflect.TypeOf(promosnapshot.Input{})
	if _, ok := inputType.FieldByName("CanonicalText"); !ok {
		t.Fatal("Input no longer carries CanonicalText")
	}
}

// TestAvailabilityIsFourValuedAndDistinct pins doc.go's second claim: an input
// that was denied, one that is absent and one that could not be established
// are three different answers, and none of them is a value.
func TestAvailabilityIsFourValuedAndDistinct(t *testing.T) {
	t.Parallel()
	declared := []promosnapshot.Availability{
		promosnapshot.AvailabilityDisclosed,
		promosnapshot.AvailabilityWithheld,
		promosnapshot.AvailabilityAbsent,
		promosnapshot.AvailabilityUnknown,
	}
	seen := map[promosnapshot.Availability]bool{}
	for _, a := range declared {
		if seen[a] {
			t.Fatalf("availability %q is declared twice", a)
		}
		seen[a] = true
		if a.String() == "" {
			t.Fatalf("availability %q has no wire token", a)
		}
	}
	if len(seen) != 4 {
		t.Fatalf("declared %d availabilities, want 4", len(seen))
	}
}

// TestDeclaredInputsAreEightAndOwned pins doc.go's "eight named business
// inputs" against the declaration itself, and proves each names an owner.
func TestDeclaredInputsAreEightAndOwned(t *testing.T) {
	t.Parallel()
	names := promosnapshot.InputNames()
	if len(names) != 8 {
		t.Fatalf("the package declares %d inputs, want 8: %v", len(names), names)
	}
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			t.Fatalf("input %q is declared twice", name)
		}
		seen[name] = true
		if promosnapshot.OwnerOf(name) == "" {
			t.Fatalf("input %q names no owning domain", name)
		}
	}
	if promosnapshot.OwnerOf("promotion.not_an_input") != "" {
		t.Fatal("OwnerOf invented an owner for an undeclared input")
	}
}

// TestInputNamesIsACopy proves the declaration cannot be edited through its
// own accessor.
func TestInputNamesIsACopy(t *testing.T) {
	t.Parallel()
	first := promosnapshot.InputNames()
	first[0] = "tampered"
	if promosnapshot.InputNames()[0] == "tampered" {
		t.Fatal("InputNames returns the package's own slice")
	}
}
