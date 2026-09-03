package rewards_test

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/domains/fixtures"
	"github.com/monstercameron/hcm-next/internal/domains/rewards"
	"github.com/monstercameron/hcm-next/internal/engines/payband"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files instead of comparing against them")

// money builds a fixture amount at the corpus money contract.
func money(t *testing.T, text, currency string) values.Money {
	t.Helper()
	m, err := fixtures.Money(text, currency)
	if err != nil {
		t.Fatalf("money(%q %s): %v", text, currency, err)
	}
	return m
}

// percent builds a bonus target from its fractional text.
func percent(t *testing.T, fraction string) values.Percentage {
	t.Helper()
	p, err := fixtures.Percent(fraction)
	if err != nil {
		t.Fatalf("percent(%q): %v", fraction, err)
	}
	return p
}

// date parses a fixture business date.
func date(t *testing.T, text string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatalf("date(%q): %v", text, err)
	}
	return d
}

// revision builds the pinned read watermark a snapshot must carry.
func revision(t *testing.T, stream string, seq uint64) values.RevisionToken {
	t.Helper()
	r, err := values.NewSequenceRevision(stream, seq)
	if err != nil {
		t.Fatalf("revision: %v", err)
	}
	return r
}

func catalog(t *testing.T) *fixtures.MemoryBandCatalog {
	t.Helper()
	c, err := fixtures.NewMemoryBandCatalog()
	if err != nil {
		t.Fatalf("band catalog fixture: %v", err)
	}
	return c
}

func subject(t *testing.T) values.EntityRef {
	t.Helper()
	ref, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("subject: %v", err)
	}
	return ref
}

// snapshot builds a complete, pinned compensation snapshot.
func snapshot(t *testing.T, amount, currency string, basis rewards.PayBasis, bonus string, seq uint64) rewards.CompensationSnapshot {
	t.Helper()
	s := rewards.CompensationSnapshot{
		Base:          values.Value(money(t, amount, currency)),
		PayBasis:      basis,
		EffectiveDate: date(t, "2026-06-01"),
		Watermark:     revision(t, "rewards.package.omar", seq),
		Complete:      true,
	}
	if bonus != "" {
		s.BonusTargetPercent = values.Value(percent(t, bonus))
	} else {
		s.BonusTargetPercent = values.Absent[values.Percentage]()
	}
	return s
}

// bandQuery is the OPS-HRBP3 band question the promotion asks.
func bandQuery(t *testing.T) rewards.BandQuery {
	t.Helper()
	return rewards.BandQuery{
		Tenant:   fixtures.Tenant,
		JobCode:  "OPS-HRBP3",
		Grade:    "P3",
		PayZone:  "US-EAST",
		Currency: "USD",
		AsOf:     date(t, "2026-06-01"),
	}
}

// baseInput is the canonical simulation: the legacy 93,000 -> 98,000 USD
// annual raise with a 5% bonus target, evaluated against the OPS-HRBP3 band.
func baseInput(t *testing.T) rewards.SimulateCompensationInput {
	t.Helper()
	q := bandQuery(t)
	return rewards.SimulateCompensationInput{
		Tenant:        fixtures.Tenant,
		Subject:       subject(t),
		Current:       snapshot(t, "93000.00", "USD", rewards.PayBasisAnnualSalary, "0.0500", 11),
		Proposed:      snapshot(t, "98000.00", "USD", rewards.PayBasisAnnualSalary, "0.0500", 11),
		Annualization: rewards.DefaultAnnualization(),
		EffectiveDate: date(t, "2026-06-01"),
		Band:          &q,
	}
}

