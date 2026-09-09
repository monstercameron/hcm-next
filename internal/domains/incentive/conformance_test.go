package incentive

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func conformanceDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func conformanceMoney(t *testing.T, text string) values.Money {
	t.Helper()
	m, err := values.NewMoney(text, "USD", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func conformanceAward(t *testing.T, amount string, state AwardState, revision, supersedes uint64) AwardCalculation {
	t.Helper()
	a, err := NewAwardCalculation(AwardCalculation{CalculationID: "award", WorkerRef: "worker", PlanDigest: "plan-digest", PlanRevision: 1, PeriodRef: "P1", EligibilityRef: "eligibility", FormulaRef: "formula", Inputs: []AwardInput{{Name: "sales", MeasureID: "sales", ObservationDigest: "obs-digest", Watermark: "wm", Attainment: conformanceDecimal(t, "1.00"), Target: conformanceDecimal(t, "1.00"), Weight: conformanceDecimal(t, "100.00")}}, Amount: conformanceDecimal(t, amount), Currency: "USD", State: AwardCalculated, Revision: revision, SupersedesRevision: supersedes})
	if err != nil {
		t.Fatal(err)
	}
	if state == AwardApproved || state == AwardFinalized {
		a, err = a.Approve("approval", exactApprovalVerifier{claim: a.approvalClaim(), ref: "approval"})
	}
	if err == nil && state == AwardFinalized {
		a, err = a.Finalize("finalization")
	}
	if err != nil {
		t.Fatal(err)
	}
	return a
}

type exactApprovalVerifier struct {
	claim ApprovalClaim
	ref   string
}

func (v exactApprovalVerifier) VerifyApproval(got ApprovalClaim, ref string) error {
	if ref != v.ref || got.CalculationID != v.claim.CalculationID || got.WorkerRef != v.claim.WorkerRef || got.PeriodRef != v.claim.PeriodRef || got.Currency != v.claim.Currency || got.AwardDigest != v.claim.AwardDigest || got.AwardRevision != v.claim.AwardRevision || !got.Amount.Equal(v.claim.Amount) {
		return errors.New("receipt mismatch")
	}
	return nil
}
func verifierForApproved(a AwardCalculation) exactApprovalVerifier {
	return exactApprovalVerifier{ref: a.ApprovalRef, claim: a.approvalClaim()}
}

func TestIncentiveConformanceHandlesRestatementClawbackAndPayrollWithoutRewrite(t *testing.T) {
	prior := conformanceAward(t, "100.00", AwardFinalized, 1, 0)
	successor := conformanceAward(t, "60.00", AwardCalculated, 4, 3)
	r := Restatement{RestatementID: "restatement-1", Prior: prior, Successor: successor, Reason: "restated sales", EvidenceRef: "evidence-1"}
	result, err := r.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if result.Delta.String() != "-40.00 USD" || result.PriorDigest != prior.Digest || result.Successor.Digest != successor.Digest {
		t.Fatalf("restatement result = %#v", result)
	}
	if prior.Amount.String() != "100.00" {
		t.Fatal("restatement rewrote prior award")
	}
	result.Successor.Inputs[0].Name = "mutated"
	if successor.Inputs[0].Name != "sales" {
		t.Fatal("restatement result aliases the successor input slice")
	}
	clawback := ClawbackCalculation{ClawbackID: "clawback-1", Award: prior, Rule: ClawbackRule{ID: "rule", Version: "1", Trigger: ClawbackMisstatement, FormulaRef: "recover", EvidenceRequired: true, EvidenceRef: "policy"}, Amount: conformanceMoney(t, "40.00"), RecoveredToDate: conformanceMoney(t, "0.00"), EvidenceRef: "evidence-1"}
	if err := clawback.Validate(); err != nil {
		t.Fatal(err)
	}
	payroll, err := PayrollInputForAward(prior, verifierForApproved(prior))
	if err != nil {
		t.Fatal(err)
	}
	if err := payroll.Validate(); err != nil || payroll.Amount.String() != "100.00 USD" {
		t.Fatalf("payroll = %#v err=%v", payroll, err)
	}
}

func TestTodo_INCENTIVE_002_Property(t *testing.T) {
	p := conformanceAward(t, "100.00", AwardFinalized, 1, 0)
	n := conformanceAward(t, "110.00", AwardCalculated, 3, 1)
	if _, err := (Restatement{RestatementID: "r", Prior: p, Successor: n, Reason: "x", EvidenceRef: "e"}).Apply(); !errors.Is(err, ErrInvalidRestatement) {
		t.Fatalf("invalid lineage error=%v", err)
	}
	n1 := conformanceAward(t, "60.00", AwardCalculated, p.Revision+1, p.Revision)
	r1, err := (Restatement{RestatementID: "r1", Prior: p, Successor: n1, Reason: "restated sales", EvidenceRef: "e1"}).Apply()
	if err != nil || r1.Delta.String() != "-40.00 USD" {
		t.Fatalf("first restatement=%#v err=%v", r1, err)
	}
	n1Final := conformanceAward(t, "60.00", AwardFinalized, p.Revision+1, p.Revision)
	n2 := conformanceAward(t, "80.00", AwardCalculated, n1Final.Revision+1, n1Final.Revision)
	r2, err := (Restatement{RestatementID: "r2", Prior: n1Final, Successor: n2, Reason: "second correction", EvidenceRef: "e2"}).Apply()
	if err != nil || r2.Delta.String() != "20.00 USD" {
		t.Fatalf("second restatement=%#v err=%v", r2, err)
	}
	base, _ := values.NewMoneyFromDecimal(p.Amount, p.Currency)
	conserved, err := base.Add(r1.Delta)
	if err != nil {
		t.Fatal(err)
	}
	conserved, err = conserved.Add(r2.Delta)
	if err != nil {
		t.Fatal(err)
	}
	wantFinal, _ := values.NewMoneyFromDecimal(n2.Amount, n2.Currency)
	if cmp, err := conserved.Cmp(wantFinal); err != nil || cmp != 0 {
		t.Fatalf("exact conservation got=%s want=%s err=%v", conserved, wantFinal, err)
	}
	c := ClawbackCalculation{ClawbackID: "c", Award: p, Rule: ClawbackRule{ID: "rule", Version: "1", Trigger: ClawbackMisstatement, FormulaRef: "recover"}, Amount: conformanceMoney(t, "40.00"), RecoveredToDate: conformanceMoney(t, "0.00"), EvidenceRef: "e"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c2 := ClawbackCalculation{ClawbackID: "c2", Award: p, Rule: c.Rule, Amount: conformanceMoney(t, "30.00"), RecoveredToDate: conformanceMoney(t, "40.00"), PriorClawbackDigests: []string{"sha256:c"}, EvidenceRef: "e2"}
	if err := c2.Validate(); err != nil {
		t.Fatal(err)
	}
	c3 := ClawbackCalculation{ClawbackID: "c3", Award: p, Rule: c.Rule, Amount: conformanceMoney(t, "31.00"), RecoveredToDate: conformanceMoney(t, "70.00"), PriorClawbackDigests: []string{"sha256:c", "sha256:c2"}, EvidenceRef: "e3"}
	if err := c3.Validate(); !errors.Is(err, ErrInvalidClawback) {
		t.Fatalf("cumulative over-recovery accepted: %v", err)
	}
	badLineage := n2
	badLineage.SupersedesRevision--
	if _, err := (Restatement{RestatementID: "bad", Prior: n1Final, Successor: badLineage, Reason: "x", EvidenceRef: "e"}).Apply(); !errors.Is(err, ErrInvalidRestatement) {
		t.Fatalf("bad successor lineage accepted: %v", err)
	}
}

func TestTodo_INCENTIVE_002_Golden(t *testing.T) {
	p := conformanceAward(t, "100.00", AwardFinalized, 1, 0)
	i, err := PayrollInputForAward(p, verifierForApproved(p))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := i.InputID+"|"+i.Amount.String()+"|"+i.SourceKind+"|"+i.ApprovalRef, "payroll:sha256:fe7f5e22547d3af1a1ef76d21288ee6b629c4309d66bb76a433fbeb43fab519a|100.00 USD|AWARD|approval"; got != want {
		t.Fatalf("payroll golden = %q, want %q", got, want)
	}
}

func TestTodo_INCENTIVE_002_Race(t *testing.T) {
	internal := AttainmentObservation{ObservationID: "obs", PlanID: "plan", PlanRevision: 1, MeasureID: "sales", WorkerRef: "worker", Value: conformanceDecimal(t, "100.00"), AsOfEffective: mustDateForConformance(t, "2026-06-30"), AsKnownAt: knownAtForConformance(t), SourceRef: "internal", Watermark: "wm-internal"}
	internal, _ = NewAttainmentObservation(internal)
	const workers = 32
	results := make(chan MeasureReconciliation, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := ReconcileExternalMeasure(internal, ExternalMeasureObservation{SourceRef: "crm", Watermark: "wm-external", ObservationDigest: "external-digest", Value: conformanceDecimal(t, "99.00")})
			results <- got
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for got := range results {
		if got.Status != ReconciliationRepairRequired || got.InternalDigest != internal.Digest {
			t.Fatalf("reconciliation=%#v", got)
		}
	}
}

func TestTodo_INCENTIVE_002_Fault(t *testing.T) {
	p := conformanceAward(t, "100.00", AwardCalculated, 1, 0)
	if _, err := PayrollInputForAward(p, exactApprovalVerifier{}); !errors.Is(err, ErrInvalidPayroll) {
		t.Fatalf("unapproved payroll error=%v", err)
	}
	n := conformanceAward(t, "90.00", AwardCalculated, 2, 1)
	if _, err := (Restatement{RestatementID: "r", Prior: p, Successor: n, Reason: "correction", EvidenceRef: "e"}).Apply(); !errors.Is(err, ErrInvalidRestatement) {
		t.Fatalf("unfinalized prior accepted: %v", err)
	}
}
func TestTodo_INCENTIVE_002_Security(t *testing.T) {
	p := conformanceAward(t, "100.00", AwardFinalized, 1, 0)
	r, err := ReconcileExternalMeasure(mustObservationForConformance(t), ExternalMeasureObservation{SourceRef: "crm", Watermark: "wm", ObservationDigest: "external", Value: conformanceDecimal(t, "100.00")})
	if err != nil || r.Status != Reconciled || r.InternalDigest == "" {
		t.Fatalf("reconciliation=%#v err=%v", r, err)
	}
	internal := mustObservationForConformance(t)
	before := internal.Digest
	_, err = ReconcileExternalMeasure(internal, ExternalMeasureObservation{SourceRef: "crm", Watermark: "new", ObservationDigest: "external-new", Value: conformanceDecimal(t, "999.00")})
	if err != nil || internal.Digest != before || internal.Value.String() != "100.00" {
		t.Fatalf("external observation overwrote internal truth: %#v err=%v", internal, err)
	}
	forged := PayrollInput{InputID: "forged", WorkerRef: p.WorkerRef, PeriodRef: p.PeriodRef, Amount: conformanceMoney(t, "100.00"), SourceDigest: p.Digest, SourceKind: "AWARD", ApprovalRef: "approval"}
	if err := forged.Validate(); !errors.Is(err, ErrInvalidPayroll) {
		t.Fatalf("forged payroll authority accepted: %v", err)
	}
	if _, err := NewAwardCalculation(AwardCalculation{CalculationID: "forged", WorkerRef: "worker", PlanDigest: "plan", PlanRevision: 1, PeriodRef: "P1", EligibilityRef: "e", FormulaRef: "f", Inputs: p.Inputs, Amount: conformanceDecimal(t, "1.00"), Currency: "USD", State: AwardApproved, ApprovalRef: "forged", Revision: 1}); !errors.Is(err, ErrInvalidAward) {
		t.Fatalf("forged approval accepted: %v", err)
	}
	calculated := conformanceAward(t, "100.00", AwardCalculated, 1, 0)
	if _, err := calculated.Approve("forged", exactApprovalVerifier{claim: calculated.approvalClaim(), ref: "authentic"}); !errors.Is(err, ErrApprovalRefused) {
		t.Fatalf("arbitrary approval ref accepted: %v", err)
	}
	forgedAward := p
	forgedAward.Amount = conformanceDecimal(t, "999.00")
	forgedAward.CanonicalDigest, forgedAward.Digest = "", ""
	if _, err := PayrollInputForAward(forgedAward, verifierForApproved(p)); !errors.Is(err, ErrInvalidPayroll) {
		t.Fatalf("mutated approved award without digest authorized payroll: %v", err)
	}
	for _, mutate := range []func(*AwardCalculation){
		func(a *AwardCalculation) { a.WorkerRef = "other-worker" },
		func(a *AwardCalculation) { a.PeriodRef = "other-period" },
		func(a *AwardCalculation) { a.Amount = conformanceDecimal(t, "999.00") },
	} {
		changed := p
		mutate(&changed)
		changed.CanonicalDigest, changed.Digest = "", ""
		changed, err = newAwardCalculation(changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := PayrollInputForAward(changed, verifierForApproved(p)); !errors.Is(err, ErrInvalidPayroll) {
			t.Fatalf("cross-subject approval receipt accepted: %v", err)
		}
	}
	stale := verifierForApproved(p)
	stale.claim.AwardRevision--
	if _, err := PayrollInputForAward(p, stale); !errors.Is(err, ErrInvalidPayroll) {
		t.Fatalf("stale approval receipt accepted: %v", err)
	}
}
func TestTodo_INCENTIVE_002_Conformance(t *testing.T) {
	if Reconciled == ReconciliationRepairRequired || ReconciliationUnknown == "" {
		t.Fatal("status vocabulary invalid")
	}
}
func TestTodo_INCENTIVE_002_Mutation(t *testing.T) {
	p := conformanceAward(t, "100.00", AwardFinalized, 1, 0)
	i, err := PayrollInputForAward(p, verifierForApproved(p))
	if err != nil {
		t.Fatal(err)
	}
	before := i.SourceDigest
	i.Amount = conformanceMoney(t, "1.00")
	if err := i.Validate(); !errors.Is(err, ErrInvalidPayroll) {
		t.Fatalf("mutated payroll payload accepted: %v", err)
	}
	p.Amount = conformanceDecimal(t, "1.00")
	if i.SourceDigest != before {
		t.Fatal("payroll input changed through award mutation")
	}
}

func mustDateForConformance(t *testing.T, s string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func knownAtForConformance(t *testing.T) values.KnownAt {
	t.Helper()
	k, err := values.NewKnownAt(values.NewInstant(mustTimeForConformance(t)))
	if err != nil {
		t.Fatal(err)
	}
	return k
}
func mustTimeForConformance(t *testing.T) (v time.Time) {
	t.Helper()
	v, _ = time.Parse(time.RFC3339, "2026-07-01T00:00:00Z")
	return
}
func mustObservationForConformance(t *testing.T) AttainmentObservation {
	o, err := NewAttainmentObservation(AttainmentObservation{ObservationID: "obs-security", PlanID: "plan", PlanRevision: 1, MeasureID: "sales", WorkerRef: "worker", Value: conformanceDecimal(t, "100.00"), AsOfEffective: mustDateForConformance(t, "2026-06-30"), AsKnownAt: knownAtForConformance(t), SourceRef: "internal", Watermark: "wm"})
	if err != nil {
		t.Fatal(err)
	}
	return o
}
