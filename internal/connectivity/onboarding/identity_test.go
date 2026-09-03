package onboarding_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity"
	"github.com/monstercameron/hcm-next/internal/connectivity/onboarding"
)

const crosswalkName = "crosswalk.worker.workday->hcmnext"

func pin() onboarding.ReferencePin {
	return onboarding.ReferencePin{Name: crosswalkName, Version: "2026.09.01", Digest: strings.Repeat("ab", 32)}
}

func snapshot(entries ...onboarding.CrosswalkEntry) onboarding.CrosswalkSnapshot {
	p := pin()
	return onboarding.CrosswalkSnapshot{Name: p.Name, Version: p.Version, Digest: p.Digest, Entries: entries}
}

func adjudicatorAt(t time.Time, snap onboarding.CrosswalkSnapshot) onboarding.Adjudicator {
	return onboarding.Adjudicator{
		Snapshots: map[connectivity.ObjectKind]onboarding.CrosswalkSnapshot{connectivity.ObjectWorker: snap},
		Now:       func() time.Time { return t },
	}
}

// TestTodo_ONBOARD_003 proves identity adjudication never guesses: every
// external id resolves to exactly one of EXACT, CANDIDATE, UNMATCHED
// (UNRESOLVED) or CONFLICT (AMBIGUOUS), a mutable or fuzzy match never
// auto-commits, and every row records the exact crosswalk version it was
// adjudicated against.
func TestTodo_ONBOARD_003(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	t.Run("one exact entry resolves to EXACT with its canonical id", func(t *testing.T) {
		t.Parallel()
		snap := snapshot(onboarding.CrosswalkEntry{
			Object: connectivity.ObjectWorker, ExternalID: "W-1001", CanonicalID: "person-001", Exact: true,
		})
		a := adjudicatorAt(at, snap)
		got, err := a.Adjudicate(connectivity.ObjectWorker, "W-1001", pin())
		if err != nil {
			t.Fatalf("adjudicate: %v", err)
		}
		if got.Outcome != onboarding.IdentityExact {
			t.Fatalf("outcome = %s, want EXACT", got.Outcome)
		}
		if got.CanonicalID != "person-001" {
			t.Fatalf("canonical id = %q, want person-001", got.CanonicalID)
		}
		if got.CrosswalkVersion != pin().Version {
			t.Fatalf("crosswalk version = %q, want %q", got.CrosswalkVersion, pin().Version)
		}
	})

	t.Run("no entry resolves to UNMATCHED (operator language: UNRESOLVED)", func(t *testing.T) {
		t.Parallel()
		a := adjudicatorAt(at, snapshot())
		got, err := a.Adjudicate(connectivity.ObjectWorker, "W-9999", pin())
		if err != nil {
			t.Fatalf("adjudicate: %v", err)
		}
		if got.Outcome != onboarding.IdentityUnmatched {
			t.Fatalf("outcome = %s, want UNMATCHED", got.Outcome)
		}
		if got.CanonicalID != "" || len(got.Candidates) != 0 {
			t.Fatalf("an unmatched row carries a candidate: %+v", got)
		}
	})

	t.Run("one fuzzy entry resolves to CANDIDATE and never auto-commits", func(t *testing.T) {
		t.Parallel()
		snap := snapshot(onboarding.CrosswalkEntry{
			Object: connectivity.ObjectWorker, ExternalID: "W-1002", CanonicalID: "person-002", Exact: false,
		})
		a := adjudicatorAt(at, snap)
		got, err := a.Adjudicate(connectivity.ObjectWorker, "W-1002", pin())
		if err != nil {
			t.Fatalf("adjudicate: %v", err)
		}
		if got.Outcome != onboarding.IdentityCandidate {
			t.Fatalf("outcome = %s, want CANDIDATE", got.Outcome)
		}
		if got.CanonicalID != "" {
			t.Fatalf("a candidate row auto-committed a canonical id: %q", got.CanonicalID)
		}
		if len(got.Candidates) != 1 || got.Candidates[0] != "person-002" {
			t.Fatalf("candidates = %v, want [person-002]", got.Candidates)
		}
	})

	t.Run("two exact entries disagreeing resolve to CONFLICT (operator language: AMBIGUOUS)", func(t *testing.T) {
		t.Parallel()
		snap := snapshot(
			onboarding.CrosswalkEntry{Object: connectivity.ObjectWorker, ExternalID: "W-1003", CanonicalID: "person-003a", Exact: true},
			onboarding.CrosswalkEntry{Object: connectivity.ObjectWorker, ExternalID: "W-1003", CanonicalID: "person-003b", Exact: true},
		)
		a := adjudicatorAt(at, snap)
		got, err := a.Adjudicate(connectivity.ObjectWorker, "W-1003", pin())
		if err != nil {
			t.Fatalf("adjudicate: %v", err)
		}
		if got.Outcome != onboarding.IdentityConflict {
			t.Fatalf("outcome = %s, want CONFLICT", got.Outcome)
		}
		if got.CanonicalID != "" {
			t.Fatalf("a conflict row auto-committed a canonical id: %q", got.CanonicalID)
		}
		if len(got.Candidates) != 2 {
			t.Fatalf("candidates = %v, want 2 entries", got.Candidates)
		}
	})

	t.Run("a reviewer may resolve a CANDIDATE or CONFLICT to one of its own candidates, never to an unrelated id", func(t *testing.T) {
		t.Parallel()
		snap := snapshot(onboarding.CrosswalkEntry{
			Object: connectivity.ObjectWorker, ExternalID: "W-1004", CanonicalID: "person-004", Exact: false,
		})
		a := adjudicatorAt(at, snap)
		candidate, err := a.Adjudicate(connectivity.ObjectWorker, "W-1004", pin())
		if err != nil {
			t.Fatalf("adjudicate: %v", err)
		}
		resolved, err := candidate.Resolve("user:steward@harborcare", "person-004", at)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if resolved.ReviewerCanonicalID != "person-004" || resolved.ReviewedBy == "" || resolved.ReviewedAt.IsZero() {
			t.Fatalf("resolved decision incomplete: %+v", resolved)
		}

		if _, err := candidate.Resolve("user:steward@harborcare", "person-not-a-candidate", at); !errors.Is(err, onboarding.ErrInvalidReview) {
			t.Fatalf("resolving to a non-candidate id returned %v, want ErrInvalidReview", err)
		}

		exact := onboarding.Adjudication{Outcome: onboarding.IdentityExact, CanonicalID: "person-005"}
		if _, err := exact.Resolve("user:steward@harborcare", "person-005", at); !errors.Is(err, onboarding.ErrInvalidReview) {
			t.Fatalf("resolving an EXACT row returned %v, want ErrInvalidReview", err)
		}
	})

	t.Run("adjudicating against a crosswalk that does not match the manifest pin is refused", func(t *testing.T) {
		t.Parallel()
		snap := snapshot(onboarding.CrosswalkEntry{
			Object: connectivity.ObjectWorker, ExternalID: "W-1001", CanonicalID: "person-001", Exact: true,
		})
		a := adjudicatorAt(at, snap)
		stalePin := pin()
		stalePin.Version = "2020.01.01"
		_, err := a.Adjudicate(connectivity.ObjectWorker, "W-1001", stalePin)
		if !errors.Is(err, onboarding.ErrStalePin) {
			t.Fatalf("error = %v, want ErrStalePin", err)
		}
	})

	t.Run("every declared outcome is a valid value", func(t *testing.T) {
		t.Parallel()
		for _, o := range []onboarding.IdentityOutcome{
			onboarding.IdentityExact, onboarding.IdentityCandidate,
			onboarding.IdentityUnmatched, onboarding.IdentityConflict,
		} {
			if !o.Valid() {
				t.Fatalf("%s is not reported valid", o)
			}
		}
		if onboarding.IdentityOutcome("GUESS").Valid() {
			t.Fatal("an invented outcome reports valid")
		}
	})
}

