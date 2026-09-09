package simcomp_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/budget"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simassign"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcomp"
	promosnapshot "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
)

// TestTodo_PROMO_003 is the primary: over the harborcare-demo Promotion
// snapshot, the simulation states the base-pay change in exact decimal on the
// annualized basis the band positions were decided on, places the desired pay
// in the band with a typed class and the approval it triggers, prorates the
// change across the pay period the effective date falls in, and weighs the
// reservation delta against the observed pool -- proposing two typed effects
// and reserving nothing.
func TestTodo_PROMO_003(t *testing.T) {
	t.Parallel()
	result := simulate(t, newHarness(t), nil)

	if !result.Executable() {
		t.Fatalf("the fixture promotion was refused: %v", result.Err())
	}
	if got, want := len(result.Effects), 2; got != want {
		t.Fatalf("proposed %d effects, want %d:\n%s", got, want, result.Explain())
	}
	for i, want := range simcomp.EffectKinds() {
		if got := result.Effects[i].Kind; got != want {
			t.Fatalf("effect %d is %s, want %s", i, got, want)
		}
	}

	// The two annualized amounts are the snapshot's own COMP-002 figures, and
	// the delta between them is exact.
	if !result.BasePay.Evaluated {
		t.Fatal("the base-pay change was not evaluated")
	}
	if got, want := result.BasePay.CurrentAnnualized.String(), "93000.00 USD"; got != want {
		t.Fatalf("current annualized is %s, want %s", got, want)
	}
	if got, want := result.BasePay.DesiredAnnualized.String(), "98000.00 USD"; got != want {
		t.Fatalf("desired annualized is %s, want %s", got, want)
	}
	if got, want := result.BasePay.Delta.String(), "5000.00 USD"; got != want {
		t.Fatalf("annualized delta is %s, want %s", got, want)
	}
	if got, want := result.BasePay.Direction, simcomp.DirectionIncrease; got != want {
		t.Fatalf("direction is %s, want %s", got, want)
	}

	// The band finding is typed, and an in-band promotion triggers no extra
	// approval on top of the base graph.
	if !result.Band.Evaluated {
		t.Fatal("the band finding was not evaluated")
	}
	if got, want := result.Band.DesiredClass, rewards.PositionClassWithin.String(); got != want {
		t.Fatalf("desired band class is %s, want %s", got, want)
	}
	if result.Band.ApprovalRequirementID != "" {
		t.Fatalf("an in-band promotion triggered %s", result.Band.ApprovalRequirementID)
	}
	if result.Band.BandID == "" || result.Band.BandVersion == "" {
		t.Fatal("the band finding names no band")
	}

	// Proration splits the declared period at the effective date, and both
	// sides are non-empty so the split is actually exercised.
	p := result.Proration
	if !p.Evaluated {
		t.Fatal("proration was not evaluated")
	}
	if got, want := p.DaysInPeriod, 31; got != want {
		t.Fatalf("the period is %d days, want %d", got, want)
	}
	if got, want := p.DaysAtPriorRate, 16; got != want {
		t.Fatalf("%d day(s) at the prior rate, want %d", got, want)
	}
	if got, want := p.DaysAtNewRate, 15; got != want {
		t.Fatalf("%d day(s) at the new rate, want %d", got, want)
	}
	if p.DaysAtPriorRate+p.DaysAtNewRate != p.DaysInPeriod {
		t.Fatalf("the split does not cover the period: %d + %d != %d",
			p.DaysAtPriorRate, p.DaysAtNewRate, p.DaysInPeriod)
	}
	sum, err := p.PriorPortion.Add(p.NewPortion)
	if err != nil {
		t.Fatalf("add the two portions: %v", err)
	}
	if sum.String() != p.PeriodAmount.String() {
		t.Fatalf("the portions sum to %s but the period amount is %s", sum, p.PeriodAmount)
	}
	if p.PeriodDelta.Amount().Sign() <= 0 {
		t.Fatalf("a raise produced a non-positive period delta %s", p.PeriodDelta)
	}

	// The budget plan states the pool exactly and builds the reservation that
	// would be issued, without issuing it.
	if !result.Budget.Evaluated || !result.Budget.Planned {
		t.Fatalf("the budget reservation was not planned:\n%s", result.Budget.Explain())
	}
	if got, want := result.Budget.Available.String(), "50000.00 USD"; got != want {
		t.Fatalf("available pool is %s, want %s", got, want)
	}
	if got, want := result.Budget.Amount.String(), "5000.00 USD"; got != want {
		t.Fatalf("reservation amount is %s, want %s", got, want)
	}
	if got, want := result.Budget.Remaining.String(), "45000.00 USD"; got != want {
		t.Fatalf("remaining pool is %s, want %s", got, want)
	}
	if !result.Budget.Sufficient {
		t.Fatal("a 5,000 reservation against a 50,000 pool was reported insufficient")
	}
	if got := result.Budget.Request.BudgetID; got != fixtureBudgetID {
		t.Fatalf("the reservation names budget %q, want %q", got, fixtureBudgetID)
	}
	if result.Budget.Request.IdempotencyKey == "" {
		t.Fatal("the reservation carries no idempotency key")
	}

	// Every effect cites the snapshot inputs it derived from, and the pool
	// reservation is an external effect because the pool is an external
	// observation.
	declared := map[string]bool{}
	for _, name := range promosnapshot.InputNames() {
		declared[name] = true
	}
	for _, effect := range result.Effects {
		for _, name := range effect.DerivedFrom {
			if !declared[name] {
				t.Fatalf("effect %s cites undeclared input %q", effect.Kind, name)
			}
		}
		if effect.CompensationRef == "" || effect.ObservationRef == "" {
			t.Fatalf("effect %s declares no compensation or observation", effect.Kind)
		}
	}
	revision, ok := result.Lookup(simcomp.EffectCompensationRevision)
	if !ok {
		t.Fatal("no compensation revision was proposed")
	}
	if !revision.Local || revision.Reversibility != simassign.Reversible {
		t.Fatalf("the compensation revision is local=%t/%s, want local REVERSIBLE",
			revision.Local, revision.Reversibility)
	}
	reservation, ok := result.Lookup(simcomp.EffectBudgetReservation)
	if !ok {
		t.Fatal("no budget reservation was proposed")
	}
	if reservation.Local {
		t.Fatal("a reservation against an external pool observation was placed inside the local commit")
	}
	if reservation.Reversibility != simassign.Compensatable {
		t.Fatalf("the reservation is %s, want COMPENSATABLE", reservation.Reversibility)
	}
	if reservation.CompensationRef != budget.ReleaseCompensationBudgetIntentType {
		t.Fatalf("the reservation compensates with %q, want %q",
			reservation.CompensationRef, budget.ReleaseCompensationBudgetIntentType)
	}
	if got := len(result.OutboxEffects()); got != 1 {
		t.Fatalf("projected %d outbox effect(s), want 1", got)
	}
	if got := len(result.Compensations()); got != 1 {
		t.Fatalf("projected %d compensation(s) for 1 outbox effect", got)
	}
	if got := len(result.Observations(mustInstant(t, fixtureReservationExpiry))); got != 1 {
		t.Fatalf("projected %d observation(s) for 1 outbox effect", got)
	}
}

