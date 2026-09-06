package temporal

import (
	"testing"

	"github.com/google/uuid"

	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
)

func sampleState() State {
	a := at(1, datalogger.DomainFact, "f@1", tMarch, tMarch)
	// Pinned, because a digest that is not a pure function of the answer is
	// not evidence: at() mints a fresh event id on every call.
	a.SourceEventID = uuid.MustParse("33333333-3333-3333-3333-333333333333")
	a.Authority = AuthorityLabel{Present: true, Resolved: true, Ref: "hcmnext:people", Kind: "INTERNAL", CoversEffectiveAt: true}
	a.TruthClass = TruthDomain
	return State{
		Tenant:     uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Subject:    "worker:1",
		Coordinate: Coordinate{EffectiveAt: tJune, KnownAt: tJune},
		Domain:     []Assertion{a},
		Considered: 1,
	}
}

func digestOf(t *testing.T, s State) string {
	t.Helper()
	got, err := s.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	return got
}

func TestStateDigestIsStableAndSensitive(t *testing.T) {
	base := sampleState()
	want := digestOf(t, base)

	if want != digestOf(t, sampleState()) {
		t.Fatal("two identical states digest differently")
	}
	if len(want) != 64 {
		t.Fatalf("digest %q is not a %s hex digest", want, DigestAlgorithm)
	}

	t.Run("a different truth class is a different answer", func(t *testing.T) {
		changed := sampleState()
		changed.Domain[0].TruthClass = TruthTransaction
		if digestOf(t, changed) == want {
			t.Fatal("changing an assertion's truth class did not change the digest")
		}
	})

	t.Run("a different authority label is a different answer", func(t *testing.T) {
		changed := sampleState()
		changed.Domain[0].Authority.CoversEffectiveAt = false
		if digestOf(t, changed) == want {
			t.Fatal("a lapsed authority digests the same as a live one")
		}
	})

	t.Run("a different coordinate is a different answer", func(t *testing.T) {
		changed := sampleState()
		changed.Coordinate.KnownAt = tApril
		if digestOf(t, changed) == want {
			t.Fatal("changing the knowledge horizon did not change the digest")
		}
	})

	t.Run("moving an assertion between buckets is a different answer", func(t *testing.T) {
		changed := sampleState()
		changed.Unpromoted = changed.Domain
		changed.Domain = nil
		if digestOf(t, changed) == want {
			t.Fatal("promoting evidence into domain truth did not change the digest")
		}
	})

	t.Run("how much history was read is bound in", func(t *testing.T) {
		changed := sampleState()
		changed.Considered = 99
		if digestOf(t, changed) == want {
			t.Fatal("the number of assertions considered is not covered by the digest")
		}
	})

	t.Run("the source event id is bound in", func(t *testing.T) {
		changed := sampleState()
		changed.Domain[0].SourceEventID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
		if digestOf(t, changed) == want {
			t.Fatal("substituting the source event did not change the digest")
		}
	})
}

func TestResultDigestIgnoresTheCursorButNotTheAnswer(t *testing.T) {
	a := at(1, datalogger.DomainFact, "f@1", tMarch, tMarch)
	base := Result{
		Mode:       ModeHistory,
		Coordinate: Coordinate{KnownAt: tJune},
		Assertions: []Assertion{a},
	}
	withCursor := base
	withCursor.NextCursor = "opaque"

	baseDigest, err := base.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	cursorDigest, err := withCursor.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if baseDigest != cursorDigest {
		t.Fatal("the cursor changed the digest; how a page was reached is not what it says")
	}

	changed := base
	changed.Mode = ModeCurrent
	changedDigest, err := changed.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if changedDigest == baseDigest {
		t.Fatal("the mode is not covered by the digest")
	}
}

func TestAssertionProjectionCoversTheCorrectionTarget(t *testing.T) {
	plain := at(1, datalogger.DomainFact, "f@1", tMarch, tMarch)
	correcting := corrects(at(2, datalogger.Correction, "f@1", tMarch, tApril), 1)

	if got := plain.project().CorrectsSequence; got != 0 {
		t.Fatalf("an assertion that corrects nothing projects sequence %d, want 0", got)
	}
	p := correcting.project()
	if p.CorrectsStream != "worker:1" || p.CorrectsSequence != 1 {
		t.Fatalf("correction projects target %s@%d, want worker:1@1", p.CorrectsStream, p.CorrectsSequence)
	}
	// Payload bytes are covered through the recorded digest, never inlined.
	if p.Digest != correcting.Digest {
		t.Fatalf("projection digest %q does not carry the recorded digest %q", p.Digest, correcting.Digest)
	}
}
