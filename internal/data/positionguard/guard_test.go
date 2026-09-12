package positionguard_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/positionguard"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func seedTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenantID, key, "positionguard test tenant "+key)
	return tenantID
}

func beginScoped(t *testing.T, db *pgtest.DB, tenantID uuid.UUID) dbport.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	return tx
}

// TestPositionGuardAdmitAndRelease exercises Admit/Release directly against
// real PostgreSQL: a fresh admission, a non-overlapping date for the same
// position (not a conflict), a conflicting idempotency key (refused, zero
// rows written to the row it did not own), a replay (resolves to the same
// row) and Release freeing the window again.
func TestPositionGuardAdmitAndRelease(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedTenant(t, db, "positionguard-primary")
	ctx := context.Background()
	const posRef = "entity:positionguard-primary:position:pos-1"

	tx := beginScoped(t, db, tenantID)
	defer func() { _ = tx.Rollback(ctx) }()

	guardID := uuid.New()
	decision, err := positionguard.Admit(ctx, tx, tenantID, guardID, posRef, "2027-01-01", "proposal-alpha", "req-alpha")
	if err != nil {
		t.Fatalf("Admit = %v, want nil", err)
	}
	if decision.Replay {
		t.Fatal("a fresh admission must not report Replay")
	}

	if _, err := positionguard.Admit(ctx, tx, tenantID, uuid.New(), posRef, "2028-06-01", "proposal-beta", "req-beta"); err != nil {
		t.Fatalf("Admit(non-overlapping date) = %v, want nil (not a conflict)", err)
	}

	var activeRowsBefore int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM promotion_target_position_guard WHERE tenant_id=$1 AND position_ref=$2 AND effective_date='2027-01-01' AND status='ACTIVE'`,
		tenantID, posRef).Scan(&activeRowsBefore); err != nil {
		t.Fatalf("count rows before conflict: %v", err)
	}
	if _, err := positionguard.Admit(ctx, tx, tenantID, uuid.New(), posRef, "2027-01-01", "proposal-gamma", "req-gamma-conflicting"); !errors.Is(err, positionguard.ErrActiveConflict) {
		t.Fatalf("Admit(conflicting) = %v, want ErrActiveConflict", err)
	}
	var activeRowsAfter int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM promotion_target_position_guard WHERE tenant_id=$1 AND position_ref=$2 AND effective_date='2027-01-01' AND status='ACTIVE'`,
		tenantID, posRef).Scan(&activeRowsAfter); err != nil {
		t.Fatalf("count rows after conflict: %v", err)
	}
	if activeRowsAfter != activeRowsBefore {
		t.Fatalf("a refused conflicting admission changed the active row count: before=%d after=%d", activeRowsBefore, activeRowsAfter)
	}

	replay, err := positionguard.Admit(ctx, tx, tenantID, uuid.New(), posRef, "2027-01-01", "proposal-alpha", "req-alpha")
	if err != nil {
		t.Fatalf("Admit(replay) = %v, want nil", err)
	}
	if !replay.Replay || replay.GuardID != guardID {
		t.Fatalf("replay = %+v, want Replay=true and GuardID=%s", replay, guardID)
	}

	if err := positionguard.Release(ctx, tx, tenantID, posRef, "2027-01-01"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	reopened, err := positionguard.Admit(ctx, tx, tenantID, uuid.New(), posRef, "2027-01-01", "proposal-delta", "req-delta")
	if err != nil {
		t.Fatalf("Admit after Release = %v, want nil (the window was freed)", err)
	}
	if reopened.Replay {
		t.Fatal("a reservation opened after Release must not be reported as a replay of the closed one")
	}
}

// TestPositionGuardZeroValueFailsClosed proves every input Admit/Release
// take is validated rather than silently accepted.
func TestPositionGuardZeroValueFailsClosed(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedTenant(t, db, "positionguard-zero")
	ctx := context.Background()
	tx := beginScoped(t, db, tenantID)
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := positionguard.Admit(ctx, tx, uuid.Nil, uuid.New(), "pos", "2027-01-01", "proposal", "req"); !errors.Is(err, positionguard.ErrInvalid) {
		t.Fatalf("Admit(nil tenant) = %v, want ErrInvalid", err)
	}
	if _, err := positionguard.Admit(ctx, tx, tenantID, uuid.Nil, "pos", "2027-01-01", "proposal", "req"); !errors.Is(err, positionguard.ErrInvalid) {
		t.Fatalf("Admit(nil guard id) = %v, want ErrInvalid", err)
	}
	if _, err := positionguard.Admit(ctx, tx, tenantID, uuid.New(), "", "2027-01-01", "proposal", "req"); !errors.Is(err, positionguard.ErrInvalid) {
		t.Fatalf("Admit(empty position) = %v, want ErrInvalid", err)
	}
	if _, err := positionguard.Admit(ctx, tx, tenantID, uuid.New(), "pos", "", "proposal", "req"); !errors.Is(err, positionguard.ErrInvalid) {
		t.Fatalf("Admit(empty date) = %v, want ErrInvalid", err)
	}
	if _, err := positionguard.Admit(ctx, tx, tenantID, uuid.New(), "pos", "2027-01-01", "", "req"); !errors.Is(err, positionguard.ErrInvalid) {
		t.Fatalf("Admit(empty proposal ref) = %v, want ErrInvalid", err)
	}
	if _, err := positionguard.Admit(ctx, tx, tenantID, uuid.New(), "pos", "2027-01-01", "proposal", ""); !errors.Is(err, positionguard.ErrInvalid) {
		t.Fatalf("Admit(empty idempotency key) = %v, want ErrInvalid", err)
	}
	if err := positionguard.Release(ctx, tx, uuid.Nil, "pos", "2027-01-01"); !errors.Is(err, positionguard.ErrInvalid) {
		t.Fatalf("Release(nil tenant) = %v, want ErrInvalid", err)
	}
}