// TestTodo_PROMO_003_Property proves the determinism the artifact rests on:
// identical snapshots and identical conventions yield identical digests, and
// any material change to either yields a different one.
func TestTodo_PROMO_003_Property(t *testing.T) {
	t.Parallel()
	base := simulate(t, newHarness(t), nil)

	for i := range 8 {
		again := simulate(t, newHarness(t), nil)
		if again.Digest != base.Digest {
			t.Fatalf("run %d produced digest %s, want %s", i, again.Digest, base.Digest)
		}
		if again.SnapshotDigest != base.SnapshotDigest {
			t.Fatalf("run %d read a different snapshot: %s", i, again.SnapshotDigest)
		}
	}

	// A different convention is a different simulation, even over the same
	// snapshot: that is the point of declaring it.
	conv := simulate(t, newHarness(t), func(r *simcomp.Request) {
		r.DaysPerYear = mustDecimal(t, "360.0000", 4)
	})
	if conv.Digest == base.Digest {
		t.Fatal("changing the days-per-year divisor did not change the result digest")
	}
	if conv.Proration.PriorDailyRate.String() == base.Proration.PriorDailyRate.String() {
		t.Fatal("changing the days-per-year divisor did not change the daily rate")
	}

	// A different snapshot is a different simulation.
	h := newHarness(t)
	h.Budget.ref.AvailableQuantity = mustDecimal(t, "40000.00", 2)
	shifted := simulate(t, h, nil)
	if shifted.SnapshotDigest == base.SnapshotDigest {
		t.Fatal("a changed pool did not change the snapshot digest")
	}
	if shifted.Digest == base.Digest {
		t.Fatal("a changed snapshot did not change the result digest")
	}
}

