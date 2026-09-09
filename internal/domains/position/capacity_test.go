package position_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// fte builds a fixed-decimal FTE amount at the fixture's declared capacity
// scale (4), so occupant and pending-proposal amounts always compare exactly
// against CapacityFTE with no float and no scale-mismatch error.
func fte(t testing.TB, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("fte %q: %v", text, err)
	}
	return d
}

func workerRefFor(t testing.TB, tenant values.TenantId, id string) values.EntityRef {
	t.Helper()
	ref := values.EntityRef{Tenant: tenant, Kind: "worker", Id: id}
	if err := ref.Validate(); err != nil {
		t.Fatalf("worker ref: %v", err)
	}
	return ref
}

func proposalRefFor(t testing.TB, tenant values.TenantId, id string) values.EntityRef {
	t.Helper()
	ref := values.EntityRef{Tenant: tenant, Kind: "proposal", Id: id}
	if err := ref.Validate(); err != nil {
		t.Fatalf("proposal ref: %v", err)
	}
	return ref
}

func interval(t testing.TB, start, end string) values.EffectiveInterval {
	t.Helper()
	if end == "" {
		iv, err := values.NewOpenLocalDateInterval(mustDate(t, start), testCalendar)
		if err != nil {
			t.Fatalf("open interval [%s,): %v", start, err)
		}
		return iv
	}
	iv, err := values.NewLocalDateInterval(mustDate(t, start), mustDate(t, end), testCalendar)
	if err != nil {
		t.Fatalf("interval [%s,%s): %v", start, end, err)
	}
	return iv
}

