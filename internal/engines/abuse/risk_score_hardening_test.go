package abuse

import (
	"errors"
	"testing"
	"time"
)

func hardeningTable(t *testing.T) WeightingTable {
	t.Helper()
	table, err := NewWeightingTable("hardening", "1", []FindingWeight{
		{Category: FindingBulkExport, Weight: 40, Confidence: 80},
		{Category: FindingFakeWorker, Weight: 70, Confidence: 60},
	}, 100, 50)
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func hardeningFinding(id string, category FindingCategory) RiskFinding {
	return RiskFinding{
		DetectorID: "detector", DetectorSemver: "1.0.0", DetectorDigest: "sha256:detector",
		SignalID: id, Kind: SignalKindBulkExport, Category: category, Severity: SeverityHigh,
		Evidence: RiskEvidence{Scope: FindingScopePrincipal, Principal: "principal"},
	}
}

func hardeningWindow() RiskWindow {
	return RiskWindow{Start: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)}
}

func TestRiskScoreValidationAndImmutableTableState(t *testing.T) {
	for _, category := range []FindingCategory{FindingBulkExport, FindingBankDetailChange, FindingPayRateChange, FindingNetPayRedirect, FindingSelfGrant, FindingRoleEscalation, FindingFakeWorker, FindingPrivilegeBurst} {
		if !findingCategoryValid(category) {
			t.Errorf("category %q reported invalid", category)
		}
	}
	if findingCategoryValid(FindingCategory("unknown")) {
		t.Fatal("unknown finding category reported valid")
	}
	valid := hardeningTable(t)
	for _, tc := range []struct {
		name   string
		mutate func(*WeightingTable)
	}{
		{"identity", func(t *WeightingTable) { t.ID = "" }},
		{"score bounds", func(t *WeightingTable) { t.MaxScore = 0 }},
		{"threshold bounds", func(t *WeightingTable) { t.ReviewThreshold = t.MaxScore + 1 }},
		{"no weights", func(t *WeightingTable) { t.Weights = nil }},
		{"unknown category", func(t *WeightingTable) { t.Weights[0].Category = FindingCategory("unknown") }},
		{"weight bounds", func(t *WeightingTable) { t.Weights[0].Weight = t.MaxScore + 1 }},
		{"confidence bounds", func(t *WeightingTable) { t.Weights[0].Confidence = 101 }},
		{"duplicate category", func(t *WeightingTable) { t.Weights = append(t.Weights, t.Weights[0]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := valid
			copy.Weights = append([]FindingWeight(nil), valid.Weights...)
			tc.mutate(&copy)
			if !errors.Is(copy.Validate(), ErrInvalidWeightingTable) {
				t.Fatalf("error=%v", copy.Validate())
			}
		})
	}
	if valid.canonical() == nil {
		t.Fatal("valid table did not produce canonical bytes")
	}
	invalid := valid
	invalid.ID = ""
	if invalid.canonical() != nil {
		t.Fatal("invalid table produced canonical bytes")
	}

	weights := []FindingWeight{{Category: FindingBulkExport, Weight: 40, Confidence: 80}, {Category: FindingFakeWorker, Weight: 70, Confidence: 60}}
	table, err := NewRiskWeightingTable("copy", "1", weights, 100, 50)
	if err != nil {
		t.Fatal(err)
	}
	weights[0].Weight = 0
	if table.Weights[0].Weight == 0 {
		t.Fatal("weighting constructor retained caller-owned slice")
	}
	scorer, err := NewRiskScorer(table)
	if err != nil {
		t.Fatal(err)
	}
	gotTable := scorer.WeightingTable()
	gotTable.Weights[0].Weight = 0
	if scorer.WeightingTable().Weights[0].Weight == 0 {
		t.Fatal("WeightingTable returned aliased slice")
	}
}

func TestRiskAssessmentScoreErrorsBranchesAndAliases(t *testing.T) {
	scorer, err := NewRiskScorer(hardeningTable(t))
	if err != nil {
		t.Fatal(err)
	}
	window := hardeningWindow()
	if _, err := scorer.Score("", window, nil); !errors.Is(err, ErrInvalidRiskWindow) {
		t.Fatalf("blank principal error=%v", err)
	}
	if _, err := scorer.Score("principal", RiskWindow{}, nil); !errors.Is(err, ErrInvalidRiskWindow) {
		t.Fatalf("invalid window error=%v", err)
	}
	if _, err := scorer.Score("principal", window, []RiskFinding{hardeningFinding("bad", FindingCategory("unknown"))}); !errors.Is(err, ErrInvalidRiskFinding) {
		t.Fatalf("invalid finding error=%v", err)
	}
	wrongPrincipal := hardeningFinding("mismatch", FindingBulkExport)
	wrongPrincipal.Evidence.Principal = "other"
	if _, err := scorer.Score("principal", window, []RiskFinding{wrongPrincipal}); !errors.Is(err, ErrFindingPrincipalMismatch) {
		t.Fatalf("principal mismatch error=%v", err)
	}
	missingWeight := hardeningFinding("missing", FindingPrivilegeBurst)
	if _, err := scorer.Score("principal", window, []RiskFinding{missingWeight}); !errors.Is(err, ErrFindingWeightMissing) {
		t.Fatalf("missing weight error=%v", err)
	}
	duplicate := hardeningFinding("dup", FindingBulkExport)
	if _, err := scorer.Score("principal", window, []RiskFinding{duplicate, duplicate}); !errors.Is(err, ErrInvalidRiskFinding) {
		t.Fatalf("duplicate error=%v", err)
	}
	badSeverity := hardeningFinding("severity", FindingBulkExport)
	badSeverity.Severity = FindingSeverity("bad")
	if _, err := scorer.Score("principal", window, []RiskFinding{badSeverity}); !errors.Is(err, ErrInvalidRiskFinding) {
		t.Fatalf("severity error=%v", err)
	}

	assessment, err := scorer.Score("principal", window, nil)
	if err != nil || assessment.Score != 0 || assessment.Confidence != 0 || assessment.ReviewRequired || assessment.Validate() != nil {
		t.Fatalf("empty assessment=%+v err=%v", assessment, err)
	}
	assessment, err = ScoreRisk(scorer, "principal", window, []RiskFinding{hardeningFinding("one", FindingBulkExport), hardeningFinding("two", FindingFakeWorker)})
	if err != nil || assessment.Score != 100 || assessment.Confidence != 67 || !assessment.ReviewRequired {
		t.Fatalf("bounded assessment=%+v err=%v", assessment, err)
	}
	refs := assessment.FindingRefs()
	refs[0].SignalID = "mutated"
	if assessment.FindingRefs()[0].SignalID == "mutated" {
		t.Fatal("FindingRefs returned an aliased slice")
	}
	if _, err := assessment.Explain(); err != nil {
		t.Fatal(err)
	}
	if _, err := ExplainRiskAssessment(assessment); err != nil {
		t.Fatal(err)
	}
	if _, err := scorer.Explain(assessment); err != nil {
		t.Fatal(err)
	}

	badAssessment := assessment
	badAssessment.Digest = "bad"
	if !errors.Is(badAssessment.Validate(), ErrInvalidRiskAssessment) {
		t.Fatalf("bad assessment accepted: %v", badAssessment.Validate())
	}
	duplicateRef := assessment
	duplicateRef.ContributingFindingRefs = append(append([]FindingRef(nil), assessment.ContributingFindingRefs...), assessment.ContributingFindingRefs[0])
	duplicateRef.Digest = duplicateRef.computedDigest()
	if !errors.Is(duplicateRef.Validate(), ErrInvalidRiskAssessment) {
		t.Fatalf("duplicate assessment refs accepted: %v", duplicateRef.Validate())
	}
	invalidRef := FindingRef{}
	if !errors.Is(invalidRef.Validate(), ErrInvalidRiskFinding) {
		t.Fatalf("invalid finding ref accepted")
	}
	if invalidRef.key() == "" {
		t.Fatal("FindingRef.key unexpectedly empty")
	}
}

func TestRiskReviewAndConsequenceGuards(t *testing.T) {
	scorer, err := NewRiskScorer(hardeningTable(t))
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := scorer.Score("principal", hardeningWindow(), []RiskFinding{hardeningFinding("one", FindingBulkExport)})
	if err != nil {
		t.Fatal(err)
	}
	for _, decision := range []ReviewDecision{ReviewAcknowledged, ReviewDismissed, ReviewInconclusive} {
		if !decision.Valid() {
			t.Errorf("review decision %q invalid", decision)
		}
	}
	if ReviewDecision("bad").Valid() {
		t.Fatal("unknown review decision valid")
	}
	validReview := HumanReviewRecord{RecordID: "review", ReviewerID: "human", AssessmentDigest: assessment.Digest, ReviewedAt: time.Now().UTC(), Decision: ReviewAcknowledged}
	for _, tc := range []struct {
		name   string
		mutate func(*HumanReviewRecord)
	}{
		{"record", func(r *HumanReviewRecord) { r.RecordID = "" }}, {"reviewer", func(r *HumanReviewRecord) { r.ReviewerID = "" }},
		{"digest", func(r *HumanReviewRecord) { r.AssessmentDigest = "" }}, {"time", func(r *HumanReviewRecord) { r.ReviewedAt = time.Time{} }},
		{"decision", func(r *HumanReviewRecord) { r.Decision = "bad" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := validReview
			tc.mutate(&r)
			if !errors.Is(r.Validate(), ErrInvalidHumanReview) {
				t.Fatalf("error=%v", r.Validate())
			}
		})
	}
	validMarker := ConsequenceMarker{MarkerID: "marker", Kind: "STEP_UP", ReviewRecordID: "review", AttachedAt: time.Now().UTC()}
	for _, tc := range []struct {
		name   string
		mutate func(*ConsequenceMarker)
	}{
		{"id", func(m *ConsequenceMarker) { m.MarkerID = "" }}, {"kind", func(m *ConsequenceMarker) { m.Kind = "" }},
		{"review", func(m *ConsequenceMarker) { m.ReviewRecordID = "" }}, {"time", func(m *ConsequenceMarker) { m.AttachedAt = time.Time{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := validMarker
			tc.mutate(&m)
			if !errors.Is(m.Validate(), ErrInvalidConsequenceMarker) {
				t.Fatalf("error=%v", m.Validate())
			}
		})
	}
	if _, err := AttachConsequenceMarker(assessment, HumanReviewRecord{}, validMarker); !errors.Is(err, ErrHumanReviewRequired) {
		t.Fatalf("missing review error=%v", err)
	}
	wrongAssessment := validReview
	wrongAssessment.AssessmentDigest = "other"
	if _, err := AttachConsequenceMarker(assessment, wrongAssessment, validMarker); !errors.Is(err, ErrReviewAssessmentMismatch) {
		t.Fatalf("mismatched review error=%v", err)
	}
	wrongMarker := validMarker
	wrongMarker.ReviewRecordID = "other"
	if _, err := AttachConsequenceMarker(assessment, validReview, wrongMarker); !errors.Is(err, ErrReviewRecordMarkerMismatch) {
		t.Fatalf("mismatched marker error=%v", err)
	}
	linked, err := AttachReviewedConsequence(assessment, validReview, validMarker)
	if err != nil || linked.AssessmentDigest != assessment.Digest || linked.Review.RecordID != validReview.RecordID {
		t.Fatalf("linked consequence=%+v err=%v", linked, err)
	}
}
