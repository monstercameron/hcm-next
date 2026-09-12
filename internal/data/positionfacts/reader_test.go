package positionfacts_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/positionfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func seedReaderTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenantID, key, "positionfacts test tenant "+key)
	return tenantID
}

// seededPosition creates a full legal_entity/organization_unit/job/job_position
// chain and returns the job_position's own entity id.
func seededPosition(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, jobCode, orgCode string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}

	store := aggregates.OrganizationStore{}
	from := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	recorded := time.Date(2020, 1, 2, 9, 0, 0, 0, time.UTC)

	// Put*'s own return value is the row's row_id, not its entity id -- a
	// cross-entity reference column (legal_entity_ref, organization_ref,
	// job_ref) foreign-keys to aggregate_entity by entity id, so each entity
	// id is minted here and threaded through explicitly rather than reusing
	// a Put call's return value for that purpose.
	legalEntityID := uuid.New()
	legalEntity, err := aggregates.NewLegalEntity(tenantID, legalEntityID, from, nil, recorded, "ACME US Inc.", "ACTIVE")
	if err != nil {
		t.Fatalf("NewLegalEntity: %v", err)
	}
	if _, err := store.PutLegalEntity(ctx, tx, legalEntity); err != nil {
		t.Fatalf("PutLegalEntity: %v", err)
	}

	orgUnitID := uuid.New()
	orgUnit, err := aggregates.NewOrganizationUnit(tenantID, orgUnitID, from, nil, recorded, "DEPARTMENT", orgCode, "Engineering", &legalEntityID, nil, "ACTIVE")
	if err != nil {
		t.Fatalf("NewOrganizationUnit: %v", err)
	}
	if _, err := store.PutOrganizationUnit(ctx, tx, orgUnit); err != nil {
		t.Fatalf("PutOrganizationUnit: %v", err)
	}

	jobID := uuid.New()
	job, err := aggregates.NewJob(tenantID, jobID, from, nil, recorded, jobCode, "Engineering Manager", "ENGINEERING", "M1", "EXEMPT")
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	if _, err := store.PutJob(ctx, tx, job); err != nil {
		t.Fatalf("PutJob: %v", err)
	}

	positionEntityID := uuid.New()
	jobPosition, err := aggregates.NewJobPosition(tenantID, positionEntityID, jobID, orgUnitID, nil, from, nil, recorded,
		"POS-"+jobCode, "Remote", "1.0000", "OPEN")
	if err != nil {
		t.Fatalf("NewJobPosition: %v", err)
	}
	if _, err := store.PutJobPosition(ctx, tx, jobPosition); err != nil {
		t.Fatalf("PutJobPosition: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return positionEntityID
}

func readerAsOf(t *testing.T) position.AsOf {
	t.Helper()
	effectiveOn, err := values.ParseLocalDate("2026-01-01")
	if err != nil {
		t.Fatalf("ParseLocalDate: %v", err)
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("NewKnownAt: %v", err)
	}
	return position.AsOf{EffectiveOn: effectiveOn, KnownAt: knownAt}
}

// TestReaderResolvesARealSeededPosition proves the adapter reads a genuine
// job_position row -- job code, organization code, legal entity, lifecycle,
// capacity and a content-addressed revision -- rather than fabricating any
// of it.
func TestReaderResolvesARealSeededPosition(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedReaderTenant(t, db, "positionfacts-primary")
	tenant := values.TenantId("positionfacts-primary")
	entityID := seededPosition(t, db, tenantID, "ENG-MGR", "ENGINEERING")

	reader := positionfacts.Reader{DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return tenantID }}
	pos := values.EntityRef{Tenant: tenant, Kind: position.KindPosition, Id: entityID.String()}

	rev, exists, err := reader.PositionRevisionAt(context.Background(), position.PositionQuery{
		Tenant: tenant, Position: pos, AsOf: readerAsOf(t),
	})
	if err != nil {
		t.Fatalf("PositionRevisionAt: %v", err)
	}
	if !exists {
		t.Fatal("exists = false, want true for a real seeded position")
	}
	if rev.JobCode != "ENG-MGR" {
		t.Fatalf("JobCode = %q, want ENG-MGR", rev.JobCode)
	}
	if rev.OrgUnit != "ENGINEERING" {
		t.Fatalf("OrgUnit = %q, want ENGINEERING", rev.OrgUnit)
	}
	if rev.LegalEntity != "ACME US Inc." {
		t.Fatalf("LegalEntity = %q, want ACME US Inc.", rev.LegalEntity)
	}
	if rev.Lifecycle != position.LifecycleOpen {
		t.Fatalf("Lifecycle = %q, want OPEN", rev.Lifecycle)
	}
	if rev.Capacity.CapacityHeads != 1 || rev.Capacity.CapacityFTE.Sign() <= 0 {
		t.Fatalf("Capacity = %+v, want one head and positive FTE", rev.Capacity)
	}
	if !rev.Revision.IsSpecified() {
		t.Fatal("Revision must be specified for a resolved position")
	}
	if err := rev.Validate(); err != nil {
		t.Fatalf("the disclosed revision must validate as complete: %v", err)
	}

	// Reading again at the same coordinate must produce an equal revision:
	// the token is content-addressed from the row's own stored digest, not
	// minted fresh per read.
	again, existsAgain, err := reader.PositionRevisionAt(context.Background(), position.PositionQuery{
		Tenant: tenant, Position: pos, AsOf: readerAsOf(t),
	})
	if err != nil || !existsAgain {
		t.Fatalf("second read: exists=%v err=%v", existsAgain, err)
	}
	if !rev.Revision.Equal(again.Revision) {
		t.Fatal("two reads of an unchanged row must produce the same revision token")
	}
}

