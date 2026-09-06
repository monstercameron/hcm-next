package simcomp_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/budget"
	"github.com/monstercameron/hcm-next/internal/domains/promotion/simassign"
	"github.com/monstercameron/hcm-next/internal/domains/promotion/simcomp"
	promosnapshot "github.com/monstercameron/hcm-next/internal/domains/promotion/snapshot"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TestRequestValidateNamesTheBrokenContract proves each malformed request is
// refused for its own stated reason rather than folded into one error.
func TestRequestValidateNamesTheBrokenContract(t *testing.T) {
	t.Parallel()
	snap := newHarness(t).build(t, fixtureRequest(t))

	for _, tc := range []struct {
		name   string
		damage func(*simcomp.Request)
		want   string
	}{
		{"no proposal revision", func(r *simcomp.Request) { r.ProposalRevisionID = "" }, "proposal revision id"},
		{"no proposal digest", func(r *simcomp.Request) { r.ProposalDigest = "" }, "proposal revision id"},
		{"rate scale below money scale", func(r *simcomp.Request) { r.RateScale = 0 }, "rate scale"},
		{"no rounding mode", func(r *simcomp.Request) { r.MoneyRounding = 0 }, "rounding mode"},
		{"no days per year", func(r *simcomp.Request) { r.DaysPerYear = values.Decimal{} }, "days per year"},
		{"no pay period", func(r *simcomp.Request) { r.PayPeriod = values.PayPeriod{} }, "pay period"},
		{"no budget id", func(r *simcomp.Request) { r.BudgetID = "" }, "budget id"},
		{"no authority digest", func(r *simcomp.Request) { r.AuthorityDigest = "" }, "budget id"},
		{"expired reservation", func(r *simcomp.Request) {
			r.ReservationExpiry = mustInstant(t, "2020-01-01T00:00:00Z")
		}, "known-at horizon"},
		{"no authority decision", func(r *simcomp.Request) { r.AuthorityDecision = "" }, "source-authority decision"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := simulateRequest(t, snap)
			tc.damage(&req)
			_, err := simcomp.Simulate(req)
			if !errors.Is(err, simcomp.ErrRequestInvalid) {
				t.Fatalf("Simulate returned %v, want ErrRequestInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Simulate said %q, which does not name %q", err, tc.want)
			}
		})
	}
}

// TestSimulateTakesNoHold proves the promise the whole package rests on: the
// reservation is built and validated against BUDGET-002's own contract, and
// nothing is ever handed to a reservation store.
func TestSimulateTakesNoHold(t *testing.T) {
	t.Parallel()
	result := simulate(t, newHarness(t), nil)
	if !result.Budget.Planned {
		t.Fatal("the fixture promotion planned no reservation")
	}

	// The request the simulation built is the exact one BUDGET-002 would
	// accept: validating it here proves the plan is issuable without issuing
	// it.
	if err := result.Budget.Request.Validate(mustInstant(t, fixtureKnownAtText).Time()); err != nil {
		t.Fatalf("the planned reservation would be refused by BUDGET-002: %v", err)
	}

	// A fresh store, never touched by the simulation, still holds nothing --
	// and accepts the planned request when a caller finally chooses to issue
	// it, which is the separation the package exists to keep.
	store := budget.NewReservationStore()
	if _, ok := store.Get(result.Budget.Request.IdempotencyKey); ok {
		t.Fatal("the simulation registered a hold with a reservation store")
	}
	authority := budget.CompensationBudgetAuthority{
		BudgetID:        result.Budget.Request.BudgetID,
		Currency:        result.Budget.Request.Currency,
		Available:       result.Budget.Available.Amount(),
		AuthorityDigest: result.Budget.Request.AuthorityDigest,
	}
	if _, err := store.Reserve(result.Budget.Request, authority, mustInstant(t, fixtureKnownAtText).Time()); err != nil {
		t.Fatalf("the planned reservation is not issuable as built: %v", err)
	}
}

