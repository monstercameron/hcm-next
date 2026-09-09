package balance

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func lifecycleTestRequest(policy BalanceLifecyclePolicy) LifecycleRequest {
	start := instant(2026, 1, 1, 0, 0, 0)
	middle := instant(2026, 1, 15, 0, 0, 0)
	end := instant(2026, 2, 1, 0, 0, 0)
	targetEnd := instant(2026, 3, 1, 0, 0, 0)
	template := ruleTestEntry(Credit, "1.00", "template")
	return LifecycleRequest{
		AccountID: "acct-1",
		Period:    PeriodDefinition{ID: "p-2026-01", Version: "period-v4", Kind: PeriodCalendarYear, Start: start, End: end},
		Next:      PeriodDefinition{ID: "p-2026-02", Version: "period-v4", Kind: PeriodCalendarYear, Start: end, End: targetEnd},
		Template:  template,
		Opening:   decimal2("0.00"), Scale: 2, Rounding: decimal2("0.00").Rounding(), Policy: policy,
		Ledger: []BalanceEntry{
			bal003Entry(Credit, "GRANT", "8.00", "grant", middle, middle),
			bal003Entry(Debit, "USAGE", "2.00", "usage", middle, middle),
			bal003Entry(Credit, "GRANT", "99.00", "next-period", end, end),
		},
	}
}

func rolloverPolicy() BalanceLifecyclePolicy {
	return BalanceLifecyclePolicy{
		Expiry:    ExpiryPolicy{ID: "expiry", Version: "e2", Enabled: true},
		Carryover: CarryoverPolicy{ID: "carry", Version: "c5", Enabled: true, Cap: decimal2("3.00")},
		Rollover:  RolloverPolicy{ID: "roll", Version: "r9", Enabled: true},
	}
}

func TestTodo_BAL_005(t *testing.T) {
	got, err := ApplyLifecycle(lifecycleTestRequest(rolloverPolicy()))
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceBalance.String() != "6.00" || got.Expired.String() != "3.00" || got.Carried.String() != "3.00" {
		t.Fatalf("balances source=%s expired=%s carried=%s", got.SourceBalance, got.Expired, got.Carried)
	}
	if len(got.Entries) != 3 || len(got.Explanation) != 3 {
		t.Fatalf("entries=%d explanations=%d", len(got.Entries), len(got.Explanation))
	}
	if got.Entries[0].Operation != LifecycleExpiry || got.Entries[0].PolicyVersion != "e2" || got.Entries[0].Entry.EffectiveAt != got.SourcePeriod.End {
		t.Fatalf("expiry entry=%+v", got.Entries[0])
	}
	if got.Entries[1].Operation != LifecycleRollover || got.Entries[2].Entry.Kind != Credit || got.Entries[2].Entry.EffectiveAt != got.TargetPeriod.Start {
		t.Fatalf("rollover entries=%+v", got.Entries[1:])
	}
	if got.Entries[2].PolicyID != "roll" || got.Entries[2].PolicyVersion != "r9" || got.Digest == "" {
		t.Fatalf("provenance/digest=%+v", got.Entries[2])
	}
}

func TestTodo_BAL_005_Property(t *testing.T) {
	req := lifecycleTestRequest(rolloverPolicy())
	first, err := ApplyLifecycle(req)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range first.Entries {
		req.Ledger = append(req.Ledger, entry.Entry)
	}
	second, err := ApplyLifecycle(req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest || len(second.Entries) != len(first.Entries) {
		t.Fatalf("repeat changed lifecycle: %s/%s", first.Digest, second.Digest)
	}
	if req.Ledger[0].Amount.String() != "8.00" {
		t.Fatal("lifecycle mutated source ledger")
	}
}

func TestTodo_BAL_005_Golden(t *testing.T) {
	policy := BalanceLifecyclePolicy{Expiry: ExpiryPolicy{ID: "expiry", Version: "1", Enabled: true}}
	got, err := ApplyLifecycle(lifecycleTestRequest(policy))
	if err != nil {
		t.Fatal(err)
	}
	if got.Expired.String() != "6.00" || got.Carried.String() != "0.00" || len(got.Entries) != 1 || got.Entries[0].Entry.EntryType != ExpiryEntryType || got.Entries[0].Reason != "UNUSED_VALUE_EXPIRED_AT_PERIOD_END" {
		t.Fatalf("expiry golden=%+v", got)
	}
}

func TestTodo_BAL_005_Race(t *testing.T) {
	req := lifecycleTestRequest(rolloverPolicy())
	want, err := ApplyLifecycle(req)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, e := ApplyLifecycle(req)
			if e != nil || got.Digest != want.Digest {
				t.Errorf("parallel lifecycle digest=%q err=%v", got.Digest, e)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_BAL_005_Mutation(t *testing.T) {
	base := lifecycleTestRequest(rolloverPolicy())
	cases := []struct {
		name   string
		mutate func(*LifecycleRequest)
	}{
		{"period_gap", func(r *LifecycleRequest) { r.Next.Start = instant(2026, 2, 2, 0, 0, 0) }},
		{"period_kind", func(r *LifecycleRequest) { r.Next.Kind = PeriodFiscalYear }},
		{"missing_policy_version", func(r *LifecycleRequest) { r.Policy.Expiry.Version = "" }},
		{"missing_effective_time", func(r *LifecycleRequest) { r.Ledger[0].EffectiveAt = values.Instant{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			req.Ledger = append([]BalanceEntry(nil), base.Ledger...)
			tc.mutate(&req)
			if _, err := ApplyLifecycle(req); err == nil || (!errors.Is(err, ErrLifecycleInvalid) && !errors.Is(err, ErrPeriodInvalid)) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