// TestTodo_POSITION_002 is the registry's exact primary matrix symbol.
//
// It proves the GREEN clause - exact fixed-decimal consumed, reserved and
// available heads/FTE, plus typed conflicts - and, as subtests, each RED
// vector named for this todo: over-capacity heads/FTE, overlapping exclusive
// occupancy and a fractional/float-free vacancy-after-date computation.
func TestTodo_POSITION_002(t *testing.T) {
	ctx := context.Background()
	rev := validRevision(t, "tenant-a", "pos-1") // CapacityFTE=2.0000, CapacityHeads=2, OverfillAllowed=false
	reader := newMemoryPositionFacts(rev)
	asOf := testAsOf(t) // effective 2026-06-01

	t.Run("exact consumed/reserved/available at capacity", func(t *testing.T) {
		req := position.CapacityRequest{
			Tenant: "tenant-a", Position: rev.Position, AsOf: asOf,
			Occupants: []position.Occupant{
				{Worker: workerRefFor(t, "tenant-a", "11111111-1111-4111-8111-100000000001"),
					FTE: fte(t, "1.0000"), Effective: interval(t, "2026-01-01", ""), Exclusive: true},
			},
			Pending: []position.PendingProposal{
				{Proposal: proposalRefFor(t, "tenant-a", "22222222-2222-4222-8222-200000000001"),
					FTE: fte(t, "1.0000"), Effective: interval(t, "2026-05-01", ""), Exclusive: true},
			},
		}
		result, err := position.CalculateCapacity(ctx, reader, req)
		if err != nil {
			t.Fatalf("CalculateCapacity: %v", err)
		}
		if !result.Exists {
			t.Fatal("existing position reported as not found")
		}
		if result.ConsumedHeads != 1 || result.ReservedHeads != 1 {
			t.Fatalf("consumed=%d reserved=%d heads, want 1 and 1", result.ConsumedHeads, result.ReservedHeads)
		}
		if !result.ConsumedFTE.Equal(fte(t, "1.0000")) || !result.ReservedFTE.Equal(fte(t, "1.0000")) {
			t.Fatalf("consumed=%s reserved=%s fte, want 1.0000 and 1.0000", result.ConsumedFTE, result.ReservedFTE)
		}
		if result.AvailableHeads != 0 {
			t.Errorf("available heads = %d, want 0 (2 capacity - 1 consumed - 1 reserved)", result.AvailableHeads)
		}
		if !result.AvailableFTE.Equal(fte(t, "0.0000")) {
			t.Errorf("available fte = %s, want 0.0000", result.AvailableFTE)
		}
		if len(result.Findings) != 0 {
			t.Fatalf("exactly-at-capacity must not be reported over capacity, got %v", result.Findings)
		}
	})

	t.Run("over-capacity heads and FTE are typed findings, never silently clamped", func(t *testing.T) {
		req := position.CapacityRequest{
			Tenant: "tenant-a", Position: rev.Position, AsOf: asOf,
			Occupants: []position.Occupant{
				{Worker: workerRefFor(t, "tenant-a", "11111111-1111-4111-8111-100000000002"),
					FTE: fte(t, "1.5000"), Effective: interval(t, "2026-01-01", ""), Exclusive: true},
				{Worker: workerRefFor(t, "tenant-a", "11111111-1111-4111-8111-100000000003"),
					FTE: fte(t, "1.5000"), Effective: interval(t, "2026-01-01", ""), Exclusive: true},
				{Worker: workerRefFor(t, "tenant-a", "11111111-1111-4111-8111-100000000004"),
					FTE: fte(t, "1.5000"), Effective: interval(t, "2026-01-01", ""), Exclusive: true},
			},
		}
		result, err := position.CalculateCapacity(ctx, reader, req)
		if err != nil {
			t.Fatalf("CalculateCapacity: %v", err)
		}
		wantCodes := map[position.FindingCode]bool{
			position.FindingOverCapacityHeads: false,
			position.FindingOverCapacityFTE:   false,
		}
		for _, f := range result.Findings {
			if _, ok := wantCodes[f.Code]; ok {
				wantCodes[f.Code] = true
			}
		}
		for code, seen := range wantCodes {
			if !seen {
				t.Errorf("missing expected finding %s in %v", code, result.Findings)
			}
		}
		// Nothing clamps the exact negative result to zero: three heads of 1.5
		// FTE against a 2-head/2.0000-FTE capacity leaves an exact negative
		// available amount, not a floored zero.
		if result.AvailableHeads != -1 {
			t.Errorf("available heads = %d, want -1 (exact, not clamped)", result.AvailableHeads)
		}
		if !result.AvailableFTE.Equal(fte(t, "-2.5000")) {
			t.Errorf("available fte = %s, want -2.5000 (exact, not clamped)", result.AvailableFTE)
		}
	})

	t.Run("overfill-allowed position suppresses the over-capacity findings", func(t *testing.T) {
		overfill := rev
		overfill.Capacity.OverfillAllowed = true
		reader := newMemoryPositionFacts(overfill)
		req := position.CapacityRequest{
			Tenant: "tenant-a", Position: overfill.Position, AsOf: asOf,
			Occupants: []position.Occupant{
				{Worker: workerRefFor(t, "tenant-a", "11111111-1111-4111-8111-100000000005"),
					FTE: fte(t, "3.0000"), Effective: interval(t, "2026-01-01", ""), Exclusive: false},
			},
		}
		result, err := position.CalculateCapacity(ctx, reader, req)
		if err != nil {
			t.Fatalf("CalculateCapacity: %v", err)
		}
		for _, f := range result.Findings {
			if f.Code == position.FindingOverCapacityFTE || f.Code == position.FindingOverCapacityHeads {
				t.Fatalf("overfill-allowed position must not report over-capacity, got %v", result.Findings)
			}
		}
	})

	t.Run("overlapping exclusive occupancy is a typed conflict, not a silent double count", func(t *testing.T) {
		req := position.CapacityRequest{
			Tenant: "tenant-a", Position: rev.Position, AsOf: asOf,
			Occupants: []position.Occupant{
				{Worker: workerRefFor(t, "tenant-a", "11111111-1111-4111-8111-100000000006"),
					FTE: fte(t, "1.0000"), Effective: interval(t, "2026-01-01", "2026-12-01"), Exclusive: true},
				{Worker: workerRefFor(t, "tenant-a", "11111111-1111-4111-8111-100000000007"),
					FTE: fte(t, "1.0000"), Effective: interval(t, "2026-03-01", "2026-09-01"), Exclusive: true},
			},
		}
		result, err := position.CalculateCapacity(ctx, reader, req)
		if err != nil {
			t.Fatalf("CalculateCapacity: %v", err)
		}
		found := false
		for _, f := range result.Findings {
			if f.Code == position.FindingOverlappingExclusiveOccupancy {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected OVERLAPPING_EXCLUSIVE_OCCUPANCY finding, got %v", result.Findings)
		}
	})

	t.Run("vacancy-after-date fires on a determinate end with no successor", func(t *testing.T) {
		req := position.CapacityRequest{
			Tenant: "tenant-a", Position: rev.Position, AsOf: asOf,
			Occupants: []position.Occupant{
				{Worker: workerRefFor(t, "tenant-a", "11111111-1111-4111-8111-100000000008"),
					FTE: fte(t, "1.0000"), Effective: interval(t, "2026-01-01", "2026-07-01"), Exclusive: true},
			},
		}
		result, err := position.CalculateCapacity(ctx, reader, req)
		if err != nil {
			t.Fatalf("CalculateCapacity: %v", err)
		}
		if !result.HasVacantAfter {
			t.Fatal("expected a vacancy-after-date finding")
		}
		want := mustDate(t, "2026-07-01")
		if result.VacantAfter.Compare(want) != 0 {
			t.Errorf("vacant after = %s, want %s", result.VacantAfter, want)
		}
		hasFinding := false
		for _, f := range result.Findings {
			if f.Code == position.FindingVacantAfterDate {
				hasFinding = true
			}
		}
		if !hasFinding {
			t.Errorf("VACANT_AFTER_DATE finding missing from %v", result.Findings)
		}
	})

	t.Run("a contiguous successor never trips vacancy-after-date", func(t *testing.T) {
		req := position.CapacityRequest{
			Tenant: "tenant-a", Position: rev.Position, AsOf: asOf,
			Occupants: []position.Occupant{
				{Worker: workerRefFor(t, "tenant-a", "11111111-1111-4111-8111-100000000009"),
					FTE: fte(t, "1.0000"), Effective: interval(t, "2026-01-01", "2026-07-01"), Exclusive: true},
				{Worker: workerRefFor(t, "tenant-a", "11111111-1111-4111-8111-10000000000a"),
					FTE: fte(t, "1.0000"), Effective: interval(t, "2026-07-01", ""), Exclusive: true},
			},
		}
		result, err := position.CalculateCapacity(ctx, reader, req)
		if err != nil {
			t.Fatalf("CalculateCapacity: %v", err)
		}
		if result.HasVacantAfter {
			t.Fatalf("a contiguous hand-off must not report vacancy, got vacant after %s", result.VacantAfter)
		}
	})

	t.Run("a not-found position reports the typed finding, not zero capacity", func(t *testing.T) {
		missing := positionRef(t, "tenant-a", "pos-missing")
		result, err := position.CalculateCapacity(ctx, reader, position.CapacityRequest{
			Tenant: "tenant-a", Position: missing, AsOf: asOf,
		})
		if err != nil {
			t.Fatalf("CalculateCapacity: %v", err)
		}
		if result.Exists {
			t.Fatal("a position the reader never seeded must not be reported as existing")
		}
		if len(result.Findings) != 1 || result.Findings[0].Code != position.FindingNotFound {
			t.Fatalf("findings = %v, want exactly [POSITION_NOT_FOUND]", result.Findings)
		}
	})
}

// TestTodo_POSITION_002_Property is the registry's exact property matrix
// symbol: for any combination of occupant/pending FTE and exclusivity,
// Available must equal Capacity minus Consumed+Reserved exactly, and an
// over-capacity finding must appear if and only if the totals exceed
// capacity under a non-overfill policy.
func TestTodo_POSITION_002_Property(t *testing.T) {
	ctx := context.Background()
	rev := validRevision(t, "tenant-a", "pos-1")
	reader := newMemoryPositionFacts(rev)
	asOf := testAsOf(t)

	amounts := []string{"0.0000", "0.5000", "1.0000", "1.5000", "2.5000"}
	for _, occAmt := range amounts {
		for _, pendAmt := range amounts {
			for _, occExclusive := range []bool{false, true} {
				name := fmt.Sprintf("occ=%s(excl=%v)/pend=%s", occAmt, occExclusive, pendAmt)
				t.Run(name, func(t *testing.T) {
					req := position.CapacityRequest{
						Tenant: "tenant-a", Position: rev.Position, AsOf: asOf,
						Occupants: []position.Occupant{
							{Worker: workerRefFor(t, "tenant-a", "11111111-1111-4111-8111-100000000010"),
								FTE: fte(t, occAmt), Effective: interval(t, "2026-01-01", ""), Exclusive: occExclusive},
						},
						Pending: []position.PendingProposal{
							{Proposal: proposalRefFor(t, "tenant-a", "22222222-2222-4222-8222-200000000010"),
								FTE: fte(t, pendAmt), Effective: interval(t, "2026-01-01", ""), Exclusive: false},
						},
					}
					result, err := position.CalculateCapacity(ctx, reader, req)
					if err != nil {
						t.Fatalf("CalculateCapacity: %v", err)
					}
					gotTotalFTE, err := result.ConsumedFTE.Add(result.ReservedFTE)
					if err != nil {
						t.Fatalf("add: %v", err)
					}
					wantAvailableFTE, err := result.CapacityFTE.Sub(gotTotalFTE)
					if err != nil {
						t.Fatalf("sub: %v", err)
					}
					if !result.AvailableFTE.Equal(wantAvailableFTE) {
						t.Fatalf("available fte = %s, want capacity-consumed-reserved = %s", result.AvailableFTE, wantAvailableFTE)
					}
					wantAvailableHeads := result.CapacityHeads - result.ConsumedHeads - result.ReservedHeads
					if result.AvailableHeads != wantAvailableHeads {
						t.Fatalf("available heads = %d, want %d", result.AvailableHeads, wantAvailableHeads)
					}
					overFTE := gotTotalFTE.Cmp(result.CapacityFTE) > 0
					overHeads := (result.ConsumedHeads + result.ReservedHeads) > result.CapacityHeads
					hasOverFinding := false
					for _, f := range result.Findings {
						if f.Code == position.FindingOverCapacityFTE || f.Code == position.FindingOverCapacityHeads {
							hasOverFinding = true
						}
						if !f.Code.Valid() {
							t.Fatalf("undefined finding code %q", f.Code)
						}
					}
					if (overFTE || overHeads) != hasOverFinding {
						t.Fatalf("overFTE=%v overHeads=%v but hasOverFinding=%v (findings=%v)", overFTE, overHeads, hasOverFinding, result.Findings)
					}
				})
			}
		}
	}
}

// TestTodo_POSITION_002_Race is the registry's exact race matrix symbol: many
// goroutines calling the pure calculation concurrently over the same and
// different inputs must never corrupt or cross-contaminate a result, with or
// without the race detector attached.
func TestTodo_POSITION_002_Race(t *testing.T) {
	ctx := context.Background()
	rev := validRevision(t, "tenant-a", "pos-1")
	reader := newMemoryPositionFacts(rev)
	asOf := testAsOf(t)

	req := position.CapacityRequest{
		Tenant: "tenant-a", Position: rev.Position, AsOf: asOf,
		Occupants: []position.Occupant{
			{Worker: workerRefFor(t, "tenant-a", "11111111-1111-4111-8111-100000000020"),
				FTE: fte(t, "1.0000"), Effective: interval(t, "2026-01-01", ""), Exclusive: true},
		},
	}
	want, err := position.CalculateCapacity(ctx, reader, req)
	if err != nil {
		t.Fatalf("CalculateCapacity: %v", err)
	}
	wantCanonical := string(want.Canonical())

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := position.CalculateCapacity(ctx, reader, req)
			if err != nil {
				errs <- err
				return
			}
			if string(got.Canonical()) != wantCanonical {
				errs <- fmt.Errorf("concurrent result diverged from the sequential baseline")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// BenchmarkTodo_POSITION_002 is the registry's required benchmark matrix
// symbol.
func BenchmarkTodo_POSITION_002(b *testing.B) {
	ctx := context.Background()
	rev := validRevision(b, "tenant-a", "pos-1")
	reader := newMemoryPositionFacts(rev)
	req := position.CapacityRequest{
		Tenant: "tenant-a", Position: rev.Position, AsOf: testAsOf(b),
		Occupants: []position.Occupant{
			{Worker: workerRefFor(b, "tenant-a", "11111111-1111-4111-8111-100000000030"),
				FTE: fte(b, "1.0000"), Effective: interval(b, "2026-01-01", ""), Exclusive: true},
		},
		Pending: []position.PendingProposal{
			{Proposal: proposalRefFor(b, "tenant-a", "22222222-2222-4222-8222-200000000030"),
				FTE: fte(b, "0.5000"), Effective: interval(b, "2026-05-01", ""), Exclusive: false},
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := position.CalculateCapacity(ctx, reader, req); err != nil {
			b.Fatalf("CalculateCapacity: %v", err)
		}
	}
}

// TestTodo_POSITION_002_Mutation is the registry's exact mutation matrix
// symbol: every material change to a capacity result must change its
// canonical encoding.
func TestTodo_POSITION_002_Mutation(t *testing.T) {
	ctx := context.Background()
	rev := validRevision(t, "tenant-a", "pos-1")
	reader := newMemoryPositionFacts(rev)
	req := position.CapacityRequest{
		Tenant: "tenant-a", Position: rev.Position, AsOf: testAsOf(t),
		Occupants: []position.Occupant{
			{Worker: workerRefFor(t, "tenant-a", "11111111-1111-4111-8111-100000000040"),
				FTE: fte(t, "1.0000"), Effective: interval(t, "2026-01-01", "2026-07-01"), Exclusive: true},
		},
	}
	base, err := position.CalculateCapacity(ctx, reader, req)
	if err != nil {
		t.Fatalf("CalculateCapacity: %v", err)
	}
	baseCanonical := string(base.Canonical())
	if baseCanonical == "" {
		t.Fatal("valid capacity result has no canonical encoding")
	}

	cases := []struct {
		name   string
		mutate func(*position.CapacityResult)
	}{
		{"consumed heads", func(r *position.CapacityResult) { r.ConsumedHeads = 2 }},
		{"available heads", func(r *position.CapacityResult) { r.AvailableHeads = 5 }},
		{"consumed fte", func(r *position.CapacityResult) { r.ConsumedFTE = fte(t, "2.0000") }},
		{"reserved fte", func(r *position.CapacityResult) { r.ReservedFTE = fte(t, "0.2500") }},
		{"vacant after", func(r *position.CapacityResult) {
			r.VacantAfter = mustDate(t, "2026-08-01")
			r.HasVacantAfter = true
		}},
		{"overfill allowed", func(r *position.CapacityResult) { r.OverfillAllowed = true }},
		{"findings", func(r *position.CapacityResult) {
			r.Findings = append(r.Findings, position.Finding{Code: position.FindingOverCapacityHeads, Detail: "x"})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := base
			tc.mutate(&result)
			if got := string(result.Canonical()); got == baseCanonical {
				t.Fatal("material capacity-result mutation did not change canonical encoding")
			}
		})
	}
}