// TestTodo_ONBOARD_003_Golden pins the four adjudication outcomes' rendered
// evidence, so a change to outcome selection or evidence shape is caught
// here.
func TestTodo_ONBOARD_003_Golden(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	snap := snapshot(
		onboarding.CrosswalkEntry{Object: connectivity.ObjectWorker, ExternalID: "W-EXACT", CanonicalID: "person-e", Exact: true},
		onboarding.CrosswalkEntry{Object: connectivity.ObjectWorker, ExternalID: "W-CANDIDATE", CanonicalID: "person-c", Exact: false},
		onboarding.CrosswalkEntry{Object: connectivity.ObjectWorker, ExternalID: "W-CONFLICT", CanonicalID: "person-x1", Exact: true},
		onboarding.CrosswalkEntry{Object: connectivity.ObjectWorker, ExternalID: "W-CONFLICT", CanonicalID: "person-x2", Exact: true},
	)
	a := adjudicatorAt(at, snap)

	var b strings.Builder
	for _, externalID := range []string{"W-EXACT", "W-CANDIDATE", "W-CONFLICT", "W-UNMATCHED"} {
		got, err := a.Adjudicate(connectivity.ObjectWorker, externalID, pin())
		if err != nil {
			t.Fatalf("adjudicate %s: %v", externalID, err)
		}
		fmt.Fprintf(&b, "%s -> outcome=%s canonical=%q candidates=%v crosswalk=%s@%s\n",
			externalID, got.Outcome, got.CanonicalID, got.Candidates, got.CrosswalkName, got.CrosswalkVersion)
	}
	compareGoldenFile(t, filepath.Join("testdata", "onboard003_identity.golden"), b.String())
}

