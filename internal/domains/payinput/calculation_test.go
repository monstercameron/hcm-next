package payinput

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func calcPeriod(t *testing.T, id string) CalculationPeriod {
	t.Helper()
	return CalculationPeriod{ID: id, Currency: "USD", Effective: payInputInterval(t, "2026-01-01", "2026-02-01"), Base: payInputDecimal(t, "1000.00"), AvailableForDeduction: payInputDecimal(t, "100.00"), ProtectedFloor: payInputDecimal(t, "50.00")}
}

func calcAssignment(t *testing.T, d Definition, id string, amount string, recurrence Recurrence) WorkerAssignment {
	t.Helper()
	a := WorkerAssignment{AssignmentID: id, WorkerRef: "worker-1", Effective: payInputInterval(t, "2026-01-01", "2026-02-01"), Amount: payInputDecimal(t, amount), Recurrence: recurrence, RecurrenceRule: "PAY_PERIOD"}
	if recurrence == RecurrenceOneTime {
		a.RecurrenceRule = ""
	}
	bound, err := NewWorkerAssignment(d, a)
	if err != nil {
		t.Fatal(err)
	}
	return bound
}

func TestPayInputCalculationAppliesTaxabilityLimitsArrearsAndExactDecimalRules(t *testing.T) {
	d := validDefinition(t, StatePublished)
	a := calcAssignment(t, d, "a-1", "30.10", RecurrencePerPayroll)
	p := calcPeriod(t, "2026-01")
	got, err := Calculate(CalculationInput{Definition: d, Assignment: a, Period: p})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Lines) != 1 || got.Lines[0].Applied.String() != "30.10" || !got.Lines[0].Taxable[JurisdictionFederal] {
		t.Fatalf("line = %+v", got.Lines)
	}
	if got.Lines[0].Deferred.Sign() != 0 || got.ReplayDigest == "" {
		t.Fatalf("result = %+v", got)
	}

	ded := d
	ded.Kind, ded.Type = KindDeduction, KindDeduction
	ded.Code = "DED"
	ded.DefinitionID, ded.ID = "ded", "ded"
	ded.AccountingCode = "DED"
	ded.Limits = DecimalLimits{}
	ded, err = NewDefinition(ded)
	if err != nil {
		t.Fatal(err)
	}
	da := calcAssignment(t, ded, "ded-1", "80.00", RecurrencePerPayroll)
	state := CalculationState{Arrears: map[string]values.Decimal{"ded-1": payInputDecimal(t, "20.00")}}
	got, err = Calculate(CalculationInput{Definition: ded, Assignment: da, Period: p, State: state})
	if err != nil {
		t.Fatal(err)
	}
	if got.Lines[0].Applied.String() != "50.00" || got.Lines[0].Deferred.String() != "50.00" || got.Lines[0].RemainingArrears.String() != "50.00" {
		t.Fatalf("protected floor/arrears = %+v", got.Lines[0])
	}
}

func TestTodo_PAYINPUT_002_Property(t *testing.T) {
	d := validDefinition(t, StatePublished)
	a := calcAssignment(t, d, "a-1", "10.00", RecurrencePerPayroll)
	p := calcPeriod(t, "p")
	one, err := Calculate(CalculationInput{Definition: d, Assignment: a, Period: p})
	if err != nil {
		t.Fatal(err)
	}
	two, err := Calculate(CalculationInput{Definition: d, Assignment: a, Period: p})
	if err != nil {
		t.Fatal(err)
	}
	if one.ReplayDigest != two.ReplayDigest {
		t.Fatal("replay digest changed")
	}
}

