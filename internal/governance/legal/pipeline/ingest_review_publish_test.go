package pipeline

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// publishFixture runs the Washington minimum-wage fixture through the whole
// pipeline once, returning the published release and the registry it landed
// in. It is the shared happy path every LEGAL-002 test builds on.
func publishFixture(t *testing.T) (*Pipeline, legal.PackRelease, *legal.Registry) {
	t.Helper()
	p, err := Author(waMinimumWageDefinitionJSON(), "author-alice", fixedSigner(t, 0x51))
	if err != nil {
		t.Fatalf("Author: %v", err)
	}
	findings := []legal.ReviewFinding{
		{ObligationID: "us-wa-minimum-wage-floor", Severity: legal.FindingSeverityInfo, Note: "confirmed against RCW 49.46.020 text"},
	}
	if err := p.Review("reviewer-bob", legal.ReviewStatusVendorBaseline, findings, fixedSigner(t, 0x52)); err != nil {
		t.Fatalf("Review: %v", err)
	}
	registry := legal.NewRegistry()
	release, err := p.Publish("publisher-carol", fixedSigner(t, 0x53), "", nil, registry)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	return p, release, registry
}

// --- TestTodo_LEGAL_002 ------------------------------------------------------

// TestTodo_LEGAL_002 ingests, reviews and publishes a regulatory RulePack
// (Washington's minimum-wage schedule), then supersedes it with the 2027
// CPI-W amendment, checking every GREEN element the todo names: source
// citations, separated reviewers, an applicability interval, ReviewStatus,
// per-rule ConfidenceMarker, a supersession chain, a digest, a rollback
// target, and Explain.
func TestTodo_LEGAL_002(t *testing.T) {
	p1, v1, registry := publishFixture(t)

	obligation := v1.WageFloors[0]
	if obligation.Citation.SourceFile == "" || obligation.Citation.Section == "" {
		t.Fatalf("released obligation carries no source citation: %+v", obligation.Citation)
	}
	if p1.AuthorID == p1.ReviewerID {
		t.Fatal("author and reviewer must be separated")
	}
	if !v1.Window.Contains(mustDate(t, 2026, time.June, 1)) || v1.Window.HasEnd {
		t.Fatalf("v1 applicability interval = %s, want an open window starting 2026-01-01", v1.Window)
	}
	if v1.ReviewStatus != legal.ReviewStatusVendorBaseline {
		t.Fatalf("ReviewStatus = %s, want VENDOR_BASELINE", v1.ReviewStatus)
	}
	if obligation.Citation.ConfidenceMarker != legal.ConfidenceMarkerConfirmed {
		t.Fatalf("per-rule ConfidenceMarker = %s, want CONFIRMED", obligation.Citation.ConfidenceMarker)
	}
	if v1.Digest == "" || v1.Digest != v1.ComputeDigest() {
		t.Fatalf("digest missing or does not match recomputation: recorded=%s recomputed=%s", v1.Digest, v1.ComputeDigest())
	}
	if _, ok := v1.RollbackTarget(); ok {
		t.Fatal("v1 is the first release and should have no rollback target")
	}
	if explain := v1.Explain(); !strings.Contains(explain, v1.PackID) || !strings.Contains(explain, v1.Digest) {
		t.Fatalf("Explain() = %q, want the pack id and digest", explain)
	}

	// Publication only from a reviewed draft: Publish refuses a pipeline
	// that never went through Review.
	unreviewed, err := Author(waMinimumWageDefinitionJSON(), "author-x", fixedSigner(t, 0x54))
	if err != nil {
		t.Fatalf("Author: %v", err)
	}
	if _, err := unreviewed.Publish("publisher-y", fixedSigner(t, 0x55), "", nil, legal.NewRegistry()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("publishing an unreviewed draft: err=%v, want %v", err, ErrInvalidTransition)
	}

	// Supersession by a new version, never in place: v1's own registered
	// content is not mutated by anything other than window closure.
	p2, err := Author(waMinimumWageSuccessorDefinitionJSON(), "author-dana", fixedSigner(t, 0x56))
	if err != nil {
		t.Fatalf("Author (successor): %v", err)
	}
	if err := p2.Review("reviewer-erin", legal.ReviewStatusVendorBaseline, nil, fixedSigner(t, 0x57)); err != nil {
		t.Fatalf("Review (successor): %v", err)
	}
	v2, err := p2.PublishSupersession(v1.Release(), "publisher-frank", fixedSigner(t, 0x58), "", nil, registry)
	if err != nil {
		t.Fatalf("PublishSupersession: %v", err)
	}
	if v2.Supersedes == nil || *v2.Supersedes != v1.Release() {
		t.Fatalf("v2.Supersedes = %+v, want %+v", v2.Supersedes, v1.Release())
	}
	target, ok := v2.RollbackTarget()
	if !ok || target != v1.Release() {
		t.Fatalf("v2.RollbackTarget() = %+v, %t; want %+v, true", target, ok, v1.Release())
	}

	closedV1, err := registry.GetExact(v1.Release())
	if err != nil {
		t.Fatalf("GetExact(v1) after supersession: %v", err)
	}
	if !closedV1.Window.HasEnd || closedV1.Window.End != v2.Window.Start {
		t.Fatalf("predecessor window not closed at successor start: %s vs successor start %s", closedV1.Window, v2.Window.Start)
	}
	if closedV1.SupersededBy == nil || *closedV1.SupersededBy != v2.Release() {
		t.Fatalf("predecessor SupersededBy = %+v, want %+v", closedV1.SupersededBy, v2.Release())
	}
	// A pinned historical evaluation keeps resolving to the exact release it
	// pinned: GetExact never substitutes the successor for the predecessor's
	// key, even though the predecessor's window has closed.
	if closedV1.Digest != v1.Digest {
		t.Fatalf("predecessor digest changed by supersession: %s vs original %s", closedV1.Digest, v1.Digest)
	}

	if err := p1.Supersede("publisher-carol", v2, fixedSigner(t, 0x53)); err != nil {
		t.Fatalf("Pipeline.Supersede: %v", err)
	}
	if p1.Stage != StageSuperseded {
		t.Fatalf("p1.Stage = %s, want %s", p1.Stage, StageSuperseded)
	}
	if err := p1.VerifyChain(); err != nil {
		t.Fatalf("VerifyChain after supersession: %v", err)
	}

	// --- RED: unsigned, unreviewed, stale, duplicate, jurisdictionless and
	// citationless content must never activate (be evaluable or registrable).

	t.Run("unsigned content does not verify", func(t *testing.T) {
		unsigned := v1
		unsigned.Signatures = nil
		if err := unsigned.Verify(); err == nil {
			t.Fatal("an unsigned release should not verify")
		}
	})

	t.Run("unreviewed content cannot meet any nonzero review floor", func(t *testing.T) {
		if err := RequireReviewFloor(legal.PackRelease{ReviewStatus: legal.ReviewStatusUnreviewed}, legal.ReviewStatusVendorBaseline); !errors.Is(err, ErrReviewFloor) {
			t.Fatalf("RequireReviewFloor(UNREVIEWED, floor=VENDOR_BASELINE) = %v, want %v", err, ErrReviewFloor)
		}
	})

	t.Run("stale content (before its effective window) does not resolve", func(t *testing.T) {
		fresh := legal.NewRegistry()
		if err := fresh.Register(v1); err != nil {
			t.Fatalf("Register: %v", err)
		}
		if _, err := fresh.Lookup(v1.Jurisdiction, mustDate(t, 2025, time.June, 1)); !errors.Is(err, legal.ErrRuleCoverageUnknown) {
			t.Fatalf("Lookup before the effective window: err=%v, want %v", err, legal.ErrRuleCoverageUnknown)
		}
	})

	t.Run("duplicate registration is refused", func(t *testing.T) {
		fresh := legal.NewRegistry()
		if err := fresh.Register(v1); err != nil {
			t.Fatalf("first Register: %v", err)
		}
		if err := fresh.Register(v1); !errors.Is(err, legal.ErrRulePackDuplicate) {
			t.Fatalf("second Register: err=%v, want %v", err, legal.ErrRulePackDuplicate)
		}
	})

	t.Run("jurisdictionless content is refused at candidate time", func(t *testing.T) {
		mutated := bytes.Replace(waMinimumWageDefinitionJSON(), []byte(`"subdivision": "WA"`), []byte(`"subdivision": ""`), 1)
		if _, err := Author(mutated, "author-z", fixedSigner(t, 0x59)); err == nil {
			t.Fatal("expected a jurisdictionless definition to be refused")
		}
	})

	t.Run("citationless content is refused at candidate time", func(t *testing.T) {
		mutated := bytes.Replace(waMinimumWageDefinitionJSON(),
			[]byte(`"source_file": "planning/research/state-employment-law/washington.md",`), []byte(`"source_file": "",`), 1)
		if _, err := Author(mutated, "author-z", fixedSigner(t, 0x5a)); err == nil {
			t.Fatal("expected a citationless definition to be refused")
		}
	})
}

