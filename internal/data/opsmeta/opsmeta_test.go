package opsmeta_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/opsmeta"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var fixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, "tenant "+key)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func digestOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func newIncident(tenant uuid.UUID) opsmeta.OperationalIncident {
	return opsmeta.OperationalIncident{
		TenantID:       tenant,
		IncidentID:     uuid.New(),
		IncidentKey:    "inc:" + uuid.NewString(),
		Severity:       "SEV2",
		ImpactRevision: 1,
		Scope:          json.RawMessage(`{"cell":"local","service":"payroll"}`),
		CorrelationKey: "corr:" + uuid.NewString(),
		EvidenceDigest: digestOf("incident-" + uuid.NewString()),
		DeclaredAt:     fixedInstant,
		Status:         "OPEN",
	}
}

func newBackup(tenant uuid.UUID) opsmeta.BackupRun {
	return opsmeta.BackupRun{
		TenantID:           tenant,
		RunID:              uuid.New(),
		PolicyKey:          "policy:" + uuid.NewString(),
		Plane:              "DATA",
		StoreRef:           "store:primary",
		BackupMode:         "FULL",
		PointInTime:        fixedInstant,
		Watermark:          "lsn-1",
		LocationRef:        "s3://vault/" + uuid.NewString(),
		KeyVersion:         1,
		ManifestDigest:     digestOf("manifest-" + uuid.NewString()),
		StartedAt:          fixedInstant,
		VerificationResult: "PENDING",
		Status:             "RUNNING",
	}
}

