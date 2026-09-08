package balance

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func correctionInstant(t *testing.T, text string) values.Instant {
	t.Helper()
	v, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return values.NewInstant(v)
}

func correctionFixture(t *testing.T) (AccumulatorDefinition, BalanceEntry, BalanceEntry) {
	t.Helper()
	d := validDefinition()
	at := correctionInstant(t, "2026-06-01T00:00:00Z")
	recorded := correctionInstant(t, "2026-06-02T00:00:00Z")
	original := validEntry()
	original.Kind = Credit
	original.EntryType = "GRANT"
	original.EffectiveAt, original.RecordedAt, original.AuthorizedAt = at, recorded, recorded
	original.Amount = values.MustDecimal("10.00", 2, values.RoundingExactRequired)
	corrected := original
	corrected.Amount = values.MustDecimal("15.00", 2, values.RoundingExactRequired)
	corrected.SourceTransactionID = "leave-correction-1"
	corrected.IdempotencyKey = "leave-correction-1"
	corrected.RecordedAt, corrected.AuthorizedAt = recorded, recorded
	return d, original, corrected
}

func correctionDeps() []CorrectionDependency {
	return []CorrectionDependency{{ID: "balance-result", Owner: "balance", Version: "balance/1"}, {ID: "payroll-result", Owner: "payroll", Version: "payroll/1", DependsOn: []string{"balance-result"}}, {ID: "tax-result", Owner: "tax", Version: "tax/1", DependsOn: []string{"payroll-result"}}, {ID: "benefits-result", Owner: "benefits", Version: "benefits/1", DependsOn: []string{"tax-result"}}}
}

func TestTodo_BAL_006(t *testing.T) {
	d, original, corrected := correctionFixture(t)
	result, err := ApplyRetroCorrection(RetroCorrectionRequest{Definition: d, Original: original, Corrected: corrected, Ledger: []BalanceEntry{original}, Opening: values.MustDecimal("0.00", 2, values.RoundingExactRequired), ExpectedDependencies: correctionDeps(), Dependencies: correctionDeps()})
	if err != nil {
		t.Fatal(err)
	}
	if result.Correction.SupersedesDigest != original.Digest() || result.Correction.Amount.String() != "5.00" || result.Balance.Ending.String() != "15.00" {
		t.Fatalf("result=%+v", result)
	}
	if result.Correction.SourceTransactionID != corrected.SourceTransactionID || result.Correction.IdempotencyKey != corrected.IdempotencyKey {
		t.Fatalf("correction replaced governed transaction identity: %+v", result.Correction)
	}
	if len(result.Impacts) != 4 || result.Impacts[0].ID != "balance-result" || result.Impacts[3].State != ReconciliationRequired {
		t.Fatalf("impacts=%+v", result.Impacts)
	}
}

func TestTodo_BAL_006_Property(t *testing.T) {
	d, original, corrected := correctionFixture(t)
	result, err := ApplyRetroCorrection(RetroCorrectionRequest{Definition: d, Original: original, Corrected: corrected, Ledger: []BalanceEntry{original}, Opening: values.MustDecimal("0.00", 2, values.RoundingExactRequired), ExpectedDependencies: correctionDeps(), Dependencies: correctionDeps()})
	if err != nil {
		t.Fatal(err)
	}
	if result.OriginalDigest != original.Digest() || original.Amount.String() != "10.00" || result.Correction.Kind != Credit {
		t.Fatalf("lineage mutated: original=%+v correction=%+v", original, result.Correction)
	}
}

func TestTodo_BAL_006_Race(t *testing.T) {
	d, original, corrected := correctionFixture(t)
	req := RetroCorrectionRequest{Definition: d, Original: original, Corrected: corrected, Ledger: []BalanceEntry{original}, Opening: values.MustDecimal("0.00", 2, values.RoundingExactRequired), ExpectedDependencies: correctionDeps(), Dependencies: correctionDeps()}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := ApplyRetroCorrection(req)
			if err != nil || result.Digest == "" {
				t.Errorf("result=%+v err=%v", result, err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_BAL_006_Mutation(t *testing.T) {
	d, original, corrected := correctionFixture(t)
	existing := corrected
	existing.SupersedesDigest = original.Digest()
	ledger := []BalanceEntry{original, existing}
	if _, err := ApplyRetroCorrection(RetroCorrectionRequest{Definition: d, Original: original, Corrected: corrected, Ledger: ledger, Opening: values.MustDecimal("0.00", 2, values.RoundingExactRequired), ExpectedDependencies: correctionDeps(), Dependencies: correctionDeps()}); !errors.Is(err, ErrCorrectionConflict) {
		t.Fatalf("duplicate lineage err=%v", err)
	}
	missing := correctionDeps()[:3]
	if _, err := ApplyRetroCorrection(RetroCorrectionRequest{Definition: d, Original: original, Corrected: corrected, Ledger: []BalanceEntry{original}, Opening: values.MustDecimal("0.00", 2, values.RoundingExactRequired), ExpectedDependencies: correctionDeps(), Dependencies: missing}); err == nil {
		t.Fatal("missing downstream owner accepted")
	}
	if _, err := ApplyRetroCorrection(RetroCorrectionRequest{Definition: d, Original: original, Corrected: corrected, Ledger: nil, Opening: values.MustDecimal("0.00", 2, values.RoundingExactRequired), ExpectedDependencies: correctionDeps(), Dependencies: correctionDeps()}); !errors.Is(err, ErrCorrectionInvalid) {
		t.Fatalf("non-member original err=%v", err)
	}
	wrongOrder := correctionDeps()
	wrongOrder[2].DependsOn = []string{"balance-result"}
	if _, err := ApplyRetroCorrection(RetroCorrectionRequest{Definition: d, Original: original, Corrected: corrected, Ledger: []BalanceEntry{original}, Opening: values.MustDecimal("0.00", 2, values.RoundingExactRequired), ExpectedDependencies: correctionDeps(), Dependencies: wrongOrder}); !errors.Is(err, ErrCorrectionInvalid) {
		t.Fatalf("incomplete dependency graph err=%v", err)
	}
	falseCompletion := correctionDeps()
	falseCompletion[0].State = RecalculationComplete
	if _, err := ApplyRetroCorrection(RetroCorrectionRequest{Definition: d, Original: original, Corrected: corrected, Ledger: []BalanceEntry{original}, Opening: values.MustDecimal("0.00", 2, values.RoundingExactRequired), ExpectedDependencies: correctionDeps(), Dependencies: falseCompletion}); !errors.Is(err, ErrCorrectionInvalid) {
		t.Fatalf("completion without evidence err=%v", err)
	}
}

func TestTodo_BAL_006_GoldenMaterialIncludesTimeAndDependencyGraph(t *testing.T) {
	d, original, corrected := correctionFixture(t)
	req := RetroCorrectionRequest{Definition: d, Original: original, Corrected: corrected, Ledger: []BalanceEntry{original}, Opening: values.MustDecimal("0.00", 2, values.RoundingExactRequired), ExpectedDependencies: correctionDeps(), Dependencies: correctionDeps()}
	first, err := ApplyRetroCorrection(req)
	if err != nil {
		t.Fatal(err)
	}
	req.Corrected.EffectiveAt = correctionInstant(t, "2026-06-01T00:00:00.000001Z")
	second, err := ApplyRetroCorrection(req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Correction.Digest() == second.Correction.Digest() || first.Digest == second.Digest {
		t.Fatal("correction digest did not bind exact effective time")
	}
}