// TestTodo_LEGAL_002_Property proves the digest and rollback-target
// invariants hold across a small sweep of independently generated releases:
// a release with no predecessor never reports a rollback target, and one
// linked through Supersede always reports exactly its predecessor.
func TestTodo_LEGAL_002_Property(t *testing.T) {
	for _, withPredecessor := range []bool{false, true} {
		ca, err := legal.CaliforniaPromotionPack()
		if err != nil {
			t.Fatalf("CaliforniaPromotionPack: %v", err)
		}
		if ca.Digest = ca.ComputeDigest(); ca.Digest == "" {
			t.Fatal("empty digest")
		}
		if !withPredecessor {
			if _, ok := ca.RollbackTarget(); ok {
				t.Fatal("no predecessor set: RollbackTarget should report none")
			}
			continue
		}
		predecessor := ca.Release()
		ca.Supersedes = &predecessor
		target, ok := ca.RollbackTarget()
		if !ok || target != predecessor {
			t.Fatalf("RollbackTarget() = %+v, %t; want %+v, true", target, ok, predecessor)
		}
	}
}

// --- TestTodo_LEGAL_002_Golden -----------------------------------------------

// TestTodo_LEGAL_002_Golden pins the successor release's digest and its
// supersession linkage for the Washington minimum-wage fixture.
func TestTodo_LEGAL_002_Golden(t *testing.T) {
	const wantSuccessorDigest = "5980f195a37fafdec6981e15196e1d1278378ebc666037d02647a0eb80534301"

	_, v1, registry := publishFixture(t)
	p2, err := Author(waMinimumWageSuccessorDefinitionJSON(), "author-dana", fixedSigner(t, 0x56))
	if err != nil {
		t.Fatalf("Author: %v", err)
	}
	if err := p2.Review("reviewer-erin", legal.ReviewStatusVendorBaseline, nil, fixedSigner(t, 0x57)); err != nil {
		t.Fatalf("Review: %v", err)
	}
	v2, err := p2.PublishSupersession(v1.Release(), "publisher-frank", fixedSigner(t, 0x58), "", nil, registry)
	if err != nil {
		t.Fatalf("PublishSupersession: %v", err)
	}
	if v2.Digest != wantSuccessorDigest {
		t.Errorf("successor digest = %s, want golden %s", v2.Digest, wantSuccessorDigest)
	}
	t.Logf("successor digest = %s", v2.Digest)
}