func TestSimulateCompensationReturnsExactVersionedResultWithZeroEffects(t *testing.T) {
	ctx := context.Background()
	in := baseInput(t)

	result, err := rewards.SimulateCompensation(ctx, catalog(t), in)
	if err != nil {
		t.Fatalf("SimulateCompensation: %v", err)
	}

	// Exact components. Every one of these is a fixed-point decimal computed
	// with a declared scale and rounding mode; a float implementation would
	// drift on at least the bonus figures.
	exact := []struct {
		label string
		got   string
		want  string
	}{
		{"current annualized base", result.Current.AnnualizedBase.Amount().String(), "93000.00"},
		{"current annualized bonus", result.Current.AnnualizedBonusTarget.Amount().String(), "4650.00"},
		{"current annualized total cash", result.Current.AnnualizedTotalCash.Amount().String(), "97650.00"},
		{"proposed annualized base", result.Proposed.AnnualizedBase.Amount().String(), "98000.00"},
		{"proposed annualized bonus", result.Proposed.AnnualizedBonusTarget.Amount().String(), "4900.00"},
		{"proposed annualized total cash", result.Proposed.AnnualizedTotalCash.Amount().String(), "102900.00"},
		{"annualized base delta", result.Delta.AnnualizedBase.Amount().String(), "5000.00"},
		{"annualized bonus delta", result.Delta.AnnualizedBonusTarget.Amount().String(), "250.00"},
		{"annualized total cash delta", result.Delta.AnnualizedTotalCash.Amount().String(), "5250.00"},
		{"increase percent", result.Delta.IncreasePercent.String(), "5.3763"},
	}
	for _, e := range exact {
		if e.got != e.want {
			t.Errorf("%s = %s, want %s", e.label, e.got, e.want)
		}
	}
	if baseDelta, ok := result.Delta.BaseAmount.Get(); !ok {
		t.Error("base delta must be a value when the pay basis is unchanged")
	} else if baseDelta.Amount().String() != "5000.00" {
		t.Errorf("base delta = %s, want 5000.00", baseDelta.Amount())
	}

	// Versioning. A result that cannot say which rules produced it cannot be
	// replayed or approved against.
	if result.RulePackVersion != rewards.CompensationRulePackVersion {
		t.Errorf("rule pack version = %q", result.RulePackVersion)
	}
	if result.AnnualizationVersion != in.Annualization.Version {
		t.Errorf("annualization version = %q, want %q", result.AnnualizationVersion, in.Annualization.Version)
	}
	if result.CatalogVersion != catalog(t).CatalogVersion() {
		t.Errorf("catalog version = %q, want %q", result.CatalogVersion, catalog(t).CatalogVersion())
	}
	if result.InputsDigest == "" || !strings.HasPrefix(result.InputsDigest, "sha256:") {
		t.Errorf("inputs digest = %q, want a sha256 digest", result.InputsDigest)
	}
	if result.CurrentSnapshotMark != in.Current.Watermark ||
		result.ProposedSnapshotMark != in.Proposed.Watermark {
		t.Error("the result must pin the snapshot watermarks it read")
	}

	// Band section.
	if result.Band.State != rewards.BandResultEvaluated {
		t.Fatalf("band state = %s, want EVALUATED", result.Band.State)
	}
	if result.Band.Evaluation.Position.Placement != payband.PlacementInBand {
		t.Errorf("placement = %s, want IN_BAND", result.Band.Evaluation.Position.Placement)
	}
	if got := result.Band.Evaluation.Position.CompaRatio.String(); got != "0.8750" {
		t.Errorf("compa ratio = %s, want 0.8750", got)
	}
	if got := result.Band.Evaluation.Position.RangePenetration.String(); got != "0.1500" {
		t.Errorf("range penetration = %s, want 0.1500", got)
	}

	// Zero effects, proved by count and by receipt.
	if !result.Effects.IsZero() {
		t.Fatalf("simulation counted effects: %v", result.Effects.NonZero())
	}
	if err := result.Receipt.Validate(); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	if result.Receipt.Mode != evidence.ModeSimulate {
		t.Errorf("receipt mode = %s, want SIMULATE", result.Receipt.Mode)
	}
	if result.Receipt.ExecutionState != evidence.ExecutionStateNotPlanned {
		t.Errorf("execution state = %q, want NOT_PLANNED", result.Receipt.ExecutionState)
	}
	if result.Receipt.RequestState != evidence.RequestStateSimulated {
		t.Errorf("request state = %q, want SIMULATED", result.Receipt.RequestState)
	}
	wantControls := map[string]string{
		"compensation_rule_pack": rewards.CompensationRulePackVersion,
		"annualization_rule":     in.Annualization.Version,
		"pay_band_catalog":       catalog(t).CatalogVersion(),
		"pay_band_rule_pack":     rewards.BandRulePackVersion,
	}
	if len(result.Receipt.Controls) != len(wantControls) {
		t.Fatalf("receipt pins %d controls, want %d: %+v",
			len(result.Receipt.Controls), len(wantControls), result.Receipt.Controls)
	}
	for _, c := range result.Receipt.Controls {
		if want, ok := wantControls[c.Name]; !ok || want != c.Version {
			t.Errorf("control %s@%s is not the pinned version", c.Name, c.Version)
		}
	}

	// Reproducible byte-for-byte for identical inputs, in a fresh catalog and a
	// fresh call.
	repeat, err := rewards.SimulateCompensation(ctx, catalog(t), baseInput(t))
	if err != nil {
		t.Fatalf("repeat simulation: %v", err)
	}
	if !bytes.Equal(result.Canonical(), repeat.Canonical()) {
		t.Fatal("identical inputs produced different canonical bytes")
	}
	if result.ResultDigest != repeat.ResultDigest || result.InputsDigest != repeat.InputsDigest {
		t.Fatalf("digests drifted: %s/%s vs %s/%s",
			result.InputsDigest, result.ResultDigest, repeat.InputsDigest, repeat.ResultDigest)
	}
}

