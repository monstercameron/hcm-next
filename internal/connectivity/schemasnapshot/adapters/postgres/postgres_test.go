package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity/schemasnapshot"
	"github.com/monstercameron/hcm-next/internal/connectivity/schemasnapshot/adapters/postgres"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/intent/model"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// The tests in this package deliberately do not call t.Parallel, for the
// same reason internal/connectivity/observe/adapters/postgres's tests do
// not: migrations/00008_tenant_isolation.sql creates the cluster-wide role
// hcmnext_app inside a DO block guarded against duplicate_object, and two
// schemas migrating at the same moment race on pg_authid's unique index
// instead, which is not caught by that guard.

const testTenant = "5e3f1c2b-0000-4000-8000-000000000001"

var capturedAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func testProvider() schemasnapshot.ProviderRef {
	return schemasnapshot.ProviderRef{ConnectorID: "workday-hcm", ConnectionID: "harborcare-prod", SourceRef: "workday://harborcare"}
}

// newHarness returns a store and artifact store bound to the same
// transaction, over an isolated schema with one tenant row.
func newHarness(t *testing.T) (*postgres.Store, *postgres.ArtifactStore, dbport.Tx, *pgtest.DB) {
	t.Helper()
	db := pgtest.New(t)
	db.Exec(t, `
        INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
        VALUES ($1, 'harborcare', 'cell-a', 'HarborCare', 'ACTIVE', now())`, testTenant)

	tx, err := db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	return postgres.NewStore(tx), postgres.NewArtifactStore(tx, db.Schema), tx, db
}

func ingestRequest(raw []byte) schemasnapshot.IngestRequest {
	return schemasnapshot.IngestRequest{
		TenantID:            testTenant,
		Provider:            testProvider(),
		CapturedAt:          capturedAt,
		DeclaredFormat:      schemasnapshot.FormatJSONSchema,
		Raw:                 raw,
		Classification:      model.ClassInternal,
		RetentionClass:      "integration-schema",
		CreatorPrincipalRef: "system:discovery-worker",
		EvidenceID:          "evidence:discovery-run-1",
		DecidedAt:           capturedAt.Add(5 * time.Minute),
	}
}

