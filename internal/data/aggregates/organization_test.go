package aggregates_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/hcm-next/internal/data/aggregates"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
)

// TestTodo_DB_009 proves DB-009's LegalEntity/OrganizationUnit/Job/
// JobPosition/PositionOccupancy adapter: Put appends and supersedes exactly
// as DB-008's does, a direct in-place update is refused the same way, an
// organization_unit whose parent chain would loop back to itself is refused
// (RED: "manager/org cycle"), and a position_occupancy allocation that would
// push a position's committed FTE past its own capacity is refused (RED:
// "reservation beyond FTE commits").
func TestTodo_DB_009(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	org := aggregates.OrganizationStore{}

	legalEntityID := uuid.New()
	legalEntity, err := aggregates.NewLegalEntity(tenant, legalEntityID, date(t, "2020-01-01"), nil,
		instant(t, "2020-01-01T00:00:00Z"), "Acme Inc.", "ACTIVE")
	if err != nil {
		t.Fatalf("NewLegalEntity: %v", err)
	}
	inTx(t, db, func(tx pgx.Tx) error {
		_, err := org.PutLegalEntity(ctx, tx, legalEntity)
		return err
	})

	t.Run("append, supersede and current read", func(t *testing.T) {
		orgID := uuid.New()
		unit, err := aggregates.NewOrganizationUnit(tenant, orgID, date(t, "2024-01-01"), nil,
			instant(t, "2024-01-01T00:00:00Z"), "DEPARTMENT", "eng", "Engineering", &legalEntityID, nil, "ACTIVE")
		if err != nil {
			t.Fatalf("NewOrganizationUnit: %v", err)
		}
		inTx(t, db, func(tx pgx.Tx) error {
			_, err := org.PutOrganizationUnit(ctx, tx, unit)
			return err
		})

		renamed, err := aggregates.NewOrganizationUnit(tenant, orgID, date(t, "2025-01-01"), nil,
			instant(t, "2025-01-01T00:00:00Z"), "DEPARTMENT", "eng", "Platform Engineering", &legalEntityID, nil, "ACTIVE")
		if err != nil {
			t.Fatalf("NewOrganizationUnit (renamed): %v", err)
		}
		inTx(t, db, func(tx pgx.Tx) error {
			_, err := org.PutOrganizationUnit(ctx, tx, renamed)
			return err
		})

		got, err := org.CurrentOrganizationUnit(ctx, db.Conn, tenant, orgID, date(t, "2025-06-01"))
		if err != nil {
			t.Fatalf("CurrentOrganizationUnit: %v", err)
		}
		if got.Name != "Platform Engineering" {
			t.Fatalf("CurrentOrganizationUnit.Name = %q, want Platform Engineering", got.Name)
		}

		if err := db.ExecErr(`UPDATE organization_unit SET name = 'Forged' WHERE entity_id = $1 AND superseded_at IS NULL`, orgID); err == nil {
			t.Fatal("a direct UPDATE of organization_unit.name was accepted")
		}
	})

	t.Run("RED: an organization cycle is refused", func(t *testing.T) {
		parentID, childID := uuid.New(), uuid.New()
		parent, err := aggregates.NewOrganizationUnit(tenant, parentID, date(t, "2024-01-01"), nil,
			instant(t, "2024-01-01T00:00:00Z"), "DIVISION", "parent-div", "Parent Division", &legalEntityID, nil, "ACTIVE")
		if err != nil {
			t.Fatalf("NewOrganizationUnit (parent): %v", err)
		}
		inTx(t, db, func(tx pgx.Tx) error {
			_, err := org.PutOrganizationUnit(ctx, tx, parent)
			return err
		})

		child, err := aggregates.NewOrganizationUnit(tenant, childID, date(t, "2024-01-01"), nil,
			instant(t, "2024-01-01T00:00:00Z"), "DEPARTMENT", "child-dept", "Child Department", &legalEntityID, &parentID, "ACTIVE")
		if err != nil {
			t.Fatalf("NewOrganizationUnit (child): %v", err)
		}
		inTx(t, db, func(tx pgx.Tx) error {
			_, err := org.PutOrganizationUnit(ctx, tx, child)
			return err
		})

		// Re-point the parent to the child it already reports to: parent -> child -> parent.
		loop, err := aggregates.NewOrganizationUnit(tenant, parentID, date(t, "2025-01-01"), nil,
			instant(t, "2025-01-01T00:00:00Z"), "DIVISION", "parent-div", "Parent Division", &legalEntityID, &childID, "ACTIVE")
		if err != nil {
			t.Fatalf("NewOrganizationUnit (loop): %v", err)
		}
		putErr := inTxErr(t, db, func(tx pgx.Tx) error {
			_, err := org.PutOrganizationUnit(ctx, tx, loop)
			return err
		})
		if putErr == nil {
			t.Fatal("an organization_unit parent chain forming a cycle was accepted")
		}

		// The rolled-back transaction must have left the original parent row
		// live: the cycle attempt cannot have silently superseded it despite
		// the INSERT that would have replaced it failing.
		stillLive, err := org.CurrentOrganizationUnit(ctx, db.Conn, tenant, parentID, date(t, "2025-06-01"))
		if err != nil {
			t.Fatalf("CurrentOrganizationUnit after the rejected cycle: %v", err)
		}
		if stillLive.ParentOrganizationRef != nil {
			t.Fatalf("parent org still has a parent_organization_ref %v after the cycle attempt rolled back", stillLive.ParentOrganizationRef)
		}
	})

	t.Run("RED: an occupancy allocation beyond a position's FTE capacity is refused", func(t *testing.T) {
		orgID := uuid.New()
		unit, err := aggregates.NewOrganizationUnit(tenant, orgID, date(t, "2024-01-01"), nil,
			instant(t, "2024-01-01T00:00:00Z"), "DEPARTMENT", "ops", "Ops", &legalEntityID, nil, "ACTIVE")
		if err != nil {
			t.Fatalf("NewOrganizationUnit: %v", err)
		}
		inTx(t, db, func(tx pgx.Tx) error {
			_, err := org.PutOrganizationUnit(ctx, tx, unit)
			return err
		})

		jobID := uuid.New()
		job, err := aggregates.NewJob(tenant, jobID, date(t, "2024-01-01"), nil,
			instant(t, "2024-01-01T00:00:00Z"), "OPS-1", "Ops Specialist", "", "P1", "NON_EXEMPT")
		if err != nil {
			t.Fatalf("NewJob: %v", err)
		}
		inTx(t, db, func(tx pgx.Tx) error {
			_, err := org.PutJob(ctx, tx, job)
			return err
		})

		positionID := uuid.New()
		position, err := aggregates.NewJobPosition(tenant, positionID, jobID, orgID, &legalEntityID,
			date(t, "2024-01-01"), nil, instant(t, "2024-01-01T00:00:00Z"), "POS-OPS-1", "Remote", "1.0000", "OPEN")
		if err != nil {
			t.Fatalf("NewJobPosition: %v", err)
		}
		inTx(t, db, func(tx pgx.Tx) error {
			_, err := org.PutJobPosition(ctx, tx, position)
			return err
		})

		firstOccupancyID := uuid.New()
		full, err := aggregates.NewPositionOccupancy(tenant, firstOccupancyID, positionID, nil, nil,
			date(t, "2024-02-01"), nil, instant(t, "2024-02-01T00:00:00Z"), "1.0000", true)
		if err != nil {
			t.Fatalf("NewPositionOccupancy (full): %v", err)
		}
		inTx(t, db, func(tx pgx.Tx) error {
			_, err := org.PutPositionOccupancy(ctx, tx, full)
			return err
		})

		overCommitID := uuid.New()
		overCommit, err := aggregates.NewPositionOccupancy(tenant, overCommitID, positionID, nil, nil,
			date(t, "2024-02-01"), nil, instant(t, "2024-02-01T00:00:01Z"), "0.5000", false)
		if err != nil {
			t.Fatalf("NewPositionOccupancy (over-commit): %v", err)
		}
		putErr := inTxErr(t, db, func(tx pgx.Tx) error {
			_, err := org.PutPositionOccupancy(ctx, tx, overCommit)
			return err
		})
		if putErr == nil {
			t.Fatal("a position_occupancy allocation beyond the position's live capacity was accepted")
		}
	})
}