func TestEvaluatePayBandPositionUsesTheGovernedCatalogAndPinsItsVersion(t *testing.T) {
	ctx := context.Background()
	c := catalog(t)

	evaluation, err := rewards.EvaluatePayBandPosition(ctx, c, bandQuery(t), money(t, "98000.00", "USD"))
	if err != nil {
		t.Fatalf("EvaluatePayBandPosition: %v", err)
	}
	if evaluation.Outcome != rewards.BandOutcomeWithin {
		t.Errorf("outcome = %s, want WITHIN_BAND", evaluation.Outcome)
	}
	if evaluation.CatalogVersion != c.CatalogVersion() {
		t.Errorf("catalog version = %q, want %q", evaluation.CatalogVersion, c.CatalogVersion())
	}
	if evaluation.Authority.Validate() != nil || evaluation.Provenance.Validate() != nil {
		t.Error("a band evaluation must carry the catalog's authority and provenance")
	}
	if !evaluation.Effects.IsZero() {
		t.Error("a band evaluation must count zero effects")
	}

	t.Run("an amount above a blocking band is a blocking exception", func(t *testing.T) {
		q := rewards.BandQuery{
			Tenant: fixtures.Tenant, JobCode: "CLN-NURSE4", Grade: "N4",
			PayZone: "US-EAST", Currency: "USD", AsOf: date(t, "2026-06-01"),
		}
		got, err := rewards.EvaluatePayBandPosition(ctx, c, q, money(t, "130000.00", "USD"))
		if err != nil {
			t.Fatalf("EvaluatePayBandPosition: %v", err)
		}
		if got.Outcome != rewards.BandOutcomeExceptionBlocking {
			t.Fatalf("outcome = %s, want BAND_EXCEPTION_BLOCKING", got.Outcome)
		}
		if got.Position.Placement != payband.PlacementAboveMaximum {
			t.Fatalf("placement = %s, want ABOVE_MAXIMUM", got.Position.Placement)
		}
	})

	t.Run("an amount below an advisory band is an advisory exception", func(t *testing.T) {
		got, err := rewards.EvaluatePayBandPosition(ctx, c, bandQuery(t), money(t, "70000.00", "USD"))
		if err != nil {
			t.Fatalf("EvaluatePayBandPosition: %v", err)
		}
		if got.Outcome != rewards.BandOutcomeExceptionAdvisory {
			t.Fatalf("outcome = %s, want BAND_EXCEPTION_ADVISORY", got.Outcome)
		}
	})

	t.Run("an unmatched scope is a typed miss, not a zero band", func(t *testing.T) {
		q := bandQuery(t)
		q.Currency = "EUR"
		_, err := rewards.EvaluatePayBandPosition(ctx, c, q, money(t, "98000.00", "EUR"))
		if !errors.Is(err, rewards.ErrBandNotFound) {
			t.Fatalf("error = %v, want ErrBandNotFound", err)
		}
	})

	t.Run("a catalog fault is an error, never an empty band", func(t *testing.T) {
		broken := catalog(t)
		broken.Fail = errors.New("connection reset")
		_, err := rewards.EvaluatePayBandPosition(ctx, broken, bandQuery(t), money(t, "98000.00", "USD"))
		if !errors.Is(err, rewards.ErrCatalogFailed) {
			t.Fatalf("error = %v, want ErrCatalogFailed", err)
		}
	})
}