// TestExplainCarriesNoWithheldValue proves the explanation is safe to log: it
// reports what was refused and why, and never what it could not disclose.
func TestExplainCarriesNoWithheldValue(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	req := fixtureRequest(t)
	req.Authorization.Compensation.SubjectDisclosable = false
	req.Authorization.Compensation.SubjectDenialReason = "policy:no_compensation_disclosure"
	req.Authorization.BudgetDisclosable = false
	req.Authorization.BudgetDenialReason = "policy:no_budget_disclosure"
	partial, buildErr := promosnapshot.Build(t.Context(), h.readers(), req)
	if buildErr == nil {
		t.Fatal("a build with two withheld required inputs was accepted")
	}
	result, err := simcomp.Simulate(simulateRequest(t, partial))
	if err != nil {
		t.Fatalf("Simulate over a refused snapshot: %v", err)
	}
	explanation := result.Explain()
	for _, leaked := range []string{"93000", "98000", "50000"} {
		if strings.Contains(explanation, leaked) {
			t.Fatalf("Explain leaks the withheld amount %q:\n%s", leaked, explanation)
		}
	}
	if !strings.Contains(explanation, string(simassign.ReasonInputWithheld)) {
		t.Fatalf("Explain does not report the refusal:\n%s", explanation)
	}
	if len(result.Effects) != 0 {
		t.Fatalf("effects were proposed from two withheld inputs:\n%s", explanation)
	}
	if len(result.Refusals) != 2 {
		t.Fatalf("two withheld inputs produced %d refusal(s):\n%s", len(result.Refusals), explanation)
	}
}

// TestBudgetIsWeighedEvenWhenThePayChangeIsUnknown proves a withheld pay read
// does not silently suppress the pool observation: the budget is still
// reported, and the reason it could not be reserved against is stated.
func TestBudgetIsWeighedEvenWhenThePayChangeIsUnknown(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	req := fixtureRequest(t)
	req.Authorization.Compensation.SubjectDisclosable = false
	req.Authorization.Compensation.SubjectDenialReason = "policy:no_compensation_disclosure"
	partial, buildErr := promosnapshot.Build(t.Context(), h.readers(), req)
	if buildErr == nil {
		t.Fatal("a build with a withheld compensation read was accepted")
	}
	result, err := simcomp.Simulate(simulateRequest(t, partial))
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	if !result.Budget.Evaluated {
		t.Fatal("the pool observation was dropped because the pay read was denied")
	}
	if result.Budget.Planned {
		t.Fatal("a reservation was planned without a known pay change")
	}
	if result.Budget.Reason == "" {
		t.Fatal("the budget plan states no reason for planning nothing")
	}
	if !result.Budget.Available.Amount().IsZero() && result.Budget.Available.String() != "50000.00 USD" {
		t.Fatalf("the pool was reported as %s", result.Budget.Available)
	}
}

// TestPlannedWritesCarryTheirBaselineAndAuthority proves every projection a
// plan would consume is fully declared, since the kernel refuses a write that
// pins no baseline or records no authority decision.
func TestPlannedWritesCarryTheirBaselineAndAuthority(t *testing.T) {
	t.Parallel()
	result := simulate(t, newHarness(t), nil)
	writes := result.PlannedWrites()
	if len(writes) == 0 {
		t.Fatal("the simulation implies no planned write")
	}
	for _, write := range writes {
		if write.SourceAuthorityDecision == "" {
			t.Fatalf("planned write on %s records no authority decision", write.FieldPath)
		}
		if !write.ExpectedRevision.IsSpecified() {
			t.Fatalf("planned write on %s pins no baseline", write.FieldPath)
		}
		if write.CurrentCanonicalText == write.ProposedCanonicalText {
			t.Fatalf("planned write on %s changes nothing", write.FieldPath)
		}
	}
	if got, want := len(result.Reads()), len(result.Effects); got != want {
		t.Fatalf("projected %d baseline reads for %d effects", got, want)
	}
	for _, read := range result.Reads() {
		if err := read.ResourceKey.Validate(); err != nil {
			t.Fatalf("planned read carries an invalid resource key: %v", err)
		}
	}
	if got, want := len(result.EffectSubjects()), 2; got != want {
		t.Fatalf("the effects name %d material subjects, want %d", got, want)
	}
	if got, want := len(result.PlannedEffects()), len(result.Effects); got != want {
		t.Fatalf("projected %d planned effects for %d proposed effects", got, want)
	}
}
