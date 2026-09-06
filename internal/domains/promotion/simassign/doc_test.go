package simassign_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/promotion/simassign"
	promosnapshot "github.com/monstercameron/hcm-next/internal/domains/promotion/snapshot"
)

// TestRequestCarriesNoCurrentValue holds doc.go's first claim to account
// structurally: the request type has no field through which a caller could
// state a current fact, so "the simulation cannot be told what the baseline is"
// is a property of the type rather than a rule somebody has to remember.
//
// It reflects over the type rather than trusting the reader, because a future
// convenience field named CurrentJobCode would silently retire the property.
func TestRequestCarriesNoCurrentValue(t *testing.T) {
	t.Parallel()
	forbidden := []string{"current", "baseline", "existing", "today", "prior"}
	requestType := reflect.TypeOf(simassign.Request{})
	for i := range requestType.NumField() {
		field := requestType.Field(i)
		if !field.IsExported() {
			continue
		}
		lower := strings.ToLower(field.Name)
		for _, token := range forbidden {
			if strings.Contains(lower, token) {
				t.Fatalf("Request.%s looks like a caller-supplied current value; "+
					"every current fact must come from the snapshot", field.Name)
			}
		}
	}
	if _, ok := requestType.FieldByName("Snapshot"); !ok {
		t.Fatal("Request no longer carries the input snapshot")
	}
}

// TestThreeEffectKindsAreDeclaredAndDistinct pins doc.go's "three effects"
// claim against the declaration itself.
func TestThreeEffectKindsAreDeclaredAndDistinct(t *testing.T) {
	t.Parallel()
	kinds := simassign.EffectKinds()
	if len(kinds) != 3 {
		t.Fatalf("the package declares %d effect kinds, want 3: %v", len(kinds), kinds)
	}
	seen := map[simassign.EffectKind]bool{}
	for _, kind := range kinds {
		if seen[kind] {
			t.Fatalf("effect kind %q is declared twice", kind)
		}
		seen[kind] = true
		if kind.String() == "" {
			t.Fatalf("effect kind %q has no wire token", kind)
		}
	}
}

// TestReversibilityIsThreeValuedAndDistinct pins the closed undo vocabulary.
func TestReversibilityIsThreeValuedAndDistinct(t *testing.T) {
	t.Parallel()
	classes := simassign.ReversibilityClasses()
	if len(classes) != 3 {
		t.Fatalf("the package declares %d reversibility classes, want 3", len(classes))
	}
	seen := map[simassign.Reversibility]bool{}
	for _, class := range classes {
		if seen[class] {
			t.Fatalf("reversibility %q is declared twice", class)
		}
		seen[class] = true
		if !class.Valid() {
			t.Fatalf("declared reversibility %q does not validate", class)
		}
	}
	if simassign.Reversibility("MAYBE").Valid() {
		t.Fatal("an undeclared reversibility class validated")
	}
}

// TestEffectKindsIsACopy proves the declaration cannot be edited through its
// own accessor.
func TestEffectKindsIsACopy(t *testing.T) {
	t.Parallel()
	first := simassign.EffectKinds()
	first[0] = "tampered"
	if simassign.EffectKinds()[0] == "tampered" {
		t.Fatal("EffectKinds returns the package's own slice")
	}
	classes := simassign.ReversibilityClasses()
	classes[0] = "tampered"
	if simassign.ReversibilityClasses()[0] == "tampered" {
		t.Fatal("ReversibilityClasses returns the package's own slice")
	}
}

// TestEveryEffectCitesADeclaredInput pins doc.go's "every effect derives from
// named snapshot inputs and says so" claim over the fixture promotion.
func TestEveryEffectCitesADeclaredInput(t *testing.T) {
	t.Parallel()
	result := simulate(t, newHarness(t), nil)
	declared := map[string]bool{}
	for _, name := range promosnapshot.InputNames() {
		declared[name] = true
	}
	for _, effect := range result.Effects {
		if len(effect.DerivedFrom) == 0 {
			t.Fatalf("effect %s cites no input", effect.Kind)
		}
		for _, name := range effect.DerivedFrom {
			if !declared[name] {
				t.Fatalf("effect %s cites undeclared input %q", effect.Kind, name)
			}
		}
	}
}
