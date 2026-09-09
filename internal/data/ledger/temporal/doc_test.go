package temporal

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/bitemporal"
)

// TestDocDelegatedModesAreBitemporalModes proves the claim the package doc
// makes and Request.delegate relies on: the five delegated modes are exactly
// internal/data/bitemporal's, so the conversion is a cast rather than a
// translation table that could silently fall out of date.
func TestDocDelegatedModesAreBitemporalModes(t *testing.T) {
	for _, m := range []Mode{ModeCurrent, ModeEffectiveAsOf, ModeKnownAsOf, ModeBetween, ModeHistory} {
		if !m.delegated() {
			t.Errorf("mode %q is not marked delegated", m)
		}
		if !bitemporal.Mode(m).Valid() {
			t.Errorf("mode %q is not a valid internal/data/bitemporal mode", m)
		}
	}
	if ModeReconstruct.delegated() {
		t.Error("RECONSTRUCT is marked delegated; it is this package's own mode")
	}
	if bitemporal.Mode(ModeReconstruct).Valid() {
		t.Error("internal/data/bitemporal accepts RECONSTRUCT; this package must own it")
	}
}

// TestDocOnlyDomainAndTransactionArePromotable proves the separation the
// package doc rests on: no truth class derived from an external observation
// or a claim can ever be resolved into state.
func TestDocOnlyDomainAndTransactionArePromotable(t *testing.T) {
	promotable := map[TruthClass]bool{
		TruthDomain:      true,
		TruthTransaction: true,
		TruthObserved:    false,
		TruthClaimed:     false,
		TruthUnresolved:  false,
	}
	for class, want := range promotable {
		if got := class.Promotable(); got != want {
			t.Errorf("%q.Promotable() = %t, want %t", class, got, want)
		}
	}
}
