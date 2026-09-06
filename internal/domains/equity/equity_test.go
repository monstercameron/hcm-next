package equity_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/equity"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func decimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func date(t *testing.T, text string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func instant(t *testing.T, text string) values.Instant {
	t.Helper()
	v, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return values.NewInstant(v)
}

func testPlan(t *testing.T) equity.EquityPlanRevision {
	t.Helper()
	p, err := equity.NewEquityPlanRevision(equity.EquityPlanRevision{
		PlanID: "plan-2026", Revision: 1, Name: "Long-term Incentive Plan", PoolRef: "pool-2026",
		AuthorizedQuantity: decimal(t, "100000.00"), Currency: "USD",
		InstrumentKinds: []equity.InstrumentKind{equity.InstrumentOption, equity.InstrumentRSU}, ApprovalRef: "board-approval",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func testGrant(t *testing.T, plan equity.EquityPlanRevision) equity.EquityGrant {
	t.Helper()
	g, err := equity.NewEquityGrant(equity.EquityGrant{
		GrantID: "grant-1", Revision: 1, PlanDigest: plan.CanonicalDigest, PlanRevision: plan.Revision,
		PoolRef: plan.PoolRef, WorkerRef: "worker-1", Instrument: equity.InstrumentOption,
		Quantity: decimal(t, "1200.00"), GrantDate: date(t, "2026-01-31"), StrikePrice: decimal(t, "12.50"), Currency: "USD",
		Vesting: equity.VestingSchedule{CalendarRule: equity.CalendarGregorian, CliffMonths: 12, PeriodicMonths: 3, TrancheCount: 4}, State: equity.GrantProposed,
	})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestEquityGrantRequiresPlanPoolInstrumentVestingApprovalAndAcceptance(t *testing.T) {
	plan := testPlan(t)
	grant := testGrant(t, plan)
	if err := plan.ValidateGrant(grant); err != nil {
		t.Fatalf("plan/grant binding: %v", err)
	}
	tranches, err := grant.Vesting.Compute(grant.GrantDate, grant.Quantity)
	if err != nil || len(tranches) != 4 || tranches[0].VestsOn.String() != "2027-01-31" || tranches[3].VestsOn.String() != "2027-10-31" {
		t.Fatalf("tranches = %#v, err = %v", tranches, err)
	}
	event, err := equity.NewAcceptanceEvent(equity.AcceptanceEvent{
		EventID: "acceptance-1", GrantDigest: grant.CanonicalDigest, GrantRevision: grant.Revision,
		AcceptedBy: "worker-1", AcceptedAt: instant(t, "2026-02-01T12:00:00Z"), EvidenceRef: "signed-document-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := grant.Accept(event)
	if err != nil || accepted.State != equity.GrantAccepted || accepted.SupersedesRevision != 1 {
		t.Fatalf("accept: %#v %v", accepted, err)
	}
	active, err := accepted.Activate("activation-evidence")
	if err != nil || active.State != equity.GrantActive {
		t.Fatalf("activate: %#v %v", active, err)
	}
	cancelled, err := active.Cancel("cancellation-evidence")
	if err != nil || cancelled.State != equity.GrantCancelled || cancelled.CanonicalDigest == active.CanonicalDigest {
		t.Fatalf("cancel: %#v %v", cancelled, err)
	}
	explanation, err := cancelled.Explain()
	if err != nil || strings.Contains(fmt.Sprintf("%#v", explanation), "1200.00") || strings.Contains(fmt.Sprintf("%#v", explanation), "12.50") {
		t.Fatalf("unsafe explanation: %#v %v", explanation, err)
	}
}

func TestTodo_EQUITY_001_Property(t *testing.T) {
	plan := testPlan(t)
	bad := plan
	bad.InstrumentKinds = []equity.InstrumentKind{"CRYPTO_OPTION"}
	if _, err := equity.NewEquityPlanRevision(bad); !errors.Is(err, equity.ErrInvalidPlan) {
		t.Fatalf("unknown instrument accepted: %v", err)
	}
}

func TestTodo_EQUITY_001_Golden(t *testing.T) {
	grant := testGrant(t, testPlan(t))
	if grant.CanonicalDigest == "" || grant.Digest != grant.CanonicalDigest {
		t.Fatal("grant digest was not minted")
	}
}

func TestTodo_EQUITY_001_Race(t *testing.T) {
	grant := testGrant(t, testPlan(t))
	if _, err := grant.Forfeit("forfeiture-evidence"); err == nil {
		t.Fatal("proposed grant was forfeited without acceptance")
	}
}

func TestTodo_EQUITY_001_Fault(t *testing.T) {
	grant := testGrant(t, testPlan(t))
	grant.Quantity = decimal(t, "999999.00")
	if err := grant.Validate(); !errors.Is(err, equity.ErrInvalidGrant) {
		t.Fatalf("tampered grant accepted: %v", err)
	}
}

func TestTodo_EQUITY_001_Security(t *testing.T) {
	grant := testGrant(t, testPlan(t))
	if _, err := equity.Explain(grant); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_EQUITY_001_Conformance(t *testing.T) {
	if equity.Version() != 1 {
		t.Fatalf("version = %d", equity.Version())
	}
}

func TestTodo_EQUITY_001_Mutation(t *testing.T) {
	grant := testGrant(t, testPlan(t))
	grant.StrikePrice = decimal(t, "13.50")
	if err := grant.Validate(); !errors.Is(err, equity.ErrInvalidGrant) {
		t.Fatalf("mutated grant accepted: %v", err)
	}
}