func TestTodo_COMP_006_Security(t *testing.T) {
	ctx := context.Background()

	t.Run("a redacted material input is refused, not coerced to zero", func(t *testing.T) {
		in := baseInput(t)
		in.Current.Base = values.Redacted[values.Money]("compensation_compartment")
		_, err := rewards.SimulateCompensation(ctx, catalog(t), in)
		if !errors.Is(err, rewards.ErrMaterialInputUnavailable) {
			t.Fatalf("error = %v, want ErrMaterialInputUnavailable", err)
		}
	})

	t.Run("an unknown bonus target is refused rather than assumed zero", func(t *testing.T) {
		in := baseInput(t)
		in.Proposed.BonusTargetPercent = values.Unknown[values.Percentage]("not_yet_modelled")
		_, err := rewards.SimulateCompensation(ctx, catalog(t), in)
		if !errors.Is(err, rewards.ErrMaterialInputUnavailable) {
			t.Fatalf("error = %v, want ErrMaterialInputUnavailable", err)
		}
	})

	t.Run("a partial snapshot is refused", func(t *testing.T) {
		in := baseInput(t)
		in.Proposed.Complete = false
		_, err := rewards.SimulateCompensation(ctx, catalog(t), in)
		if !errors.Is(err, rewards.ErrSnapshotIncomplete) {
			t.Fatalf("error = %v, want ErrSnapshotIncomplete", err)
		}
	})

	t.Run("an unpinned snapshot is refused", func(t *testing.T) {
		in := baseInput(t)
		in.Current.Watermark = values.UnspecifiedRevision()
		_, err := rewards.SimulateCompensation(ctx, catalog(t), in)
		if !errors.Is(err, rewards.ErrSnapshotIncomplete) {
			t.Fatalf("error = %v, want ErrSnapshotIncomplete", err)
		}
	})

	t.Run("a currency mismatch is refused; there is no implicit FX", func(t *testing.T) {
		in := baseInput(t)
		in.Proposed.Base = values.Value(money(t, "98000.00", "EUR"))
		_, err := rewards.SimulateCompensation(ctx, catalog(t), in)
		if !errors.Is(err, rewards.ErrCurrencyMismatch) {
			t.Fatalf("error = %v, want ErrCurrencyMismatch", err)
		}
	})

	t.Run("an unspecified pay basis is refused", func(t *testing.T) {
		in := baseInput(t)
		in.Proposed.PayBasis = rewards.PayBasisUnspecified
		_, err := rewards.SimulateCompensation(ctx, catalog(t), in)
		if !errors.Is(err, rewards.ErrPayBasisUnspecified) {
			t.Fatalf("error = %v, want ErrPayBasisUnspecified", err)
		}
	})

	t.Run("an unpinned annualization rule is refused", func(t *testing.T) {
		in := baseInput(t)
		in.Annualization.Version = ""
		_, err := rewards.SimulateCompensation(ctx, catalog(t), in)
		if !errors.Is(err, rewards.ErrAnnualizationInvalid) {
			t.Fatalf("error = %v, want ErrAnnualizationInvalid", err)
		}
	})

	t.Run("a receipt cannot be minted over counted effects", func(t *testing.T) {
		_, err := evidence.NewZeroEffectReceipt("t", "v1", evidence.ModeSimulate,
			evidence.RequestStateSimulated,
			[]evidence.ControlVersion{{Name: "n", Version: "1"}},
			"sha256:a", "sha256:b",
			evidence.EffectCounters{OutboxEntries: 1})
		if !errors.Is(err, evidence.ErrEffectsNotZero) {
			t.Fatalf("error = %v, want ErrEffectsNotZero", err)
		}
	})
}