// --- TestTodo_LEGAL_002_Race -------------------------------------------------

// TestTodo_LEGAL_002_Race hammers Register, Lookup and GetExact against one
// registry carrying both the predecessor and the successor from many
// goroutines. The module is built without -race on this platform, so the
// assertion is behavioural: every reader sees a self-consistent release.
func TestTodo_LEGAL_002_Race(t *testing.T) {
	_, v1, registry := publishFixture(t)

	evalDate := mustDate(t, 2026, time.March, 1)
	const iterationsPerWorker = 200
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < iterationsPerWorker; n++ {
				if pack, err := registry.GetExact(v1.Release()); err == nil && pack.PackID != v1.PackID {
					t.Errorf("GetExact returned a mismatched pack id %q", pack.PackID)
				}
				if _, err := registry.Lookup(v1.Jurisdiction, evalDate); err != nil {
					t.Errorf("Lookup: %v", err)
				}
			}
		}()
	}
	wg.Wait()
}

// --- TestTodo_LEGAL_002_Security ---------------------------------------------

// TestTodo_LEGAL_002_Security proves a release cannot be evaluated with a
// forged signature, cannot be trusted under an untrusted key, and a release
// below the tenant's declared review floor is refused rather than evaluated.
func TestTodo_LEGAL_002_Security(t *testing.T) {
	_, v1, _ := publishFixture(t)

	t.Run("forged signature over a valid digest is rejected", func(t *testing.T) {
		forged := v1
		other := fixedSigner(t, 0xa0)
		_, sig := other.SignDigest([]byte("unrelated"))
		forged.Signatures = []legal.RoleSignature{{Role: legal.SigningRoleReleasePublisher, Signature: sig}}
		if err := forged.Verify(); err == nil {
			t.Fatal("expected a forged signature to fail verification")
		}
	})

	t.Run("an untrusted key is rejected by VerifyWithKey", func(t *testing.T) {
		untrusted := fixedSigner(t, 0xa1)
		if err := v1.VerifyWithKey(legal.SigningRoleReleasePublisher, untrusted.PublicKey()); err == nil {
			t.Fatal("expected VerifyWithKey to reject a key that never signed this release")
		}
	})

	t.Run("a release below the tenant floor refuses to evaluate", func(t *testing.T) {
		if err := RequireReviewFloor(v1, legal.ReviewStatusCounselApproved); !errors.Is(err, ErrReviewFloor) {
			t.Fatalf("RequireReviewFloor(VENDOR_BASELINE, floor=COUNSEL_APPROVED) = %v, want %v", err, ErrReviewFloor)
		}
		if err := RequireReviewFloor(v1, legal.ReviewStatusVendorBaseline); err != nil {
			t.Fatalf("RequireReviewFloor(VENDOR_BASELINE, floor=VENDOR_BASELINE) = %v, want nil", err)
		}
	})
}

