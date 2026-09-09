package balance

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func ruleTestEntry(kind EntryKind, amount, key string) BalanceEntry {
	e := bal003Entry(kind, "GRANT", amount, key, instant(2026, 1, 2, 0, 0, 0), instant(2026, 1, 2, 0, 0, 0))
	e.EntryType = "GRANT"
	return e
}

func ruleTestRequest(rules ...BalanceRule) RuleApplicationRequest {
	return RuleApplicationRequest{AccountID: "acct-1", Opening: decimal2("0.00"), Scale: 2, Rounding: values.RoundingExactRequired, Rules: rules}
}

func TestTodo_BAL_004(t *testing.T) {
	threshold := BalanceRule{ID: "credit-limit", Version: "v3", Kind: BalanceRuleThreshold, AppliesTo: Credit, Limit: decimal2("10.00")}
	cap := BalanceRule{ID: "period-cap", Version: "v7", Kind: BalanceRuleCap, Limit: decimal2("8.00")}
	floor := BalanceRule{ID: "minimum", Version: "v2", Kind: BalanceRuleFloor, AppliesTo: Debit, Limit: decimal2("5.00")}
	ledger := []BalanceEntry{ruleTestEntry(Credit, "12.00", "grant-1"), ruleTestEntry(Debit, "2.00", "use-1")}
	got, err := ApplyRules(ruleTestRequest(threshold, cap, floor), ledger)
	if err != nil {
		t.Fatal(err)
	}
	if got.Ending.String() != "6.00" || got.Accepted.String() != "12.00" || got.Rejected.String() != "2.00" || got.Overflow.String() != "2.00" {
		t.Fatalf("result ending=%s accepted=%s rejected=%s overflow=%s", got.Ending, got.Accepted, got.Rejected, got.Overflow)
	}
	if len(got.Adjustments) != 1 || got.Adjustments[0].Reason != "CAP_OVERFLOW_ADJUSTED" || got.Adjustments[0].RuleVersion != "v7" {
		t.Fatalf("adjustments=%+v", got.Adjustments)
	}
	if len(got.Events) != 1 || got.Events[0].Type != "THRESHOLD_BREACHED" {
		t.Fatalf("events=%+v", got.Events)
	}
	floorResult, err := ApplyRules(ruleTestRequest(floor), []BalanceEntry{ruleTestEntry(Debit, "2.00", "use-2")})
	if err != nil || floorResult.Ending.String() != "5.00" || len(floorResult.Adjustments) != 1 || floorResult.Adjustments[0].Entry.Kind != Credit {
		t.Fatalf("floor result=%+v err=%v", floorResult, err)
	}
	if got.Digest == "" {
		t.Fatal("missing derivation digest")
	}
}

func TestTodo_BAL_004_Property(t *testing.T) {
	rules := []BalanceRule{{ID: "cap", Version: "1", Kind: BalanceRuleCap, Limit: decimal2("5.00")}}
	ledger := []BalanceEntry{ruleTestEntry(Credit, "8.00", "grant")}
	first, err := ApplyRules(ruleTestRequest(rules...), ledger)
	if err != nil {
		t.Fatal(err)
	}
	withDerived := append(append([]BalanceEntry(nil), ledger...), first.Adjustments[0].Entry)
	second, err := ApplyRules(ruleTestRequest(rules...), withDerived)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest || first.Ending.String() != second.Ending.String() {
		t.Fatalf("repeated application changed result: %s/%s", first.Digest, second.Digest)
	}
	if ledger[0].Amount.String() != "8.00" || ledger[0].EntryType != "GRANT" {
		t.Fatal("ApplyRules mutated source ledger")
	}
}

func TestTodo_BAL_004_Golden(t *testing.T) {
	rules := []BalanceRule{{ID: "threshold", Version: "2026.1", Kind: BalanceRuleThreshold, Limit: decimal2("4.00")}, {ID: "cap", Version: "2026.2", Kind: BalanceRuleCap, Limit: decimal2("3.00")}}
	got, err := ApplyRules(ruleTestRequest(rules...), []BalanceEntry{ruleTestEntry(Credit, "5.00", "g1")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Ending.String() != "3.00" || got.Decisions[0].Reason != "THRESHOLD_EXCEEDED_REJECTED" || got.Decisions[1].Reason != "CAP_OVERFLOW_ADJUSTED" || got.Adjustments[0].Entry.EntryType != RuleApplicationEntryType {
		t.Fatalf("golden result=%+v", got)
	}
}

func TestTodo_BAL_004_Race(t *testing.T) {
	req := ruleTestRequest(BalanceRule{ID: "cap", Version: "1", Kind: BalanceRuleCap, Limit: decimal2("5.00")})
	ledger := []BalanceEntry{ruleTestEntry(Credit, "9.00", "g1")}
	want, err := ApplyRules(req, ledger)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, e := ApplyRules(req, ledger)
			if e != nil || got.Digest != want.Digest {
				t.Errorf("parallel result digest=%q err=%v", got.Digest, e)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_BAL_004_Mutation(t *testing.T) {
	base := ruleTestRequest(BalanceRule{ID: "cap", Version: "1", Kind: BalanceRuleCap, Limit: decimal2("5.00")})
	cases := []struct {
		name   string
		mutate func(*RuleApplicationRequest)
	}{
		{"missing_account", func(r *RuleApplicationRequest) { r.AccountID = "" }},
		{"unknown_kind", func(r *RuleApplicationRequest) { r.Rules[0].Kind = "OTHER" }},
		{"duplicate_revision", func(r *RuleApplicationRequest) { r.Rules = append(r.Rules, r.Rules[0]) }},
		{"bad_limit", func(r *RuleApplicationRequest) { r.Rules[0].Limit = values.Decimal{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			req.Rules = append([]BalanceRule(nil), base.Rules...)
			tc.mutate(&req)
			if _, err := ApplyRules(req, nil); err == nil || (!errors.Is(err, ErrRuleApplicationInvalid) && !errors.Is(err, ErrRuleInvalid)) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
