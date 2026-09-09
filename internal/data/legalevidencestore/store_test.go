package legalevidencestore

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	legalpipeline "github.com/monstercameron/human-capital-management-suite/internal/governance/legal/pipeline"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
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
	migration, err = os.ReadFile("../../../migrations/00275_legal_evaluation_receipts.sql")
	if err != nil {
		t.Fatal(err)
	}
	up = strings.SplitN(string(migration), "-- +goose Down", 2)[0]
	if _, err := db.SQL.ExecContext(context.Background(), up); err != nil {
		t.Fatalf("apply evaluation migration: %v", err)
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

func signedEvaluationEvidence(t *testing.T, tenant uuid.UUID) (EvaluationReceiptEntry, EvaluationBindingEntry, *legal.Signer) {
	t.Helper()
	signer, err := legal.NewSigner(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x44}, ed25519.SeedSize)))
	if err != nil {
		t.Fatal(err)
	}
	date, _ := values.NewLocalDate(2026, time.January, 1)
	known, _ := values.NewKnownAt(values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
	r := legal.LegalEvaluationReceipt{LegalContextDigest: strings.Repeat("c", 64), JurisdictionSet: legal.ReceiptJurisdictionSet{Primary: legal.Jurisdiction{Country: "US", State: "CA"}}, PinnedReleases: []legal.PinnedReleaseEvidence{{Release: legal.RulePackRelease{PackID: "pack-ca", Version: 1, Jurisdiction: legal.Jurisdiction{Country: "US", State: "CA"}}, Digest: strings.Repeat("a", 64)}}, AttributionRuleFired: legal.AttributionA1, RemoteWorkPolicyApplied: "not_remote", ObligationsApplied: []legal.ReceiptAppliedObligation{}, ObligationsNotApplicable: []legal.ConsideredObligation{}, ObligationsNotConsidered: []legal.NotConsideredKind{}, CompositionTrace: []legal.CompositionTrace{}, Status: legal.LegalEvaluationStatusResolvedAllow, EvaluatedAt: values.NewInstant(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)), EffectiveDate: date, KnownAt: known}
	r.Digest, r.Signature = signer.SignDigest(r.CanonicalBytes())
	ref := uuid.NewString()
	b := legal.EvaluationBinding{Tenant: tenant.String(), IntentID: "intent-1", ProposalRevisionID: "proposal-1", MaterialDigest: strings.Repeat("d", 64), ReceiptRef: ref, ReceiptDigest: r.Digest, LegalContextDigest: r.LegalContextDigest}
	b, err = legal.SignEvaluationBinding(b, signer)
	if err != nil {
		t.Fatal(err)
	}
	return EvaluationReceiptEntry{TenantID: tenant.String(), Tenant: tenant.String(), ReceiptRef: ref, Receipt: r}, EvaluationBindingEntry{TenantID: tenant.String(), Binding: b}, signer
}

