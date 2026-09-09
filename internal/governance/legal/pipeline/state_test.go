package pipeline

import (
	"errors"
	"strings"
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/sod"
)

// --- TestTodo_LEGAL_015 ------------------------------------------------------

// TestTodo_LEGAL_015 drives the whole authoring and review pipeline over the
// Washington minimum-wage fixture: three distinct principals sign three
// distinct digests, the state machine advances AUTHORED -> REVIEWED ->
// PUBLISHED -> WITHDRAWN, and every transition is a verifiable, chained,
// digested event.
func TestTodo_LEGAL_015(t *testing.T) {
	author := "author-alice"
	reviewer := "reviewer-bob"
	publisher := "publisher-carol"
	authorSigner := fixedSigner(t, 0x01)
	reviewerSigner := fixedSigner(t, 0x02)
	publisherSigner := fixedSigner(t, 0x03)

	p, err := Author(waMinimumWageDefinitionJSON(), author, authorSigner)
	if err != nil {
		t.Fatalf("Author: %v", err)
	}
	if p.Stage != StageAuthored {
		t.Fatalf("stage after Author = %s, want %s", p.Stage, StageAuthored)
	}
	if len(p.Events) != 1 || p.Events[0].PrevDigest != "" {
		t.Fatalf("genesis event malformed: %+v", p.Events)
	}
	if p.Events[0].Role != legal.SigningRoleRuleAuthor {
		t.Fatalf("genesis event role = %s, want %s", p.Events[0].Role, legal.SigningRoleRuleAuthor)
	}

	// RED: the author may not review their own draft.
	if err := p.Review(author, legal.ReviewStatusVendorBaseline, nil, reviewerSigner); !errors.Is(err, ErrAuthorReviewerSame) {
		t.Fatalf("self-review error = %v, want %v", err, ErrAuthorReviewerSame)
	}
	if p.Stage != StageAuthored {
		t.Fatalf("a refused review must not advance the stage; got %s", p.Stage)
	}

	findings := []legal.ReviewFinding{
		{ObligationID: "us-wa-minimum-wage-floor", Severity: legal.FindingSeverityInfo, Note: "confirmed against RCW 49.46.020 text"},
	}
	if err := p.Review(reviewer, legal.ReviewStatusVendorBaseline, findings, reviewerSigner); err != nil {
		t.Fatalf("Review: %v", err)
	}
	if p.Stage != StageReviewed {
		t.Fatalf("stage after Review = %s, want %s", p.Stage, StageReviewed)
	}
	if p.ReviewRecord.ReviewerID != reviewer || p.ReviewRecord.AuthorID != author {
		t.Fatalf("review record actors = %+v", p.ReviewRecord)
	}
	if len(p.Events) != 2 || p.Events[1].PrevDigest != p.Events[0].Digest {
		t.Fatalf("review event does not chain onto the genesis event: %+v", p.Events)
	}

	registry := legal.NewRegistry()

	// RED: the reviewer may not publish their own review.
	if _, err := p.Publish(reviewer, publisherSigner, "", nil, registry); !errors.Is(err, ErrPublisherReviewer) {
		t.Fatalf("reviewer-as-publisher error = %v, want %v", err, ErrPublisherReviewer)
	}
	// RED: the author may not publish either - three distinct principals,
	// not merely "not the reviewer".
	if _, err := p.Publish(author, publisherSigner, "", nil, registry); !errors.Is(err, ErrPublisherReviewer) {
		t.Fatalf("author-as-publisher error = %v, want %v", err, ErrPublisherReviewer)
	}
	if p.Stage != StageReviewed {
		t.Fatalf("a refused publish must not advance the stage; got %s", p.Stage)
	}

	release, err := p.Publish(publisher, publisherSigner, "", nil, registry)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if p.Stage != StagePublished {
		t.Fatalf("stage after Publish = %s, want %s", p.Stage, StagePublished)
	}
	if err := release.Verify(); err != nil {
		t.Fatalf("published release does not verify: %v", err)
	}
	if got, err := registry.GetExact(release.Release()); err != nil || got.Digest != release.Digest {
		t.Fatalf("registry does not carry the published release: %v", err)
	}
	if len(p.Events) != 3 || p.Events[2].PrevDigest != p.Events[1].Digest {
		t.Fatalf("publish event does not chain onto the review event: %+v", p.Events)
	}
	if p.Events[2].ArtifactDigest != release.Digest {
		t.Fatalf("publish event artifact digest = %s, want the release digest %s", p.Events[2].ArtifactDigest, release.Digest)
	}

	if err := p.VerifyChain(); err != nil {
		t.Fatalf("VerifyChain on an untampered pipeline: %v", err)
	}

	explain := p.Explain()
	for _, want := range []string{"us-wa-minimum-wage-schedule", "PUBLISHED", author, reviewer, publisher} {
		if !strings.Contains(explain, want) {
			t.Fatalf("Explain() = %q, want substring %q", explain, want)
		}
	}

	// State machine: PUBLISHED -> WITHDRAWN, and nothing publishable remains
	// after that.
	if err := p.Withdraw(publisher, "superseded schedule mis-cited the wrong RCW section", publisherSigner); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if p.Stage != StageWithdrawn {
		t.Fatalf("stage after Withdraw = %s, want %s", p.Stage, StageWithdrawn)
	}
	if err := p.VerifyChain(); err != nil {
		t.Fatalf("VerifyChain after Withdraw: %v", err)
	}
	if _, err := p.Publish(publisher, publisherSigner, "", nil, registry); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("publish after withdrawal error = %v, want %v", err, ErrInvalidTransition)
	}
	if err := p.Withdraw(publisher, "double withdrawal", publisherSigner); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("double withdrawal error = %v, want %v", err, ErrInvalidTransition)
	}
}