func TestTodo_COMP_006_Property(t *testing.T) {
	ctx := context.Background()

	t.Run("an unrequested band is NOT_REQUESTED, never a silent pass", func(t *testing.T) {
		in := baseInput(t)
		in.Band = nil
		result, err := rewards.SimulateCompensation(ctx, catalog(t), in)
		if err != nil {
			t.Fatalf("SimulateCompensation: %v", err)
		}
		if result.Band.State != rewards.BandResultNotRequested {
			t.Fatalf("band state = %s, want NOT_REQUESTED", result.Band.State)
		}
		if result.CatalogVersion != "" {
			t.Error("a simulation that read no catalog must not cite a catalog version")
		}
	})

	t.Run("an unresolvable band is UNKNOWN with a reason", func(t *testing.T) {
		in := baseInput(t)
		q := *in.Band
		q.JobCode = "NOT-A-JOB"
		in.Band = &q
		result, err := rewards.SimulateCompensation(ctx, catalog(t), in)
		if err != nil {
			t.Fatalf("SimulateCompensation: %v", err)
		}
		if result.Band.State != rewards.BandResultUnknown {
			t.Fatalf("band state = %s, want UNKNOWN", result.Band.State)
		}
		if result.Band.Reason == "" {
			t.Error("an UNKNOWN band must say why")
		}
	})

	t.Run("a pay-basis change suppresses the raw base delta and keeps the annualized one", func(t *testing.T) {
		in := baseInput(t)
		in.Current = snapshot(t, "45.00", "USD", rewards.PayBasisHourly, "0.0500", 11)
		result, err := rewards.SimulateCompensation(ctx, catalog(t), in)
		if err != nil {
			t.Fatalf("SimulateCompensation: %v", err)
		}
		if result.Delta.BaseAmount.State() != values.PresenceNotApplicable {
			t.Fatalf("base delta state = %s, want NOT_APPLICABLE", result.Delta.BaseAmount.State())
		}
		// 45.00/hour at 40 hours a week for 52 weeks is exactly 93,600.00.
		if got := result.Current.AnnualizedBase.Amount().String(); got != "93600.00" {
			t.Errorf("hourly annualized base = %s, want 93600.00", got)
		}
		if got := result.Delta.AnnualizedBase.Amount().String(); got != "4400.00" {
			t.Errorf("annualized base delta = %s, want 4400.00", got)
		}
	})

	t.Run("an absent bonus target is recorded as an assumption, not applied silently", func(t *testing.T) {
		in := baseInput(t)
		in.Proposed.BonusTargetPercent = values.Absent[values.Percentage]()
		result, err := rewards.SimulateCompensation(ctx, catalog(t), in)
		if err != nil {
			t.Fatalf("SimulateCompensation: %v", err)
		}
		if !result.Proposed.AnnualizedBonusTarget.Amount().IsZero() {
			t.Error("an absent bonus target must annualize to zero")
		}
		found := false
		for _, a := range result.Assumptions {
			if a.Key == "proposed.bonus_target_percent" {
				found = true
			}
		}
		if !found {
			t.Fatalf("no assumption recorded for the absent bonus target: %+v", result.Assumptions)
		}
	})

	t.Run("the delta is exactly the difference between the two projections", func(t *testing.T) {
		in := baseInput(t)
		result, err := rewards.SimulateCompensation(ctx, catalog(t), in)
		if err != nil {
			t.Fatalf("SimulateCompensation: %v", err)
		}
		want, err := result.Proposed.AnnualizedTotalCash.Sub(result.Current.AnnualizedTotalCash)
		if err != nil {
			t.Fatalf("recompute: %v", err)
		}
		if !result.Delta.AnnualizedTotalCash.Amount().Equal(want.Amount()) {
			t.Fatalf("total cash delta = %s, recomputed %s",
				result.Delta.AnnualizedTotalCash, want)
		}
	})
}

func TestTodo_COMP_006_Golden(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name   string
		mutate func(*rewards.SimulateCompensationInput)
	}{
		{"annual-raise-in-band", func(*rewards.SimulateCompensationInput) {}},
		{"hourly-to-salary-promotion", func(in *rewards.SimulateCompensationInput) {
			in.Current = snapshot(t, "45.00", "USD", rewards.PayBasisHourly, "0.0500", 11)
		}},
		{"above-band-maximum", func(in *rewards.SimulateCompensationInput) {
			in.Proposed.Base = values.Value(money(t, "140000.00", "USD"))
		}},
		{"no-band-requested", func(in *rewards.SimulateCompensationInput) { in.Band = nil }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInput(t)
			tc.mutate(&in)
			result, err := rewards.SimulateCompensation(ctx, catalog(t), in)
			if err != nil {
				t.Fatalf("SimulateCompensation: %v", err)
			}
			compareGolden(t, filepath.Join("testdata", "golden", tc.name+".txt"), renderResult(result))
		})
	}
}