// TestTodo_ONBOARD_003_Fault proves adjudication degrades honestly when its
// own inputs are broken: an unloaded object, an empty external id, and a
// mutated crosswalk digest are all refused rather than silently resolved.
func TestTodo_ONBOARD_003_Fault(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	t.Run("no snapshot loaded for the object is NotFound", func(t *testing.T) {
		t.Parallel()
		a := onboarding.Adjudicator{Snapshots: map[connectivity.ObjectKind]onboarding.CrosswalkSnapshot{}}
		_, err := a.Adjudicate(connectivity.ObjectWorker, "W-1001", pin())
		if !errors.Is(err, onboarding.ErrNotFound) {
			t.Fatalf("error = %v, want ErrNotFound", err)
		}
	})

	t.Run("an empty external id is refused", func(t *testing.T) {
		t.Parallel()
		a := adjudicatorAt(at, snapshot())
		_, err := a.Adjudicate(connectivity.ObjectWorker, "", pin())
		if !errors.Is(err, onboarding.ErrInvalidManifest) {
			t.Fatalf("error = %v, want ErrInvalidManifest", err)
		}
	})

	t.Run("an incomplete pin is refused before any lookup", func(t *testing.T) {
		t.Parallel()
		a := adjudicatorAt(at, snapshot())
		_, err := a.Adjudicate(connectivity.ObjectWorker, "W-1001", onboarding.ReferencePin{Name: crosswalkName})
		if !errors.Is(err, onboarding.ErrInvalidManifest) {
			t.Fatalf("error = %v, want ErrInvalidManifest", err)
		}
	})

	t.Run("a crosswalk snapshot whose digest was edited after pinning is a stale pin, not a silent match", func(t *testing.T) {
		t.Parallel()
		snap := snapshot(onboarding.CrosswalkEntry{
			Object: connectivity.ObjectWorker, ExternalID: "W-1001", CanonicalID: "person-001", Exact: true,
		})
		snap.Digest = strings.Repeat("cd", 32) // the loaded snapshot content changed
		a := adjudicatorAt(at, snap)
		_, err := a.Adjudicate(connectivity.ObjectWorker, "W-1001", pin())
		if !errors.Is(err, onboarding.ErrStalePin) {
			t.Fatalf("error = %v, want ErrStalePin", err)
		}
	})
}
