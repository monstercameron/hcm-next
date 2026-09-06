package ledger

import (
	"testing"

	temporaladapter "github.com/monstercameron/hcm-next/internal/data/ledger/temporal"
)

// TestTemporalAdapterSatisfiesThePort proves the adapter this package hands
// out actually implements the port it declares. Without this, a signature
// drift in internal/data/ledger/temporal would only be caught wherever a
// caller happened to assign one to the other.
func TestTemporalAdapterSatisfiesThePort(t *testing.T) {
	var _ TemporalReader = NewTemporalReader()
	var _ TemporalReader = TemporalAdapter{}
}

// TestTemporalModesAreTheAdaptersOwn proves the re-exported mode constants
// are the adapter's values and not a second, driftable copy of the strings.
func TestTemporalModesAreTheAdaptersOwn(t *testing.T) {
	cases := map[TemporalMode]temporaladapter.Mode{
		ModeCurrent:       temporaladapter.ModeCurrent,
		ModeEffectiveAsOf: temporaladapter.ModeEffectiveAsOf,
		ModeKnownAsOf:     temporaladapter.ModeKnownAsOf,
		ModeBetween:       temporaladapter.ModeBetween,
		ModeHistory:       temporaladapter.ModeHistory,
		ModeReconstruct:   temporaladapter.ModeReconstruct,
	}
	if len(cases) != 6 {
		t.Fatalf("the port re-exports %d distinct modes, want 6", len(cases))
	}
	for exported, adapter := range cases {
		if exported != adapter {
			t.Errorf("port mode %q is not the adapter's %q", exported, adapter)
		}
		if !exported.Valid() {
			t.Errorf("port mode %q is not valid", exported)
		}
	}
}

// TestOnlyDomainAndTransactionTruthArePromotable re-states, at the port, the
// distinction the ledger specification requires: recording an external
// observation never makes it domain truth.
func TestOnlyDomainAndTransactionTruthArePromotable(t *testing.T) {
	promotable := map[TruthClass]bool{
		TruthDomain:      true,
		TruthTransaction: true,
		TruthObserved:    false,
		TruthClaimed:     false,
		TruthUnresolved:  false,
	}
	if len(promotable) != 5 {
		t.Fatalf("the port re-exports %d distinct truth classes, want 5", len(promotable))
	}
	for class, want := range promotable {
		if got := class.Promotable(); got != want {
			t.Errorf("%q.Promotable() = %t, want %t", class, got, want)
		}
	}
}
