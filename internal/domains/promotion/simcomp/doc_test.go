package simcomp_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/promotion/simassign"
	"github.com/monstercameron/hcm-next/internal/domains/promotion/simcomp"
	promosnapshot "github.com/monstercameron/hcm-next/internal/domains/promotion/snapshot"
)

// TestRequestCarriesNoAmount holds doc.go's first claim to account
// structurally: the request has no field through which a caller could state a
// current or desired amount, so both money values must come from the snapshot.
//
// It reflects over the type rather than trusting the reader, because a future
// convenience field named DesiredBasePay would silently retire the property.
func TestRequestCarriesNoAmount(t *testing.T) {
	t.Parallel()
	forbidden := []string{"pay", "salary", "amount", "current", "desired", "delta", "cost"}
	// DaysPerYear and PayPeriod are declared conventions, not amounts: they say
	// how the arithmetic is done, never what the numbers are.
	allowed := map[string]bool{"DaysPerYear": true, "PayPeriod": true}
	requestType := reflect.TypeOf(simcomp.Request{})
	for i := range requestType.NumField() {
		field := requestType.Field(i)
		if !field.IsExported() || allowed[field.Name] {
			continue
		}
		lower := strings.ToLower(field.Name)
		for _, token := range forbidden {
			if strings.Contains(lower, token) {
				t.Fatalf("Request.%s looks like a caller-supplied money value; "+
					"both amounts must come from the snapshot's band-position inputs", field.Name)
			}
		}
	}
	if _, ok := requestType.FieldByName("Snapshot"); !ok {
		t.Fatal("Request no longer carries the input snapshot")
	}
	for _, declared := range []string{"MoneyScale", "MoneyRounding", "RateScale", "DaysPerYear", "PayPeriod"} {
		if _, ok := requestType.FieldByName(declared); !ok {
			t.Fatalf("Request no longer declares %s; a convention that is not stated is a convention "+
				"that can move every number silently", declared)
		}
	}
}

// TestTwoEffectKindsAreDeclaredAndDistinct pins doc.go's "two effects" claim,
// and that neither collides with the assignment half's three.
func TestTwoEffectKindsAreDeclaredAndDistinct(t *testing.T) {
	t.Parallel()
	kinds := simcomp.EffectKinds()
	if len(kinds) != 2 {
		t.Fatalf("the package declares %d effect kinds, want 2: %v", len(kinds), kinds)
	}
	seen := map[simassign.EffectKind]bool{}
	for _, kind := range kinds {
		if seen[kind] {
			t.Fatalf("effect kind %q is declared twice", kind)
		}
		seen[kind] = true
	}
	for _, other := range simassign.EffectKinds() {
		if seen[other] {
			t.Fatalf("effect kind %q is declared by both halves of the simulation", other)
		}
	}
}

// TestEffectKindsIsACopy proves the declaration cannot be edited through its
// own accessor.
func TestEffectKindsIsACopy(t *testing.T) {
	t.Parallel()
	first := simcomp.EffectKinds()
	first[0] = "tampered"
	if simcomp.EffectKinds()[0] == "tampered" {
		t.Fatal("EffectKinds returns the package's own slice")
	}
}

// TestRefusalReasonsAreDistinctFromTheSharedVocabulary proves this package's
// added reasons neither duplicate each other nor collide with simassign's.
func TestRefusalReasonsAreDistinctFromTheSharedVocabulary(t *testing.T) {
	t.Parallel()
	added := []simassign.Reason{
		simcomp.ReasonInsufficientBudget,
		simcomp.ReasonCurrencyMismatch,
		simcomp.ReasonBudgetUnitMismatch,
		simcomp.ReasonEffectiveDateOutsidePayPeriod,
	}
	shared := []simassign.Reason{
		simassign.ReasonInputWithheld,
		simassign.ReasonInputAbsent,
		simassign.ReasonInputUnknown,
		simassign.ReasonInputUnparsable,
		simassign.ReasonManagerCycle,
		simassign.ReasonChainDepthExceeded,
		simassign.ReasonPositionAtCapacity,
		simassign.ReasonVacancyAfterEffectiveDate,
	}
	seen := map[simassign.Reason]bool{}
	for _, reason := range append(append([]simassign.Reason(nil), shared...), added...) {
		if reason.String() == "" {
			t.Fatal("a declared refusal reason has no wire token")
		}
		if seen[reason] {
			t.Fatalf("refusal reason %q is declared twice", reason)
		}
		seen[reason] = true
	}
}

// TestEveryEffectCitesADeclaredInput pins doc.go's "both money values come from
// the snapshot" claim over the fixture promotion: every effect names the inputs
// it read, and every one of them is a declared snapshot input.
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
	revision, ok := result.Lookup(simcomp.EffectCompensationRevision)
	if !ok {
		t.Fatal("no compensation revision was proposed")
	}
	cites := strings.Join(revision.DerivedFrom, ",")
	for _, want := range []string{
		promosnapshot.InputPayBandPositionCurrent,
		promosnapshot.InputPayBandPositionDesired,
	} {
		if !strings.Contains(cites, want) {
			t.Fatalf("the compensation revision does not cite %s", want)
		}
	}
}
