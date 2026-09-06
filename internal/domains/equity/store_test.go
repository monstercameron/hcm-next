package equity_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/equity"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func memoryDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	value, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func memoryPlan(t *testing.T) equity.EquityPlanRevision {
	t.Helper()
	plan, err := equity.NewEquityPlanRevision(equity.EquityPlanRevision{
		PlanID: "plan-memory", Revision: 1, Name: "Memory Plan", PoolRef: "pool-memory",
		AuthorizedQuantity: memoryDecimal(t, "100.00"), Currency: "USD",
		InstrumentKinds: []equity.InstrumentKind{equity.InstrumentOption}, ApprovalRef: "approval-memory",
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func memoryGrant(t *testing.T, plan equity.EquityPlanRevision) equity.EquityGrant {
	t.Helper()
	grant, err := equity.NewEquityGrant(equity.EquityGrant{
		GrantID: "grant-memory", Revision: 1, PlanDigest: plan.CanonicalDigest, PlanRevision: 1,
		PoolRef: plan.PoolRef, WorkerRef: "00000000-0000-0000-0000-000000000001", Instrument: equity.InstrumentOption,
		Quantity: memoryDecimal(t, "10.00"), GrantDate: func() values.LocalDate { d, _ := values.ParseLocalDate("2026-01-01"); return d }(),
		StrikePrice: memoryDecimal(t, "2.50"), Currency: "USD",
		Vesting: equity.VestingSchedule{CalendarRule: equity.CalendarGregorian, CliffMonths: 12, PeriodicMonths: 3, TrancheCount: 4}, State: equity.GrantProposed,
	})
	if err != nil {
		t.Fatal(err)
	}
	return grant
}

func TestTodo_PERSIST_EQUITY_001_MemoryStore(t *testing.T) {
	store := equity.NewMemoryStore()
	ctx := context.Background()
	plan := memoryPlan(t)
	if err := store.SavePlan(ctx, "tenant-memory", plan); err != nil {
		t.Fatal(err)
	}
	grant := memoryGrant(t, plan)
	if err := store.SaveGrant(ctx, "tenant-memory", grant); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadGrant(ctx, "other-tenant", grant.GrantID, grant.Revision); !errors.Is(err, equity.ErrStoreNotFound) {
		t.Fatalf("cross-tenant memory read = %v", err)
	}
	if err := store.SavePlan(ctx, "tenant-memory", plan); !errors.Is(err, equity.ErrStoreDuplicate) {
		t.Fatalf("duplicate plan = %v", err)
	}
	stale := plan
	stale.Revision = 2
	stale.ParentDigest = "sha256:" + strings.Repeat("0", 64)
	stale.SupersedesRevision = 1
	stale.CanonicalDigest, stale.Digest = "", ""
	stale, err := equity.NewEquityPlanRevision(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(ctx, "tenant-memory", stale); !errors.Is(err, equity.ErrStoreStaleCAS) {
		t.Fatalf("stale plan = %v", err)
	}
}
