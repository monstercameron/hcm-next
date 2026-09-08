package cycle

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type publicationVerifierFunc func(PublicationVerification) error

func (f publicationVerifierFunc) VerifyFuturePublication(e PublicationVerification) error {
	return f(e)
}

func futurePublicationFixture(t *testing.T) (FuturePublicationRequest, Revision) {
	t.Helper()
	base := validCycle()
	currentRevision, err := NewRevision(base, "current", "v1", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	current, err := NewGovernedCycle(currentRevision)
	if err != nil {
		t.Fatal(err)
	}
	candidateCycle := base
	candidateCycle.Periods = []Period{{ID: "p2", Start: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)}}
	candidateCycle.Phases = []Phase{{ID: "open", Name: "OPEN", Start: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)}}
	revision, err := NewRevision(candidateCycle, "future", "v2", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	evidence := PublicationEvidence{RevisionDigest: revision.Digest, BundleDigest: "sha256:" + strings.Repeat("a", 64), TenantID: revision.Definition.Scope.TenantID, ActivationEpoch: 7, ApprovedAt: now.Add(-4 * time.Hour), ImpactAnalyzedAt: now.Add(-3 * time.Hour), CanaryWindowEnd: now.Add(-2 * time.Hour), CanaryDecidedAt: now.Add(-time.Hour)}
	return FuturePublicationRequest{Revision: revision, Current: current, Now: now, Evidence: evidence, Verifier: publicationVerifierFunc(func(PublicationVerification) error { return nil })}, revision
}

func TestTodo_CYCLE_007(t *testing.T) {
	request, revision := futurePublicationFixture(t)
	publication, err := PublishFuture(request)
	if err != nil {
		t.Fatalf("publish future: %v", err)
	}
	if publication.RevisionDigest != revision.Digest || publication.RevisionVersion != "v2" || publication.BundleDigest != request.Evidence.BundleDigest || publication.ActivationEpoch != 7 || publication.ReceiptDigest == "" {
		t.Fatalf("publication did not bind exact approved revision: %+v", publication)
	}
	if publication.EffectiveFrom != revision.EffectiveFrom || !publication.PublishedAt.Equal(request.Now) {
		t.Fatalf("publication times changed: %+v", publication)
	}
}

func TestTodo_CYCLE_007_Property(t *testing.T) {
	request, revision := futurePublicationFixture(t)
	cases := []struct {
		name   string
		mutate func(*FuturePublicationRequest)
		want   error
	}{
		{"approval-target-mismatch", func(r *FuturePublicationRequest) { r.Evidence.RevisionDigest = "sha256:other" }, ErrPublicationApproval},
		{"impact-before-approval", func(r *FuturePublicationRequest) {
			r.Evidence.ImpactAnalyzedAt = r.Evidence.ApprovedAt.Add(-time.Second)
		}, ErrPublicationImpact},
		{"canary-before-window-end", func(r *FuturePublicationRequest) {
			r.Evidence.CanaryDecidedAt = r.Evidence.CanaryWindowEnd.Add(-time.Second)
		}, ErrPublicationCanary},
		{"mutated-candidate", func(r *FuturePublicationRequest) { r.Revision.Version = "forged" }, ErrPublicationInvalid},
		{"mutated-current", func(r *FuturePublicationRequest) { r.Current.Revision.Version = "forged" }, ErrPublicationInvalid},
		{"opened-cycle", func(r *FuturePublicationRequest) { r.Current.State = StateOpen }, ErrPublicationLifecycle},
		{"locked-cycle", func(r *FuturePublicationRequest) { r.Current.State = StateLocked }, ErrPublicationLifecycle},
		{"not-future", func(r *FuturePublicationRequest) { r.Now = revision.EffectiveFrom }, ErrPublicationNotFuture},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := request
			tc.mutate(&candidate)
			if _, err := PublishFuture(candidate); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if request.Current.State != StateUnopened {
				t.Fatal("publication mutated the caller's lifecycle")
			}
		})
	}
}

func TestTodo_CYCLE_007_RejectsUnauthenticatedEvidence(t *testing.T) {
	request, _ := futurePublicationFixture(t)
	request.Verifier = nil
	if publication, err := PublishFuture(request); !errors.Is(err, ErrPublicationInvalid) || publication != (FuturePublication{}) {
		t.Fatalf("nil verifier: publication=%+v error=%v", publication, err)
	}

	request.Verifier = publicationVerifierFunc(func(PublicationVerification) error { return errors.New("invalid signature") })
	if publication, err := PublishFuture(request); !errors.Is(err, ErrPublicationEvidence) || publication != (FuturePublication{}) {
		t.Fatalf("forged evidence: publication=%+v error=%v", publication, err)
	}
}
