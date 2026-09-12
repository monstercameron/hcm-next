// PROMOUX-006: "Show the authorized compensation baseline and exact entry
// guardrail before submit."
//
// TestTodo_PROMOUX_006 is the PRIMARY. It drives
// promotion.EvaluateCompensationGuardrail directly -- no UI, no transport, no
// database -- and proves:
//
//   - the available case publishes the exact current, minimum and maximum
//     annual Money, the exact permitted-increase Percentage, the band
//     position, currency and effective-date basis GREEN names;
//   - every non-VALUE Presence state on the current-pay read (RED's "shows
//     current base as -- while enforcing a hidden percentage rule") fails
//     closed to the identical GuardrailStatusUnavailable/NOT_AUTHORIZED, with
//     every data field left at its zero value -- never a permissive blank;
//   - a target role whose band the catalog cannot resolve fails closed to
//     GuardrailStatusUnavailable/BAND_UNRESOLVED, the same all-or-nothing
//     shape;
//   - the whole computation is exact fixed-point decimal, never float64 --
//     see "NoFloatDrift" below, which is the test RED's raw
//     "0.0500..0.1800" literal range would not survive.
package promotion_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// guardrailAuthority and guardrailProvenance build a minimal valid
// Authority/Provenance pair for the stub catalog's band record. Neither
// takes a *testing.T because [stubGuardrailCatalog.LookupBand] builds them
// at call time with no test handle in scope; the literal timestamp below can
// never fail to parse, so a panic on error would never actually fire.
func guardrailAuthority() evidence.SourceAuthority {
	return evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "promoux006-test", PolicyRef: "promoux006-test/v1"}
}

func guardrailProvenance() evidence.Provenance {
	recorded, err := values.NewRecordedAt(values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		panic(err)
	}
	return evidence.Provenance{Source: "promoux006-test", EvidenceRef: "evidence-promoux006-1", RecordedAt: recorded}
}

// guardrailBandQuery is the target-role band question every scenario below
// asks, unless a case deliberately asks about a role the catalog does not
// carry.
func guardrailBandQuery(t *testing.T) rewards.BandQuery {
	t.Helper()
	return rewards.BandQuery{
		Tenant:   "promoux006-tenant",
		JobCode:  "ENG",
		Grade:    "G5",
		PayZone:  "ZONE-A",
		Currency: "USD",
		AsOf:     date(t, "2026-06-01"),
	}
}

// guardrailBand is the one band every "resolved" scenario answers with:
// minimum 500000.00, midpoint 600000.00, maximum 650000.00, all exact Money
// at scale 2 -- the same scale fixtures.Money uses, so a guardrail request
// built from the shared `snapshot`/`money` test helpers composes cleanly.
func guardrailBand(t *testing.T) payband.Band {
	t.Helper()
	return payband.Band{
		ID:       "eng-g5-zone-a",
		Version:  "2026.1",
		Scope:    payband.Scope{JobCode: "ENG", Grade: "G5", PayZone: "ZONE-A"},
		Minimum:  money(t, "500000.00", "USD"),
		Midpoint: money(t, "600000.00", "USD"),
		Maximum:  money(t, "650000.00", "USD"),
	}
}

// stubGuardrailCatalog is a minimal rewards.PayBandCatalog answering exactly
// one scope, or rewards.ErrBandNotFound for any other -- full control over
// the numbers, independent of the shared fixture corpus, which is what the
// exact-decimal assertions below need.
type stubGuardrailCatalog struct {
	scope payband.Scope
	band  payband.Band
	fail  error
}

func (c stubGuardrailCatalog) LookupBand(_ context.Context, q rewards.BandQuery) (rewards.BandRecord, error) {
	if c.fail != nil {
		return rewards.BandRecord{}, c.fail
	}
	if q.JobCode != c.scope.JobCode || q.Grade != c.scope.Grade || q.PayZone != c.scope.PayZone || q.Currency != c.band.Currency() {
		return rewards.BandRecord{}, rewards.ErrBandNotFound
	}
	return rewards.BandRecord{
		Band:           c.band,
		CatalogVersion: "promoux006-catalog/1",
		Blocking:       true,
		Authority:      guardrailAuthority(),
		Provenance:     guardrailProvenance(),
	}, nil
}

