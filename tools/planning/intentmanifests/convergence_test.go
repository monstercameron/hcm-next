package intentmanifests

import (
	"errors"
	"reflect"
	"testing"
)

// TestFeatureVocabularyIntentCatalogConvergence is INTENT-025's PRIMARY
// proof: funding a second domain surfaces only its deferred rows whose
// future definitions are drafted, proposes compatible bindings, and leaves
// all other coverage untouched.
func TestFeatureVocabularyIntentCatalogConvergence(t *testing.T) {
	coverage, funded, descriptors := PromotionFundedFixture()
	before := coverage.Features[0]
	report, err := ConvergeFeatureVocabulary(coverage, funded, descriptors)
	if err != nil {
		t.Fatalf("ConvergeFeatureVocabulary: %v", err)
	}
	if !report.Complete() || len(report.Refusals) != 0 {
		t.Fatalf("report is not complete: %+v", report)
	}
	if len(report.Proposals) != 1 {
		t.Fatalf("proposal count = %d, want 1", len(report.Proposals))
	}
	proposal := report.Proposals[0]
	if proposal.FeatureID != "merit_cycle_manage" || proposal.FutureIntentRef != "hcmnext.rewards.merit_cycle_manage/v1" || proposal.IntentFamily != FamilyChangeRequest {
		t.Fatalf("proposal = %+v", proposal)
	}
	if !reflect.DeepEqual(coverage.Features[0], before) {
		t.Fatal("convergence mutated the coverage input")
	}

	other, otherFunded, otherDescriptors := PromotionFundedFixture()
	otherFunded.Domain = "people"
	otherReport, err := ConvergeFeatureVocabulary(other, otherFunded, otherDescriptors)
	if err != nil {
		t.Fatalf("unrelated funded domain: %v", err)
	}
	if len(otherReport.Proposals) != 0 {
		t.Fatalf("unrelated domain proposals = %+v, want none", otherReport.Proposals)
	}
}

// TestTodo_INTENT_025_Golden pins the funded Promotion-domain convergence
// report rather than a count that could hide a changed feature identity.
func TestTodo_INTENT_025_Golden(t *testing.T) {
	coverage, funded, descriptors := PromotionFundedFixture()
	report, err := ConvergeFeatureVocabulary(coverage, funded, descriptors)
	if err != nil {
		t.Fatalf("ConvergeFeatureVocabulary: %v", err)
	}
	const wantDigest = "sha256:ef21718beb0cd4aff41f8cd102a9620fe80fb2c1e451a7ddc16e284449bb077e"
	if report.Digest != wantDigest {
		t.Fatalf("Promotion convergence digest = %q, want pinned golden %q", report.Digest, wantDigest)
	}
}

func TestFeatureVocabularyConvergenceRefusesClassificationFamilyConflict(t *testing.T) {
	coverage, funded, descriptors := PromotionFundedFixture()
	coverage.Features[0].Classification = ClassObserve
	report, err := ConvergeFeatureVocabulary(coverage, funded, descriptors)
	if !errors.Is(err, ErrFeatureFamilyConflict) {
		t.Fatalf("conflicting convergence error = %v, want ErrFeatureFamilyConflict", err)
	}
	if len(report.Refusals) != 1 || report.Refusals[0].FeatureID != "merit_cycle_manage" {
		t.Fatalf("refusals = %+v, want one refusal for merit_cycle_manage", report.Refusals)
	}
}

func TestFeatureVocabularyConvergenceRequiresCatalogedDrafts(t *testing.T) {
	coverage, funded, descriptors := PromotionFundedFixture()
	funded.DraftedIntentIDs = []string{"hcmnext.rewards.not_drafted/v1"}
	if _, err := ConvergeFeatureVocabulary(coverage, funded, descriptors); !errors.Is(err, ErrDraftedIntentNotCataloged) {
		t.Fatalf("uncataloged funded id error = %v, want ErrDraftedIntentNotCataloged", err)
	}
}