// TestTodo_PROMO_003_Race proves the function holds no state: many goroutines
// simulating the same request agree on the digest, byte for byte. It is a
// purity assertion rather than a detector run, because this lane's toolchain
// does not build with -race.
func TestTodo_PROMO_003_Race(t *testing.T) {
	t.Parallel()
	snap := newHarness(t).build(t, fixtureRequest(t))
	req := simulateRequest(t, snap)

	const runs = 32
	digests := make(chan string, runs)
	for range runs {
		go func() {
			result, err := simcomp.Simulate(req)
			if err != nil {
				digests <- "error: " + err.Error()
				return
			}
			digests <- result.Digest + "|" + result.Budget.Request.IdempotencyKey
		}()
	}
	first := <-digests
	for range runs - 1 {
		if got := <-digests; got != first {
			t.Fatalf("concurrent simulations disagreed: %s vs %s", got, first)
		}
	}
	if strings.HasPrefix(first, "error:") {
		t.Fatalf("concurrent simulation failed: %s", first)
	}
}

// TestTodo_PROMO_003_Security proves the two disclosure rules: a caller cannot
// supply a current value, and a withheld compensation input refuses the effect
// that needed it instead of producing a number.
func TestTodo_PROMO_003_Security(t *testing.T) {
	t.Parallel()

	// A hand-built snapshot -- the only shape a caller-supplied current pay can
	// take -- is refused, digest and all.
	forged := promosnapshot.PromotionInputSnapshot{
		Tenant:      newHarness(t).build(t, fixtureRequest(t)).Tenant,
		EffectiveOn: mustLocalDate(t, fixtureEffectiveOn),
		Digest:      "sha256:whatever-the-caller-says",
	}
	if _, err := simcomp.Simulate(simulateRequest(t, forged)); !errors.Is(err, simcomp.ErrSnapshotUntrusted) {
		t.Fatalf("a hand-built snapshot was accepted or refused for the wrong reason: %v", err)
	}

	// A real snapshot whose digest was edited is refused too.
	tampered := newHarness(t).build(t, fixtureRequest(t))
	tampered.Digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := simcomp.Simulate(simulateRequest(t, tampered)); !errors.Is(err, simcomp.ErrSnapshotUntrusted) {
		t.Fatalf("a re-digested snapshot was accepted: %v", err)
	}

	// Mutating the copy the accessor hands back changes nothing.
	snap := newHarness(t).build(t, fixtureRequest(t))
	stolen := snap.Inputs()
	for i := range stolen {
		stolen[i].CanonicalText = "annualized=999999.00 USD"
	}
	after, err := simcomp.Simulate(simulateRequest(t, snap))
	if err != nil {
		t.Fatalf("Simulate after tampering with the returned slice: %v", err)
	}
	if got := after.BasePay.DesiredAnnualized.String(); got != "98000.00 USD" {
		t.Fatalf("the desired pay came from the caller's copy: %s", got)
	}

	// A withheld compensation read refuses the compensation revision by name,
	// and no number is invented in its place.
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
		t.Fatalf("Simulate over a refused snapshot: %v", err)
	}
	if result.Executable() {
		t.Fatal("a promotion whose pay was withheld reported itself executable")
	}
	if result.BasePay.Evaluated {
		t.Fatal("a base-pay change was computed from a withheld read")
	}
	if _, ok := result.Lookup(simcomp.EffectCompensationRevision); ok {
		t.Fatal("a compensation revision was proposed from a withheld read")
	}
	var refusal simassign.Refusal
	if !errors.As(result.Err(), &refusal) {
		t.Fatalf("the refusal is not typed: %v", result.Err())
	}
	if refusal.Reason != simassign.ReasonInputWithheld {
		t.Fatalf("the refusal reason is %s, want %s", refusal.Reason, simassign.ReasonInputWithheld)
	}
	if !strings.HasPrefix(refusal.InputName, "promotion.pay_band_position") {
		t.Fatalf("the refusal names %q, want a pay-band input", refusal.InputName)
	}
	if strings.Contains(result.Explain(), "93000") {
		t.Fatalf("the explanation leaks a withheld amount:\n%s", result.Explain())
	}
}

