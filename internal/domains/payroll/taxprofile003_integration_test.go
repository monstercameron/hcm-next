package payroll

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/payroll/calcpolicy"
	"github.com/monstercameron/hcm-next/internal/domains/taxprofile"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func taxProfile003Instant(t *testing.T, text string) values.Instant {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return values.NewInstant(parsed)
}

func taxProfile003Interval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	interval, err := values.NewOpenInstantInterval(taxProfile003Instant(t, "2026-01-01T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func taxProfile003Binding(t *testing.T) TaxPopulationInput {
	t.Helper()
	interval := taxProfile003Interval(t)
	registration, err := taxprofile.NewTaxRegistrationRevision(taxprofile.TaxRegistrationRevision{RegistrationIDRef: "reg-1", Jurisdiction: "US-CA", AuthorityRef: "authority-ca", Revision: 1, Effective: interval, KnownAt: taxProfile003Instant(t, "2026-01-01T01:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	election, err := taxprofile.NewWithholdingElectionRevision(taxprofile.WithholdingElectionRevision{ElectionID: "election-1", WorkerRef: "worker-1", Jurisdiction: "US-CA", Kind: taxprofile.ElectionStandardWithholding, FormRevisionRef: "form-w4-2026", EvidenceRef: "evidence-1", Effective: interval, KnownAt: taxProfile003Instant(t, "2026-01-02T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := taxprofile.NewWorkerTaxProfileRevision(taxprofile.WorkerTaxProfileRevision{WorkerRef: "worker-1", Revision: 1, ResidenceJurisdictions: []string{"US-CA"}, WorkJurisdictions: []string{"US-CA"}, FilingStatus: taxprofile.FilingSingle, Classification: taxprofile.ClassificationResident, ClassificationEvidenceRef: "classification-evidence", Registrations: []taxprofile.TaxRegistrationRevision{registration}, Elections: []taxprofile.WithholdingElectionRevision{election}, Effective: interval, KnownAt: taxProfile003Instant(t, "2026-01-03T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := taxprofile.BuildTaxProfileSnapshot(taxprofile.TaxProfileSnapshotRequest{Profile: profile, EmploymentRef: "employment-1", PayGroupRef: "monthly", EffectiveAsOf: taxProfile003Instant(t, "2026-06-01T00:00:00Z"), KnownAt: taxProfile003Instant(t, "2026-06-02T00:00:00Z"), FormReleaseDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111", RuleReleaseDigest: "sha256:2222222222222222222222222222222222222222222222222222222222222222", RegistrationPresence: taxprofile.PresencePresent, ElectionPresence: taxprofile.PresencePresent, ClassificationPresence: taxprofile.PresencePresent})
	if err != nil {
		t.Fatal(err)
	}
	amount, err := values.NewDecimal("125.00", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	input, err := calcpolicy.NewPinnedTaxInput(snapshot, calcpolicy.Input{Kind: calcpolicy.KindTax, Currency: "USD", Amount: amount})
	if err != nil {
		t.Fatal(err)
	}
	return TaxPopulationInput{WorkerRef: "worker-1", EmploymentRef: "employment-1", PayGroupRef: "monthly", Snapshot: snapshot, Input: input}
}

func TestTodo_TAXPROFILE_003_CrossPackagePayrollBinding(t *testing.T) {
	binding := taxProfile003Binding(t)
	rule := calcpolicy.CalculationRule{Scale: 2, Rounding: values.RoundingHalfEven, Allocation: calcpolicy.AllocationLargestRemainder, Negative: calcpolicy.NegativeReject, Zero: calcpolicy.ZeroAllow}
	policy, err := calcpolicy.NewPolicy(calcpolicy.Policy{ID: "tax-policy", Version: "tax-policy/v1", Revision: 1, Rules: map[calcpolicy.CalculationKind]calcpolicy.CalculationRule{calcpolicy.KindPayroll: rule, calcpolicy.KindTax: rule, calcpolicy.KindDeduction: rule, calcpolicy.KindRate: rule}, CurrencyRules: map[string]calcpolicy.CurrencyRule{"USD": {Code: "USD", Scale: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := calcpolicy.Calculate(policy, binding.Input)
	if err != nil || receipt.InputDigest != binding.Input.CanonicalDigest || receipt.Output.Amount.String() != "125.00" {
		t.Fatalf("pinned tax calculation = %+v, %v", receipt, err)
	}
	digest, err := TaxPopulationInputDigest([]TaxPopulationInput{binding})
	if err != nil {
		t.Fatal(err)
	}
	run, err := NewTaxPayrollRun("tax-run", "monthly", PeriodRef{ID: "period", Version: "v1", Digest: "sha256:period"}, PopulationBindingRef{DefinitionID: "population", RevisionVersion: "v1", Digest: "sha256:population"}, digest)
	if err != nil {
		t.Fatal(err)
	}
	population, err := FreezePopulation(run, taxProfile003Instant(t, "2026-06-01T00:00:00Z"), []PopulationMember{{WorkerRef: "worker-1", EmploymentRef: "employment-1", PayGroupRef: "monthly"}}, LateEntryPolicyExplicitAmendment)
	if err != nil {
		t.Fatal(err)
	}
	calculated, err := CalculateAgainstPopulationWithTaxInputs(run, population, []TaxPopulationInput{binding})
	if err != nil || calculated.State != PayrollRunStateCalculated {
		t.Fatalf("typed tax calculation = %+v, %v", calculated, err)
	}
	if _, err := run.Calculate(digest); !errors.Is(err, ErrInvalidPayrollRun) {
		t.Fatalf("generic string route bypassed tax bindings: %v", err)
	}
	if _, err := CalculateAgainstPopulation(run, population, digest); !errors.Is(err, ErrTaxPopulationBinding) {
		t.Fatalf("generic population route bypassed tax bindings: %v", err)
	}
	reminted := run
	reminted.CanonicalDigest = ""
	if _, err := CalculateAgainstPopulation(reminted, population, digest); !errors.Is(err, ErrTaxPopulationBinding) {
		t.Fatalf("clearing the public digest reminted a tax run as non-tax: %v", err)
	}

	wrongEmployment := binding
	wrongEmployment.EmploymentRef = "employment-other"
	if _, err := CalculateAgainstPopulationWithTaxInputs(run, population, []TaxPopulationInput{wrongEmployment}); !errors.Is(err, ErrTaxPopulationBinding) {
		t.Fatalf("employment mismatch accepted: %v", err)
	}
	wrongPayGroup := binding
	wrongPayGroup.PayGroupRef = "weekly"
	if _, err := CalculateAgainstPopulationWithTaxInputs(run, population, []TaxPopulationInput{wrongPayGroup}); !errors.Is(err, ErrTaxPopulationBinding) {
		t.Fatalf("pay-group mismatch accepted: %v", err)
	}
	mutated := binding
	mutated.Snapshot.ProfileRevision++
	if _, err := CalculateAgainstPopulationWithTaxInputs(run, population, []TaxPopulationInput{mutated}); !errors.Is(err, ErrTaxPopulationBinding) {
		t.Fatalf("mutated snapshot accepted: %v", err)
	}
	if _, err := CalculateAgainstPopulationWithTaxInputs(run, population, nil); !errors.Is(err, ErrTaxPopulationBinding) {
		t.Fatalf("incomplete frozen member set accepted: %v", err)
	}

	freeze := func(t *testing.T, candidate PayrollRun, payGroup string) FrozenPopulation {
		t.Helper()
		frozen, freezeErr := FreezePopulation(candidate, taxProfile003Instant(t, "2026-06-01T00:00:00Z"), []PopulationMember{{WorkerRef: "worker-1", EmploymentRef: "employment-1", PayGroupRef: payGroup}}, LateEntryPolicyExplicitAmendment)
		if freezeErr != nil {
			t.Fatal(freezeErr)
		}
		return frozen
	}
	otherRunID, err := NewTaxPayrollRun("other-tax-run", "monthly", run.Period, run.Population, digest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CalculateAgainstPopulationWithTaxInputs(run, freeze(t, otherRunID, "monthly"), []TaxPopulationInput{binding}); !errors.Is(err, ErrPopulationBindingMismatch) {
		t.Fatalf("different run population accepted: %v", err)
	}
	otherBinding := PopulationBindingRef{DefinitionID: "other-population", RevisionVersion: "v1", Digest: "sha256:other-population"}
	otherPopulation, err := NewTaxPayrollRun(run.RunID, "monthly", run.Period, otherBinding, digest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CalculateAgainstPopulationWithTaxInputs(run, freeze(t, otherPopulation, "monthly"), []TaxPopulationInput{binding}); !errors.Is(err, ErrPopulationBindingMismatch) {
		t.Fatalf("different population binding accepted: %v", err)
	}
	weeklyRun, err := NewTaxPayrollRun(run.RunID, "weekly", run.Period, run.Population, digest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CalculateAgainstPopulationWithTaxInputs(run, freeze(t, weeklyRun, "weekly"), []TaxPopulationInput{binding}); !errors.Is(err, ErrPopulationBindingMismatch) {
		t.Fatalf("different pay-group population accepted: %v", err)
	}
	nonTaxRun, err := NewPayrollRun("plain-run", "monthly", run.Period, run.Population, digest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CalculateAgainstPopulationWithTaxInputs(nonTaxRun, freeze(t, nonTaxRun, "monthly"), []TaxPopulationInput{binding}); !errors.Is(err, ErrTaxPopulationBinding) {
		t.Fatalf("non-tax run acquired typed tax authority: %v", err)
	}
	if _, err := CalculateAgainstPopulationWithTaxInputs(calculated, population, []TaxPopulationInput{binding}); !errors.Is(err, ErrPopulationUnfrozen) {
		t.Fatalf("non-draft tax run recalculated: %v", err)
	}
}
