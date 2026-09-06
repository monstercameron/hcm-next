package abuse_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/abuse"
)

func riskTable(t *testing.T) abuse.WeightingTable {
	t.Helper()
	table, err := abuse.NewWeightingTable("insider-risk", "2026.09", []abuse.FindingWeight{
		{Category: abuse.FindingBulkExport, Weight: 30, Confidence: 80},
		{Category: abuse.FindingFakeWorker, Weight: 70, Confidence: 60},
		{Category: abuse.FindingPrivilegeBurst, Weight: 90, Confidence: 90},
	}, 100, 50)
	if err != nil {
		t.Fatalf("NewWeightingTable: %v", err)
	}
	return table
}

func riskFinding(category abuse.FindingCategory, id string) abuse.RiskFinding {
	return abuse.RiskFinding{
		DetectorID: "detector-" + string(category), DetectorSemver: "1.0.0", DetectorDigest: "sha256:" + string(category),
		SignalID: id, Kind: abuse.SignalKindBulkExport, Category: category, Severity: abuse.SeverityHigh,
		Evidence: abuse.RiskEvidence{Scope: abuse.FindingScopePrincipal, Principal: "principal-1"},
	}
}

func riskWindow() abuse.RiskWindow {
	return abuse.RiskWindow{Start: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)}
}

func TestTodo_ABUSE_004(t *testing.T) {
	scorer, err := abuse.NewRiskScorer(riskTable(t))
	if err != nil {
		t.Fatalf("NewRiskScorer: %v", err)
	}
	assessment, err := scorer.Score("principal-1", riskWindow(), []abuse.RiskFinding{
		riskFinding(abuse.FindingBulkExport, "finding-1"), riskFinding(abuse.FindingFakeWorker, "finding-2"),
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if assessment.Score != 100 || assessment.Confidence != 66 || !assessment.ReviewRequired {
		t.Fatalf("assessment = %+v, want bounded score 100, confidence 66, review required", assessment)
	}
	if len(assessment.ContributingFindingRefs) != 2 || assessment.Digest == "" || assessment.WeightingTableVersion != "2026.09" {
		t.Fatalf("assessment references = %+v", assessment)
	}
	if _, err := assessment.Explain(); err != nil {
		t.Fatalf("Explain: %v", err)
	}
}

func TestTodo_ABUSE_004_Security(t *testing.T) {
	scorer, err := abuse.NewRiskScorer(riskTable(t))
	if err != nil {
		t.Fatalf("NewRiskScorer: %v", err)
	}
	assessment, err := scorer.Score("principal-1", riskWindow(), []abuse.RiskFinding{riskFinding(abuse.FindingBulkExport, "finding-1")})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if _, err := abuse.AttachConsequenceMarker(assessment, abuse.HumanReviewRecord{}, abuse.ConsequenceMarker{}); !errors.Is(err, abuse.ErrHumanReviewRequired) {
		t.Fatalf("missing review error = %v, want human-review refusal", err)
	}
	if _, err := abuse.AttachConsequenceMarker(assessment, abuse.HumanReviewRecord{
		RecordID: "review-1", ReviewerID: "human-1", AssessmentDigest: "other", ReviewedAt: time.Now().UTC(), Decision: abuse.ReviewAcknowledged,
	}, abuse.ConsequenceMarker{MarkerID: "marker-1", Kind: "STEP_UP", ReviewRecordID: "review-1", AttachedAt: time.Now().UTC()}); !errors.Is(err, abuse.ErrReviewAssessmentMismatch) {
		t.Fatalf("mismatched review error = %v, want binding refusal", err)
	}
	review := abuse.HumanReviewRecord{RecordID: "review-1", ReviewerID: "human-1", AssessmentDigest: assessment.Digest, ReviewedAt: time.Now().UTC(), Decision: abuse.ReviewAcknowledged}
	linked, err := abuse.AttachConsequenceMarker(assessment, review, abuse.ConsequenceMarker{MarkerID: "marker-1", Kind: "STEP_UP", ReviewRecordID: "review-1", AttachedAt: time.Now().UTC()})
	if err != nil || linked.AssessmentDigest != assessment.Digest {
		t.Fatalf("reviewed consequence = %+v, err=%v", linked, err)
	}
}

func TestTodo_ABUSE_004_Golden(t *testing.T) {
	scorer, err := abuse.NewRiskScorer(riskTable(t))
	if err != nil {
		t.Fatalf("NewRiskScorer: %v", err)
	}
	assessment, err := scorer.Score("principal-1", riskWindow(), []abuse.RiskFinding{riskFinding(abuse.FindingPrivilegeBurst, "finding-3")})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if assessment.Score != 90 || assessment.Confidence != 90 || !assessment.ReviewRequired {
		t.Fatalf("golden assessment = %+v, want score=90 confidence=90 review=true", assessment)
	}
	if len(assessment.ContributingFindingRefs) != 1 || assessment.ContributingFindingRefs[0].SignalID != "finding-3" {
		t.Fatalf("golden finding refs = %+v", assessment.ContributingFindingRefs)
	}
}