func TestTodo_LEGAL_015_ExternalSigningFailureLeavesNoEventOrTransition(t *testing.T) {
	want := errors.New("custody unavailable")
	key := fixedSigner(t, 0x7a)
	failing, err := legal.NewPortSigner(key.PublicKey(), legal.SignerFunc(func([]byte) ([]byte, error) { return nil, want }))
	if err != nil {
		t.Fatal(err)
	}
	if p, err := Author(waMinimumWageDefinitionJSON(), "author", failing); p != nil || !errors.Is(err, legal.ErrSignerPort) || !errors.Is(err, want) {
		t.Fatalf("Author returned pipeline=%+v error=%v", p, err)
	}
	p, err := Author(waMinimumWageDefinitionJSON(), "author", key)
	if err != nil {
		t.Fatal(err)
	}
	before := *p
	before.Events = append([]Event(nil), p.Events...)
	if err := p.Review("reviewer", legal.ReviewStatusVendorBaseline, nil, failing); !errors.Is(err, legal.ErrSignerPort) || !errors.Is(err, want) {
		t.Fatalf("Review error=%v", err)
	}
	if p.Stage != before.Stage || p.ReviewerID != before.ReviewerID || len(p.Events) != len(before.Events) {
		t.Fatalf("failed signing mutated pipeline: before=%+v after=%+v", before, p)
	}
}

// TestTodo_LEGAL_015_Property proves the state machine only ever advances
// through valid transitions and never lets two roles coincide, across a small
// combinatorial sweep of actor assignments.
func TestTodo_LEGAL_015_Property(t *testing.T) {
	actors := []string{"p1", "p2", "p3"}
	for _, reviewerID := range actors {
		for _, publisherID := range actors {
			p, err := Author(waMinimumWageDefinitionJSON(), "p1", fixedSigner(t, 0x10))
			if err != nil {
				t.Fatalf("Author: %v", err)
			}
			reviewErr := p.Review(reviewerID, legal.ReviewStatusVendorBaseline, nil, fixedSigner(t, 0x11))
			wantReviewOK := reviewerID != "p1"
			if (reviewErr == nil) != wantReviewOK {
				t.Fatalf("Review(reviewer=%s) err=%v, wantOK=%t", reviewerID, reviewErr, wantReviewOK)
			}
			if reviewErr != nil {
				continue
			}
			_, pubErr := p.Publish(publisherID, fixedSigner(t, 0x12), "", nil, legal.NewRegistry())
			wantPublishOK := publisherID != "p1" && publisherID != reviewerID
			if (pubErr == nil) != wantPublishOK {
				t.Fatalf("reviewer=%s publisher=%s: publish err=%v, wantOK=%t", reviewerID, publisherID, pubErr, wantPublishOK)
			}
			if pubErr == nil {
				if err := p.VerifyChain(); err != nil {
					t.Fatalf("reviewer=%s publisher=%s: VerifyChain: %v", reviewerID, publisherID, err)
				}
			}
		}
	}
}

// --- TestTodo_LEGAL_015_Security ---------------------------------------------

