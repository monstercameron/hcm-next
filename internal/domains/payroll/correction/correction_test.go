package correction_test

import (
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll/correction"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func mustDecimal(t *testing.T, s string) values.Decimal {
	t.Helper()
	rounding, err := values.ParseRoundingMode("HALF_UP")
	if err != nil {
		t.Fatal(err)
	}
	dec, err := values.NewDecimal(s, 2, rounding)
	if err != nil {
		t.Fatal(err)
	}
	return dec
}

// fixture builds the bitemporal CONF-006 scenario: version 2026-A misprices
// overtime at straight time; version 2026-B repairs it to time-and-a-half.
// Ana works 80 regular hours plus 10 overtime hours at $25/hour.
func fixture(t *testing.T) (correction.Registry, correction.Inputs, correction.RuleVersion, correction.RuleVersion) {
	t.Helper()
	buggy := correction.RuleVersion{
		ID:                 "2026-A",
		OvertimeMultiplier: mustDecimal(t, "1.00"),
		TaxRate:            mustDecimal(t, "0.20"),
	}
	fixed := correction.RuleVersion{
		ID:                 "2026-B",
		OvertimeMultiplier: mustDecimal(t, "1.50"),
		TaxRate:            mustDecimal(t, "0.20"),
	}
	registry := correction.NewRegistry(buggy, fixed)
	inputs := correction.Inputs{
		EmployeeID:    "ana",
		RegularHours:  mustDecimal(t, "80"),
		OvertimeHours: mustDecimal(t, "10"),
		HourlyRate:    mustDecimal(t, "25.00"),
	}
	return registry, inputs, buggy, fixed
}

func mustCalculate(t *testing.T, runID string, in correction.Inputs, rule correction.RuleVersion, effective string, known time.Time) correction.PayRun {
	t.Helper()
	run, err := correction.Calculate(runID, in, rule, effective, known)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestTodo_CONF_006(t *testing.T) {
	registry, inputs, buggy, fixed := fixture(t)
	effective := "2026-01-15"
	originalKnown := time.Date(2026, time.January, 16, 9, 0, 0, 0, time.UTC)
	correctedKnown := time.Date(2026, time.February, 1, 9, 0, 0, 0, time.UTC)

	original := mustCalculate(t, "run/2026-01", inputs, buggy, effective, originalKnown)
	if got := original.Gross.String(); got != "2250.00" {
		t.Fatalf("original gross = %s, want 2250.00 (80*25 + 10*25 straight time)", got)
	}

	// The pinned version reproduces the original exactly.
	if err := correction.Reproduce(original, inputs, registry); err != nil {
		t.Fatalf("Reproduce under pinned version: %v", err)
	}

	// The current rule version rewriting history is the RED: recalculating
	// the original inputs under 2026-B must NOT match the stored run.
	recalculated, err := correction.Calculate(original.RunID, inputs, fixed, effective, originalKnown)
	if err != nil {
		t.Fatal(err)
	}
	if recalculated.Digest == original.Digest {
		t.Fatal("current rule version reproduces the original: history rewrite went undetected")
	}

	corrected := mustCalculate(t, "run/2026-01/corrected", inputs, fixed, effective, correctedKnown)
	if got := corrected.Gross.String(); got != "2375.00" {
		t.Fatalf("corrected gross = %s, want 2375.00 (2000 + 10*37.50)", got)
	}

	delta, err := correction.ComputeDelta(original, corrected)
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}
	if got := delta.DGross.String(); got != "125.00" {
		t.Fatalf("delta gross = %s, want 125.00", got)
	}
	if got := delta.DTax.String(); got != "25.00" {
		t.Fatalf("delta tax = %s, want 25.00", got)
	}
	if got := delta.DNet.String(); got != "100.00" {
		t.Fatalf("delta net = %s, want 100.00", got)
	}

	proposal := correction.Propose("corr/1", original, corrected, delta)
	simulated, err := correction.Simulate(proposal, original)
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	approved, err := correction.Approve(simulated, "payroll-lead")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}

	store := correction.NewStore()
	if err := store.AppendRun(original); err != nil {
		t.Fatalf("AppendRun: %v", err)
	}
	before, _ := store.Original(original.RunID)
	evidence, err := store.AppendCorrection(approved)
	if err != nil {
		t.Fatalf("AppendCorrection: %v", err)
	}
	after, _ := store.Original(original.RunID)
	if after.Digest != before.Digest || after.Gross.String() != "2250.00" {
		t.Fatal("correction overwrote the original run instead of superseding it")
	}
	if evidence.OriginalRunID != original.RunID {
		t.Fatalf("filing evidence references %s, want original %s", evidence.OriginalRunID, original.RunID)
	}
	if evidence.FilingRef == "" || evidence.PayDigest == "" {
		t.Fatal("correction appended without reconciled pay/filing evidence")
	}
}

