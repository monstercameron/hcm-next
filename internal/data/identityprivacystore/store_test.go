package identityprivacystore_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/identityprivacystore"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/proofing"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var testTenantA = uuid.MustParse("00000000-0000-4000-8000-0000000000a1")
var testTenantB = uuid.MustParse("00000000-0000-4000-8000-0000000000b1")

func database(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 96); err != nil {
		t.Fatalf("apply migrations through 00096: %v", err)
	}
	for _, tenant := range []uuid.UUID{testTenantA, testTenantB} {
		db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenant, tenant.String(), tenant.String())
	}
	return db
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	return conn
}

func proofingSubject() values.EntityRef {
	return values.EntityRef{Tenant: "tenant-1", Kind: "worker", Id: "00000000-0000-4000-8000-000000000001"}
}

func proofingFixture(t *testing.T, id string) proofing.ProofingSession {
	t.Helper()
	at := values.NewInstant(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
	item, err := proofing.NewEvidenceItem(proofing.EvidencePassport, "sha256:passport-digest", "sha256:provider-digest", "custody://passport-1", proofing.AssuranceIAL2, at)
	if err != nil {
		t.Fatal(err)
	}
	session, err := proofing.NewProofingSession(id, proofingSubject(), "onboarding", proofing.AssuranceIAL2, []proofing.EvidenceItem{item}, "verifier-1", values.NewInstant(time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func transaction(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	t.Helper()
	tx, err := conn.Begin(context.Background())
	if err != nil {
		return err
	}
	if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
		_ = tx.Rollback(context.Background())
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(context.Background())
		return err
	}
	return tx.Commit(context.Background())
}

func TestTodo_PERSIST_IDENTITYPRIVACY_001(t *testing.T) {
	db := database(t)
	store := identityprivacystore.New(appConn(t, db))
	session := proofingFixture(t, "session-primary")
	if err := store.SaveSession(context.Background(), testTenantA.String(), session, 0); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadSession(context.Background(), testTenantA.String(), session.SessionID, session.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalDigest != session.CanonicalDigest || got.Subject != session.Subject || len(got.Evidence) != 1 {
		t.Fatalf("loaded session lost persisted value: got=%+v want=%+v", got, session)
	}
}

func TestTodo_PERSIST_IDENTITYPRIVACY_001_Fault(t *testing.T) {
	db := database(t)
	store := identityprivacystore.New(appConn(t, db))
	session := proofingFixture(t, "session-fault")
	if err := store.SaveSession(context.Background(), testTenantA.String(), session, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(context.Background(), testTenantA.String(), session, 0); !errors.Is(err, proofing.ErrStoreDuplicate) {
		t.Fatalf("duplicate=%v, want typed duplicate", err)
	}
	next, err := session.RecordOutcome(proofing.OutcomeReviewRequired)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(context.Background(), testTenantA.String(), next, 99); !errors.Is(err, proofing.ErrStoreStaleCAS) {
		t.Fatalf("stale=%v, want typed stale CAS", err)
	}
}

func TestTodo_PERSIST_IDENTITYPRIVACY_001_Integration(t *testing.T) {
	db := database(t)
	store := identityprivacystore.New(appConn(t, db))
	first := proofingFixture(t, "session-integration")
	second, err := first.RecordOutcome(proofing.OutcomeVerified)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(context.Background(), testTenantA.String(), first, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(context.Background(), testTenantA.String(), second, first.Revision); err != nil {
		t.Fatal(err)
	}
	current, err := store.CurrentSession(context.Background(), testTenantA.String(), first.SessionID)
	if err != nil || current.Revision != 2 {
		t.Fatalf("current=%+v err=%v", current, err)
	}
	history, err := store.ListSessions(context.Background(), testTenantA.String(), first.SessionID)
	if err != nil || len(history) != 2 {
		t.Fatalf("history=%+v err=%v", history, err)
	}
}

func TestTodo_PERSIST_IDENTITYPRIVACY_001_Security(t *testing.T) {
	db := database(t)
	store := identityprivacystore.New(appConn(t, db))
	session := proofingFixture(t, "session-security")
	if err := store.SaveSession(context.Background(), testTenantA.String(), session, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(context.Background(), testTenantB.String(), proofingFixture(t, "session-security"), 0); err != nil {
		t.Fatal(err)
	}
	err := transaction(t, appConn(t, db), testTenantA, func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM proofing_session WHERE tenant_id=$1`, testTenantB).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatalf("tenant A read %d tenant B rows under RLS", count)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTodo_PERSIST_IDENTITYPRIVACY_001_Recovery(t *testing.T) {
	db := database(t)
	first := identityprivacystore.New(appConn(t, db))
	session := proofingFixture(t, "session-recovery")
	if err := first.SaveSession(context.Background(), testTenantA.String(), session, 0); err != nil {
		t.Fatal(err)
	}
	fresh := identityprivacystore.New(appConn(t, db))
	got, err := fresh.LoadSession(context.Background(), testTenantA.String(), session.SessionID, session.Revision)
	if err != nil || got.CanonicalDigest != session.CanonicalDigest {
		t.Fatalf("fresh connection got=%+v err=%v", got, err)
	}
}

func TestTodo_PERSIST_IDENTITYPRIVACY_001_Mutation(t *testing.T) {
	db := database(t)
	store := identityprivacystore.New(appConn(t, db))
	session := proofingFixture(t, "session-mutation")
	if err := store.SaveSession(context.Background(), testTenantA.String(), session, 0); err != nil {
		t.Fatal(err)
	}
	err := transaction(t, appConn(t, db), testTenantA, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE proofing_session SET outcome='REJECTED' WHERE tenant_id=$1`, testTenantA)
		return err
	})
	if err == nil {
		t.Fatal("UPDATE proofing_session succeeded")
	}
}

func authorizationFixture(t *testing.T, id string) proofing.WorkAuthorizationEvidence {
	t.Helper()
	date := func(year int, month time.Month, day int) values.LocalDate {
		value, err := values.NewLocalDate(year, month, day)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	value, err := proofing.NewWorkAuthorizationEvidence(proofing.WorkAuthorizationEvidence{
		EvidenceID: id, Subject: proofingSubject(), Revision: 1,
		DocumentClass: proofing.DocumentPassport, VerificationMethod: proofing.VerificationManualReview,
		Jurisdiction: "US", Category: "employee", ValidFrom: date(2026, time.January, 1),
		ValidUntil: date(2027, time.January, 1), ReverificationDue: date(2026, time.July, 1),
		EvidenceDigest: "sha256:" + "a" + strings.Repeat("b", 63), SourceRef: "custody://authorization-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestIdentityPrivacyStore_AuthorizationLifecycleAndNegativePaths(t *testing.T) {
	db := database(t)
	store := identityprivacystore.New(appConn(t, db))
	ctx := context.Background()
	value := authorizationFixture(t, "authorization-lifecycle")
	if err := store.SaveAuthorization(ctx, testTenantA.String(), value, 0); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadAuthorization(ctx, testTenantA.String(), value.EvidenceID, 1)
	if err != nil || loaded.CanonicalDigest != value.CanonicalDigest || loaded.SourceRef != value.SourceRef {
		t.Fatalf("loaded authorization = %+v, err=%v", loaded, err)
	}
	current, err := store.CurrentAuthorization(ctx, testTenantA.String(), value.EvidenceID)
	if err != nil || current.Revision != 1 {
		t.Fatalf("current authorization = %+v, err=%v", current, err)
	}
	history, err := store.ListAuthorizations(ctx, testTenantA.String(), value.EvidenceID)
	if err != nil || len(history) != 1 || history[0].EvidenceID != value.EvidenceID {
		t.Fatalf("authorization history = %+v, err=%v", history, err)
	}
	err = store.SaveAuthorization(ctx, testTenantA.String(), value, 0)
	var typed *proofing.StoreError
	if !errors.As(err, &typed) || typed.Code != proofing.StoreDuplicateCode {
		t.Fatalf("duplicate authorization = %v", err)
	}
	missing, err := store.LoadAuthorization(ctx, testTenantA.String(), value.EvidenceID, 99)
	if err == nil || missing.EvidenceID != "" {
		t.Fatalf("missing authorization = %+v, err=%v", missing, err)
	}
	if _, err := store.CurrentAuthorization(ctx, testTenantA.String(), "missing"); err == nil {
		t.Fatal("missing current authorization was accepted")
	}
	if _, err := store.ListAuthorizations(ctx, testTenantA.String(), "missing"); err == nil {
		t.Fatal("missing authorization history was accepted")
	}
	next := value
	next.Revision = 2
	next.SupersedesRevision = 1
	next.Category = "employee-corrected"
	next, err = proofing.NewWorkAuthorizationEvidence(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAuthorization(ctx, testTenantA.String(), next, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAuthorization(ctx, testTenantA.String(), next, 1); err == nil {
		t.Fatal("duplicate successor authorization was accepted")
	}
	if _, err := store.LoadAuthorization(ctx, "not-a-uuid", value.EvidenceID, 1); err == nil {
		t.Fatal("malformed tenant was accepted")
	}
}