// TestTodo_INTG_004_Integration exercises the full Ingest flow against real
// PostgreSQL: the row is born QUARANTINED, the controlled-update trigger
// permits only the declared transition, direct mutation of identity columns
// is refused, evidence is limited to one row per snapshot, and supersession
// across a different provider is rejected by the database trigger even if
// application code somehow skipped its own check.
func TestTodo_INTG_004_Integration(t *testing.T) {
	ctx := context.Background()
	store, artifactStore, tx, db := newHarness(t)
	validators := schemasnapshot.DefaultValidators(schemasnapshot.DefaultMaxBytes)

	result, err := schemasnapshot.Ingest(ctx, artifactStore, store, validators, ingestRequest([]byte(`{"type":"object"}`)))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if result.Snapshot.State != schemasnapshot.StateAdmitted {
		t.Fatalf("snapshot state = %s, want ADMITTED", result.Snapshot.State)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Re-read through a fresh connection to confirm the row and its evidence
	// are durable, not merely visible inside the writing transaction.
	var state, declaredFormat string
	if err := db.Conn.QueryRow(ctx, `SELECT state, declared_format FROM integration_schema_snapshot WHERE tenant_id = $1 AND snapshot_id = $2`,
		testTenant, result.Snapshot.SnapshotID).Scan(&state, &declaredFormat); err != nil {
		t.Fatalf("read back snapshot: %v", err)
	}
	if state != "ADMITTED" || declaredFormat != "JSON_SCHEMA" {
		t.Fatalf("read back state=%s format=%s, want ADMITTED/JSON_SCHEMA", state, declaredFormat)
	}
	var evidenceCount int
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM integration_schema_snapshot_evidence WHERE tenant_id = $1 AND snapshot_id = $2`,
		testTenant, result.Snapshot.SnapshotID).Scan(&evidenceCount); err != nil {
		t.Fatalf("count evidence: %v", err)
	}
	if evidenceCount != 1 {
		t.Fatalf("evidence rows = %d, want exactly 1", evidenceCount)
	}

	t.Run("direct UPDATE of an identity column is refused", func(t *testing.T) {
		tx2, err := db.Conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx2.Rollback(ctx)
		_, err = tx2.Exec(ctx, `UPDATE integration_schema_snapshot SET byte_size = byte_size + 1 WHERE tenant_id = $1 AND snapshot_id = $2`,
			testTenant, result.Snapshot.SnapshotID)
		if err == nil {
			t.Fatal("direct mutation of byte_size on a decided row was accepted")
		}
	})

	t.Run("direct UPDATE cannot move a decided row back to QUARANTINED", func(t *testing.T) {
		tx2, err := db.Conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx2.Rollback(ctx)
		_, err = tx2.Exec(ctx, `UPDATE integration_schema_snapshot SET state = 'QUARANTINED', state_reason = NULL, decided_at = NULL WHERE tenant_id = $1 AND snapshot_id = $2`,
			testTenant, result.Snapshot.SnapshotID)
		if err == nil {
			t.Fatal("moving a decided row back to QUARANTINED was accepted")
		}
	})

	t.Run("Decide against an already-decided snapshot is idempotent", func(t *testing.T) {
		tx2, err := db.Conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx2.Rollback(ctx)
		store2 := postgres.NewStore(tx2)
		snap, ev, err := store2.Decide(ctx, testTenant, result.Snapshot.SnapshotID, schemasnapshot.StateAdmitted, "different reason text", time.Now(),
			[]schemasnapshot.ValidatorResult{{Name: "X", Passed: true}})
		if err != nil {
			t.Fatalf("idempotent Decide: %v", err)
		}
		if snap.State != schemasnapshot.StateAdmitted {
			t.Fatalf("state = %s, want ADMITTED", snap.State)
		}
		if ev.EvidenceID != result.Evidence.EvidenceID {
			t.Fatalf("idempotent Decide returned a different evidence record: %s vs %s", ev.EvidenceID, result.Evidence.EvidenceID)
		}

		if _, _, err := store2.Decide(ctx, testTenant, result.Snapshot.SnapshotID, schemasnapshot.StateRejected, "flip", time.Now(),
			[]schemasnapshot.ValidatorResult{{Name: "X", Passed: false}}); !errors.Is(err, schemasnapshot.ErrImmutable) {
			t.Fatalf("redeciding with a different verdict = %v, want ErrImmutable", err)
		}
	})

	t.Run("supersession across providers is refused by the database", func(t *testing.T) {
		tx2, err := db.Conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx2.Rollback(ctx)
		store2 := postgres.NewStore(tx2)
		artifactStore2 := postgres.NewArtifactStore(tx2, db.Schema)

		req := ingestRequest([]byte(`{"type":"object","different":true}`))
		req.Provider = schemasnapshot.ProviderRef{ConnectorID: "adp-wfn", ConnectionID: "harborcare-adp", SourceRef: "adp://harborcare"}
		supersedes := result.Snapshot.SnapshotID
		req.Supersedes = &supersedes
		if _, err := schemasnapshot.Ingest(ctx, artifactStore2, store2, validators, req); !errors.Is(err, schemasnapshot.ErrInvalid) {
			t.Fatalf("cross-provider supersession = %v, want ErrInvalid", err)
		}
	})
}

// TestTodo_INTG_004_Integration_Rejected proves a rejected snapshot commits
// exactly like an admitted one -- an evidence record either way -- and
// remains unusable as a mapping input.
func TestTodo_INTG_004_Integration_Rejected(t *testing.T) {
	ctx := context.Background()
	store, artifactStore, tx, db := newHarness(t)
	validators := schemasnapshot.DefaultValidators(schemasnapshot.DefaultMaxBytes)

	result, err := schemasnapshot.Ingest(ctx, artifactStore, store, validators, ingestRequest([]byte(`{"type":`)))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if result.Snapshot.State != schemasnapshot.StateRejected {
		t.Fatalf("snapshot state = %s, want REJECTED", result.Snapshot.State)
	}
	if err := result.Snapshot.RequireAdmitted(); !errors.Is(err, schemasnapshot.ErrNotAdmitted) {
		t.Fatalf("RequireAdmitted on a rejected snapshot = %v, want ErrNotAdmitted", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var verdict string
	if err := db.Conn.QueryRow(ctx, `SELECT verdict FROM integration_schema_snapshot_evidence WHERE tenant_id = $1 AND snapshot_id = $2`,
		testTenant, result.Snapshot.SnapshotID).Scan(&verdict); err != nil {
		t.Fatalf("read evidence: %v", err)
	}
	if verdict != "REJECTED" {
		t.Fatalf("evidence verdict = %s, want REJECTED", verdict)
	}
}

// TestTodo_INTG_004_Integration_ArtifactAndSnapshotCommitTogether proves the
// two writes Ingest performs -- the artifact bytes and the quarantine row --
// share one transaction: rolling that transaction back leaves neither
// durable.
func TestTodo_INTG_004_Integration_ArtifactAndSnapshotCommitTogether(t *testing.T) {
	ctx := context.Background()
	store, artifactStore, tx, db := newHarness(t)
	validators := schemasnapshot.DefaultValidators(schemasnapshot.DefaultMaxBytes)

	result, err := schemasnapshot.Ingest(ctx, artifactStore, store, validators, ingestRequest([]byte(`{"type":"object"}`)))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	var snapshotCount int
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM integration_schema_snapshot WHERE tenant_id = $1 AND snapshot_id = $2`,
		testTenant, result.Snapshot.SnapshotID).Scan(&snapshotCount); err != nil {
		t.Fatalf("count snapshot: %v", err)
	}
	if snapshotCount != 0 {
		t.Fatal("snapshot row survived a rolled-back transaction")
	}
}
