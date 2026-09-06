package legalevidencestore

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	legal "github.com/monstercameron/hcm-next/internal/governance/legal"
	legalpipeline "github.com/monstercameron/hcm-next/internal/governance/legal/pipeline"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant
		(tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`,
		tenant, key, "tenant "+key)
	return tenant
}

func newLegalDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 41); err != nil {
		t.Fatalf("apply prerequisite migrations: %v", err)
	}
	migration, err := os.ReadFile("../../../migrations/00053_legal_evidence.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.SplitN(string(migration), "-- +goose Down", 2)[0]
	if _, err := db.SQL.ExecContext(context.Background(), up); err != nil {
		t.Fatalf("apply legal evidence migration: %v", err)
	}
	return db
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) {
	t.Helper()
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) error {
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

func reviewEntry(tenant uuid.UUID, sequence int64) legal.ReviewRecordEntry {
	record := legal.ReviewRecord{
		PackDigest: strings.Repeat("a", 64),
		AuthorID:   "author",
		ReviewerID: "reviewer",
		Status:     legal.ReviewStatusVendorBaseline,
	}
	return legal.ReviewRecordEntry{
		TenantID:      tenant.String(),
		RowID:         uuid.NewString(),
		EventSequence: sequence,
		Record:        record,
		Digest:        record.ComputeDigest(),
	}
}

func receiptEntry(tenant uuid.UUID, sequence int64) legal.CompositionReceiptEntry {
	receipt := legal.CompositionReceipt{
		Status:         legal.CompositionResolved,
		Jurisdictions:  []legal.Jurisdiction{},
		Inputs:         []legal.ObligationEvidence{},
		Obligations:    []legal.ComposedObligation{},
		Traces:         []legal.CompositionTrace{},
		Contradictions: []legal.ContradictoryRequirement{},
		Digest:         strings.Repeat("b", 64),
	}
	return legal.CompositionReceiptEntry{
		TenantID:      tenant.String(),
		RowID:         uuid.NewString(),
		EventSequence: sequence,
		Receipt:       receipt,
		Digest:        receipt.Digest,
	}
}

func pipelineEntries(t *testing.T) (legalpipeline.EventEntry, legalpipeline.EventEntry) {
	t.Helper()
	b, err := os.ReadFile("../../../definitions/legal/packs/seed/us-ca.json")
	if err != nil {
		t.Fatal(err)
	}
	key1 := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x11}, ed25519.SeedSize))
	key2 := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x22}, ed25519.SeedSize))
	author, err := legal.NewSigner(key1)
	if err != nil {
		t.Fatal(err)
	}
	reviewer, err := legal.NewSigner(key2)
	if err != nil {
		t.Fatal(err)
	}
	p, err := legalpipeline.Author(b, "author", author)
	if err != nil {
		t.Fatal(err)
	}
	first := legalpipeline.EventEntry{TenantID: uuid.NewString(), RowID: uuid.NewString(), EventSequence: 1, Event: p.Events[0]}
	if err := p.Review("reviewer", legal.ReviewStatusVendorBaseline, nil, reviewer); err != nil {
		t.Fatal(err)
	}
	second := legalpipeline.EventEntry{TenantID: first.TenantID, RowID: uuid.NewString(), EventSequence: 2, Event: p.Events[1]}
	return first, second
}

func TestTodo_PERSIST_LEGALEVIDENCE_001(t *testing.T) {
	db := newLegalDB(t)
	tenant := insertTenant(t, db, "legal-primary")
	conn := appConn(t, db)
	store := New(conn)
	entry := reviewEntry(tenant, 1)
	got, err := store.AppendReviewRecord(context.Background(), entry)
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != entry.Digest {
		t.Fatalf("stored digest = %q, want %q", got.Digest, entry.Digest)
	}
	rows, err := store.ListReviewRecords(context.Background(), tenant.String(), entry.Record.PackDigest)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Record.AuthorID != "author" {
		t.Fatalf("review rows = %+v", rows)
	}
}

func TestTodo_PERSIST_LEGALEVIDENCE_001_Fault(t *testing.T) {
	db := newLegalDB(t)
	tenant := insertTenant(t, db, "legal-fault")
	conn := appConn(t, db)
	store := New(conn)
	entry := reviewEntry(tenant, 1)
	if _, err := store.AppendReviewRecord(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendReviewRecord(context.Background(), entry); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate review error = %v, want ErrDuplicate", err)
	}
	first, second := pipelineEntries(t)
	first.TenantID = tenant.String()
	second.TenantID = tenant.String()
	if _, err := store.AppendEvent(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second.Event.PrevDigest = "stale"
	if _, err := store.AppendEvent(context.Background(), second); !errors.Is(err, ErrChainBroken) {
		t.Fatalf("stale pipeline predecessor error = %v, want ErrChainBroken", err)
	}
}

func TestTodo_PERSIST_LEGALEVIDENCE_001_Integration(t *testing.T) {
	db := newLegalDB(t)
	tenant := insertTenant(t, db, "legal-integration")
	conn := appConn(t, db)
	store := New(conn)
	if _, err := store.AppendReviewRecord(context.Background(), reviewEntry(tenant, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendCompositionReceipt(context.Background(), receiptEntry(tenant, 1)); err != nil {
		t.Fatal(err)
	}
	first, second := pipelineEntries(t)
	first.TenantID = tenant.String()
	second.TenantID = tenant.String()
	if _, err := store.AppendEvent(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListEvents(context.Background(), tenant.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].Event.PrevDigest != events[0].Event.Digest {
		t.Fatalf("pipeline events = %+v", events)
	}
}

func TestTodo_PERSIST_LEGALEVIDENCE_001_Security(t *testing.T) {
	db := newLegalDB(t)
	tenantA := insertTenant(t, db, "legal-security-a")
	tenantB := insertTenant(t, db, "legal-security-b")
	conn := appConn(t, db)
	store := New(conn)
	entry := reviewEntry(tenantA, 1)
	if _, err := store.AppendReviewRecord(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListReviewRecords(context.Background(), tenantB.String(), entry.Record.PackDigest)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("cross-tenant review rows = %d, want 0", len(rows))
	}
	if err := inTenantTxErr(conn, tenantB, func(tx dbport.Tx) error {
		return InsertReviewRecord(context.Background(), tx, entry)
	}); err == nil {
		t.Fatal("cross-tenant insert was accepted")
	}
}

func TestTodo_PERSIST_LEGALEVIDENCE_001_Recovery(t *testing.T) {
	db := newLegalDB(t)
	tenant := insertTenant(t, db, "legal-recovery")
	conn := appConn(t, db)
	store := New(conn)
	entry := receiptEntry(tenant, 1)
	if _, err := store.AppendCompositionReceipt(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	fresh := New(appConn(t, db))
	rows, err := fresh.ListCompositionReceipts(context.Background(), tenant.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Receipt.Digest != entry.Digest {
		t.Fatalf("recovered receipts = %+v", rows)
	}
}

func TestTodo_PERSIST_LEGALEVIDENCE_001_Mutation(t *testing.T) {
	db := newLegalDB(t)
	tenant := insertTenant(t, db, "legal-mutation")
	conn := appConn(t, db)
	store := New(conn)
	entry := reviewEntry(tenant, 1)
	if _, err := store.AppendReviewRecord(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE legal_review_record SET status='REJECTED' WHERE tenant_id=$1 AND row_id=$2`,
		`DELETE FROM legal_review_record WHERE tenant_id=$1 AND row_id=$2`,
	} {
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(context.Background(), statement, tenant, entry.RowID)
			return err
		})
		if err == nil {
			t.Fatalf("mutation succeeded: %s", statement)
		}
	}
}
