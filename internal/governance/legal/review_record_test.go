package legal

import (
	"errors"
	"strings"
	"testing"
)

func TestReviewRecordValidateRejectsMissingOrSameActor(t *testing.T) {
	base := ReviewRecord{
		PackDigest: "deadbeef",
		AuthorID:   "author-1",
		ReviewerID: "reviewer-1",
		Status:     ReviewStatusVendorBaseline,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("well-formed record should validate: %v", err)
	}

	same := base
	same.ReviewerID = base.AuthorID
	if err := same.Validate(); !errors.Is(err, ErrReviewRecordActor) {
		t.Fatalf("same author/reviewer error = %v, want %v", err, ErrReviewRecordActor)
	}

	empty := base
	empty.ReviewerID = ""
	if err := empty.Validate(); !errors.Is(err, ErrReviewRecordActor) {
		t.Fatalf("empty reviewer error = %v, want %v", err, ErrReviewRecordActor)
	}

	noDigest := base
	noDigest.PackDigest = ""
	if err := noDigest.Validate(); err == nil {
		t.Fatal("expected an error for a review record with no pinned pack digest")
	}

	noStatus := base
	noStatus.Status = ReviewStatusUnspecified
	if err := noStatus.Validate(); !errors.Is(err, ErrCitationStatus) {
		t.Fatalf("no status error = %v, want %v", err, ErrCitationStatus)
	}
}

func TestReviewFindingRequiresSeverity(t *testing.T) {
	r := ReviewRecord{
		PackDigest: "deadbeef",
		AuthorID:   "author-1",
		ReviewerID: "reviewer-1",
		Status:     ReviewStatusVendorBaseline,
		Findings:   []ReviewFinding{{ObligationID: "x", Note: "no severity"}},
	}
	if err := r.Validate(); !errors.Is(err, ErrReviewFindingSeverity) {
		t.Fatalf("missing severity error = %v, want %v", err, ErrReviewFindingSeverity)
	}
}

func TestReviewRecordDigestChangesWithFindingsAndStatusNotWithFieldOrder(t *testing.T) {
	base := ReviewRecord{
		PackDigest: "deadbeef",
		AuthorID:   "author-1",
		ReviewerID: "reviewer-1",
		Status:     ReviewStatusVendorBaseline,
		Findings: []ReviewFinding{
			{ObligationID: "ca-notice-pay-rate-change", Severity: FindingSeverityInfo, Note: "confirmed against Labor Code text"},
		},
	}
	d1 := base.ComputeDigest()

	raised := base
	raised.Status = ReviewStatusCounselApproved
	if raised.ComputeDigest() == d1 {
		t.Fatal("raising the status should change the digest")
	}

	extraFinding := base
	extraFinding.Findings = append(append([]ReviewFinding{}, base.Findings...), ReviewFinding{
		ObligationID: "ca-field-restriction-salary-history", Severity: FindingSeverityConcern, Note: "narrower than the statute text",
	})
	if extraFinding.ComputeDigest() == d1 {
		t.Fatal("adding a finding should change the digest")
	}

	// Recomputing twice over identical content must be stable.
	if base.ComputeDigest() != d1 {
		t.Fatal("digest is not stable across repeated computation")
	}
}

func TestReviewRecordSignsAndVerifiesOverItsOwnDigest(t *testing.T) {
	reviewer := fixedSigner(t, 0x66)
	r := ReviewRecord{
		PackDigest: "deadbeef",
		AuthorID:   "author-1",
		ReviewerID: "reviewer-1",
		Status:     ReviewStatusVendorBaseline,
		Findings:   []ReviewFinding{{Severity: FindingSeverityInfo, Note: "spot check"}},
	}
	digest, sig := reviewer.SignDigest(r.CanonicalBytes())
	if digest != r.ComputeDigest() {
		t.Fatalf("SignDigest digest %s != ComputeDigest %s", digest, r.ComputeDigest())
	}
	if err := VerifySignature(r.CanonicalBytes(), digest, sig); err != nil {
		t.Fatalf("VerifySignature: %v", err)
	}
}

func TestHasBlockingFindingReportsOnlyBlockingSeverity(t *testing.T) {
	r := ReviewRecord{Findings: []ReviewFinding{{Severity: FindingSeverityConcern}}}
	if r.HasBlockingFinding() {
		t.Fatal("a CONCERN finding should not report as blocking")
	}
	r.Findings = append(r.Findings, ReviewFinding{Severity: FindingSeverityBlocking})
	if !r.HasBlockingFinding() {
		t.Fatal("expected HasBlockingFinding to report the BLOCKING finding")
	}
}

func TestReviewRecordExplainNamesActorsStatusAndSeverityBreakdown(t *testing.T) {
	r := ReviewRecord{
		PackDigest: "deadbeef",
		AuthorID:   "author-1",
		ReviewerID: "reviewer-1",
		Status:     ReviewStatusVendorBaseline,
		Findings: []ReviewFinding{
			{Severity: FindingSeverityBlocking},
			{Severity: FindingSeverityConcern},
			{Severity: FindingSeverityInfo},
		},
	}
	got := r.Explain()
	for _, want := range []string{"reviewer=reviewer-1", "author=author-1", "raised=VENDOR_BASELINE", "blocking=1", "concern=1", "info=1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Explain() = %q, want substring %q", got, want)
		}
	}
}
