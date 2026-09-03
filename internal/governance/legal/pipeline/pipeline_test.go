package pipeline

import (
	"errors"
	"os"
	"testing"

	legal "github.com/monstercameron/hcm-next/internal/governance/legal"
)

func TestIngestAndReviewEnforceSeparation(t *testing.T) {
	b, err := os.ReadFile("../../../../definitions/legal/packs/seed/us-ca.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Ingest(b, "author")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Review(d, "author", legal.ReviewStatusVendorBaseline); !errors.Is(err, ErrAuthorReviewerSame) {
		t.Fatalf("same actor review error = %v", err)
	}
	r, err := Review(d, "reviewer", legal.ReviewStatusVendorBaseline)
	if err != nil {
		t.Fatal(err)
	}
	if r.Candidate.Pack().ReviewStatus != legal.ReviewStatusVendorBaseline {
		t.Fatalf("status = %v", r.Candidate.Pack().ReviewStatus)
	}
}

func TestCounselRejectsUncertainRules(t *testing.T) {
	b, err := os.ReadFile("../../../../definitions/legal/packs/seed/us-ca.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Ingest(b, "author")
	if err != nil {
		t.Fatal(err)
	}
	// Seed packs are intentionally unreviewed; force one citation to VERIFY.
	d.Definition.Obligations[0].Citation.ConfidenceMarker = "VERIFY"
	if _, err := Review(d, "counsel", legal.ReviewStatusCounselApproved); !errors.Is(err, ErrCounselUncertain) {
		t.Fatalf("uncertain counsel review error = %v", err)
	}
}

func TestReviewFloorFailsClosed(t *testing.T) {
	if err := RequireReviewFloor(legal.PackRelease{ReviewStatus: legal.ReviewStatusVendorBaseline}, legal.ReviewStatusCounselApproved); !errors.Is(err, ErrReviewFloor) {
		t.Fatalf("floor error = %v", err)
	}
	if !MeetsReviewFloor(legal.ReviewStatusCounselApproved, legal.ReviewStatusVendorBaseline) {
		t.Fatal("counsel approval should satisfy vendor floor")
	}
}