// renderResult prints the simulation in a stable, reviewable form, digests
// included so a golden file fails when the canonical encoding changes.
func renderResult(r rewards.SimulateCompensationResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "intent: %s/%s\n", r.IntentType, r.IntentVersion)
	fmt.Fprintf(&b, "subject: %s\n", r.Subject)
	fmt.Fprintf(&b, "effective: %s\n", r.EffectiveDate)
	for _, side := range []struct {
		label string
		p     rewards.Projection
	}{{"current", r.Current}, {"proposed", r.Proposed}} {
		fmt.Fprintf(&b, "%s base=%s basis=%s annual_base=%s bonus_pct=%s annual_bonus=%s annual_total=%s\n",
			side.label, side.p.Base, side.p.PayBasis, side.p.AnnualizedBase,
			side.p.BonusTargetPercent, side.p.AnnualizedBonusTarget, side.p.AnnualizedTotalCash)
	}
	fmt.Fprintf(&b, "delta base_state=%s", r.Delta.BaseAmount.State())
	if v, ok := r.Delta.BaseAmount.Get(); ok {
		fmt.Fprintf(&b, " base=%s", v)
	}
	fmt.Fprintf(&b, " annual_base=%s annual_bonus=%s annual_total=%s increase_percent=%s\n",
		r.Delta.AnnualizedBase, r.Delta.AnnualizedBonusTarget,
		r.Delta.AnnualizedTotalCash, r.Delta.IncreasePercent)
	fmt.Fprintf(&b, "band state=%s", r.Band.State)
	if r.Band.State == rewards.BandResultEvaluated {
		p := r.Band.Evaluation.Position
		fmt.Fprintf(&b, " id=%s@%s placement=%s compa=%s penetration=%s quartile=%d outcome=%s",
			p.BandID, p.BandVersion, p.Placement, p.CompaRatio, p.RangePenetration,
			p.Quartile, r.Band.Evaluation.Outcome)
	}
	if r.Band.Reason != "" {
		fmt.Fprintf(&b, " reason=%q", r.Band.Reason)
	}
	b.WriteString("\n")
	for _, a := range r.Assumptions {
		fmt.Fprintf(&b, "assumption %s=%s (%s)\n", a.Key, a.Value, a.Reason)
	}
	fmt.Fprintf(&b, "rule_pack: %s\n", r.RulePackVersion)
	fmt.Fprintf(&b, "annualization: %s\n", r.AnnualizationVersion)
	fmt.Fprintf(&b, "catalog: %s\n", r.CatalogVersion)
	for _, c := range r.Receipt.Controls {
		fmt.Fprintf(&b, "control %s@%s\n", c.Name, c.Version)
	}
	fmt.Fprintf(&b, "effects_zero: %v\n", r.Effects.IsZero())
	fmt.Fprintf(&b, "inputs_digest: %s\n", r.InputsDigest)
	fmt.Fprintf(&b, "result_digest: %s\n", r.ResultDigest)
	return b.String()
}

// compareGolden compares got against the golden file at path, or rewrites it
// under -update.
func compareGolden(t *testing.T, path, got string) {
	t.Helper()
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create it)", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden %s mismatch\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

