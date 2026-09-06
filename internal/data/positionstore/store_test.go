package positionstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/positionstore"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/position"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var testNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 43); err != nil {
		t.Fatalf("apply migrations through 00043: %v", err)
	}
	return db
}

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',$4)`, id, key, key, testNow)
	return id
}

func appDB(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("set role: %v", err)
	}
	return conn
}

func request(t *testing.T, tenant string) position.PositionReservationRequest {
	t.Helper()
	date, err := values.ParseLocalDate("2026-09-05")
	if err != nil {
		t.Fatal(err)
	}
	known, err := values.NewKnownAt(values.NewInstant(testNow))
	if err != nil {
		t.Fatal(err)
	}
	ref := values.EntityRef{Tenant: values.TenantId(tenant), Kind: position.KindPosition, Id: "00000000-0000-4000-8000-000000000001"}
	fte, err := values.NewDecimal("1.0000", 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return position.PositionReservationRequest{Tenant: values.TenantId(tenant), Position: ref, AsOf: position.AsOf{EffectiveOn: date, KnownAt: known}, ProposalRevisionID: "proposal-1", ProposalDigest: canonicalbytes.Digest([]byte("proposal")), EffectiveDate: date, FTE: fte, Heads: 1, DesiredJobCode: "HR", DesiredOrgUnit: "ORG", DesiredLegalEntity: "LE", AuthorityDigest: canonicalbytes.Digest([]byte("authority")), IdempotencyKey: "idem-1", ExpiresAt: testNow.Add(24 * time.Hour)}
}

func seed(t *testing.T, db *pgtest.DB, tenantKey string) (positionstore.Store, position.PositionReservation, uuid.UUID) {
	t.Helper()
	tenantID := insertTenant(t, db, tenantKey)
	conn := appDB(t, db)
	store := positionstore.New(conn, func(got values.TenantId) uuid.UUID {
		if got == values.TenantId(tenantKey) {
			return tenantID
		}
		return uuid.Nil
	})
	req := request(t, tenantKey)
	r := position.PositionReservation{ID: "reservation-1", Request: req, State: position.PositionReservationHeld, Fence: 1, CreatedAt: testNow, UpdatedAt: testNow}
	e := position.PositionReservationEvent{Sequence: 1, ReservationID: r.ID, To: position.PositionReservationHeld, Reason: "ACQUIRED", Fence: 1, At: testNow}
	if err := store.Put(context.Background(), r, e); err != nil {
		t.Fatal(err)
	}
	return store, r, tenantID
}

func TestTodo_PERSIST_POSITION_001(t *testing.T) {
	db := newDB(t)
	store, want, _ := seed(t, db, "position-primary")
	got, err := store.Load(context.Background(), want.Request.Tenant, want.ID)
	if err != nil || got.ID != want.ID || got.State != position.PositionReservationHeld || got.Fence != 1 || got.Request.ProposalDigest != want.Request.ProposalDigest {
		t.Fatalf("Load = %#v, %v", got, err)
	}
	events, err := store.Events(context.Background(), want.Request.Tenant, want.ID)
	if err != nil || len(events) != 1 || events[0].To != position.PositionReservationHeld {
		t.Fatalf("Events = %#v, %v", events, err)
	}
}

func TestTodo_PERSIST_POSITION_001_Fault(t *testing.T) {
	db := newDB(t)
	store, want, _ := seed(t, db, "position-fault")
	if err := store.Put(context.Background(), want, position.PositionReservationEvent{Sequence: 1, ReservationID: want.ID, To: want.State, Fence: 1, At: testNow}); !errors.Is(err, positionstore.ErrDuplicate) {
		t.Fatalf("duplicate = %v", err)
	}
	if _, err := store.Transition(context.Background(), want.Request.Tenant, want.ID, 1, position.PositionReservationReleased, "RELEASED", testNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition(context.Background(), want.Request.Tenant, want.ID, 1, position.PositionReservationExpired, "EXPIRED", testNow.Add(2*time.Minute)); !errors.Is(err, positionstore.ErrVersionConflict) {
		t.Fatalf("stale fence = %v", err)
	}
}

func TestTodo_PERSIST_POSITION_001_Integration(t *testing.T) {
	db := newDB(t)
	store, want, _ := seed(t, db, "position-integration")
	got, err := store.Transition(context.Background(), want.Request.Tenant, want.ID, 1, position.PositionReservationReleased, "PROPOSAL_REJECTED", testNow.Add(time.Minute))
	if err != nil || got.State != position.PositionReservationReleased || got.Fence != 2 {
		t.Fatalf("Transition = %#v, %v", got, err)
	}
	events, err := store.Events(context.Background(), want.Request.Tenant, want.ID)
	if err != nil || len(events) != 2 || events[1].From != position.PositionReservationHeld || events[1].Fence != 2 {
		t.Fatalf("transition events = %#v, %v", events, err)
	}
}

func TestTodo_PERSIST_POSITION_001_Security(t *testing.T) {
	db := newDB(t)
	_, want, tenantID := seed(t, db, "position-security-a")
	other := insertTenant(t, db, "position-security-b")
	otherStore := positionstore.New(appDB(t, db), func(got values.TenantId) uuid.UUID {
		if got == "position-security-b" {
			return other
		}
		return uuid.Nil
	})
	if _, err := otherStore.Load(context.Background(), "position-security-b", want.ID); !errors.Is(err, positionstore.ErrNotFound) {
		t.Fatalf("cross-tenant load = %v", err)
	}
	if err := otherStore.Put(context.Background(), want, position.PositionReservationEvent{Sequence: 2, ReservationID: want.ID, To: want.State, Fence: 1, At: testNow}); !errors.Is(err, positionstore.ErrDuplicate) && !errors.Is(err, positionstore.ErrInvalid) {
		t.Fatalf("cross-tenant put = %v", err)
	}
	var count int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM position_reservation WHERE tenant_id=$1`, tenantID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("primary count = %d, %v", count, err)
	}
}

func TestTodo_PERSIST_POSITION_001_Recovery(t *testing.T) {
	db := newDB(t)
	_, want, tenantID := seed(t, db, "position-recovery")
	conn := appDB(t, db)
	recovered := positionstore.New(conn, func(got values.TenantId) uuid.UUID {
		if got == want.Request.Tenant {
			return tenantID
		}
		return uuid.Nil
	})
	got, err := recovered.Load(context.Background(), want.Request.Tenant, want.ID)
	if err != nil || got.ID != want.ID || got.Fence != 1 {
		t.Fatalf("recovered = %#v, %v", got, err)
	}
}

func TestTodo_PERSIST_POSITION_001_Mutation(t *testing.T) {
	db := newDB(t)
	store, want, tenantID := seed(t, db, "position-mutation")
	if err := db.ExecErr(`UPDATE position_reservation_event SET reason='tampered' WHERE tenant_id=$1 AND reservation_id=$2`, tenantID, want.ID); err == nil {
		t.Fatal("event update was accepted")
	}
	if err := db.ExecErr(`DELETE FROM position_reservation_event WHERE tenant_id=$1 AND reservation_id=$2`, tenantID, want.ID); err == nil {
		t.Fatal("event delete was accepted")
	}
	if err := db.ExecErr(`UPDATE position_reservation SET updated_at=$3 WHERE tenant_id=$1 AND reservation_id=$2`, tenantID, want.ID, testNow.Add(time.Hour)); err == nil {
		t.Fatal("fence-less current-state update was accepted")
	}
	if _, err := store.Load(context.Background(), want.Request.Tenant, want.ID); err != nil {
		t.Fatal(err)
	}
}
