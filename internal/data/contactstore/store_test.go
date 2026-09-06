package contactstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/contactstore"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/contact"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	return conn
}

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 80); err != nil {
		t.Fatalf("apply migrations through 00080: %v", err)
	}
	return db
}

func endpoint(t *testing.T, tenant uuid.UUID, suffix string) contact.ContactEndpointRevision {
	t.Helper()
	subject := values.EntityRef{Tenant: values.TenantId(tenant.String()), Kind: "worker", Id: uuid.NewString()}
	got, err := contact.NewContactEndpointRevision(subject, uuid.NewString(), contact.EndpointEmail, "Person"+suffix+"@example.com", "account-recovery", 1, "worker-profile")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func challenge(t *testing.T, endpoint contact.ContactEndpointRevision, suffix string) contact.ContactVerificationChallenge {
	t.Helper()
	got, _, err := contact.IssueContactChallenge(uuid.NewString(), endpoint.Subject, endpoint, "account-recovery", "123456"+suffix, time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestTodo_PERSIST_CONTACT_001(t *testing.T) {
	db := newDB(t)
	id := insertTenant(t, db, "contact-primary")
	key := values.TenantId(id.String())
	conn := appConn(t, db)
	store := contactstore.New(conn)
	e := endpoint(t, id, "-primary")
	if err := store.PutEndpointRevision(context.Background(), key, e); err != nil {
		t.Fatal(err)
	}
	c := challenge(t, e, "-primary")
	if err := store.PutChallenge(context.Background(), key, c); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetChallenge(context.Background(), key, c.ChallengeID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CanonicalDigest != c.CanonicalDigest || loaded.NormalizedValueDigest != e.NormalizedValueDigest {
		t.Fatalf("loaded challenge lost digest identity: %+v", loaded)
	}
	var endpointRows, challengeRows, eventRows int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM contact_endpoint_revision WHERE tenant_id=$1`, id).Scan(&endpointRows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM contact_verification_challenge WHERE tenant_id=$1`, id).Scan(&challengeRows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM contact_challenge_event WHERE tenant_id=$1`, id).Scan(&eventRows); err != nil {
		t.Fatal(err)
	}
	if endpointRows != 1 || challengeRows != 1 || eventRows != 1 {
		t.Fatalf("row counts = %d/%d/%d", endpointRows, challengeRows, eventRows)
	}
}

func TestTodo_PERSIST_CONTACT_001_Fault(t *testing.T) {
	db := newDB(t)
	id := insertTenant(t, db, "contact-fault")
	key := values.TenantId(id.String())
	store := contactstore.New(appConn(t, db))
	e := endpoint(t, id, "-fault")
	ctx := context.Background()
	if err := store.PutEndpointRevision(ctx, key, e); err != nil {
		t.Fatal(err)
	}
	if got := contactstore.CodeOf(store.PutEndpointRevision(ctx, key, e)); got != contactstore.CodeDuplicate {
		t.Fatalf("duplicate revision code = %q", got)
	}
	next, err := e.MarkVerified()
	if err != nil {
		t.Fatal(err)
	}
	if got := contactstore.CodeOf(store.PutEndpointRevision(ctx, key, next, 0)); got != contactstore.CodeStaleCAS {
		t.Fatalf("stale revision code = %q", got)
	}
	c := challenge(t, e, "-fault")
	if err := store.PutChallenge(ctx, key, c); err != nil {
		t.Fatal(err)
	}
	updated, _, err := c.Respond("wrong", c.IssuedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if got := contactstore.CodeOf(store.PutChallenge(ctx, key, updated, "sha256:forged")); got != contactstore.CodeStaleCAS {
		t.Fatalf("stale challenge code = %q", got)
	}
}

func TestTodo_PERSIST_CONTACT_001_Integration(t *testing.T) {
	db := newDB(t)
	id := insertTenant(t, db, "contact-integration")
	key := values.TenantId(id.String())
	store := contactstore.New(appConn(t, db))
	ctx := context.Background()
	e := endpoint(t, id, "-integration")
	if err := store.PutEndpointRevision(ctx, key, e); err != nil {
		t.Fatal(err)
	}
	next, err := e.MarkVerified()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutEndpointRevision(ctx, key, next, 1); err != nil {
		t.Fatal(err)
	}
	history, err := store.ListEndpointRevisions(ctx, key, e.EndpointID)
	if err != nil || len(history) != 2 || history[1].Verification != contact.Verified {
		t.Fatalf("endpoint history = %+v, err=%v", history, err)
	}
	c := challenge(t, next, "-integration")
	if err := store.PutChallenge(ctx, key, c); err != nil {
		t.Fatal(err)
	}
	updated, _, err := c.Respond("wrong-integration", c.IssuedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutChallenge(ctx, key, updated, c.CanonicalDigest); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListChallengeEvents(ctx, key, c.ChallengeID)
	if err != nil || len(events) != 2 || events[1].AnswerDigest == "wrong-integration" {
		t.Fatalf("challenge events = %+v, err=%v", events, err)
	}
}

func TestTodo_PERSIST_CONTACT_001_Security(t *testing.T) {
	db := newDB(t)
	a := insertTenant(t, db, "contact-security-a")
	b := insertTenant(t, db, "contact-security-b")
	store := contactstore.New(appConn(t, db))
	ctx := context.Background()
	ea, eb := endpoint(t, a, "-a"), endpoint(t, b, "-b")
	if err := store.PutEndpointRevision(ctx, values.TenantId(a.String()), ea); err != nil {
		t.Fatal(err)
	}
	if err := store.PutEndpointRevision(ctx, values.TenantId(b.String()), eb); err != nil {
		t.Fatal(err)
	}
	tx, err := appConn(t, db).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, tx, a); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM contact_endpoint_revision`).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if visible != 1 {
		t.Fatalf("tenant A visible rows = %d, want 1", visible)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM contact_endpoint_revision WHERE endpoint_id=$1`, eb.EndpointID).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatal("tenant A read tenant B endpoint")
	}
	_ = tx.Rollback(ctx)
}

func TestTodo_PERSIST_CONTACT_001_Recovery(t *testing.T) {
	db := newDB(t)
	id := insertTenant(t, db, "contact-recovery")
	key := values.TenantId(id.String())
	e := endpoint(t, id, "-recovery")
	first := contactstore.New(appConn(t, db))
	ctx := context.Background()
	if err := first.PutEndpointRevision(ctx, key, e); err != nil {
		t.Fatal(err)
	}
	c := challenge(t, e, "-recovery")
	if err := first.PutChallenge(ctx, key, c); err != nil {
		t.Fatal(err)
	}
	second := contactstore.New(appConn(t, db))
	loaded, err := second.GetChallenge(ctx, key, c.ChallengeID)
	if err != nil || loaded.CanonicalDigest != c.CanonicalDigest {
		t.Fatalf("fresh connection challenge = %+v, err=%v", loaded, err)
	}
}

func TestTodo_PERSIST_CONTACT_001_Mutation(t *testing.T) {
	db := newDB(t)
	id := insertTenant(t, db, "contact-mutation")
	key := values.TenantId(id.String())
	store := contactstore.New(appConn(t, db))
	e := endpoint(t, id, "-mutation")
	ctx := context.Background()
	if err := store.PutEndpointRevision(ctx, key, e); err != nil {
		t.Fatal(err)
	}
	c := challenge(t, e, "-mutation")
	if err := store.PutChallenge(ctx, key, c); err != nil {
		t.Fatal(err)
	}
	if err := db.ExecErr(`UPDATE contact_endpoint_revision SET purpose='forged' WHERE tenant_id=$1`, id); err == nil {
		t.Fatal("endpoint revision accepted UPDATE")
	}
	if err := db.ExecErr(`DELETE FROM contact_endpoint_revision WHERE tenant_id=$1`, id); err == nil {
		t.Fatal("endpoint revision accepted DELETE")
	}
	if err := db.ExecErr(`UPDATE contact_challenge_event SET attempt=99 WHERE tenant_id=$1`, id); err == nil {
		t.Fatal("challenge event accepted UPDATE")
	}
	if err := db.ExecErr(`DELETE FROM contact_challenge_event WHERE tenant_id=$1`, id); err == nil {
		t.Fatal("challenge event accepted DELETE")
	}
	if errors.Is(store.PutChallenge(ctx, key, c, c.CanonicalDigest), contact.ErrStoreDuplicate) {
		t.Fatal("unchanged challenge unexpectedly reported duplicate as a database error")
	}
}