// TestTodo_DB_015_Operations is DB-015's operations-side test. The matrix
// names live in internal/data/recordsmeta (the lane's primary package for
// DB-015); this suite proves the same clauses for the three operations
// tables migration 00032 creates.
func TestTodo_DB_015_Operations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "db015-operations")
	other := insertTenant(t, db, "db015-operations-other")

	t.Run("the operations table set is exactly what 00032 declares", func(t *testing.T) {
		want := []string{"backup_run", "operational_incident", "recovery_run"}
		if !slices.Equal(opsmeta.OperationsTables, want) {
			t.Fatalf("OperationsTables=%v, want %v", opsmeta.OperationsTables, want)
		}
		for _, table := range want {
			var found string
			if err := db.Conn.QueryRow(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=$1`, table).Scan(&found); err != nil {
				t.Errorf("table %s missing from the live schema: %v", table, err)
			}
		}
	})

	t.Run("an incident keeps its scope, revision and evidence lineage", func(t *testing.T) {
		incident := newIncident(tenant)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return opsmeta.InsertOperationalIncident(ctx, tx, incident)
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			loaded, err := opsmeta.LoadOperationalIncident(ctx, tx, tenant, incident.IncidentID)
			if err != nil {
				return err
			}
			if loaded.EvidenceDigest == "" || loaded.CorrelationKey == "" || loaded.ImpactRevision != 1 {
				return fmt.Errorf("incident %+v lost its evidence, correlation or impact revision", loaded)
			}
			return nil
		})
		noEvidence := newIncident(tenant)
		noEvidence.EvidenceDigest = ""
		if err := noEvidence.Validate(); !errors.Is(err, opsmeta.ErrMissingLineage) {
			t.Errorf("an incident with no evidence digest validated as %v", err)
		}
		duplicate := newIncident(tenant)
		duplicate.IncidentKey = incident.IncidentKey
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return opsmeta.InsertOperationalIncident(ctx, tx, duplicate)
		}); err == nil {
			t.Fatal("two incidents claimed the same incident key")
		}
	})

	t.Run("an incident resolves only after containment and closes only with a postmortem", func(t *testing.T) {
		resolved := fixedInstant.Add(time.Hour)
		uncontained := newIncident(tenant)
		uncontained.ResolvedAt = &resolved
		uncontained.Status = "RESOLVED"
		if err := uncontained.Validate(); !errors.Is(err, opsmeta.ErrInvalidInterval) {
			t.Errorf("an incident resolved without containment validated as %v", err)
		}
		contained := fixedInstant.Add(30 * time.Minute)
		noPostmortem := newIncident(tenant)
		noPostmortem.ContainedAt = &contained
		noPostmortem.ResolvedAt = &resolved
		noPostmortem.Status = "CLOSED"
		if err := noPostmortem.Validate(); !errors.Is(err, opsmeta.ErrMissingLineage) {
			t.Errorf("an incident closed with no postmortem validated as %v", err)
		}
		if err := db.ExecErr(`INSERT INTO operational_incident (tenant_id, incident_id, incident_key, severity, impact_revision, correlation_key, evidence_digest, declared_at, contained_at, resolved_at, status) VALUES ($1,$2,$3,'SEV2',1,'c',$4,now(),now(),now(),'CLOSED')`,
			tenant, uuid.New(), "inc:"+uuid.NewString(), digestOf("e")); err == nil {
			t.Fatal("the schema closed an incident with no postmortem reference")
		}
	})

	t.Run("a completed backup is immutable for a stated window", func(t *testing.T) {
		backup := newBackup(tenant)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return opsmeta.InsertBackupRun(ctx, tx, backup)
		})
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return opsmeta.CompleteBackupRun(ctx, tx, tenant, backup.RunID, fixedInstant.Add(time.Hour), fixedInstant, "PASS")
		}); !errors.Is(err, opsmeta.ErrMutableBackup) {
			t.Fatalf("a backup completed with a horizon in the past: %v", err)
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return opsmeta.CompleteBackupRun(ctx, tx, tenant, backup.RunID, fixedInstant.Add(time.Hour), fixedInstant.Add(720*time.Hour), "PASS")
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			loaded, err := opsmeta.LoadBackupRun(ctx, tx, tenant, backup.RunID)
			if err != nil {
				return err
			}
			if loaded.Status != "COMPLETED" || loaded.ImmutableUntil == nil || loaded.ManifestDigest == "" {
				return fmt.Errorf("backup %+v did not record its immutability horizon or manifest", loaded)
			}
			return nil
		})
		// A second completion finds nothing RUNNING to complete.
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return opsmeta.CompleteBackupRun(ctx, tx, tenant, backup.RunID, fixedInstant.Add(2*time.Hour), fixedInstant.Add(800*time.Hour), "PASS")
		}); !errors.Is(err, dbport.ErrNoRows) {
			t.Fatalf("a completed backup was completed twice: %v", err)
		}
	})

	t.Run("one backup covers one store at one point in time", func(t *testing.T) {
		first := newBackup(tenant)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return opsmeta.InsertBackupRun(ctx, tx, first)
		})
		duplicate := newBackup(tenant)
		duplicate.PolicyKey = first.PolicyKey
		duplicate.StoreRef = first.StoreRef
		duplicate.PointInTime = first.PointInTime
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return opsmeta.InsertBackupRun(ctx, tx, duplicate)
		}); err == nil {
			t.Fatal("the same point-in-time backup was recorded twice")
		}
	})

	t.Run("a REBUILD names no backup and a RESTORE always does", func(t *testing.T) {
		rebuild := opsmeta.RecoveryRun{
			TenantID: tenant, RunID: uuid.New(), ScenarioKey: "scenario:projection-loss",
			PlanRef: "plan:rebuild", RecoveryMode: "REBUILD", IsolatedEnvironment: "cell-local",
			TargetRPOSeconds: 0, TargetRTOSeconds: 1800, ValidationResult: "PENDING",
			StartedAt: fixedInstant, Status: "RUNNING",
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return opsmeta.InsertRecoveryRun(ctx, tx, rebuild)
		})
		restore := rebuild
		restore.RunID = uuid.New()
		restore.RecoveryMode = "RESTORE"
		if err := restore.Validate(); !errors.Is(err, opsmeta.ErrMissingLineage) {
			t.Fatalf("a RESTORE naming no backup validated as %v", err)
		}
	})

	t.Run("a recovery run reports the actuals it achieved", func(t *testing.T) {
		backup := newBackup(tenant)
		run := opsmeta.RecoveryRun{
			TenantID: tenant, RunID: uuid.New(), ScenarioKey: "scenario:cell-loss",
			PlanRef: "plan:restore", BackupRunID: &backup.RunID, RecoveryMode: "RESTORE",
			IsolatedEnvironment: "recovery-cell", TargetRPOSeconds: 900, TargetRTOSeconds: 3600,
			ValidationResult: "PENDING", StartedAt: fixedInstant, Status: "RUNNING",
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			if err := opsmeta.InsertBackupRun(ctx, tx, backup); err != nil {
				return err
			}
			return opsmeta.InsertRecoveryRun(ctx, tx, run)
		})
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return opsmeta.CompleteRecoveryRun(ctx, tx, tenant, run.RunID, fixedInstant.Add(time.Hour), 600, 3000, "UNKNOWN")
		}); !errors.Is(err, opsmeta.ErrUnvalidatedCompletion) {
			t.Fatalf("an UNKNOWN validation closed a recovery run: %v", err)
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return opsmeta.CompleteRecoveryRun(ctx, tx, tenant, run.RunID, fixedInstant.Add(time.Hour), 600, 3000, "PARTIAL")
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			loaded, err := opsmeta.LoadRecoveryRun(ctx, tx, tenant, run.RunID)
			if err != nil {
				return err
			}
			if loaded.ActualRTOSeconds == nil || *loaded.ActualRTOSeconds != 3000 || loaded.ValidationResult != "PARTIAL" {
				return fmt.Errorf("recovery run %+v lost its actuals or validation", loaded)
			}
			return nil
		})
	})

	t.Run("operational telemetry stays out of these tables", func(t *testing.T) {
		// DB-015 REFACTOR: no metric series, log line or span column. The
		// only large-payload columns are the jsonb scope objects.
		for _, table := range opsmeta.OperationsTables {
			var byteColumns int
			if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND data_type IN ('bytea')`, table).Scan(&byteColumns); err != nil {
				t.Fatalf("inspect %s: %v", table, err)
			}
			if byteColumns != 0 {
				t.Errorf("table %s holds %d byte-bearing columns, want 0", table, byteColumns)
			}
		}
	})

	t.Run("one tenant cannot reach another tenant's runs", func(t *testing.T) {
		backup := newBackup(tenant)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return opsmeta.InsertBackupRun(ctx, tx, backup)
		})
		if err := inTenantTxErr(conn, other, func(tx dbport.Tx) error {
			_, err := opsmeta.LoadBackupRun(ctx, tx, tenant, backup.RunID)
			return err
		}); !errors.Is(err, dbport.ErrNoRows) {
			t.Fatalf("another tenant read this tenant's backup: %v", err)
		}
		crossTenant := opsmeta.RecoveryRun{
			TenantID: other, RunID: uuid.New(), ScenarioKey: "s", PlanRef: "p",
			BackupRunID: &backup.RunID, RecoveryMode: "RESTORE", IsolatedEnvironment: "env",
			TargetRPOSeconds: 1, TargetRTOSeconds: 1, ValidationResult: "PENDING",
			StartedAt: fixedInstant, Status: "RUNNING",
		}
		if err := inTenantTxErr(conn, other, func(tx dbport.Tx) error {
			return opsmeta.InsertRecoveryRun(ctx, tx, crossTenant)
		}); err == nil {
			t.Fatal("another tenant restored from this tenant's backup")
		}
	})
}
