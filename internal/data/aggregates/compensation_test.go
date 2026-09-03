package aggregates_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/hcm-next/internal/data/aggregates"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func usd(t *testing.T, text string) values.Money {
	t.Helper()
	m, err := values.NewMoney(text, "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("NewMoney(%q, USD): %v", text, err)
	}
	return m
}

// TestTodo_DB_010 proves DB-010's CompensationPackage/CompensationComponent/
// CompensationBand/WorkforceBudget/BudgetReservation adapter: exact-decimal
// money end to end (never a float, RED: "floating-point money"), a
// component whose currency does not match its package's is refused (RED:
// "currency mismatch"), a band whose bounds are out of order is refused
// (RED: "invalid band range"), and a reservation that would push a budget's
// committed total past its available quantity is refused (RED: "over-reserved
// budget commits").
func TestTodo_DB_010(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	comp := aggregates.CompensationStore{}
	workerRef := uuid.New()
	registerStandinEntity(t, db, tenant, workerRef, "worker")

	packageID := uuid.New()
	pkg, err := aggregates.NewCompensationPackage(tenant, packageID, workerRef, nil, nil,
		date(t, "2024-01-01"), nil, instant(t, "2024-01-01T00:00:00Z"), "USD")
	if err != nil {
		t.Fatalf("NewCompensationPackage: %v", err)
	}
	inTx(t, db, func(tx pgx.Tx) error {
		_, err := comp.PutCompensationPackage(ctx, tx, pkg)
		return err
	})

	t.Run("exact decimal money round-trips, append and supersede", func(t *testing.T) {
		componentID := uuid.New()
		base, err := aggregates.NewCompensationComponent(tenant, componentID, packageID,
			date(t, "2024-01-01"), nil, instant(t, "2024-01-01T00:00:00Z"), "BASE_PAY", usd(t, "93000.00"), "ANNUAL")
		if err != nil {
			t.Fatalf("NewCompensationComponent: %v", err)
		}
		inTx(t, db, func(tx pgx.Tx) error {
			_, err := comp.PutCompensationComponent(ctx, tx, base)
			return err
		})

		got, err := comp.CurrentCompensationComponent(ctx, db.Conn, tenant, componentID, date(t, "2024-06-01"))
		if err != nil {
			t.Fatalf("CurrentCompensationComponent: %v", err)
		}
		gotAmount, err := values.NewDecimal(got.Amount, 4, values.RoundingHalfEven)
		if err != nil {
			t.Fatalf("parse read-back amount %q: %v", got.Amount, err)
		}
		wantAmount, err := values.NewDecimal("93000.00", 4, values.RoundingHalfEven)
		if err != nil {
			t.Fatalf("parse expected amount: %v", err)
		}
		if !gotAmount.Equal(wantAmount) {
			t.Fatalf("CurrentCompensationComponent.Amount = %q, want exactly 93000.00 (got %s)", got.Amount, gotAmount.String())
		}

		raised, err := aggregates.NewCompensationComponent(tenant, componentID, packageID,
			date(t, "2025-01-01"), nil, instant(t, "2025-01-01T00:00:00Z"), "BASE_PAY", usd(t, "98000.00"), "ANNUAL")
		if err != nil {
			t.Fatalf("NewCompensationComponent (raise): %v", err)
		}
		inTx(t, db, func(tx pgx.Tx) error {
			_, err := comp.PutCompensationComponent(ctx, tx, raised)
			return err
		})

		current, err := comp.CurrentCompensationComponent(ctx, db.Conn, tenant, componentID, date(t, "2025-06-01"))
		if err != nil {
			t.Fatalf("CurrentCompensationComponent after raise: %v", err)
		}
		currentAmount, err := values.NewDecimal(current.Amount, 4, values.RoundingHalfEven)
		if err != nil {
			t.Fatalf("parse raised amount %q: %v", current.Amount, err)
		}
		wantRaised, _ := values.NewDecimal("98000.00", 4, values.RoundingHalfEven)
		if !currentAmount.Equal(wantRaised) {
			t.Fatalf("CurrentCompensationComponent.Amount after raise = %s, want 98000.00", currentAmount.String())
		}

		if err := db.ExecErr(`UPDATE compensation_component SET amount = 999999 WHERE entity_id = $1 AND superseded_at IS NULL`, componentID); err == nil {
			t.Fatal("a direct UPDATE of compensation_component.amount was accepted")
		}
	})

	t.Run("RED: a component currency mismatched with its package is refused", func(t *testing.T) {
		eur, err := values.NewMoney("1000.00", "EUR", 2, values.RoundingHalfEven)
		if err != nil {
			t.Fatalf("NewMoney EUR: %v", err)
		}
		mismatched, err := aggregates.NewCompensationComponent(tenant, uuid.New(), packageID,
			date(t, "2024-01-01"), nil, instant(t, "2024-01-01T00:00:00Z"), "ALLOWANCE", eur, "MONTHLY")
		if err != nil {
			t.Fatalf("NewCompensationComponent (mismatched): %v", err)
		}
		putErr := inTxErr(t, db, func(tx pgx.Tx) error {
			_, err := comp.PutCompensationComponent(ctx, tx, mismatched)
			return err
		})
		if putErr == nil {
			t.Fatal("a compensation_component with a currency not matching its package's was accepted")
		}
	})

	t.Run("RED: an out-of-order band range is refused", func(t *testing.T) {
		// The Go constructor itself refuses an out-of-order range before any
		// SQL is issued.
		if _, err := aggregates.NewCompensationBand(tenant, uuid.New(), date(t, "2024-01-01"), nil,
			instant(t, "2024-01-01T00:00:00Z"), "ENG-SWE3", "P3", "US-WEST",
			usd(t, "171000.00"), usd(t, "142000.00"), usd(t, "205000.00")); err == nil {
			t.Fatal("NewCompensationBand accepted minimum > midpoint")
		}

		// A band that bypasses the Go constructor (a hand-built raw insert,
		// standing in for a future caller that skips this package's own
		// validation) is still refused by the database's own CHECK.
		bandID := uuid.New()
		err := db.ExecErr(`
			INSERT INTO compensation_band (
				row_id, tenant_id, entity_id, canonical_id, job_code, grade, pay_zone, currency,
				minimum, midpoint, maximum, effective_from, recorded_at, digest_algorithm, digest)
			VALUES ($1, $2, $3, $4, 'ENG-SWE3', 'P3', 'US-WEST', 'USD',
				171000.00, 142000.00, 205000.00, timestamptz '2024-01-01T00:00:00Z', now(), 'sha256', repeat('a', 64))`,
			uuid.New(), tenant, bandID, "eid:v1:compensation_band:"+bandID.String())
		if err == nil {
			t.Fatal("a raw compensation_band insert with minimum > midpoint was accepted")
		}
	})

	t.Run("RED: over-reserving a budget is refused", func(t *testing.T) {
		budgetID := uuid.New()
		budget, err := aggregates.NewWorkforceBudget(tenant, budgetID, date(t, "2024-01-01"), nil,
			instant(t, "2024-01-01T00:00:00Z"), "COMPENSATION_POOL", "hcmnext.rewards.catalog",
			"eng/P3", "2024-cycle", "USD", "MONEY", "50000.00", "")
		if err != nil {
			t.Fatalf("NewWorkforceBudget: %v", err)
		}
		inTx(t, db, func(tx pgx.Tx) error {
			_, err := comp.PutWorkforceBudget(ctx, tx, budget)
			return err
		})

		held, err := aggregates.NewBudgetReservation(tenant, uuid.New(), budgetID, nil,
			date(t, "2024-02-01"), nil, instant(t, "2024-02-01T00:00:00Z"), "40000.00", "USD", "HELD", nil)
		if err != nil {
			t.Fatalf("NewBudgetReservation (held): %v", err)
		}
		inTx(t, db, func(tx pgx.Tx) error {
			_, err := comp.PutBudgetReservation(ctx, tx, held)
			return err
		})

		over, err := aggregates.NewBudgetReservation(tenant, uuid.New(), budgetID, nil,
			date(t, "2024-02-01"), nil, instant(t, "2024-02-01T00:00:01Z"), "20000.00", "USD", "HELD", nil)
		if err != nil {
			t.Fatalf("NewBudgetReservation (over): %v", err)
		}
		putErr := inTxErr(t, db, func(tx pgx.Tx) error {
			_, err := comp.PutBudgetReservation(ctx, tx, over)
			return err
		})
		if putErr == nil {
			t.Fatal("a budget_reservation that overcommits its budget's available_quantity was accepted")
		}

		// A currency that does not match the budget's is refused the same way.
		wrongCurrency, err := aggregates.NewBudgetReservation(tenant, uuid.New(), budgetID, nil,
			date(t, "2024-02-01"), nil, instant(t, "2024-02-01T00:00:02Z"), "1000.00", "EUR", "HELD", nil)
		if err != nil {
			t.Fatalf("NewBudgetReservation (wrong currency): %v", err)
		}
		putErr = inTxErr(t, db, func(tx pgx.Tx) error {
			_, err := comp.PutBudgetReservation(ctx, tx, wrongCurrency)
			return err
		})
		if putErr == nil {
			t.Fatal("a budget_reservation with a currency not matching its budget's was accepted")
		}

		// A REQUESTED (not yet HELD/COMMITTED) reservation never competes for
		// the budget's available quantity.
		requested, err := aggregates.NewBudgetReservation(tenant, uuid.New(), budgetID, nil,
			date(t, "2024-02-01"), nil, instant(t, "2024-02-01T00:00:03Z"), "20000.00", "USD", "REQUESTED", nil)
		if err != nil {
			t.Fatalf("NewBudgetReservation (requested): %v", err)
		}
		inTx(t, db, func(tx pgx.Tx) error {
			_, err := comp.PutBudgetReservation(ctx, tx, requested)
			return err
		})
	})
}

