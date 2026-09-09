package effects

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTerminal_Smoke(t *testing.T) {
	if t == nil {
		t.Fatalf("nil tester")
	}
}

func TestTerminal_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestDeriveEffectiveAtUsesTheProposalInterval(t *testing.T) {
	wanted := time.Date(2026, 12, 1, 5, 0, 0, 0, time.UTC)
	interval, err := values.NewOpenInstantInterval(values.NewInstant(wanted))
	if err != nil {
		t.Fatal(err)
	}
	revision := intent.ProposalRevision{EffectiveTime: interval}
	recorded := time.Date(2026, 9, 5, 20, 41, 0, 0, time.UTC)
	if got := deriveEffectiveAt(revision, recorded); !got.Equal(wanted) {
		t.Fatalf("deriveEffectiveAt = %s, want proposal effective instant %s", got, wanted)
	}
}

func TestDeriveEffectiveAtFallsBackToRecordedAtWithoutAnInterval(t *testing.T) {
	recorded := time.Date(2026, 9, 5, 20, 41, 0, 123, time.FixedZone("local", -4*60*60))
	if got := deriveEffectiveAt(intent.ProposalRevision{}, recorded); !got.Equal(recorded.UTC()) || got.Location() != time.UTC {
		t.Fatalf("deriveEffectiveAt = %s (%s), want UTC fallback %s", got, got.Location(), recorded.UTC())
	}
}