// TestTodo_PROMO_003_Mutation kills the mutants that would make the arithmetic
// look right while being wrong: a pool that cannot fund the change treated as
// if it could, a band violation that triggers no approval, a raise reserved
// against a pool in another currency, and an effective date outside the period
// prorated anyway.
func TestTodo_PROMO_003_Mutation(t *testing.T) {
	t.Parallel()

	t.Run("an insufficient pool refuses the reservation", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.Budget.ref.AvailableQuantity = mustDecimal(t, "1000.00", 2)
		result := simulate(t, h, nil)
		if result.Budget.Sufficient {
			t.Fatal("a 5,000 reservation against a 1,000 pool was reported sufficient")
		}
		if result.Budget.Planned {
			t.Fatal("a reservation was planned against an insufficient pool")
		}
		if _, ok := result.Lookup(simcomp.EffectBudgetReservation); ok {
			t.Fatal("a reservation effect was proposed against an insufficient pool")
		}
		if !hasRefusal(result, simcomp.ReasonInsufficientBudget) {
			t.Fatalf("no insufficient-budget refusal:\n%s", result.Explain())
		}
		if result.Executable() {
			t.Fatal("an unfundable promotion reported itself executable")
		}
	})

	t.Run("pay above the band requires the finance partner", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.Budget.ref.AvailableQuantity = mustDecimal(t, "500000.00", 2)
		snap := h.build(t, aboveBandRequest(t))
		result, err := simcomp.Simulate(simulateRequest(t, snap))
		if err != nil {
			t.Fatalf("Simulate: %v", err)
		}
		if got, want := result.Band.DesiredClass, rewards.PositionClassAbove.String(); got != want {
			t.Fatalf("desired band class is %s, want %s", got, want)
		}
		if got, want := result.Band.ApprovalRequirementID, simcomp.ApprovalFinancePartner; got != want {
			t.Fatalf("above-band pay triggers %q, want %q", got, want)
		}
		if len(result.RequiredApprovals) != 1 || result.RequiredApprovals[0] != simcomp.ApprovalFinancePartner {
			t.Fatalf("required approvals are %v", result.RequiredApprovals)
		}
	})

	t.Run("pay below the band requires the compensation partner", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		snap := h.build(t, belowBandRequest(t))
		result, err := simcomp.Simulate(simulateRequest(t, snap))
		if err != nil {
			t.Fatalf("Simulate: %v", err)
		}
		if got, want := result.Band.DesiredClass, rewards.PositionClassBelow.String(); got != want {
			t.Fatalf("desired band class is %s, want %s", got, want)
		}
		if got, want := result.Band.ApprovalRequirementID, simcomp.ApprovalCompensationPartner; got != want {
			t.Fatalf("below-band pay triggers %q, want %q", got, want)
		}
	})

	t.Run("a pool in another currency refuses the reservation", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.Budget.ref.Currency = "EUR"
		result := simulate(t, h, nil)
		if !hasRefusal(result, simcomp.ReasonCurrencyMismatch) {
			t.Fatalf("no currency refusal:\n%s", result.Explain())
		}
		if result.Budget.Planned {
			t.Fatal("a reservation was planned across a currency boundary")
		}
	})

	t.Run("an effective date outside the pay period is not prorated", func(t *testing.T) {
		t.Parallel()
		result := simulate(t, newHarness(t), func(r *simcomp.Request) {
			r.PayPeriod = payPeriod(t, "2026-07A", "2026-07-01", "2026-07-16", 13)
		})
		if result.Proration.Evaluated {
			t.Fatal("a period that does not contain the effective date was prorated anyway")
		}
		if !hasRefusal(result, simcomp.ReasonEffectiveDateOutsidePayPeriod) {
			t.Fatalf("no out-of-period refusal:\n%s", result.Explain())
		}
	})

	t.Run("a change that costs nothing reserves nothing", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		snap := h.build(t, sameBasePayRequest(t))
		result, err := simcomp.Simulate(simulateRequest(t, snap))
		if err != nil {
			t.Fatalf("Simulate: %v", err)
		}
		if got, want := result.BasePay.Direction, simcomp.DirectionUnchanged; got != want {
			t.Fatalf("direction is %s, want %s", got, want)
		}
		if result.Budget.Planned {
			t.Fatal("a reservation was planned for a promotion that costs nothing")
		}
		if !result.Executable() {
			t.Fatalf("a zero-cost promotion was refused: %v", result.Err())
		}
		if result.Proration.PeriodDelta.Amount().Sign() != 0 {
			t.Fatalf("a zero-cost promotion produced period delta %s", result.Proration.PeriodDelta)
		}
	})
}
