package compensation_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/compensation"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func changeDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	rounding, err := values.ParseRoundingMode("HALF_UP")
	if err != nil {
		t.Fatal(err)
	}
	decimal, err := values.NewDecimal(text, 2, rounding)
	if err != nil {
		t.Fatalf("decimal %q: %v", text, err)
	}
	return decimal
}

func changeNow() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) }

func changeLines(t *testing.T) []compensation.ChangeLine {
	return []compensation.ChangeLine{
		{Type: compensation.ComponentBase, Op: compensation.OpRevise, Revision: 4,
			Amount: changeDecimal(t, "95000.00"), HasAmount: true, Currency: "USD", Frequency: "annual"},
		{Type: compensation.ComponentBonusTarget, Op: compensation.OpRevise, Revision: 2,
			Amount: changeDecimal(t, "10000.00"), HasAmount: true, Currency: "USD", Frequency: "annual"},
		{Type: compensation.ComponentAllowance, Op: compensation.OpRevise, Revision: 7,
			Amount: changeDecimal(t, "500.00"), HasAmount: true, Currency: "USD", Frequency: "monthly"},
	}
}

func changeBudget(t *testing.T) compensation.BudgetFence {
	return compensation.BudgetFence{
		BudgetID: "budget/fy26", Amount: changeDecimal(t, "200000.00"),
		ExpiresAt:  changeNow().Add(time.Hour),
		BandDigest: "sha256:band", PayrollDigest: "sha256:payroll", LegalDigest: "sha256:legal",
	}
}

func changeDisclosure() compensation.AuthorizationDecision {
	allow := func() compensation.FieldRuling { return compensation.FieldRuling{Effect: compensation.EffectAllow} }
	return compensation.AuthorizationDecision{
		PolicyVersion: "v1", Purpose: "compensation-change",
		Fields: map[compensation.FieldID]compensation.FieldRuling{
			compensation.FieldAmount: allow(), compensation.FieldComponentType: allow(),
			compensation.FieldFrequency: allow(), compensation.FieldEffectiveInterval: allow(),
		},
	}
}

func changeRequest(t *testing.T) compensation.ChangeRequest {
	return compensation.ChangeRequest{
		PackageRef: "package/assignment-1", EffectiveDate: "2026-10-01",
		SnapshotDigest: "sha256:snap", Approvals: []string{"approval/captains-1"},
		TransactionPlan: "txplan/88", Fence: 11,
		Lines: changeLines(t), Budget: changeBudget(t),
		Disclosure: changeDisclosure(), HoursPerYear: changeDecimal(t, "2080.00"),
		Now: changeNow(),
	}
}

func changePolicy() compensation.CompositionPolicy {
	return compensation.CompositionPolicy{
		Version: "v1",
		AtomicBoundary: []compensation.ComponentType{
			compensation.ComponentBase, compensation.ComponentBonusTarget, compensation.ComponentAllowance,
		},
		MaxChildren: 4,
	}
}

// TestCompensationChangeConformancePreservesExactMoneyPresenceAndAuthority
// proves one standalone compensation change simulates with exact decimals,
// reserves against a live budget fence, commits atomically and repairs
// only failed effects.
func TestCompensationChangeConformancePreservesExactMoneyPresenceAndAuthority(t *testing.T) {
	simulation, err := compensation.SimulateChange(compCurrent(), changeRequest(t), changePolicy())
	if err != nil {
		t.Fatalf("valid change rejected: %v", err)
	}
	if simulation.TotalAnnual.String() != "111000.00" {
		t.Fatalf("total annual = %s, want 111000.00", simulation.TotalAnnual)
	}
	reservation, err := compensation.ReserveChange(simulation, changeRequest(t))
	if err != nil {
		t.Fatalf("reservation rejected: %v", err)
	}
	committed, err := compensation.CommitChange(reservation, simulation, changeRequest(t))
	if err != nil {
		t.Fatalf("commit rejected: %v", err)
	}
	if len(committed.Effects) != 3 {
		t.Fatalf("effects = %d, want one atomic set of 3", len(committed.Effects))
	}
	for _, effect := range committed.Effects {
		if !effect.Applied {
			t.Fatalf("partial package write: %+v", effect)
		}
	}

	adversaries := []struct {
		name  string
		cause error
		stage func(*testing.T) error
	}{
		{"expired budget fence", compensation.ErrBudgetFenceExpired, func(t *testing.T) error {
			req := changeRequest(t)
			req.Now = changeNow().Add(2 * time.Hour)
			simulation, err := compensation.SimulateChange(compCurrent(), req, changePolicy())
			if err != nil {
				return err
			}
			_, err = compensation.ReserveChange(simulation, req)
			return err
		}},
		{"stale legal context", compensation.ErrStaleContext, func(t *testing.T) error {
			simulation, err := compensation.SimulateChange(compCurrent(), changeRequest(t), changePolicy())
			if err != nil {
				return err
			}
			req := changeRequest(t)
			req.Budget.LegalDigest = "sha256:stale"
			_, err = compensation.ReserveChange(simulation, req)
			return err
		}},
		{"unauthorized disclosure", compensation.ErrUnauthorizedFact, func(t *testing.T) error {
			simulation, err := compensation.SimulateChange(compCurrent(), changeRequest(t), changePolicy())
			if err != nil {
				return err
			}
			reservation, err := compensation.ReserveChange(simulation, changeRequest(t))
			if err != nil {
				return err
			}
			req := changeRequest(t)
			req.Disclosure.Fields[compensation.FieldAmount] = compensation.FieldRuling{Effect: compensation.EffectDeny, Reason: "need-to-know"}
			_, err = compensation.CommitChange(reservation, simulation, req)
			return err
		}},
	}
	for _, tc := range adversaries {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.stage(t); !errors.Is(err, tc.cause) {
				t.Fatalf("want %v, got %v", tc.cause, err)
			}
		})
	}

	t.Run("repair redrives only failed effects", func(t *testing.T) {
		simulation, err := compensation.SimulateChange(compCurrent(), changeRequest(t), changePolicy())
		if err != nil {
			t.Fatal(err)
		}
		reservation, err := compensation.ReserveChange(simulation, changeRequest(t))
		if err != nil {
			t.Fatal(err)
		}
		committed, err := compensation.CommitChange(reservation, simulation, changeRequest(t))
		if err != nil {
			t.Fatal(err)
		}
		failed := committed.Effects[0].EffectID
		repair, err := compensation.RepairChange(committed, []string{failed}, map[string]string{failed: committed.Effects[0].Digest})
		if err != nil {
			t.Fatal(err)
		}
		if len(repair.Redriven) != 1 || repair.Redriven[0] != failed {
			t.Fatalf("repair redrove %+v", repair.Redriven)
		}
		if len(repair.Untouched) != 2 {
			t.Fatalf("repair touched healthy effects: %+v", repair)
		}
		if _, err := compensation.RepairChange(committed, []string{"effect/ghost"}, nil); err == nil {
			t.Fatal("repair invented work")
		}
	})
}