func TestTodo_CONF_006_Property(t *testing.T) {
	_, _, _, _ = fixture(t)
	rng := rand.New(rand.NewSource(20260115))
	rounding, _ := values.ParseRoundingMode("HALF_UP")
	// hundredths renders an int64 count of hundredths as a decimal string.
	hundredths := func(n int64) values.Decimal {
		out, err := values.NewDecimal(sprintf2(n), 2, rounding)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	for i := 0; i < 64; i++ {
		// Regular hours 0..160, overtime 0..40, rate $10.00..$100.00,
		// multiplier 1.00..2.00, tax 0..30%.
		rule := correction.RuleVersion{
			ID:                 "prop",
			OvertimeMultiplier: hundredths(100 + rng.Int63n(101)),
			TaxRate:            hundredths(rng.Int63n(31)),
		}
		inputs := correction.Inputs{
			EmployeeID:    "prop",
			RegularHours:  hundredths(rng.Int63n(16001)),
			OvertimeHours: hundredths(rng.Int63n(4001)),
			HourlyRate:    hundredths(1000 + rng.Int63n(9001)),
		}
		registry := correction.NewRegistry(rule)
		known := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)
		run, err := correction.Calculate("run/prop", inputs, rule, "2026-01-15", known)
		if err != nil {
			t.Fatal(err)
		}
		// Pinned reproduction is exact.
		if err := correction.Reproduce(run, inputs, registry); err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		// Net and accounting legs always balance.
		net, err := run.Gross.Sub(run.Tax)
		if err != nil {
			t.Fatal(err)
		}
		if !net.Equal(run.Net) {
			t.Fatalf("case %d: net does not balance", i)
		}
		if !run.DebitWages.Equal(run.Gross) || !run.CreditCash.Equal(run.Net) || !run.CreditTax.Equal(run.Tax) {
			t.Fatalf("case %d: accounting legs do not balance", i)
		}
		// Zero-change delta against itself is refused (same version).
		if _, err := correction.ComputeDelta(run, run); !errors.Is(err, correction.ErrUnbalancedDelta) {
			t.Fatalf("case %d: same-version delta = %v, want ErrUnbalancedDelta", i, err)
		}
	}
}

