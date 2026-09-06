package auditpack_test

import (
	"testing"

	"github.com/google/uuid"

	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/domains/payroll/auditpack"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestTotalKindValid(t *testing.T) {
	t.Parallel()
	for _, k := range auditpack.Kinds() {
		if !k.Valid() {
			t.Errorf("declared kind %s reports invalid", k)
		}
	}
	if auditpack.TotalKind("NOT_A_KIND").Valid() {
		t.Error("an undeclared kind reports valid")
	}
}

func TestKindsIsTheFourDeclaredKindsInAFixedOrder(t *testing.T) {
	t.Parallel()
	want := []auditpack.TotalKind{
		auditpack.KindRegister, auditpack.KindBankFile,
		auditpack.KindTaxLiability, auditpack.KindFilingAcknowledgment,
	}
	got := auditpack.Kinds()
	if len(got) != len(want) {
		t.Fatalf("Kinds() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Kinds()[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func decimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("decimal %s: %v", text, err)
	}
	return d
}

func TestLineFactValidate(t *testing.T) {
	t.Parallel()
	valid := auditpack.LineFact{
		Kind: auditpack.KindRegister, RunID: "run-1", Amount: decimal(t, "10.00"), Currency: "USD",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("a complete line fact refused: %v", err)
	}

	cases := []struct {
		name string
		fact auditpack.LineFact
	}{
		{"invalid kind", auditpack.LineFact{Kind: "NOT_A_KIND", RunID: "run-1", Amount: decimal(t, "1.00"), Currency: "USD"}},
		{"empty run id", auditpack.LineFact{Kind: auditpack.KindRegister, Amount: decimal(t, "1.00"), Currency: "USD"}},
		{"unset amount", auditpack.LineFact{Kind: auditpack.KindRegister, RunID: "run-1", Currency: "USD"}},
		{"empty currency", auditpack.LineFact{Kind: auditpack.KindRegister, RunID: "run-1", Amount: decimal(t, "1.00")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fact.Validate(); err == nil {
				t.Fatal("an incomplete line fact was accepted")
			}
		})
	}
}

func TestRunTotalsTotalAndLinesOf(t *testing.T) {
	t.Parallel()
	totals := auditpack.RunTotals{
		Tenant: uuid.New(), RunID: "run-1",
		Totals: map[auditpack.TotalKind]values.Decimal{auditpack.KindRegister: decimal(t, "10.00")},
		Lines: []auditpack.LineFact{
			{Kind: auditpack.KindRegister, RunID: "run-1", Amount: decimal(t, "4.00"), Currency: "USD", Ref: datalogger.EventRef{StreamKey: "s", Sequence: 1}},
			{Kind: auditpack.KindRegister, RunID: "run-1", Amount: decimal(t, "6.00"), Currency: "USD", Ref: datalogger.EventRef{StreamKey: "s", Sequence: 2}},
			{Kind: auditpack.KindBankFile, RunID: "run-1", Amount: decimal(t, "3.00"), Currency: "USD", Ref: datalogger.EventRef{StreamKey: "s", Sequence: 3}},
		},
	}
	if got, ok := totals.Total(auditpack.KindRegister); !ok || got.String() != "10.00" {
		t.Fatalf("Total(REGISTER) = %v/%v, want 10.00/true", got, ok)
	}
	if _, ok := totals.Total(auditpack.KindTaxLiability); ok {
		t.Fatal("Total reported a kind that was never resolved")
	}
	if got := totals.LinesOf(auditpack.KindRegister); len(got) != 2 {
		t.Fatalf("LinesOf(REGISTER) = %d lines, want 2", len(got))
	}
	if got := totals.LinesOf(auditpack.KindFilingAcknowledgment); got != nil {
		t.Fatalf("LinesOf(FILING_ACKNOWLEDGMENT) = %v, want nil", got)
	}
}

func TestDecisionOKAndErr(t *testing.T) {
	t.Parallel()
	t.Run("no pairs is not a decision", func(t *testing.T) {
		if (auditpack.Decision{}).OK() {
			t.Fatal("a decision with no checked pairs reports OK")
		}
	})
	t.Run("every pair agreeing is OK with a nil error", func(t *testing.T) {
		d := auditpack.Decision{Pairs: []auditpack.PairResult{{Name: "P", OK: true}}}
		if !d.OK() {
			t.Fatal("a decision with one agreeing pair reports not OK")
		}
		if err := d.Err(); err != nil {
			t.Fatalf("Err() on a clean decision = %v, want nil", err)
		}
	})
	t.Run("one failing pair is named in Err", func(t *testing.T) {
		d := auditpack.Decision{Pairs: []auditpack.PairResult{
			{Name: "PAIR_A", LeftAmount: decimal(t, "10.00"), RightAmount: decimal(t, "9.00"), Difference: decimal(t, "1.00"), OK: false},
		}}
		if d.OK() {
			t.Fatal("a decision with a failing pair reports OK")
		}
		err := d.Err()
		if err == nil {
			t.Fatal("Err() on a failing decision returned nil")
		}
		var variance auditpack.ErrVarianceUnexplained
		if got, ok := asVariance(err); !ok {
			t.Fatalf("Err() = %v, want an ErrVarianceUnexplained", err)
		} else {
			variance = got
		}
		if variance.Pair != "PAIR_A" || variance.Difference != "1.00" {
			t.Fatalf("ErrVarianceUnexplained = %+v, want pair PAIR_A difference 1.00", variance)
		}
	})
}

func asVariance(err error) (auditpack.ErrVarianceUnexplained, bool) {
	v, ok := err.(auditpack.ErrVarianceUnexplained)
	return v, ok
}