// TestTodo_DB_010_Security proves row level security on DB-010's tables the
// same way TestTodo_DB_008_Security does for DB-008's.
func TestTodo_DB_010_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenantA := insertTenant(t, db)
	tenantB := insertTenant(t, db)
	comp := aggregates.CompensationStore{}

	budgetA := uuid.New()
	a, err := aggregates.NewWorkforceBudget(tenantA, budgetA, date(t, "2024-01-01"), nil,
		instant(t, "2024-01-01T00:00:00Z"), "COMPENSATION_POOL", "sys", "scope-a", "cycle", "USD", "MONEY", "1000.00", "")
	if err != nil {
		t.Fatalf("NewWorkforceBudget A: %v", err)
	}
	inTx(t, db, func(tx pgx.Tx) error {
		_, err := comp.PutWorkforceBudget(ctx, tx, a)
		return err
	})

	budgetB := uuid.New()
	b, err := aggregates.NewWorkforceBudget(tenantB, budgetB, date(t, "2024-01-01"), nil,
		instant(t, "2024-01-01T00:00:00Z"), "COMPENSATION_POOL", "sys", "scope-b", "cycle", "USD", "MONEY", "2000.00", "")
	if err != nil {
		t.Fatalf("NewWorkforceBudget B: %v", err)
	}
	inTx(t, db, func(tx pgx.Tx) error {
		_, err := comp.PutWorkforceBudget(ctx, tx, b)
		return err
	})

	connA := asAppRole(t, db, tenantA)
	if _, err := comp.CurrentWorkforceBudget(ctx, connA, tenantA, budgetA, date(t, "2024-06-01")); err != nil {
		t.Fatalf("tenant A reading its own workforce_budget: %v", err)
	}
	if _, err := comp.CurrentWorkforceBudget(ctx, connA, tenantB, budgetB, date(t, "2024-06-01")); !errors.Is(err, aggregates.ErrNotFound) {
		t.Fatalf("tenant A's connection read tenant B's workforce_budget (err=%v)", err)
	}
}