// TestTodo_DB_009_Security proves row level security on DB-009's tables the
// same way TestTodo_DB_008_Security does for DB-008's.
func TestTodo_DB_009_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenantA := insertTenant(t, db)
	tenantB := insertTenant(t, db)
	org := aggregates.OrganizationStore{}

	entityA := uuid.New()
	a, err := aggregates.NewLegalEntity(tenantA, entityA, date(t, "2024-01-01"), nil,
		instant(t, "2024-01-01T00:00:00Z"), "Tenant A Legal Entity", "ACTIVE")
	if err != nil {
		t.Fatalf("NewLegalEntity A: %v", err)
	}
	inTx(t, db, func(tx pgx.Tx) error {
		_, err := org.PutLegalEntity(ctx, tx, a)
		return err
	})

	entityB := uuid.New()
	b, err := aggregates.NewLegalEntity(tenantB, entityB, date(t, "2024-01-01"), nil,
		instant(t, "2024-01-01T00:00:00Z"), "Tenant B Legal Entity", "ACTIVE")
	if err != nil {
		t.Fatalf("NewLegalEntity B: %v", err)
	}
	inTx(t, db, func(tx pgx.Tx) error {
		_, err := org.PutLegalEntity(ctx, tx, b)
		return err
	})

	connA := asAppRole(t, db, tenantA)
	if _, err := org.CurrentLegalEntity(ctx, connA, tenantA, entityA, date(t, "2024-06-01")); err != nil {
		t.Fatalf("tenant A reading its own legal_entity: %v", err)
	}
	if _, err := org.CurrentLegalEntity(ctx, connA, tenantB, entityB, date(t, "2024-06-01")); !errors.Is(err, aggregates.ErrNotFound) {
		t.Fatalf("tenant A's connection read tenant B's legal_entity (err=%v)", err)
	}
}