// TestTodo_PROMOUX_004_Integration proves PROMOUX-004's ground five
// (reservation ownership) reaches a real store through the exact
// production seam internal/domains/promotion.PreflightPromotion depends on
// -- Adapter satisfies promotion.PositionReservationAdmitter, and this test
// drives it only through that interface, over real PostgreSQL via pgtest,
// never by calling Admit directly (that is TestPositionGuardAdmitAndRelease's
// and TestTodo_PROMOUX_004_Race's job).
func TestTodo_PROMOUX_004_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := seedTenant(t, db, "positionguard-integration")
	const tenantKey = values.TenantId("positionguard-integration")

	var admitter promotion.PositionReservationAdmitter = positionguard.Adapter{
		DB:         db.Conn,
		TenantUUID: func(values.TenantId) uuid.UUID { return tenantID },
	}

	pos := values.EntityRef{Tenant: tenantKey, Kind: position.KindPosition, Id: uuid.New().String()}
	effectiveDate, err := values.ParseLocalDate("2027-04-01")
	if err != nil {
		t.Fatalf("ParseLocalDate: %v", err)
	}

	first := promotion.PositionSlotAdmitRequest{
		Tenant: tenantKey, Position: pos, EffectiveDate: effectiveDate,
		ProposalRef: "proposal-first", IdempotencyKey: "req-first",
	}
	admitted, err := admitter.AdmitPositionSlot(ctx, first)
	if err != nil {
		t.Fatalf("AdmitPositionSlot(first) = %v, want nil", err)
	}
	if !admitted {
		t.Fatal("a fresh admission through the real adapter must be admitted")
	}

	// A durable row now exists in the real table, committed by the adapter's
	// own transaction -- not just an in-memory decision.
	var rowCount int
	if err := db.Conn.QueryRow(ctx,
		`SELECT count(*) FROM promotion_target_position_guard WHERE tenant_id=$1 AND position_ref=$2 AND effective_date=$3 AND status='ACTIVE'`,
		tenantID, pos.String(), effectiveDate.String(),
	).Scan(&rowCount); err != nil {
		t.Fatalf("count durable rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("durable ACTIVE rows after the first admission = %d, want exactly 1", rowCount)
	}

	// A second, genuinely different proposal for the same position and
	// effective date is refused -- reported as a plain "not admitted"
	// through the domain port, never an error, because this is a business
	// refusal (PROMOUX-004's reservation-ownership ground), not a contract
	// failure.
	second := promotion.PositionSlotAdmitRequest{
		Tenant: tenantKey, Position: pos, EffectiveDate: effectiveDate,
		ProposalRef: "proposal-second", IdempotencyKey: "req-second",
	}
	admitted, err = admitter.AdmitPositionSlot(ctx, second)
	if err != nil {
		t.Fatalf("AdmitPositionSlot(conflicting) = %v, want nil error", err)
	}
	if admitted {
		t.Fatal("a conflicting proposal for the same position and effective date must not be admitted")
	}
	if err := db.Conn.QueryRow(ctx,
		`SELECT count(*) FROM promotion_target_position_guard WHERE tenant_id=$1 AND position_ref=$2 AND effective_date=$3 AND status='ACTIVE'`,
		tenantID, pos.String(), effectiveDate.String(),
	).Scan(&rowCount); err != nil {
		t.Fatalf("count durable rows after conflict: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("durable ACTIVE rows after the refused conflict = %d, want still exactly 1 (the refusal must not write a second row)", rowCount)
	}

	// A replay of the first proposal's own idempotency key is admitted
	// again -- resolving to the same reservation, not refused as a
	// conflict with itself.
	admitted, err = admitter.AdmitPositionSlot(ctx, first)
	if err != nil {
		t.Fatalf("AdmitPositionSlot(replay) = %v, want nil", err)
	}
	if !admitted {
		t.Fatal("a replay of the same proposal's own idempotency key must be admitted")
	}
}