// TestReaderReportsAbsentForAGuessedOrNonexistentIdentifier proves a
// malformed identifier and a well-formed but unseeded UUID are both
// reported as absent, never as an error and never guessed at.
func TestReaderReportsAbsentForAGuessedOrNonexistentIdentifier(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedReaderTenant(t, db, "positionfacts-absent")
	tenant := values.TenantId("positionfacts-absent")
	reader := positionfacts.Reader{DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return tenantID }}

	t.Run("a free-text guess is not a canonical id", func(t *testing.T) {
		pos := values.EntityRef{Tenant: tenant, Kind: position.KindPosition, Id: "44444444-4444-4444-8444-444444444444"}
		// Even a well-formed UUID that was never seeded reports absent.
		_, exists, err := reader.PositionRevisionAt(context.Background(), position.PositionQuery{
			Tenant: tenant, Position: pos, AsOf: readerAsOf(t),
		})
		if err != nil {
			t.Fatalf("PositionRevisionAt: %v", err)
		}
		if exists {
			t.Fatal("exists = true for a position that was never seeded")
		}
	})
}

// TestReaderResolvesADeterminateEffectiveIntervalAndDirectLegalEntity
// exercises the two branches TestReaderResolvesARealSeededPosition's simpler
// open-ended fixture does not: a job_position row whose own legal_entity_ref
// is set directly (no fallback to the organization unit's), and a
// determinate [from, to) effective interval the query's AsOf falls inside.
func TestReaderResolvesADeterminateEffectiveIntervalAndDirectLegalEntity(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedReaderTenant(t, db, "positionfacts-determinate")
	tenant := values.TenantId("positionfacts-determinate")
	ctx := context.Background()

	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	store := aggregates.OrganizationStore{}
	from := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	recorded := time.Date(2020, 1, 2, 9, 0, 0, 0, time.UTC)

	directLegalEntityID := uuid.New()
	legalEntity, err := aggregates.NewLegalEntity(tenantID, directLegalEntityID, from, nil, recorded, "Direct Legal Entity Inc.", "ACTIVE")
	if err != nil {
		t.Fatalf("NewLegalEntity: %v", err)
	}
	if _, err := store.PutLegalEntity(ctx, tx, legalEntity); err != nil {
		t.Fatalf("PutLegalEntity: %v", err)
	}
	// A second legal entity backs the organization unit, so a read that
	// returned the org unit's legal entity instead of job_position's own
	// direct reference would be caught by the name assertion below.
	orgLegalEntity, err := aggregates.NewLegalEntity(tenantID, uuid.New(), from, nil, recorded, "Org Fallback Inc.", "ACTIVE")
	if err != nil {
		t.Fatalf("NewLegalEntity: %v", err)
	}
	orgLegalEntityID := orgLegalEntity.EntityID
	if _, err := store.PutLegalEntity(ctx, tx, orgLegalEntity); err != nil {
		t.Fatalf("PutLegalEntity: %v", err)
	}

	orgUnitID := uuid.New()
	orgUnit, err := aggregates.NewOrganizationUnit(tenantID, orgUnitID, from, nil, recorded, "DEPARTMENT", "SALES", "Sales", &orgLegalEntityID, nil, "ACTIVE")
	if err != nil {
		t.Fatalf("NewOrganizationUnit: %v", err)
	}
	if _, err := store.PutOrganizationUnit(ctx, tx, orgUnit); err != nil {
		t.Fatalf("PutOrganizationUnit: %v", err)
	}

	jobID := uuid.New()
	job, err := aggregates.NewJob(tenantID, jobID, from, nil, recorded, "SALES-REP", "Sales Representative", "SALES", "G3", "EXEMPT")
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	if _, err := store.PutJob(ctx, tx, job); err != nil {
		t.Fatalf("PutJob: %v", err)
	}

	positionEntityID := uuid.New()
	jobPosition, err := aggregates.NewJobPosition(tenantID, positionEntityID, jobID, orgUnitID, &directLegalEntityID, from, &to, recorded,
		"POS-SALES-REP", "Boston", "2.0000", "OPEN")
	if err != nil {
		t.Fatalf("NewJobPosition: %v", err)
	}
	if _, err := store.PutJobPosition(ctx, tx, jobPosition); err != nil {
		t.Fatalf("PutJobPosition: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	reader := positionfacts.Reader{DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return tenantID }}
	pos := values.EntityRef{Tenant: tenant, Kind: position.KindPosition, Id: positionEntityID.String()}
	rev, exists, err := reader.PositionRevisionAt(ctx, position.PositionQuery{Tenant: tenant, Position: pos, AsOf: readerAsOf(t)})
	if err != nil {
		t.Fatalf("PositionRevisionAt: %v", err)
	}
	if !exists {
		t.Fatal("exists = false, want true: the AsOf coordinate (2026-01-01) falls inside [2020-01-01, 2027-06-01)")
	}
	if rev.LegalEntity != "Direct Legal Entity Inc." {
		t.Fatalf("LegalEntity = %q, want the position's own direct legal entity, not the organization unit's fallback", rev.LegalEntity)
	}
	if rev.Capacity.CapacityHeads != 1 {
		t.Fatalf("CapacityHeads = %d, want 1 (a position with positive capacity always reports one head today)", rev.Capacity.CapacityHeads)
	}
	end, ok := rev.Effective.EndDate()
	if !ok {
		t.Fatal("Effective must carry the determinate end date the row declared")
	}
	// The row's effective_to (2027-06-01T00:00:00Z) is an exclusive
	// timestamptz boundary; the last covered calendar day is one day
	// earlier.
	if end.String() != "2027-05-31" {
		t.Fatalf("Effective end = %s, want 2027-05-31 (one day before the exclusive effective_to boundary)", end)
	}
}

// TestReaderZeroValueFailsClosed proves an unconfigured reader refuses to
// answer rather than silently reporting absence (which a caller could
// mistake for a real, checked "no such position").
func TestReaderZeroValueFailsClosed(t *testing.T) {
	var reader positionfacts.Reader
	_, _, err := reader.PositionRevisionAt(context.Background(), position.PositionQuery{
		Tenant: "some-tenant", Position: values.EntityRef{Tenant: "some-tenant", Kind: position.KindPosition, Id: uuid.New().String()},
		AsOf: readerAsOf(t),
	})
	if err == nil {
		t.Fatal("PositionRevisionAt on an unconfigured reader must return an error, not a silent not-found")
	}
}