// FuzzTodo_COMP_006 drives the compensation arithmetic with arbitrary amounts
// and pay bases. It asserts the invariants that must hold everywhere: the
// simulation never panics, never counts an effect, always produces a delta
// equal to the difference of its own projections, and is byte-stable.
func FuzzTodo_COMP_006(f *testing.F) {
	f.Add(int64(9300000), int64(9800000), uint8(1), uint8(1), int64(500))
	f.Add(int64(4500), int64(9800000), uint8(3), uint8(1), int64(0))
	f.Add(int64(1), int64(1), uint8(2), uint8(2), int64(10000))
	f.Add(int64(0), int64(100), uint8(1), uint8(3), int64(9999))

	bases := []rewards.PayBasis{
		rewards.PayBasisAnnualSalary,
		rewards.PayBasisMonthlySalary,
		rewards.PayBasisHourly,
	}

	f.Fuzz(func(t *testing.T, currentCents, proposedCents int64, currentBasis, proposedBasis uint8, bonusTenThousandths int64) {
		const limit = 100_000_000_000
		cur := abs64(currentCents) % limit
		prop := abs64(proposedCents) % limit
		bonus := abs64(bonusTenThousandths) % 10_000

		in := rewards.SimulateCompensationInput{
			Tenant:        fixtures.Tenant,
			Subject:       subject(t),
			Current:       fuzzSnapshot(t, cur, bases[int(currentBasis)%len(bases)], bonus, 1),
			Proposed:      fuzzSnapshot(t, prop, bases[int(proposedBasis)%len(bases)], bonus, 2),
			Annualization: rewards.DefaultAnnualization(),
			EffectiveDate: date(t, "2026-06-01"),
		}

		result, err := rewards.SimulateCompensation(context.Background(), nil, in)
		if err != nil {
			if in.Validate() == nil {
				t.Fatalf("SimulateCompensation failed on a valid input: %v", err)
			}
			return
		}

		if !result.Effects.IsZero() {
			t.Fatalf("simulation counted effects: %v", result.Effects.NonZero())
		}
		if err := result.Receipt.Validate(); err != nil {
			t.Fatalf("receipt: %v", err)
		}

		wantTotal, err := result.Proposed.AnnualizedTotalCash.Sub(result.Current.AnnualizedTotalCash)
		if err != nil {
			t.Fatalf("recompute total delta: %v", err)
		}
		if !result.Delta.AnnualizedTotalCash.Amount().Equal(wantTotal.Amount()) {
			t.Fatalf("total cash delta = %s, recomputed %s", result.Delta.AnnualizedTotalCash, wantTotal)
		}
		wantBase, err := result.Proposed.AnnualizedBase.Sub(result.Current.AnnualizedBase)
		if err != nil {
			t.Fatalf("recompute base delta: %v", err)
		}
		if !result.Delta.AnnualizedBase.Amount().Equal(wantBase.Amount()) {
			t.Fatalf("annualized base delta = %s, recomputed %s", result.Delta.AnnualizedBase, wantBase)
		}
		if in.Current.PayBasis != in.Proposed.PayBasis &&
			result.Delta.BaseAmount.State() != values.PresenceNotApplicable {
			t.Fatalf("a pay-basis change must suppress the raw base delta, got %s",
				result.Delta.BaseAmount.State())
		}

		repeat, err := rewards.SimulateCompensation(context.Background(), nil, in)
		if err != nil {
			t.Fatalf("repeat simulation: %v", err)
		}
		if !bytes.Equal(result.Canonical(), repeat.Canonical()) {
			t.Fatal("identical inputs produced different canonical bytes")
		}
	})
}

// fuzzSnapshot builds a complete snapshot from raw cent and basis-point counts.
func fuzzSnapshot(t *testing.T, cents int64, basis rewards.PayBasis, bonusTenThousandths int64, seq uint64) rewards.CompensationSnapshot {
	t.Helper()
	amount, err := fixtures.Money(centsText(cents), "USD")
	if err != nil {
		t.Fatalf("fuzz money %d: %v", cents, err)
	}
	pct, err := fixtures.Percent(fractionText(bonusTenThousandths))
	if err != nil {
		t.Fatalf("fuzz percent %d: %v", bonusTenThousandths, err)
	}
	return rewards.CompensationSnapshot{
		Base:               values.Value(amount),
		PayBasis:           basis,
		BonusTargetPercent: values.Value(pct),
		EffectiveDate:      date(t, "2026-06-01"),
		Watermark:          revision(t, "rewards.fuzz", seq),
		Complete:           true,
	}
}

// abs64 returns the magnitude of v without overflowing on math.MinInt64.
func abs64(v int64) int64 {
	if v < 0 {
		if v == -1<<63 {
			return 1 << 62
		}
		return -v
	}
	return v
}

// centsText renders a non-negative cent count as fixed-point text at scale 2.
func centsText(cents int64) string {
	whole, frac := new(big.Int), new(big.Int)
	whole.DivMod(big.NewInt(cents), big.NewInt(100), frac)
	return fmt.Sprintf("%s.%02d", whole.String(), frac.Int64())
}

// fractionText renders a ten-thousandths count as a fraction at scale 4.
func fractionText(tenThousandths int64) string {
	whole, frac := new(big.Int), new(big.Int)
	whole.DivMod(big.NewInt(tenThousandths), big.NewInt(10000), frac)
	return fmt.Sprintf("%s.%04d", whole.String(), frac.Int64())
}