// TestTodo_LEGAL_015_Security proves the event chain fails closed: a forged
// signature, a tampered artifact digest, and a reordered/relinked event are
// all rejected by VerifyChain rather than silently accepted because Stage
// still reads PUBLISHED.
func TestTodo_LEGAL_015_Security(t *testing.T) {
	build := func(t *testing.T) *Pipeline {
		t.Helper()
		p, err := Author(waMinimumWageDefinitionJSON(), "author", fixedSigner(t, 0x21))
		if err != nil {
			t.Fatalf("Author: %v", err)
		}
		if err := p.Review("reviewer", legal.ReviewStatusVendorBaseline, nil, fixedSigner(t, 0x22)); err != nil {
			t.Fatalf("Review: %v", err)
		}
		if _, err := p.Publish("publisher", fixedSigner(t, 0x23), "", nil, legal.NewRegistry()); err != nil {
			t.Fatalf("Publish: %v", err)
		}
		return p
	}

	t.Run("forged signature bytes", func(t *testing.T) {
		p := build(t)
		other := fixedSigner(t, 0x99)
		_, forgedSig := other.SignDigest([]byte("unrelated bytes"))
		p.Events[2].Signature = forgedSig
		if err := p.VerifyChain(); !errors.Is(err, ErrChainBroken) {
			t.Fatalf("forged signature error = %v, want %v", err, ErrChainBroken)
		}
	})

	t.Run("tampered artifact digest", func(t *testing.T) {
		p := build(t)
		p.Events[2].ArtifactDigest = "0000000000000000000000000000000000000000000000000000000000000000"
		if err := p.VerifyChain(); !errors.Is(err, ErrChainBroken) {
			t.Fatalf("tampered artifact digest error = %v, want %v", err, ErrChainBroken)
		}
	})

	t.Run("relinked prev digest", func(t *testing.T) {
		p := build(t)
		p.Events[1].PrevDigest = p.Events[1].Digest // point at itself instead of the genesis event
		if err := p.VerifyChain(); !errors.Is(err, ErrChainBroken) {
			t.Fatalf("relinked chain error = %v, want %v", err, ErrChainBroken)
		}
	})

	t.Run("reviewer signs as author role does not bypass SoD", func(t *testing.T) {
		// Even a well-formed, self-verifying signature does not let the SoD
		// check be skipped: Review still refuses before any Event is built.
		p, err := Author(waMinimumWageDefinitionJSON(), "same-person", fixedSigner(t, 0x24))
		if err != nil {
			t.Fatalf("Author: %v", err)
		}
		err = p.Review("same-person", legal.ReviewStatusVendorBaseline, nil, fixedSigner(t, 0x25))
		if !errors.Is(err, ErrAuthorReviewerSame) || !errors.Is(err, sod.ErrUnsatisfiable) {
			t.Fatalf("same-actor review error = %v, want both %v and %v", err, ErrAuthorReviewerSame, sod.ErrUnsatisfiable)
		}
	})

	t.Run("counsel approval refused while a rule is disputed", func(t *testing.T) {
		p, err := Author(waMinimumWageDisputedDefinitionJSON(), "author", fixedSigner(t, 0x26))
		if err != nil {
			t.Fatalf("Author: %v", err)
		}
		if err := p.Review("reviewer", legal.ReviewStatusCounselApproved, nil, fixedSigner(t, 0x27)); !errors.Is(err, ErrCounselUncertain) {
			t.Fatalf("counsel-approves-a-VERIFY-rule error = %v, want %v", err, ErrCounselUncertain)
		}
	})
}

// --- TestTodo_LEGAL_015_Mutation ---------------------------------------------

// TestTodo_LEGAL_015_Mutation seeds seven mutants against a valid pipeline's
// three-principal separation and its state machine, each of which must be
// refused; a mutant that is silently accepted is a surviving mutation.
func TestTodo_LEGAL_015_Mutation(t *testing.T) {
	mutants := []struct {
		name string
		run  func(t *testing.T) error
	}{
		{"author reviews own draft", func(t *testing.T) error {
			p, _ := Author(waMinimumWageDefinitionJSON(), "same", fixedSigner(t, 0x31))
			return p.Review("same", legal.ReviewStatusVendorBaseline, nil, fixedSigner(t, 0x32))
		}},
		{"reviewer publishes own review", func(t *testing.T) error {
			p, _ := Author(waMinimumWageDefinitionJSON(), "a", fixedSigner(t, 0x33))
			_ = p.Review("r", legal.ReviewStatusVendorBaseline, nil, fixedSigner(t, 0x34))
			_, err := p.Publish("r", fixedSigner(t, 0x35), "", nil, legal.NewRegistry())
			return err
		}},
		{"author publishes own draft", func(t *testing.T) error {
			p, _ := Author(waMinimumWageDefinitionJSON(), "a", fixedSigner(t, 0x36))
			_ = p.Review("r", legal.ReviewStatusVendorBaseline, nil, fixedSigner(t, 0x37))
			_, err := p.Publish("a", fixedSigner(t, 0x38), "", nil, legal.NewRegistry())
			return err
		}},
		{"publish before review", func(t *testing.T) error {
			p, _ := Author(waMinimumWageDefinitionJSON(), "a", fixedSigner(t, 0x39))
			_, err := p.Publish("pub", fixedSigner(t, 0x3a), "", nil, legal.NewRegistry())
			return err
		}},
		{"review twice", func(t *testing.T) error {
			p, _ := Author(waMinimumWageDefinitionJSON(), "a", fixedSigner(t, 0x3b))
			_ = p.Review("r1", legal.ReviewStatusVendorBaseline, nil, fixedSigner(t, 0x3c))
			return p.Review("r2", legal.ReviewStatusVendorBaseline, nil, fixedSigner(t, 0x3d))
		}},
		{"withdraw before publish", func(t *testing.T) error {
			p, _ := Author(waMinimumWageDefinitionJSON(), "a", fixedSigner(t, 0x3e))
			return p.Withdraw("a", "too early", fixedSigner(t, 0x3f))
		}},
		{"supersede before publish", func(t *testing.T) error {
			p, _ := Author(waMinimumWageDefinitionJSON(), "a", fixedSigner(t, 0x40))
			return p.Supersede("a", legal.PackRelease{}, fixedSigner(t, 0x41))
		}},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			if err := m.run(t); err == nil {
				t.Fatal("mutant survived: the separation-of-duties or state-machine check accepted it")
			}
		})
	}
}