func sprintf2(hundredths int64) string {
	dollars := hundredths / 100
	cents := hundredths % 100
	neg := ""
	if dollars < 0 {
		neg = "-"
		dollars = -dollars
	}
	digit := func(n int64) byte { return byte('0' + n) }
	return neg + itoa(dollars) + "." + string([]byte{digit(cents / 10), digit(cents % 10)})
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func TestTodo_CONF_006_Conformance(t *testing.T) {
	registry, inputs, buggy, fixed := fixture(t)
	_ = registry
	effective := "2026-01-15"
	original := mustCalculate(t, "run/2026-01", inputs, buggy, effective, time.Date(2026, time.January, 16, 9, 0, 0, 0, time.UTC))
	corrected := mustCalculate(t, "run/2026-01/corrected", inputs, fixed, effective, time.Date(2026, time.February, 1, 9, 0, 0, 0, time.UTC))
	delta, err := correction.ComputeDelta(original, corrected)
	if err != nil {
		t.Fatal(err)
	}
	proposal := correction.Propose("corr/1", original, corrected, delta)

	// Out-of-order lifecycle steps are refused.
	if _, err := correction.Approve(proposal, "payroll-lead"); !errors.Is(err, correction.ErrCorrectionState) {
		t.Fatalf("approve before simulate = %v, want ErrCorrectionState", err)
	}
	store := correction.NewStore()
	if err := store.AppendRun(original); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendCorrection(proposal); !errors.Is(err, correction.ErrCorrectionState) {
		t.Fatalf("append before approve = %v, want ErrCorrectionState", err)
	}
	simulated, err := correction.Simulate(proposal, original)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := correction.Approve(simulated, "payroll-lead")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendCorrection(approved); err != nil {
		t.Fatal(err)
	}
	// Replay is refused.
	if _, err := store.AppendCorrection(approved); !errors.Is(err, correction.ErrAlreadyAppended) {
		t.Fatalf("replay append = %v, want ErrAlreadyAppended", err)
	}
	// Overwriting the original is refused.
	if err := store.AppendRun(original); !errors.Is(err, correction.ErrOriginalTampered) {
		t.Fatalf("original overwrite = %v, want ErrOriginalTampered", err)
	}
}

func TestTodo_CONF_006_Mutation(t *testing.T) {
	registry, inputs, buggy, fixed := fixture(t)
	effective := "2026-01-15"
	original := mustCalculate(t, "run/2026-01", inputs, buggy, effective, time.Date(2026, time.January, 16, 9, 0, 0, 0, time.UTC))

	// Mutant 1: tampered stored total breaks pinned reproduction.
	tampered := original
	tampered.Gross = mustDecimal(t, "9999.99")
	if err := correction.Reproduce(tampered, inputs, registry); !errors.Is(err, correction.ErrHistoryMismatch) {
		t.Fatalf("tampered reproduction = %v, want ErrHistoryMismatch", err)
	}
	// Mutant 2: a run genuinely filed under a version the registry never
	// released reproduces consistently but resolves nothing.
	future := correction.RuleVersion{
		ID:                 "2099-X",
		OvertimeMultiplier: mustDecimal(t, "2.00"),
		TaxRate:            mustDecimal(t, "0.20"),
	}
	unknown := mustCalculate(t, "run/2099", inputs, future, effective, original.KnownAt)
	if err := correction.Reproduce(unknown, inputs, registry); !errors.Is(err, correction.ErrUnknownRuleVersion) {
		t.Fatalf("unknown version = %v, want ErrUnknownRuleVersion", err)
	}
	// Mutant 3: corrected run under the same version is not a correction.
	if _, err := correction.ComputeDelta(original, original); !errors.Is(err, correction.ErrUnbalancedDelta) {
		t.Fatalf("same-version correction = %v, want ErrUnbalancedDelta", err)
	}
	// Mutant 4: correction backdated to the original known time.
	backdated := mustCalculate(t, "run/2026-01/corrected", inputs, fixed, effective, original.KnownAt)
	if _, err := correction.ComputeDelta(original, backdated); !errors.Is(err, correction.ErrUnbalancedDelta) {
		t.Fatalf("backdated correction = %v, want ErrUnbalancedDelta", err)
	}
	// Mutant 5: correction bound to a run the ledger never filed cannot append.
	store := correction.NewStore()
	corrected := mustCalculate(t, "run/2026-01/corrected", inputs, fixed, effective, time.Date(2026, time.February, 1, 9, 0, 0, 0, time.UTC))
	delta, err := correction.ComputeDelta(original, corrected)
	if err != nil {
		t.Fatal(err)
	}
	proposal := correction.Propose("corr/evil", original, corrected, delta)
	simulated, err := correction.Simulate(proposal, original)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := correction.Approve(simulated, "payroll-lead")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendCorrection(approved); !errors.Is(err, correction.ErrOriginalTampered) {
		t.Fatalf("unknown-original append = %v, want ErrOriginalTampered", err)
	}
}