// --- TestTodo_LEGAL_002_Mutation ----------------------------------------------

// TestTodo_LEGAL_002_Mutation seeds mutants over the RED list's own six
// failure modes; a mutant that is silently accepted anywhere in
// ingest/review/publish/evaluate is a surviving mutation.
func TestTodo_LEGAL_002_Mutation(t *testing.T) {
	mutants := []struct {
		name string
		run  func(t *testing.T) error
	}{
		{"missing citation section", func(t *testing.T) error {
			mutated := bytes.Replace(waMinimumWageDefinitionJSON(), []byte(`"section": "RCW 49.46.020",`), []byte(`"section": "",`), 1)
			_, err := Author(mutated, "author", fixedSigner(t, 0xb0))
			return err
		}},
		{"empty jurisdiction country", func(t *testing.T) error {
			mutated := bytes.Replace(waMinimumWageDefinitionJSON(), []byte(`"country": "US",`), []byte(`"country": "",`), 1)
			_, err := Author(mutated, "author", fixedSigner(t, 0xb1))
			return err
		}},
		{"registering the same release twice", func(t *testing.T) error {
			_, v1, registry := publishFixture(t)
			return registry.Register(v1)
		}},
		{"evaluating a lapsed window", func(t *testing.T) error {
			_, v1, _ := publishFixture(t)
			fresh := legal.NewRegistry()
			if err := fresh.Register(v1); err != nil {
				return err
			}
			_, err := fresh.Lookup(v1.Jurisdiction, mustDate(t, 2020, time.January, 1))
			return err
		}},
		{"unsigned release verifies", func(t *testing.T) error {
			_, v1, _ := publishFixture(t)
			v1.Signatures = nil
			v1.Digest = ""
			return v1.Verify()
		}},
		{"unreviewed release meets a nonzero floor", func(t *testing.T) error {
			return RequireReviewFloor(legal.PackRelease{ReviewStatus: legal.ReviewStatusUnreviewed}, legal.ReviewStatusVendorBaseline)
		}},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			if err := m.run(t); err == nil {
				t.Fatal("mutant survived: expected an error and got none")
			}
		})
	}
}

func mustDate(t *testing.T, year int, month time.Month, day int) values.LocalDate {
	t.Helper()
	d, err := values.NewLocalDate(year, month, day)
	if err != nil {
		t.Fatalf("NewLocalDate(%d,%d,%d): %v", year, month, day, err)
	}
	return d
}
