package merit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type meritApprovalVerifier struct{ reject bool }

func (v meritApprovalVerifier) VerifyMeritApproval(r ApprovalReceipt) error {
	if v.reject {
		return errors.New("rejected")
	}
	return nil
}
func (v meritApprovalVerifier) VerifyMeritRejection(r RejectionReceipt) error {
	if v.reject {
		return errors.New("rejected")
	}
	return nil
}

func approveMerit(t *testing.T, c MeritCycle, participant, approver string) MeritCycle {
	t.Helper()
	var digest string
	for _, r := range c.Recommendations {
		if r.ParticipantID == participant {
			digest = r.CanonicalDigest
		}
	}
	next, err := c.Approve(participant, ApprovalReceipt{ReceiptID: "receipt-" + participant + fmt.Sprint(c.Revision), ApproverID: approver, RecommendationDigest: digest}, meritApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func meritTwoWorkerCycle(t *testing.T) MeritCycle {
	t.Helper()
	c := meritCycle(t)
	money, err := values.NewMoney("200.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	c.Population.Members = append(c.Population.Members, PopulationMember{ParticipantID: "worker-2", ManagerID: "manager-2", BasePay: money, PerformanceRating: meritDecimal(t, "4.00"), BandPosition: meritDecimal(t, "0.50"), SalaryRevisionRef: "salary-2", PerformanceRef: "performance-2", EffectiveAt: meritInstant(), KnownAt: meritInstant()})
	c.Population.CanonicalDigest = ""
	c.Population, err = NewPopulationSnapshot(c.Population)
	if err != nil {
		t.Fatal(err)
	}
	c.CanonicalDigest = ""
	c, err = NewMeritCycle(c)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func approvedMeritCycle(t *testing.T) MeritCycle {
	t.Helper()
	c := meritTwoWorkerCycle(t)
	var err error
	c, _, err = c.Propose("worker-1", meritDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	c, _, err = c.Propose("worker-2", meritDecimal(t, "0.05"), "manager-2")
	if err != nil {
		t.Fatalf("propose2: %v", err)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("preapprove validate: %v", err)
	}
	c = approveMerit(t, c, "worker-1", "approver-1")
	c = approveMerit(t, c, "worker-2", "approver-2")
	return c
}

func mixedMeritCycle(t *testing.T) MeritCycle {
	t.Helper()
	c := meritTwoWorkerCycle(t)
	var err error
	c, _, err = c.Propose("worker-1", meritDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	c, rejected, err := c.Propose("worker-2", meritDecimal(t, "0.05"), "manager-2")
	if err != nil {
		t.Fatal(err)
	}
	c = approveMerit(t, c, "worker-1", "approver-1")
	c, err = c.Reject("worker-2", RejectionReceipt{ReceiptID: "reject-worker-2", ApproverID: "approver-2", RecommendationDigest: rejected.CanonicalDigest}, meritApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	c, err = c.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMeritConformanceConservesBudgetAndEmitsEachCompensationChangeOnce(t *testing.T) {
	c := approvedMeritCycle(t)
	final, err := c.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	used, err := final.ConsumedBudget()
	if err != nil || used.String() != "15.00" {
		t.Fatalf("consumed=%v err=%v", used, err)
	}
	children, err := final.CompensationChangeIntents()
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 || children[0].ParticipantID != "worker-1" || children[0].TargetBasePay.String() != "105.00 USD" {
		t.Fatalf("children=%#v", children)
	}
	store := NewMemoryCompensationIntentStore()
	first, err := store.Emit(final)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.Emit(final)
	if err != nil || len(first) != len(children) || len(again) != 0 {
		t.Fatalf("emissions first=%d retry=%d err=%v", len(first), len(again), err)
	}
}

func TestTodo_MERIT_002_Property(t *testing.T) {
	final := approvedMeritCycle(t)
	var err error
	final, err = final.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := final.RemainingBudget()
	if err != nil || remaining.String() != "5.00" {
		t.Fatalf("remaining=%v err=%v", remaining, err)
	}
	for _, rate := range []string{"0.05", "0.06", "0.09", "0.10"} {
		c := meritCycle(t)
		next, _, err := c.Propose("worker-1", meritDecimal(t, rate), "manager-1")
		if err != nil {
			t.Fatalf("rate %s: %v", rate, err)
		}
		used, err := next.ConsumedBudget()
		if err != nil {
			t.Fatal(err)
		}
		sum, err := used.Add(mustRemaining(t, next))
		if err != nil || !sum.Equal(next.Budget) {
			t.Fatalf("rate %s failed conservation: %v %v", rate, sum, err)
		}
	}
}

func mustRemaining(t *testing.T, c MeritCycle) values.Decimal {
	t.Helper()
	d, err := c.RemainingBudget()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestTodo_MERIT_002_Golden(t *testing.T) {
	final := mixedMeritCycle(t)
	kids, err := final.CompensationChangeIntents()
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 1 {
		t.Fatalf("children=%#v", kids)
	}
	if final.CanonicalDigest != "sha256:ff1d00d2596509464d3d13c3d05a775fb3f25d74037efbd84737d8b432747bfd" ||
		kids[0].IntentID != "sha256:751cbe03e4572a53d57f0d288136f3dc4205e28e06f5013ba7c6d5113a0261c0" ||
		kids[0].CanonicalDigest != "sha256:5ad1f1ff55850d74d78adf6000c412811667fcb7c2e3f5243129e21535d17ea5" {
		t.Fatalf("child=%#v", kids[0])
	}
	if remaining := mustRemaining(t, final); remaining.String() != "15.00" {
		t.Fatalf("mixed remaining=%s", remaining)
	}
}

func TestTodo_MERIT_002_Race(t *testing.T) {
	c := meritCycle(t)
	c.Budget = meritDecimal(t, "10.00")
	c.CanonicalDigest = ""
	var err error
	c, err = NewMeritCycle(c)
	if err != nil {
		t.Fatal(err)
	}
	s := NewMemoryCycleStore()
	if err := s.Save(c); err != nil {
		t.Fatal(err)
	}
	left, _, err := c.Propose("worker-1", meritDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	right, _, err := c.Propose("worker-1", meritDecimal(t, "0.10"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	for _, candidate := range []MeritCycle{left, right} {
		go func(x MeritCycle) { defer wg.Done(); errs <- s.Save(x) }(candidate)
	}
	wg.Wait()
	close(errs)
	passed := 0
	for e := range errs {
		if e == nil {
			passed++
		}
	}
	if passed != 1 {
		t.Fatalf("concurrent saves accepted=%d", passed)
	}
}

func TestTodo_MERIT_002_SharedRevisionStore(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	base := meritCycle(t)
	if err := store.Save(ctx, "tenant-1", base); err != nil {
		t.Fatal(err)
	}
	next, _, err := base.Propose("worker-1", meritDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "tenant-1", next); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(ctx, "tenant-1", base.CycleID, 1)
	if err != nil || loaded.CanonicalDigest != base.CanonicalDigest {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	current, err := store.Current(ctx, "tenant-1", base.CycleID)
	if err != nil || current.Revision != 2 {
		t.Fatalf("current=%#v err=%v", current, err)
	}
	revisions, err := store.Revisions(ctx, "tenant-1", base.CycleID)
	if err != nil || len(revisions) != 2 || revisions[0] != 1 || revisions[1] != 2 {
		t.Fatalf("revisions=%v err=%v", revisions, err)
	}
	if err := store.Save(ctx, "tenant-1", next); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate=%v", err)
	}
	if _, err := store.Load(ctx, "tenant-2", base.CycleID, 1); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("cross-tenant load=%v", err)
	}
	if _, err := store.Current(ctx, "tenant-1", "missing"); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("missing current=%v", err)
	}
	if _, err := store.Revisions(ctx, "tenant-1", "missing"); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("missing revisions=%v", err)
	}
}

func TestTodo_MERIT_002_Fault(t *testing.T) {
	c := approvedMeritCycle(t)
	final, err := c.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewCalibrationAdjustment(CalibrationAdjustment{ParticipantID: "worker-1", From: final.Recommendations[0].Amount, To: meritDecimal(t, "6.00"), Reason: performance.CalibrationReasonEvidence, AdjusterID: "calibrator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := final.Calibrate("worker-1", a); !errors.Is(err, ErrCorrectionAfterFinalization) {
		t.Fatalf("post-finalize calibration=%v", err)
	}
	kids, err := final.CompensationChangeIntents()
	if err != nil {
		t.Fatal(err)
	}
	bad := kids[0]
	bad.IntentType = "wrong"
	if !errors.Is(bad.Validate(), ErrInvalidCompensationIntent) {
		t.Fatal("invalid intent type accepted")
	}
	bad = kids[0]
	bad.CanonicalDigest = "sha256:tampered"
	if !errors.Is(bad.Validate(), ErrInvalidCompensationIntent) {
		t.Fatal("tampered child digest accepted")
	}
	store := NewMemoryCompensationIntentStore()
	store.items[kids[0].IntentID] = bad
	if _, err := store.Emit(final); !errors.Is(err, ErrIntentConflict) {
		t.Fatalf("payload conflict=%v", err)
	}
}

func TestTodo_MERIT_002_Security(t *testing.T) {
	c := meritCycle(t)
	partial, _, err := c.Propose("worker-1", meritDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := partial.Finalize(); !errors.Is(err, ErrCycleTransition) {
		t.Fatalf("partial finalization=%v", err)
	}
	if _, err := partial.Approve("worker-1", ApprovalReceipt{}, nil); !errors.Is(err, ErrCycleTransition) {
		t.Fatalf("unverified approval=%v", err)
	}
	r := partial.Recommendations[0]
	if _, err := partial.Approve("worker-1", ApprovalReceipt{ReceiptID: "receipt", ApproverID: "approver", RecommendationDigest: r.CanonicalDigest}, meritApprovalVerifier{reject: true}); !errors.Is(err, ErrCycleTransition) {
		t.Fatalf("rejected verifier=%v", err)
	}
	if _, err := partial.Approve("worker-1", ApprovalReceipt{ReceiptID: "receipt", ApproverID: "approver", RecommendationDigest: "sha256:stale"}, meritApprovalVerifier{}); !errors.Is(err, ErrCycleTransition) {
		t.Fatalf("stale approval=%v", err)
	}
}

func TestTodo_MERIT_002_ApprovalBinding(t *testing.T) {
	c := meritCycle(t)
	next, rec, err := c.Propose("worker-1", meritDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	next = approveMerit(t, next, "worker-1", "approver-1")
	approved := next.Recommendations[0]
	if approved.ApprovedDigest != rec.CanonicalDigest || approved.ApprovalReceiptID == "" {
		t.Fatalf("approval binding=%#v", approved)
	}
	forged := rec
	forged.State = RecommendationApproved
	forged.CanonicalDigest = ""
	if _, err := NewMeritRecommendation(forged); !errors.Is(err, ErrInvalidRecommendation) {
		t.Fatalf("forged approval=%v", err)
	}
	if len(approved.Canonical()) == 0 {
		t.Fatal("approved canonical bytes missing")
	}
	if digest, err := approved.Digest(); err != nil || digest != approved.CanonicalDigest {
		t.Fatalf("digest=%s err=%v", digest, err)
	}
	mutated := approved
	mutated.Amount = meritDecimal(t, "9.00")
	mutated.CanonicalDigest = ""
	if _, err := NewMeritRecommendation(mutated); !errors.Is(err, ErrInvalidRecommendation) {
		t.Fatalf("approved amount remint=%v", err)
	}
	mutated = approved
	mutated.ParticipantID = "worker-2"
	mutated.CanonicalDigest = ""
	if _, err := NewMeritRecommendation(mutated); !errors.Is(err, ErrInvalidRecommendation) {
		t.Fatalf("cross-worker approval transfer=%v", err)
	}
	mutated = approved
	mutated.ApprovalReceiptID = "receipt-for-someone-else"
	mutated.CanonicalDigest = ""
	if _, err := NewMeritRecommendation(mutated); !errors.Is(err, ErrInvalidRecommendation) {
		t.Fatalf("approval receipt transfer=%v", err)
	}
	otherPay, err := values.NewMoney("999.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	mutated = approved
	mutated.BasePay = otherPay
	mutated.CanonicalDigest = ""
	if _, err := NewMeritRecommendation(mutated); !errors.Is(err, ErrInvalidRecommendation) {
		t.Fatalf("approved base-pay remint=%v", err)
	}
	mixed := mixedMeritCycle(t)
	rejected := mixed.Recommendations[1]
	rejected.ParticipantID = "worker-1"
	rejected.CanonicalDigest = ""
	if _, err := NewMeritRecommendation(rejected); !errors.Is(err, ErrInvalidRecommendation) {
		t.Fatalf("cross-worker rejection transfer=%v", err)
	}
	rejected = mixed.Recommendations[1]
	rejected.BasePay = otherPay
	rejected.CanonicalDigest = ""
	if _, err := NewMeritRecommendation(rejected); !errors.Is(err, ErrInvalidRecommendation) {
		t.Fatalf("rejected base-pay remint=%v", err)
	}
}

func TestTodo_MERIT_002_Conformance(t *testing.T) {
	final := approvedMeritCycle(t)
	var err error
	final, err = final.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewCalibrationAdjustment(CalibrationAdjustment{ParticipantID: "worker-1", From: final.Recommendations[0].Amount, To: meritDecimal(t, "6.00"), Reason: performance.CalibrationReasonEvidence, AdjusterID: "calibrator"})
	if err != nil {
		t.Fatal(err)
	}
	correction, err := final.Correct("worker-1", a)
	if err != nil {
		t.Fatal(err)
	}
	if correction.ParentDigest != final.CanonicalDigest || correction.State != CycleCalibrating {
		t.Fatalf("correction=%#v", correction)
	}
	correction = approveMerit(t, correction, "worker-1", "approver-3")
	correctedFinal, err := correction.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	if correctedFinal.Recommendations[1].ApprovedDigest != final.Recommendations[1].ApprovedDigest {
		t.Fatal("untouched approval binding was lost")
	}
	store := NewMemoryCompensationIntentStore()
	if first, err := store.Emit(final); err != nil || len(first) != 2 {
		t.Fatalf("original effects=%d err=%v", len(first), err)
	}
	fresh, err := store.Emit(correctedFinal)
	if err != nil || len(fresh) != 1 || fresh[0].ParticipantID != "worker-1" {
		t.Fatalf("correction effects=%#v err=%v", fresh, err)
	}
	if retry, err := store.Emit(correctedFinal); err != nil || len(retry) != 0 {
		t.Fatalf("correction retry=%#v err=%v", retry, err)
	}
}

func TestTodo_MERIT_002_Mutation(t *testing.T) {
	final := approvedMeritCycle(t)
	var err error
	final, err = final.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	old := final.Recommendations[0].Amount
	a, err := NewCalibrationAdjustment(CalibrationAdjustment{ParticipantID: "worker-1", From: old, To: meritDecimal(t, "6.00"), Reason: performance.CalibrationReasonEvidence, AdjusterID: "calibrator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := final.Correct("worker-1", a); err != nil {
		t.Fatal(err)
	}
	if final.Recommendations[0].Amount.String() != old.String() {
		t.Fatal("correction mutated finalized parent")
	}
	mixed := mixedMeritCycle(t)
	if mixed.Recommendations[1].State != RecommendationRejected || mixed.Recommendations[1].Amount.Sign() != 0 {
		t.Fatalf("rejected outcome=%#v", mixed.Recommendations[1])
	}
	approvedCorrection, err := NewCalibrationAdjustment(CalibrationAdjustment{ParticipantID: "worker-1", From: mixed.Recommendations[0].Amount, To: meritDecimal(t, "6.00"), Reason: performance.CalibrationReasonEvidence, AdjusterID: "calibrator"})
	if err != nil {
		t.Fatal(err)
	}
	approvedCorrected, err := mixed.Correct("worker-1", approvedCorrection)
	if err != nil {
		t.Fatal(err)
	}
	approvedCorrected = approveMerit(t, approvedCorrected, "worker-1", "approver-4")
	approvedCorrected, err = approvedCorrected.Finalize()
	if err != nil || approvedCorrected.Recommendations[1].State != RecommendationRejected {
		t.Fatalf("rejected audit lost: %#v err=%v", approvedCorrected.Recommendations[1], err)
	}
	a, err = NewCalibrationAdjustment(CalibrationAdjustment{ParticipantID: "worker-2", From: mixed.Recommendations[1].Amount, To: meritDecimal(t, "6.00"), Reason: performance.CalibrationReasonEvidence, AdjusterID: "calibrator"})
	if err != nil {
		t.Fatal(err)
	}
	corrected, err := mixed.Correct("worker-2", a)
	if err != nil {
		t.Fatal(err)
	}
	corrected = approveMerit(t, corrected, "worker-2", "approver-3")
	corrected, err = corrected.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryCompensationIntentStore()
	first, err := store.Emit(mixed)
	if err != nil || len(first) != 1 || first[0].ParticipantID != "worker-1" {
		t.Fatalf("mixed effects=%#v err=%v", first, err)
	}
	second, err := store.Emit(corrected)
	if err != nil || len(second) != 1 || second[0].ParticipantID != "worker-2" {
		t.Fatalf("corrected effects=%#v err=%v", second, err)
	}
	if retry, err := store.Emit(corrected); err != nil || len(retry) != 0 {
		t.Fatalf("corrected retry=%#v err=%v", retry, err)
	}
}