func TestTodo_PAYINPUT_002_Golden(t *testing.T) {
	d := validDefinition(t, StatePublished)
	a := calcAssignment(t, d, "a-1", "10.00", RecurrencePerPayroll)
	r, err := Calculate(CalculationInput{Definition: d, Assignment: a, Period: calcPeriod(t, "p")})
	const expected = "sha256:e166dc1877ed7029d4a3f1eb3d471a2b7dac510f3aa19be97631793f6883720e"
	if err != nil || r.Lines[0].CanonicalDigest != expected {
		t.Fatalf("golden=%+v err=%v", r, err)
	}
}
func TestTodo_PAYINPUT_002_Race(t *testing.T) {
	d := validDefinition(t, StatePublished)
	a := calcAssignment(t, d, "a-1", "10.00", RecurrencePerPayroll)
	p := calcPeriod(t, "p")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Calculate(CalculationInput{Definition: d, Assignment: a, Period: p}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
func TestTodo_PAYINPUT_002_Fault(t *testing.T) {
	d := validDefinition(t, StatePublished)
	a := calcAssignment(t, d, "a-1", "10.00", RecurrencePerPayroll)
	p := calcPeriod(t, "p")
	p.ID = ""
	if _, err := Calculate(CalculationInput{Definition: d, Assignment: a, Period: p}); !errors.Is(err, ErrInvalidCalculation) {
		t.Fatalf("err=%v", err)
	}
}
func TestTodo_PAYINPUT_002_Security(t *testing.T) {
	d := validDefinition(t, StatePublished)
	a := calcAssignment(t, d, "a-1", "10.00", RecurrencePerPayroll)
	r, err := Calculate(CalculationInput{Definition: d, Assignment: a, Period: calcPeriod(t, "p")})
	if err != nil || r.Lines[0].WorkerRef != "worker-1" {
		t.Fatalf("line=%+v err=%v", r, err)
	}
	a.WorkerRef = "worker-2"
	if _, err = Calculate(CalculationInput{Definition: d, Assignment: a, Period: calcPeriod(t, "p")}); err == nil {
		t.Fatal("assignment changed after digest was accepted")
	}
}

func TestPayInputCalculationRejectsCurrencyMismatchAndSignedCarry(t *testing.T) {
	d := validDefinition(t, StatePublished)
	a := calcAssignment(t, d, "a-1", "10.00", RecurrencePerPayroll)
	p := calcPeriod(t, "p")
	p.Currency = "EUR"
	if _, err := Calculate(CalculationInput{Definition: d, Assignment: a, Period: p}); !errors.Is(err, ErrInvalidCalculation) {
		t.Fatalf("currency mismatch err=%v", err)
	}
	p.Currency = "USD"
	state := CalculationState{Arrears: map[string]values.Decimal{a.id(): payInputDecimal(t, "-1.00")}}
	if _, err := Calculate(CalculationInput{Definition: d, Assignment: a, Period: p, State: state}); !errors.Is(err, ErrInvalidCalculation) {
		t.Fatalf("negative arrears err=%v", err)
	}
	state = CalculationState{RemainingLimits: map[string]values.Decimal{a.id(): payInputDecimal(t, "-1.00")}}
	if _, err := Calculate(CalculationInput{Definition: d, Assignment: a, Period: p, State: state}); !errors.Is(err, ErrInvalidCalculation) {
		t.Fatalf("negative limit err=%v", err)
	}
}

func TestPayInputCalculationCarriesCapAndArrearsWithoutFabricatingValue(t *testing.T) {
	d := validDefinition(t, StatePublished)
	d.Limits.Maximum = payInputDecimal(t, "100.00")
	d, err := NewDefinition(d)
	if err != nil {
		t.Fatal(err)
	}
	a := calcAssignment(t, d, "a-1", "80.00", RecurrencePerPayroll)
	state := CalculationState{
		Arrears:         map[string]values.Decimal{a.id(): payInputDecimal(t, "20.00")},
		RemainingLimits: map[string]values.Decimal{a.id(): payInputDecimal(t, "40.00")},
	}
	r, err := Calculate(CalculationInput{Definition: d, Assignment: a, Period: calcPeriod(t, "p"), State: state})
	if err != nil {
		t.Fatal(err)
	}
	line := r.Lines[0]
	if line.Applied.String() != "40.00" || line.Deferred.String() != "60.00" || line.RemainingLimit.String() != "0.00" || r.State.Arrears[a.id()].String() != "60.00" {
		t.Fatalf("capped line=%+v state=%+v", line, r.State)
	}
}

func TestPayInputCalculationPercentageRequiresExactCentResult(t *testing.T) {
	d := validDefinition(t, StatePublished)
	d.CalculationBasis, d.Basis = BasisPercentage, BasisPercentage
	d.Limits = DecimalLimits{}
	d, err := NewDefinition(d)
	if err != nil {
		t.Fatal(err)
	}
	rate, err := values.NewDecimal("0.125", 3, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	a := WorkerAssignment{AssignmentID: "rate-1", WorkerRef: "worker-1", Effective: payInputInterval(t, "2026-01-01", "2026-02-01"), Rate: rate, Recurrence: RecurrencePerPayroll, RecurrenceRule: "PAY_PERIOD"}
	a, err = NewWorkerAssignment(d, a)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Calculate(CalculationInput{Definition: d, Assignment: a, Period: calcPeriod(t, "p")})
	if err != nil || r.Lines[0].Amount.String() != "125.00" {
		t.Fatalf("percentage=%+v err=%v", r, err)
	}
	a.Rate, _ = values.NewDecimal("0.333", 3, values.RoundingExactRequired)
	a.CanonicalDigest = ""
	a, err = NewWorkerAssignment(d, a)
	if err != nil {
		t.Fatal(err)
	}
	nonExactPeriod := calcPeriod(t, "p")
	nonExactPeriod.Base = payInputDecimal(t, "1000.01")
	if _, err = Calculate(CalculationInput{Definition: d, Assignment: a, Period: nonExactPeriod}); err == nil {
		t.Fatal("non-exact cent result accepted")
	}
}

func TestPayInputCalculationFailsClosedWithoutGovernedRecurrenceAdmission(t *testing.T) {
	d := validDefinition(t, StatePublished)
	for _, tc := range []struct {
		name, rule string
		recurrence Recurrence
	}{
		{name: "opaque pay period", recurrence: RecurrencePerPayroll, rule: "EVERY_OTHER_PAY_PERIOD"},
		{name: "one time with schedule", recurrence: RecurrenceOneTime, rule: "PAY_PERIOD"},
		{name: "monthly", recurrence: RecurrenceMonthly, rule: "MONTHLY"},
		{name: "annual", recurrence: RecurrenceAnnual, rule: "ANNUAL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := calcAssignment(t, d, "a-1", "10.00", tc.recurrence)
			a.RecurrenceRule = tc.rule
			a.CanonicalDigest = ""
			a, err := NewWorkerAssignment(d, a)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = Calculate(CalculationInput{Definition: d, Assignment: a, Period: calcPeriod(t, "p")}); !errors.Is(err, ErrInvalidCalculation) {
				t.Fatalf("recurrence accepted: %v", err)
			}
		})
	}
}
func TestTodo_PAYINPUT_002_Conformance(t *testing.T) {
	d := validDefinition(t, StatePublished)
	a := calcAssignment(t, d, "a-1", "10.00", RecurrenceOneTime)
	p := calcPeriod(t, "p")
	r, err := Calculate(CalculationInput{Definition: d, Assignment: a, Period: p})
	if err != nil || !r.State.ConsumedOneTime[a.id()] || r.Lines[0].Recurrence != RecurrenceOneTime {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	if _, err = Calculate(CalculationInput{Definition: d, Assignment: a, Period: p, State: r.State}); !errors.Is(err, ErrOneTimeConsumed) {
		t.Fatalf("duplicate=%v", err)
	}
}
func TestTodo_PAYINPUT_002_Mutation(t *testing.T) {
	d := validDefinition(t, StatePublished)
	a := calcAssignment(t, d, "a-1", "10.00", RecurrencePerPayroll)
	before := a.CanonicalDigest
	state := CalculationState{ConsumedOneTime: map[string]bool{"other": true}, Arrears: map[string]values.Decimal{"other": payInputDecimal(t, "1.00")}, RemainingLimits: map[string]values.Decimal{"other": payInputDecimal(t, "2.00")}}
	result, err := Calculate(CalculationInput{Definition: d, Assignment: a, Period: calcPeriod(t, "p"), State: state})
	if err != nil {
		t.Fatal(err)
	}
	result.State.ConsumedOneTime["other"] = false
	result.State.Arrears["other"] = payInputDecimal(t, "9.00")
	result.State.RemainingLimits["other"] = payInputDecimal(t, "9.00")
	if a.CanonicalDigest != before || !state.ConsumedOneTime["other"] || state.Arrears["other"].String() != "1.00" || state.RemainingLimits["other"].String() != "2.00" {
		t.Fatalf("mutation err=%v assignment=%+v state=%+v", err, a, state)
	}
}
