package simcomp_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/promotion/simcomp"
	promosnapshot "github.com/monstercameron/hcm-next/internal/domains/promotion/snapshot"
	"github.com/monstercameron/hcm-next/internal/domains/rewards"
)

// TestBandFindingTriggersTheRightApproval proves the three placement classes
// map onto three different outcomes, and that an in-band promotion adds no
// approval on top of the base graph.
func TestBandFindingTriggersTheRightApproval(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		class    string
		approval string
	}{
		{"within", rewards.PositionClassWithin.String(), ""},
		{"above", rewards.PositionClassAbove.String(), simcomp.ApprovalFinancePartner},
		{"below", rewards.PositionClassBelow.String(), simcomp.ApprovalCompensationPartner},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.Budget.ref.AvailableQuantity = mustDecimal(t, "500000.00", 2)
			req := fixtureRequest(t)
			switch tc.name {
			case "above":
				req = aboveBandRequest(t)
			case "below":
				req = belowBandRequest(t)
			}
			snap := h.build(t, req)
			result, err := simcomp.Simulate(simulateRequest(t, snap))
			if err != nil {
				t.Fatalf("Simulate: %v", err)
			}
			if got := result.Band.DesiredClass; got != tc.class {
				t.Fatalf("desired class is %s, want %s", got, tc.class)
			}
			if got := result.Band.ApprovalRequirementID; got != tc.approval {
				t.Fatalf("class %s triggers %q, want %q", tc.class, got, tc.approval)
			}
			if tc.approval == "" && len(result.RequiredApprovals) != 0 {
				t.Fatalf("an in-band promotion required %v", result.RequiredApprovals)
			}
			if tc.approval != "" {
				if len(result.RequiredApprovals) != 1 || result.RequiredApprovals[0] != tc.approval {
					t.Fatalf("required approvals are %v, want [%s]", result.RequiredApprovals, tc.approval)
				}
			}
		})
	}
}

// TestBudgetPlanExplainStatesTheShortfallWithoutInventingOne proves the
// one-line explanation reports exactly what the pool observation disclosed, and
// says why nothing was planned when nothing was.
func TestBudgetPlanExplainStatesTheShortfallWithoutInventingOne(t *testing.T) {
	t.Parallel()

	funded := simulate(t, newHarness(t), nil).Budget
	explanation := funded.Explain()
	for _, want := range []string{"50000.00 USD", "5000.00 USD", "sufficient=true", "45000.00 USD"} {
		if !strings.Contains(explanation, want) {
			t.Fatalf("Explain %q does not report %q", explanation, want)
		}
	}
	if strings.Contains(explanation, "not planned") {
		t.Fatalf("a funded reservation reported itself unplanned: %s", explanation)
	}

	h := newHarness(t)
	h.Budget.ref.AvailableQuantity = mustDecimal(t, "100.00", 2)
	starved := simulate(t, h, nil).Budget
	starvedText := starved.Explain()
	if !strings.Contains(starvedText, "sufficient=false") {
		t.Fatalf("an underfunded pool reported sufficiency: %s", starvedText)
	}
	if !strings.Contains(starvedText, "not planned") {
		t.Fatalf("Explain does not say why nothing was planned: %s", starvedText)
	}

	// A pool nobody was allowed to see says so, and quotes nothing.
	withheld := withheldBudget(t).Budget
	if withheld.Evaluated {
		t.Fatal("a withheld pool was reported as evaluated")
	}
	if got := withheld.Explain(); got != "budget: not evaluated" {
		t.Fatalf("Explain over a withheld pool says %q", got)
	}
}

// TestProrationSplitIsExhaustiveAndExact proves the two day counts cover the
// period exactly and the two portions sum to the period amount, at the declared
// scale, with no float anywhere in the path.
func TestProrationSplitIsExhaustiveAndExact(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		id            string
		start, end    string
		ordinal       int32
		wantPrior     int
		wantNew       int
		wantTotalDays int
	}{
		{"effective date at the period start", "2026-06A", "2026-06-01", "2026-06-16", 11, 0, 15, 15},
		{"effective date mid period", "2026-05B", "2026-05-16", "2026-06-16", 10, 16, 15, 31},
		{"effective date on the last day", "2026-05C", "2026-05-16", "2026-06-02", 10, 16, 1, 17},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := simulate(t, newHarness(t), func(r *simcomp.Request) {
				r.PayPeriod = payPeriod(t, tc.id, tc.start, tc.end, tc.ordinal)
			})
			p := result.Proration
			if !p.Evaluated {
				t.Fatalf("proration was not evaluated:\n%s", result.Explain())
			}
			if p.DaysAtPriorRate != tc.wantPrior || p.DaysAtNewRate != tc.wantNew {
				t.Fatalf("split is %d + %d, want %d + %d",
					p.DaysAtPriorRate, p.DaysAtNewRate, tc.wantPrior, tc.wantNew)
			}
			if p.DaysInPeriod != tc.wantTotalDays {
				t.Fatalf("the period is %d days, want %d", p.DaysInPeriod, tc.wantTotalDays)
			}
			sum, err := p.PriorPortion.Add(p.NewPortion)
			if err != nil {
				t.Fatalf("add the portions: %v", err)
			}
			if sum.String() != p.PeriodAmount.String() {
				t.Fatalf("the portions sum to %s but the period amount is %s", sum, p.PeriodAmount)
			}
			if got := p.PeriodAmount.Amount().Scale(); got != fixtureMoneyScale {
				t.Fatalf("the period amount carries scale %d, want the declared %d", got, fixtureMoneyScale)
			}
			if got := p.PriorDailyRate.Amount().Scale(); got != fixtureRateScale {
				t.Fatalf("the daily rate carries scale %d, want the declared %d", got, fixtureRateScale)
			}
		})
	}
}

// withheldBudget simulates over a snapshot whose pool observation the caller
// was not entitled to see.
func withheldBudget(t testing.TB) simcomp.Result {
	t.Helper()
	h := newHarness(t)
	req := fixtureRequest(t)
	req.Authorization.BudgetDisclosable = false
	req.Authorization.BudgetDenialReason = "policy:no_budget_disclosure"
	partial, buildErr := promosnapshot.Build(t.Context(), h.readers(), req)
	if buildErr == nil {
		t.Fatal("a build with a withheld pool was accepted")
	}
	result, err := simcomp.Simulate(simulateRequest(t, partial))
	if err != nil {
		t.Fatalf("Simulate over a refused snapshot: %v", err)
	}
	return result
}