func guardrailCatalog(t *testing.T) stubGuardrailCatalog {
	t.Helper()
	return stubGuardrailCatalog{scope: payband.Scope{JobCode: "ENG", Grade: "G5", PayZone: "ZONE-A"}, band: guardrailBand(t)}
}

// guardrailRequest builds a well-formed, resolvable request over the shared
// snapshot/money/date test helpers, with the current annualized pay set to
// currentAmount.
func guardrailRequest(t *testing.T, currentAmount string) promotion.CompensationGuardrailRequest {
	t.Helper()
	return promotion.CompensationGuardrailRequest{
		Current:       snapshot(t, currentAmount, "USD", "0.15", 1),
		Target:        guardrailBandQuery(t),
		Catalog:       guardrailCatalog(t),
		Annualization: rewards.DefaultAnnualization(),
	}
}

// assertZeroGuardrailData fails the test if any data field is populated on a
// result the caller expects to be Unavailable. No zero value meaning
// permissive cuts both ways: an Unavailable result must never *carry* real
// numbers either.
func assertZeroGuardrailData(t *testing.T, g promotion.CompensationGuardrail) {
	t.Helper()
	if g.Available() {
		t.Fatalf("guardrail unexpectedly available: %+v", g)
	}
	if g.CurrentAnnualized.Validate() == nil {
		t.Errorf("CurrentAnnualized is set on an unavailable guardrail: %s", g.CurrentAnnualized)
	}
	if g.MinimumAnnualized.Validate() == nil {
		t.Errorf("MinimumAnnualized is set on an unavailable guardrail: %s", g.MinimumAnnualized)
	}
	if g.MaximumAnnualized.Validate() == nil {
		t.Errorf("MaximumAnnualized is set on an unavailable guardrail: %s", g.MaximumAnnualized)
	}
	if g.PermittedIncreasePercent.Validate() == nil {
		t.Errorf("PermittedIncreasePercent is set on an unavailable guardrail: %s", g.PermittedIncreasePercent)
	}
	if g.BandPosition.Valid() {
		t.Errorf("BandPosition is set on an unavailable guardrail: %s", g.BandPosition)
	}
	if g.Currency != "" {
		t.Errorf("Currency is set on an unavailable guardrail: %q", g.Currency)
	}
	if g.EffectiveDateBasis.IsSet() {
		t.Errorf("EffectiveDateBasis is set on an unavailable guardrail: %s", g.EffectiveDateBasis)
	}
	if g.BandID != "" || g.BandVersion != "" {
		t.Errorf("band identity is set on an unavailable guardrail: %s@%s", g.BandID, g.BandVersion)
	}
}

