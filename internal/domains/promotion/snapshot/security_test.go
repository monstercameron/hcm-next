package snapshot_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/fixtures"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	promosnapshot "github.com/monstercameron/hcm-next/internal/domains/promotion/snapshot"
	"github.com/monstercameron/hcm-next/internal/domains/rewards"
)

// secretValues are the exact strings the fixture's protected inputs would
// contain if any of them leaked. Every security case scans the whole rendered
// snapshot, its explanation and its refusal for all of them, rather than
// checking only the field the case is about: a leak through a neighbouring
// input is the leak that gets shipped.
var secretValues = []string{"93000.00", "98000.00", "50000.00", "0.8304", "0.8750", "OPS-HRBP2"}

// TestTodo_PROMO_001_Security proves the four ways a value could escape a
// denial are all closed: through the input, through its neighbours, through
// the explanation, and through the refusal.
func TestTodo_PROMO_001_Security(t *testing.T) {
	t.Parallel()

	t.Run("a withheld subject withholds every people input and discloses nothing", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		req := fixtureRequest(t)
		req.Authorization.Worker = fixtures.WithheldSubject(req.Authorization.Worker, "policy:subject_not_disclosable")

		snap, err := promosnapshot.Build(t.Context(), h.readers(), req)
		if err == nil {
			t.Fatal("a build for a caller who may not know the subject exists was accepted")
		}
		for _, name := range []string{promosnapshot.InputSubjectWorkerFacts, promosnapshot.InputCurrentPlacement} {
			in, ok := snap.Lookup(name)
			if !ok {
				t.Fatalf("input %s was dropped rather than withheld", name)
			}
			if in.Availability != promosnapshot.AvailabilityWithheld {
				t.Fatalf("input %s is %s, want WITHHELD", name, in.Availability)
			}
			if in.CanonicalText != "" {
				t.Fatalf("withheld input %s carries the value %q", name, in.CanonicalText)
			}
			if _, disclosed := snap.Disclosed(name); disclosed {
				t.Fatalf("Disclosed handed back a withheld input %s", name)
			}
		}
		assertNoSecrets(t, snap, err)
	})

	t.Run("a denied field withholds its whole input rather than partially disclosing it", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		req := fixtureRequest(t)
		req.Authorization.Worker = fixtures.DenyFields(req.Authorization.Worker,
			"policy:pay_zone_restricted", people.FieldPayZone)

		snap, err := promosnapshot.Build(t.Context(), h.readers(), req)
		if err == nil {
			t.Fatal("a build whose placement had a denied field was accepted")
		}
		placement, ok := snap.Lookup(promosnapshot.InputCurrentPlacement)
		if !ok || placement.Availability != promosnapshot.AvailabilityWithheld {
			t.Fatalf("the placement input is %s, want WITHHELD", placement.Availability)
		}
		if strings.Contains(placement.CanonicalText, "OPS-HRBP2") {
			t.Fatalf("the placement disclosed the job code its pay-zone denial should have withheld: %q",
				placement.CanonicalText)
		}
		// The subject facts input shares the same read and is not denied, so
		// it must still be disclosed: a denial narrows, it does not blank.
		if _, disclosed := snap.Disclosed(promosnapshot.InputSubjectWorkerFacts); !disclosed {
			t.Fatal("denying one placement field also withheld the undenied subject facts")
		}
		assertNoSecrets(t, snap, err)
	})

	t.Run("a denied compensation field withholds the pay inputs", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		req := fixtureRequest(t)
		req.Authorization.Compensation.Fields = map[rewards.CompensationField]rewards.CompensationFieldRuling{
			rewards.FieldBasePay: {Effect: people.EffectDeny, Reason: "policy:base_pay_restricted"},
		}

		snap, err := promosnapshot.Build(t.Context(), h.readers(), req)
		if err == nil {
			t.Fatal("a build whose base pay was denied was accepted")
		}
		current, ok := snap.Lookup(promosnapshot.InputPayBandPositionCurrent)
		if !ok || current.Availability != promosnapshot.AvailabilityWithheld {
			t.Fatalf("the current band position is %s, want WITHHELD", current.Availability)
		}
		if got := promosnapshot.InputNameOf(err); got != promosnapshot.InputPayBandPositionCurrent {
			t.Fatalf("the refusal names %q, want the current band position (%v)", got, err)
		}
		assertNoSecrets(t, snap, err)
	})

	t.Run("a denied ratio discloses the class without the numbers", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		req := fixtureRequest(t)
		req.Authorization.PayBand.Fields = map[rewards.PositionField]rewards.PositionFieldRuling{
			rewards.PositionFieldCompaRatio:       {Effect: people.AccessDenied, Reason: "policy:ratio_restricted"},
			rewards.PositionFieldRangePenetration: {Effect: people.AccessDenied, Reason: "policy:ratio_restricted"},
		}

		snap := h.build(t, req)
		text, disclosed := snap.Disclosed(promosnapshot.InputPayBandPositionDesired)
		if !disclosed {
			t.Fatal("denying the ratios withheld the whole band position")
		}
		if !strings.Contains(text, "class=WITHIN") {
			t.Fatalf("the band position lost its class: %q", text)
		}
		for _, ratio := range []string{"0.8304", "0.8750", "0.1500", "0.0250"} {
			if strings.Contains(text, ratio) {
				t.Fatalf("the band position disclosed the denied ratio %q: %q", ratio, text)
			}
		}
	})

	t.Run("a restrictive build digests differently from a permissive one", func(t *testing.T) {
		t.Parallel()
		permissive := newHarness(t).build(t, fixtureRequest(t))

		h := newHarness(t)
		req := fixtureRequest(t)
		req.Authorization.PayBand.Fields = map[rewards.PositionField]rewards.PositionFieldRuling{
			rewards.PositionFieldCompaRatio: {Effect: people.AccessDenied, Reason: "policy:ratio_restricted"},
		}
		restrictive := h.build(t, req)

		if restrictive.Digest == permissive.Digest {
			t.Fatal("two callers who were shown different facts produced one digest")
		}
	})

	t.Run("a withheld budget reaches the kernel baseline as a forbidden field", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		req := fixtureRequest(t)
		req.Authorization.BudgetDisclosable = false
		req.Authorization.BudgetDenialReason = "policy:no_budget_disclosure"

		snap, err := promosnapshot.Build(t.Context(), h.readers(), req)
		if err == nil {
			t.Fatal("a build with a withheld required budget was accepted")
		}
		baseline := snap.BaselineSnapshot()
		found := false
		for _, field := range baseline.ForbiddenFields {
			if field == promosnapshot.InputBudgetAvailability {
				found = true
			}
		}
		if !found {
			t.Fatalf("the baseline does not forbid the withheld budget input: %v", baseline.ForbiddenFields)
		}
		for _, present := range baseline.PresentInputs {
			if present == promosnapshot.InputBudgetAvailability {
				t.Fatal("the baseline reports a withheld input as present")
			}
		}
		assertNoSecrets(t, snap, err)
	})
}

// assertNoSecrets scans everything a caller of a refused build can see -- the
// per-input texts, the explanation and the error -- for any fixture value.
func assertNoSecrets(t *testing.T, snap promosnapshot.PromotionInputSnapshot, err error) {
	t.Helper()
	surfaces := []string{snap.Explain()}
	if err != nil {
		surfaces = append(surfaces, err.Error())
	}
	for _, in := range snap.Inputs() {
		if in.Availability == promosnapshot.AvailabilityDisclosed {
			continue
		}
		surfaces = append(surfaces, in.CanonicalText, in.Reason)
	}
	for _, surface := range surfaces {
		for _, secret := range secretValues {
			if strings.Contains(surface, secret) {
				t.Fatalf("a non-disclosing surface leaked %q:\n%s", secret, surface)
			}
		}
	}
	var refusal *promosnapshot.InputError
	if err != nil && !errors.As(err, &refusal) {
		t.Fatalf("a denied build failed with an untyped error: %v", err)
	}
}
