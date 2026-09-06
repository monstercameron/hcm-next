package performance_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/performance"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func calibrationGraph(t *testing.T) (performance.FrozenParticipantReviewerGraph, performance.ProposedRating) {
	t.Helper()
	collection := proposedCollection(t)
	proposed, err := performance.CalculateProposedRating(collection, "participant-1", proposedRule(t, 2, performance.OutlierHandlingNone))
	if err != nil {
		t.Fatalf("proposed: %v", err)
	}
	return collection.Graph, proposed
}

func calibrationRule(t *testing.T) performance.CalibrationRule {
	t.Helper()
	rule, err := performance.NewCalibrationRule("performance.calibration", "v1", proposedDecimal(t, "0.50", 2), nil)
	if err != nil {
		t.Fatalf("calibration rule: %v", err)
	}
	return rule
}

func TestTodo_PERFORMANCE_005(t *testing.T) {
	graph, proposed := calibrationGraph(t)
	session, err := performance.NewCalibrationSession("calibration-1", graph, []performance.ProposedRating{proposed}, "facilitator-1", []string{"manager-1", "peer-1"}, calibrationRule(t))
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	adjustment, err := performance.NewCalibrationAdjustment("participant-1", proposed.Rating, proposedDecimal(t, "3.50", 2), performance.CalibrationReasonEvidence, "peer-1")
	if err != nil {
		t.Fatalf("adjustment: %v", err)
	}
	updated, err := session.AddAdjustment(adjustment)
	if err != nil {
		t.Fatalf("add adjustment: %v", err)
	}
	final, err := updated.FinalizeParticipant("participant-1")
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if final.ProposedRating.Rating.String() != "3.33" || final.FinalRating.String() != "3.50" || len(final.Adjustments) != 1 || final.Adjustments[0].Digest == "" {
		t.Fatalf("final = %+v", final)
	}
	explanation, err := updated.Explain()
	if err != nil || explanation.AdjustmentCount != 1 || explanation.FacilitatorID != "facilitator-1" {
		t.Fatalf("explanation = %+v, err=%v", explanation, err)
	}
	finalExplanation, err := final.Explain()
	if err != nil || finalExplanation.FinalRating.String() != "3.50" || len(finalExplanation.Adjustments) != 1 {
		t.Fatalf("final explanation = %+v, err=%v", finalExplanation, err)
	}
}

func TestTodo_PERFORMANCE_005_Security(t *testing.T) {
	graph, proposed := calibrationGraph(t)
	session, err := performance.NewCalibrationSession("calibration-1", graph, []performance.ProposedRating{proposed}, "facilitator-1", []string{"manager-1", "peer-1"}, calibrationRule(t))
	if err != nil {
		t.Fatal(err)
	}
	managerAdjustment, err := performance.NewCalibrationAdjustment("participant-1", proposed.Rating, proposedDecimal(t, "3.50", 2), performance.CalibrationReasonEvidence, "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.AddAdjustment(managerAdjustment); !errors.Is(err, performance.ErrCalibrationSeparation) {
		t.Fatalf("manager sole adjuster error = %v", err)
	}
	facilitatorAdjustment, err := performance.NewCalibrationAdjustment("participant-1", proposed.Rating, proposedDecimal(t, "4.00", 2), performance.CalibrationReasonEvidence, "facilitator-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.AddAdjustment(facilitatorAdjustment); !errors.Is(err, performance.ErrCalibrationReviewer) {
		t.Fatalf("facilitator adjustment error = %v", err)
	}
	tooLarge, err := performance.NewCalibrationAdjustment("participant-1", proposed.Rating, proposedDecimal(t, "4.00", 2), performance.CalibrationReasonEvidence, "peer-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.AddAdjustment(tooLarge); !errors.Is(err, performance.ErrCalibrationCapExceeded) {
		t.Fatalf("cap error = %v", err)
	}
}

func TestTodo_PERFORMANCE_005_Mutation(t *testing.T) {
	graph, proposed := calibrationGraph(t)
	session, err := performance.NewCalibrationSession("calibration-1", graph, []performance.ProposedRating{proposed}, "facilitator-1", []string{"manager-1", "peer-1"}, calibrationRule(t))
	if err != nil {
		t.Fatal(err)
	}
	adjustment, err := performance.NewCalibrationAdjustment("participant-1", proposed.Rating, proposedDecimal(t, "3.50", 2), performance.CalibrationReasonConsistency, "peer-1")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := session.AddAdjustment(adjustment)
	if err != nil {
		t.Fatal(err)
	}
	if session.CanonicalDigest == updated.CanonicalDigest || len(session.Adjustments) != 0 || len(updated.Adjustments) != 1 {
		t.Fatal("session history was mutated in place")
	}
	mutated := adjustment
	mutated.To = proposedDecimal(t, "3.00", 2)
	if updated.Adjustments[0].To.String() != "3.50" {
		t.Fatal("stored adjustment aliased caller value")
	}
	if _, err := updated.AddAdjustment(mutated); !errors.Is(err, performance.ErrInvalidCalibrationAdjustment) {
		t.Fatalf("stale digest error = %v", err)
	}
	_ = values.RoundingExactRequired
}