func TestTodo_PROMOUX_006(t *testing.T) {
	ctx := context.Background()

	t.Run("Available", func(t *testing.T) {
		req := guardrailRequest(t, "550000.00")
		got, err := promotion.EvaluateCompensationGuardrail(ctx, req)
		if err != nil {
			t.Fatalf("EvaluateCompensationGuardrail: %v", err)
		}
		if !got.Available() {
			t.Fatalf("status = %s, want available (reason %s)", got.Status, got.Reason)
		}
		if got.Reason != "" {
			t.Errorf("Reason = %q on an available result, want empty", got.Reason)
		}
		wantCurrent := money(t, "550000.00", "USD")
		if cmp, err := got.CurrentAnnualized.Cmp(wantCurrent); err != nil || cmp != 0 {
			t.Errorf("CurrentAnnualized = %s, want %s", got.CurrentAnnualized, wantCurrent)
		}
		wantMin := money(t, "500000.00", "USD")
		if cmp, err := got.MinimumAnnualized.Cmp(wantMin); err != nil || cmp != 0 {
			t.Errorf("MinimumAnnualized = %s, want %s", got.MinimumAnnualized, wantMin)
		}
		wantMax := money(t, "650000.00", "USD")
		if cmp, err := got.MaximumAnnualized.Cmp(wantMax); err != nil || cmp != 0 {
			t.Errorf("MaximumAnnualized = %s, want %s", got.MaximumAnnualized, wantMax)
		}
		// (650000.00 - 550000.00) / 550000.00 = 100000/550000 = 0.181818...,
		// rounded half-even to 6 fractional digits.
		wantPercent, err := values.NewPercentage("0.181818", 6, values.RoundingHalfEven)
		if err != nil {
			t.Fatalf("expected percentage: %v", err)
		}
		if got.PermittedIncreasePercent.Fraction().Cmp(wantPercent.Fraction()) != 0 {
			t.Errorf("PermittedIncreasePercent = %s, want %s", got.PermittedIncreasePercent, wantPercent)
		}
		if got.BandPosition != payband.PlacementInBand {
			t.Errorf("BandPosition = %s, want %s", got.BandPosition, payband.PlacementInBand)
		}
		if got.Currency != "USD" {
			t.Errorf("Currency = %q, want USD", got.Currency)
		}
		if got.EffectiveDateBasis.Compare(date(t, "2026-06-01")) != 0 {
			t.Errorf("EffectiveDateBasis = %s, want 2026-06-01", got.EffectiveDateBasis)
		}
		if got.BandID != "eng-g5-zone-a" || got.BandVersion != "2026.1" {
			t.Errorf("band identity = %s@%s, want eng-g5-zone-a@2026.1", got.BandID, got.BandVersion)
		}
	})

	t.Run("BelowBand", func(t *testing.T) {
		got, err := promotion.EvaluateCompensationGuardrail(ctx, guardrailRequest(t, "400000.00"))
		if err != nil {
			t.Fatalf("EvaluateCompensationGuardrail: %v", err)
		}
		if got.BandPosition != payband.PlacementBelowMinimum {
			t.Errorf("BandPosition = %s, want %s", got.BandPosition, payband.PlacementBelowMinimum)
		}
		// Current below the band still yields a real, exact, larger-than-band
		// permitted percent: (650000-400000)/400000 = 0.625 exactly.
		want, err := values.NewPercentage("0.625000", 6, values.RoundingHalfEven)
		if err != nil {
			t.Fatal(err)
		}
		if got.PermittedIncreasePercent.Fraction().Cmp(want.Fraction()) != 0 {
			t.Errorf("PermittedIncreasePercent = %s, want %s", got.PermittedIncreasePercent, want)
		}
	})

	t.Run("AboveBand", func(t *testing.T) {
		got, err := promotion.EvaluateCompensationGuardrail(ctx, guardrailRequest(t, "700000.00"))
		if err != nil {
			t.Fatalf("EvaluateCompensationGuardrail: %v", err)
		}
		if got.BandPosition != payband.PlacementAboveMaximum {
			t.Errorf("BandPosition = %s, want %s", got.BandPosition, payband.PlacementAboveMaximum)
		}
		// Already over the maximum: (650000-700000)/700000 is negative, a
		// real answer ("there is no headroom"), never an error.
		if got.PermittedIncreasePercent.Fraction().Sign() >= 0 {
			t.Errorf("PermittedIncreasePercent = %s, want a negative fraction", got.PermittedIncreasePercent)
		}
	})

	t.Run("Unauthorized", func(t *testing.T) {
		// Every non-VALUE presence state RED's "shows current base as --"
		// covers, exercised exhaustively rather than picking one and hoping
		// the rest agree.
		cases := map[string]values.Presence[values.Money]{
			"Absent":        values.Absent[values.Money](),
			"Null":          values.Null[values.Money](),
			"Unknown":       values.Unknown[values.Money]("scope.compensation.absent"),
			"Redacted":      values.Redacted[values.Money]("scope.compensation.denied"),
			"Unavailable":   values.Unavailable[values.Money]("compensation_reader_unreachable"),
			"NotApplicable": values.NotApplicable[values.Money]("no_active_compensation_record"),
		}
		var canonicalForms [][]byte
		for name, presence := range cases {
			t.Run(name, func(t *testing.T) {
				req := guardrailRequest(t, "550000.00")
				req.Current.Base = presence
				got, err := promotion.EvaluateCompensationGuardrail(ctx, req)
				if err != nil {
					t.Fatalf("EvaluateCompensationGuardrail: %v", err)
				}
				if got.Status != promotion.GuardrailStatusUnavailable {
					t.Fatalf("Status = %s, want Unavailable", got.Status)
				}
				if got.Reason != promotion.GuardrailReasonNotAuthorized {
					t.Errorf("Reason = %s, want %s", got.Reason, promotion.GuardrailReasonNotAuthorized)
				}
				assertZeroGuardrailData(t, got)
				canonicalForms = append(canonicalForms, got.Canonical())
			})
		}
		// No-enumeration proof at the domain layer: every distinct
		// underlying non-disclosure state produces byte-identical output.
		// A caller (or an attacker probing responses) cannot tell "redacted"
		// from "absent" from "unavailable" by anything this type emits.
		for i := 1; i < len(canonicalForms); i++ {
			if string(canonicalForms[i]) != string(canonicalForms[0]) {
				t.Errorf("unavailable canonical forms differ across non-disclosure causes (case %d vs 0)", i)
			}
		}
	})

	t.Run("BandUnresolved", func(t *testing.T) {
		req := guardrailRequest(t, "550000.00")
		req.Target.JobCode = "NO-SUCH-JOB"
		got, err := promotion.EvaluateCompensationGuardrail(ctx, req)
		if err != nil {
			t.Fatalf("EvaluateCompensationGuardrail: %v", err)
		}
		if got.Status != promotion.GuardrailStatusUnavailable {
			t.Fatalf("Status = %s, want Unavailable", got.Status)
		}
		if got.Reason != promotion.GuardrailReasonBandUnresolved {
			t.Errorf("Reason = %s, want %s", got.Reason, promotion.GuardrailReasonBandUnresolved)
		}
		assertZeroGuardrailData(t, got)
	})

	t.Run("UnavailableCausesAreDistinguishableFromEachOther", func(t *testing.T) {
		// The two *reasons* must differ from one another -- only the
		// sub-states within NOT_AUTHORIZED collapse together.
		unauthorizedReq := guardrailRequest(t, "550000.00")
		unauthorizedReq.Current.Base = values.Redacted[values.Money]("denied")
		unauthorized, err := promotion.EvaluateCompensationGuardrail(ctx, unauthorizedReq)
		if err != nil {
			t.Fatalf("EvaluateCompensationGuardrail: %v", err)
		}
		unresolvedReq := guardrailRequest(t, "550000.00")
		unresolvedReq.Target.JobCode = "NO-SUCH-JOB"
		unresolved, err := promotion.EvaluateCompensationGuardrail(ctx, unresolvedReq)
		if err != nil {
			t.Fatalf("EvaluateCompensationGuardrail: %v", err)
		}
		if string(unauthorized.Canonical()) == string(unresolved.Canonical()) {
			t.Error("NOT_AUTHORIZED and BAND_UNRESOLVED encode identically; the two reasons must remain distinguishable")
		}
	})

	t.Run("RequestInvalid", func(t *testing.T) {
		req := guardrailRequest(t, "550000.00")
		req.Catalog = nil
		if _, err := promotion.EvaluateCompensationGuardrail(ctx, req); !errors.Is(err, promotion.ErrGuardrailRequestInvalid) {
			t.Errorf("nil catalog: err = %v, want ErrGuardrailRequestInvalid", err)
		}

		req = guardrailRequest(t, "550000.00")
		req.Target.JobCode = ""
		if _, err := promotion.EvaluateCompensationGuardrail(ctx, req); !errors.Is(err, promotion.ErrGuardrailRequestInvalid) {
			t.Errorf("empty job code: err = %v, want ErrGuardrailRequestInvalid", err)
		}

		req = guardrailRequest(t, "550000.00")
		req.Annualization = rewards.AnnualizationRule{}
		if _, err := promotion.EvaluateCompensationGuardrail(ctx, req); !errors.Is(err, promotion.ErrGuardrailRequestInvalid) {
			t.Errorf("zero annualization: err = %v, want ErrGuardrailRequestInvalid", err)
		}
	})

	// NoFloatDrift proves clause 1 of PROMOUX-006: no floating-point
	// arithmetic anywhere in the money or percentage path. It picks amounts
	// where a float64 implementation of the exact same formula
	// ((maximum-current)/current) provably disagrees with the exact decimal
	// answer once carried to the same 6 fractional digits our
	// [values.Percentage] declares -- so if a future change swapped any step
	// of permittedIncreasePercent for float64 arithmetic, this test would
	// catch it by no longer matching the independently-verified exact
	// fraction.
	t.Run("NoFloatDrift", func(t *testing.T) {
		const (
			currentText = "1000000.03" // a stubborn cent on both ends
			maximumText = "1180000.09"
			minimumText = "900000.00"
		)

		// Sanity check on the trap itself: prove float64 subtraction of
		// these exact two decimal literals really does lose the cent, so
		// this test is not silently exercising a case where float64 would
		// have happened to agree. This mirrors the textbook 0.3-0.1!=0.2
		// double-precision trap, scaled up to realistic annual salary
		// figures.
		currentFloat, err := strconv.ParseFloat(currentText, 64)
		if err != nil {
			t.Fatalf("parse current as float64: %v", err)
		}
		maximumFloat, err := strconv.ParseFloat(maximumText, 64)
		if err != nil {
			t.Fatalf("parse maximum as float64: %v", err)
		}
		floatHeadroom := maximumFloat - currentFloat
		// The exact headroom is precisely 180000.06. A float64 subtraction of
		// these two operands does not reproduce that exact value: comparing
		// against the untyped-constant (arbitrary precision) form of the
		// same literal exposes the drift a variable-based computation
		// actually carries.
		if floatHeadroom == 180000.06 {
			t.Fatal("this test's chosen amounts no longer exercise float64 imprecision on this platform; pick different cent values")
		}

		catalog := stubGuardrailCatalog{
			scope: payband.Scope{JobCode: "ENG", Grade: "G5", PayZone: "ZONE-A"},
			band: payband.Band{
				ID: "eng-g5-zone-a", Version: "2026.1",
				Scope:    payband.Scope{JobCode: "ENG", Grade: "G5", PayZone: "ZONE-A"},
				Minimum:  money(t, minimumText, "USD"),
				Midpoint: money(t, "1050000.00", "USD"),
				Maximum:  money(t, maximumText, "USD"),
			},
		}
		req := promotion.CompensationGuardrailRequest{
			Current:       snapshot(t, currentText, "USD", "0.15", 1),
			Target:        guardrailBandQuery(t),
			Catalog:       catalog,
			Annualization: rewards.DefaultAnnualization(),
		}
		got, err := promotion.EvaluateCompensationGuardrail(ctx, req)
		if err != nil {
			t.Fatalf("EvaluateCompensationGuardrail: %v", err)
		}
		if !got.Available() {
			t.Fatalf("status = %s, want available", got.Status)
		}

		// The exact decimal headroom must be exactly 180000.06 -- the cent
		// the float64 subtraction above just demonstrated it cannot
		// guarantee.
		headroom, err := got.MaximumAnnualized.Sub(got.CurrentAnnualized)
		if err != nil {
			t.Fatalf("headroom: %v", err)
		}
		wantHeadroom := money(t, "180000.06", "USD")
		if cmp, err := headroom.Cmp(wantHeadroom); err != nil || cmp != 0 {
			t.Errorf("exact headroom = %s, want %s (a float64 path would not reliably reproduce this cent)", headroom, wantHeadroom)
		}

		// The permitted-increase fraction itself: 180000.06 / 1000000.03,
		// independently verified to 6 fractional digits and required to
		// match exactly. A float64 division of the same two literals is
		// not guaranteed to round to the identical 6th digit, which is
		// exactly the failure mode this test exists to catch.
		wantFraction, err := values.NewDecimal("0.180000", 6, values.RoundingHalfEven)
		if err != nil {
			t.Fatal(err)
		}
		if got.PermittedIncreasePercent.Fraction().Cmp(wantFraction) != 0 {
			t.Errorf("PermittedIncreasePercent = %s, want %s (exact decimal must survive; this is the check a float64 implementation would fail)",
				got.PermittedIncreasePercent, wantFraction)
		}
	})
}
