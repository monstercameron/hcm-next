package refdata_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	dataref "github.com/monstercameron/human-capital-management-suite/internal/data/refdata"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	domainref "github.com/monstercameron/human-capital-management-suite/internal/domains/refdata"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var storeAt = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
var storeDigest = strings.Repeat("a", 64)

func tenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',$4)`, id, key, key, storeAt)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("set role: %v", err)
	}
	return conn
}

func release(t *testing.T, version string) domainref.Release {
	t.Helper()
	member, err := domainref.NewMember("USD", "US dollar", true, false, storeAt, time.Time{}, map[string]string{"minor_units": "2"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := domainref.NewRelease(domainref.Release{
		DatasetID: "currencies", Version: version,
		Source:       domainref.SourceEvidence{SourceRef: "iso4217", SourceVersion: "2026", SourceDigest: storeDigest, SignatureRef: "sig:iso4217:2026", LicenseRef: "license:iso4217", Coverage: "currency-codes", RetrievedAt: storeAt},
		SchemaDigest: storeDigest, Applicability: []string{"global"}, Members: []domainref.Member{member}, ConsumerRefs: []string{"payroll"}, AffectedIntents: []string{"promotion"}, EffectiveFrom: storeAt, KnownAt: storeAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	v, err := domainref.ValidateRelease(r, storeAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	r, err = domainref.MarkValidated(r, v)
	if err != nil {
		t.Fatal(err)
	}
	r, err = domainref.PublishRelease(r, v, storeAt.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func tenantTx(t *testing.T, conn *pgxadapter.Conn, id uuid.UUID, fn func(dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, tx, id); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestReferenceDatasetReleaseDurableAdoptionHistory(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	store := dataref.NewStore(conn)
	id := tenant(t, db, "refdata-primary")
	first, second := release(t, "1.0.0"), release(t, "2.0.0")
	for _, r := range []domainref.Release{first, second} {
		if err := store.PutRelease(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	firstAdoption, err := store.Adopt(ctx, id, first, domainref.AdoptionRequest{TenantID: id.String(), EffectiveAt: storeAt, Actor: "operator", RolloutDigest: storeDigest, ImpactRefs: first.AffectedIntents}, storeAt.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	secondAdoption, err := store.Adopt(ctx, id, second, domainref.AdoptionRequest{TenantID: id.String(), EffectiveAt: storeAt.Add(24 * time.Hour), Actor: "operator", RolloutDigest: storeDigest, ImpactRefs: second.AffectedIntents}, storeAt.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	rolledBack, err := store.Rollback(ctx, id, first, "repair", storeAt.Add(48*time.Hour), storeAt.Add(5*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.Event != domainref.EventRollback || rolledBack.PreviousVersion != second.Version || firstAdoption.Version != first.Version || secondAdoption.Version != second.Version {
		t.Fatalf("adoption sequence = %+v, %+v, %+v", firstAdoption, secondAdoption, rolledBack)
	}
	history, err := store.ListAdoptions(ctx, id, first.DatasetID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 || history[2].ReleaseDigest != first.Digest {
		t.Fatalf("durable adoption history = %+v", history)
	}
	got, found, err := store.GetRelease(ctx, first.DatasetID, first.Version)
	if err != nil || !found || got.Digest != first.Digest || got.Source.SourceVersion != first.Source.SourceVersion {
		t.Fatalf("durable release = %+v found=%v err=%v", got, found, err)
	}
}

func TestTodo_REFDATA_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	store := dataref.NewPGStore(conn)
	id := tenant(t, db, "refdata-integration")
	r := release(t, "1.0.0")
	if err := store.SaveRelease(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	a, err := store.Adopt(context.Background(), id, r, domainref.AdoptionRequest{TenantID: id.String(), EffectiveAt: storeAt, Actor: "operator", RolloutDigest: storeDigest, ImpactRefs: r.AffectedIntents}, storeAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := domainref.ValidateAdoption(a, r); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_REFDATA_001_Security(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	store := dataref.NewStore(conn)
	alpha, beta := tenant(t, db, "refdata-alpha"), tenant(t, db, "refdata-beta")
	r := release(t, "1.0.0")
	if err := store.PutRelease(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Adopt(context.Background(), beta, r, domainref.AdoptionRequest{TenantID: beta.String(), EffectiveAt: storeAt, Actor: "operator", RolloutDigest: storeDigest, ImpactRefs: r.AffectedIntents}, storeAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Adopt(context.Background(), alpha, r, domainref.AdoptionRequest{TenantID: beta.String(), EffectiveAt: storeAt, Actor: "operator", RolloutDigest: storeDigest, ImpactRefs: r.AffectedIntents}, storeAt); !errors.Is(err, dataref.ErrInvalid) {
		t.Fatalf("cross-tenant adoption = %v", err)
	}
	rows, err := store.ListAdoptions(context.Background(), alpha, r.DatasetID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("alpha saw beta adoption rows: %+v", rows)
	}
	if err := store.PutRelease(context.Background(), r); !errors.Is(err, dataref.ErrDuplicate) {
		t.Fatalf("duplicate release = %v", err)
	}
}

func TestTodo_REFDATA_001_Recovery(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	store := dataref.NewStore(conn)
	id := tenant(t, db, "refdata-recovery")
	r := release(t, "1.0.0")
	if err := store.PutRelease(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Rollback(context.Background(), id, r, "repair", storeAt, storeAt); !errors.Is(err, domainref.ErrRollbackUnavailable) {
		t.Fatalf("rollback without current pin = %v", err)
	}
}