func TestTodo_LEGAL_014_Integration(t *testing.T) {
	db := newLegalDB(t)
	tenant := insertTenant(t, db, "eval-integration")
	re, be, signer := signedEvaluationEvidence(t, tenant)
	authorize := func(_ context.Context, ref string) (uuid.UUID, error) {
		if ref != tenant.String() {
			return uuid.Nil, errors.New("tenant mismatch")
		}
		return tenant, nil
	}
	v := NewVerifier(New(appConn(t, db)), authorize, signer.PublicKey())
	if err := v.AppendEvaluationReceipt(context.Background(), re); err != nil {
		t.Fatal(err)
	}
	if err := v.AppendEvaluationReceipt(context.Background(), re); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if err := v.AppendEvaluationBinding(context.Background(), be); err != nil {
		t.Fatal(err)
	}
	got, err := v.VerifyLegalEvidence(context.Background(), EvidenceRequest{Tenant: tenant.String(), IntentID: "intent-1", ProposalRevisionID: "proposal-1", MaterialDigest: strings.Repeat("d", 64), LegalContextDigest: strings.Repeat("c", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != be.Binding.Digest {
		t.Fatalf("digest=%q", got.Digest)
	}
	fresh := NewVerifier(New(appConn(t, db)), authorize, signer.PublicKey())
	if _, err := fresh.VerifyLegalEvidence(context.Background(), EvidenceRequest{Tenant: tenant.String(), IntentID: "intent-1", ProposalRevisionID: "proposal-1", MaterialDigest: strings.Repeat("d", 64), LegalContextDigest: strings.Repeat("c", 64)}); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_LEGAL_014_Security(t *testing.T) {
	db := newLegalDB(t)
	tenant := insertTenant(t, db, "eval-security")
	re, be, signer := signedEvaluationEvidence(t, tenant)
	authorize := func(_ context.Context, ref string) (uuid.UUID, error) {
		if ref != tenant.String() {
			return uuid.Nil, errors.New("tenant mismatch")
		}
		return tenant, nil
	}
	v := NewVerifier(New(appConn(t, db)), authorize, signer.PublicKey())
	if err := v.AppendEvaluationReceipt(context.Background(), re); err != nil {
		t.Fatal(err)
	}
	bad := NewVerifier(New(appConn(t, db)), authorize, []byte("untrusted"))
	if err := bad.AppendEvaluationReceipt(context.Background(), re); !errors.Is(err, ErrInvalid) {
		t.Fatalf("untrusted=%v", err)
	}
	forged := be
	forged.Binding.IntentID = "intent-forged-obligations"
	forged.Binding.AppliedObligations = []legal.BoundObligation{{Type: legal.ObligationTypeNotice, ID: "unrelated-duty", BodyDigest: strings.Repeat("e", 64)}}
	var err error
	forged.Binding, err = legal.SignEvaluationBinding(forged.Binding, signer)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.AppendEvaluationBinding(context.Background(), forged); err != nil {
		t.Fatalf("append internally inconsistent but authentic binding: %v", err)
	}
	if _, err := v.VerifyLegalEvidence(context.Background(), EvidenceRequest{Tenant: tenant.String(), IntentID: forged.Binding.IntentID, ProposalRevisionID: "proposal-1", MaterialDigest: strings.Repeat("d", 64), LegalContextDigest: strings.Repeat("c", 64)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("forged obligation set verification=%v, want ErrInvalid", err)
	}
	if err := v.AppendEvaluationBinding(context.Background(), be); err != nil {
		t.Fatal(err)
	}
	other := insertTenant(t, db, "eval-security-other")
	inTenantTx(t, appConn(t, db), other, func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM legal_evaluation_receipt WHERE tenant_id=$1`, tenant).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatalf("cross-tenant receipt count=%d, want 0", count)
		}
		return nil
	})
	if err := inTenantTxErr(appConn(t, db), tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE legal_evaluation_receipt SET receipt_digest=$3 WHERE tenant_id=$1 AND receipt_ref=$2`, tenant, re.ReceiptRef, strings.Repeat("f", 64))
		return err
	}); err == nil {
		t.Fatal("immutable receipt accepted tampering update")
	}
	if _, err := v.VerifyLegalEvidence(context.Background(), EvidenceRequest{Tenant: tenant.String(), IntentID: "intent-1", ProposalRevisionID: "proposal-1", MaterialDigest: "wrong", LegalContextDigest: strings.Repeat("c", 64)}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mismatch=%v", err)
	}
	foreign := uuid.New()
	if _, err := v.VerifyLegalEvidence(context.Background(), EvidenceRequest{Tenant: foreign.String(), IntentID: "intent-1", ProposalRevisionID: "proposal-1", MaterialDigest: strings.Repeat("d", 64), LegalContextDigest: strings.Repeat("c", 64)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("caller-selected tenant=%v, want ErrInvalid", err)
	}
	if _, err := v.VerifyLegalEvidence(context.Background(), EvidenceRequest{Tenant: uuid.NewString(), IntentID: "intent-1", ProposalRevisionID: "proposal-1", MaterialDigest: strings.Repeat("d", 64), LegalContextDigest: strings.Repeat("c", 64)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("caller-selected tenant=%v, want ErrInvalid", err)
	}
}

func TestTodo_LEGAL_014_Recovery(t *testing.T) {
	db := newLegalDB(t)
	tenant := insertTenant(t, db, "eval-recovery")
	re, be, signer := signedEvaluationEvidence(t, tenant)
	authorize := func(_ context.Context, ref string) (uuid.UUID, error) {
		if ref != tenant.String() {
			return uuid.Nil, errors.New("tenant mismatch")
		}
		return tenant, nil
	}
	v := NewVerifier(New(appConn(t, db)), authorize, signer.PublicKey())
	v2 := NewVerifier(New(appConn(t, db)), authorize, signer.PublicKey())
	errs := make(chan error, 2)
	go func() { errs <- v.AppendEvaluationReceipt(context.Background(), re) }()
	go func() { errs <- v2.AppendEvaluationReceipt(context.Background(), re) }()
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent exact receipt retry: %v", err)
		}
	}
	conflictingReceipt := re
	conflictingReceipt.ReceiptRef = uuid.NewString()
	if err := v.AppendEvaluationReceipt(context.Background(), conflictingReceipt); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("conflicting receipt duplicate=%v, want ErrDuplicate", err)
	}
	if err := v.AppendEvaluationBinding(context.Background(), be); err != nil {
		t.Fatal(err)
	}
	conflictingBinding := be
	conflictingBinding.Binding.ReceiptRef = uuid.NewString()
	var err error
	conflictingBinding.Binding, err = legal.SignEvaluationBinding(conflictingBinding.Binding, signer)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.AppendEvaluationBinding(context.Background(), conflictingBinding); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("conflicting semantic binding=%v, want ErrDuplicate", err)
	}
	if _, err := v.VerifyLegalEvidence(context.Background(), EvidenceRequest{Tenant: tenant.String(), IntentID: be.Binding.IntentID, ProposalRevisionID: be.Binding.ProposalRevisionID, MaterialDigest: be.Binding.MaterialDigest, LegalContextDigest: be.Binding.LegalContextDigest}); err != nil {
		t.Fatalf("conflicting duplicate rolled back original binding: %v", err)
	}
}
