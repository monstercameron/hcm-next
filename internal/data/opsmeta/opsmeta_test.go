package opsmeta_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
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

func TestTodo_OBS_007_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "obs007-integration")
	otherTenant := insertTenant(t, db, "obs007-integration-other")
	a := opsmeta.AlertIncident{TenantID: tenant, IncidentID: uuid.New(), IncidentKey: "alert:queue.lag:2:fp-1", Severity: "SEV2", Scope: json.RawMessage(`{"service":"workflow","nested":{"kept":true}}`), CorrelationKey: "corr-obs007", EvidenceDigest: digestOf("alert"), DeclaredAt: fixedInstant, PrimaryOwner: "on-call", SecondaryRoute: "incident-review", StormLimit: 2, StormWindow: time.Hour}
	var first opsmeta.AlertIncidentResult
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { var err error; first, err = opsmeta.RouteAlert(ctx, tx, a); return err })
	if !first.Created {
		t.Fatal("first dedupe insert was not created")
	}
	var second opsmeta.AlertIncidentResult
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		replay := a
		replay.IncidentID = uuid.New()
		replay.Scope = json.RawMessage(`{"nested":{"kept":true},"service":"workflow"}`)
		second, err = opsmeta.RouteAlert(ctx, tx, replay)
		return err
	})
	if second.Created || second.Incident.IncidentID != first.Incident.IncidentID {
		t.Fatalf("dedupe result=%+v", second)
	}
	conflict := a
	conflict.IncidentID = uuid.New()
	conflict.EvidenceDigest = digestOf("different alert")
	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := opsmeta.RouteAlert(ctx, tx, conflict)
		return err
	}); !errors.Is(err, opsmeta.ErrAlertConflict) {
		t.Fatalf("mismatched replay err=%v", err)
	}
	scopeConflict := a
	scopeConflict.IncidentID = uuid.New()
	scopeConflict.Scope = json.RawMessage(`{"service":"workflow","nested":{"kept":false}}`)
	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := opsmeta.RouteAlert(ctx, tx, scopeConflict)
		return err
	}); !errors.Is(err, opsmeta.ErrAlertConflict) {
		t.Fatalf("changed affected scope replay err=%v", err)
	}
	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		return opsmeta.AcknowledgeOperationalIncident(ctx, tx, tenant, first.Incident.IncidentID, "secondary", digestOf("forged"), fixedInstant.Add(time.Minute))
	}); !errors.Is(err, opsmeta.ErrAcknowledgementUnauthorized) {
		t.Fatalf("unassigned acknowledgement err=%v", err)
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		return opsmeta.AcknowledgeOperationalIncident(ctx, tx, tenant, first.Incident.IncidentID, "on-call", digestOf("ack-auth"), fixedInstant.Add(time.Minute))
	})
	var loaded opsmeta.OperationalIncident
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		loaded, err = opsmeta.LoadOperationalIncident(ctx, tx, tenant, first.Incident.IncidentID)
		return err
	})
	if !strings.Contains(string(loaded.Scope), `"acknowledged": true`) {
		t.Fatalf("ack not durable: %s", loaded.Scope)
	}
	var scope map[string]any
	if err := json.Unmarshal(loaded.Scope, &scope); err != nil {
		t.Fatalf("decode acknowledged scope: %v", err)
	}
	nested, nestedOK := scope["nested"].(map[string]any)
	if !nestedOK || nested["kept"] != true || scope["acknowledgement_evidence"] == "" {
		t.Fatalf("scope semantics or acknowledgement provenance lost: %s", loaded.Scope)
	}
	if strings.Contains(opsmeta.CustomerSafeEvidence(loaded), tenant.String()) || strings.Contains(opsmeta.CustomerSafeEvidence(loaded), "workflow") {
		t.Fatal("customer evidence leaked tenant or scope")
	}

	otherAlert := a
	otherAlert.IncidentID = uuid.New()
	otherAlert.IncidentKey += ":other"
	otherAlert.CorrelationKey += ":other"
	otherAlert.EvidenceDigest = digestOf("other")
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := opsmeta.RouteAlert(ctx, tx, otherAlert)
		return err
	})
	storm := otherAlert
	storm.IncidentID = uuid.New()
	storm.IncidentKey += ":storm"
	storm.CorrelationKey += ":storm"
	storm.EvidenceDigest = digestOf("storm")
	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := opsmeta.RouteAlert(ctx, tx, storm)
		return err
	}); !errors.Is(err, opsmeta.ErrAlertStorm) {
		t.Fatalf("durable storm admission err=%v", err)
	}

	otherTenantAlert := a
	otherTenantAlert.TenantID = otherTenant
	otherTenantAlert.IncidentID = uuid.New()
	inTenantTx(t, conn, otherTenant, func(tx dbport.Tx) error {
		result, err := opsmeta.RouteAlert(ctx, tx, otherTenantAlert)
		if err == nil && !result.Created {
			return errors.New("cross-tenant dedupe suppressed a distinct incident")
		}
		return err
	})
}

var errAlertRead = errors.New("forced alert read failure")

type faultAlertRow struct{}

func (faultAlertRow) Scan(...any) error { return errAlertRead }

type faultAlertTx struct{}

func (faultAlertTx) Exec(context.Context, string, ...any) (int64, error) { return 1, nil }
func (faultAlertTx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected Query")
}
func (faultAlertTx) QueryRow(context.Context, string, ...any) dbport.Row { return faultAlertRow{} }
func (faultAlertTx) Commit(context.Context) error                        { return nil }
func (faultAlertTx) Rollback(context.Context) error                      { return nil }

type scriptedAlertTx struct {
	execs int
	reads int
}

func (tx *scriptedAlertTx) Exec(context.Context, string, ...any) (int64, error) {
	tx.execs++
	if tx.execs == 3 {
		return 0, nil
	}
	return 1, nil
}
func (*scriptedAlertTx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected Query")
}
func (tx *scriptedAlertTx) QueryRow(context.Context, string, ...any) dbport.Row {
	tx.reads++
	switch tx.reads {
	case 1:
		return staticAlertRow{err: dbport.ErrNoRows}
	case 2:
		return staticAlertRow{count: true}
	default:
		return staticAlertRow{err: errAlertRead}
	}
}
func (*scriptedAlertTx) Commit(context.Context) error   { return nil }
func (*scriptedAlertTx) Rollback(context.Context) error { return nil }

type staticAlertRow struct {
	err   error
	count bool
}

func (row staticAlertRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if row.count {
		*(dest[0].(*int)) = 0
	}
	return nil
}

func TestTodo_OBS_007_Integration_ReadFault(t *testing.T) {
	a := opsmeta.AlertIncident{
		TenantID: uuid.New(), IncidentID: uuid.New(), IncidentKey: "alert:fault", Severity: "SEV2",
		Scope: json.RawMessage(`{"service":"workflow"}`), CorrelationKey: "corr:fault", EvidenceDigest: digestOf("fault"),
		DeclaredAt: fixedInstant, PrimaryOwner: "on-call", SecondaryRoute: "fallback", StormLimit: 2, StormWindow: time.Hour,
	}
	result, err := opsmeta.RouteAlert(context.Background(), faultAlertTx{}, a)
	if !errors.Is(err, errAlertRead) || result.Created || result.Incident.IncidentID != uuid.Nil {
		t.Fatalf("read fault result=%+v err=%v", result, err)
	}
	tx := &scriptedAlertTx{}
	result, err = opsmeta.RouteAlert(context.Background(), tx, a)
	if !errors.Is(err, errAlertRead) || result.Created || result.Incident.IncidentID != uuid.Nil {
		t.Fatalf("conflict reload fault result=%+v err=%v", result, err)
	}
}

func TestTodo_OBS_007_Integration_StoreEnforcement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "obs007-store")
	other := insertTenant(t, db, "obs007-store-other")
	base := opsmeta.AlertIncident{TenantID: tenant, IncidentID: uuid.New(), IncidentKey: "alert:a", Severity: "SEV2", Scope: json.RawMessage(`{"service":"workflow"}`), CorrelationKey: "corr-a", EvidenceDigest: digestOf("a"), DeclaredAt: fixedInstant, PrimaryOwner: "primary", SecondaryRoute: "fallback", StormLimit: 1, StormWindow: time.Hour}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error { _, err := opsmeta.RouteAlert(ctx, tx, base); return err })

	conflict := base
	conflict.IncidentID = uuid.New()
	conflict.EvidenceDigest = digestOf("different")
	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error { _, err := opsmeta.RouteAlert(ctx, tx, conflict); return err }); !errors.Is(err, opsmeta.ErrAlertConflict) {
		t.Fatalf("mismatched replay err=%v", err)
	}
	second := base
	second.IncidentID, second.IncidentKey, second.CorrelationKey, second.EvidenceDigest = uuid.New(), "alert:b", "corr-b", digestOf("b")
	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error { _, err := opsmeta.RouteAlert(ctx, tx, second); return err }); !errors.Is(err, opsmeta.ErrAlertStorm) {
		t.Fatalf("durable storm admission err=%v", err)
	}
	otherAlert := base
	otherAlert.TenantID, otherAlert.IncidentID = other, uuid.New()
	inTenantTx(t, conn, other, func(tx dbport.Tx) error { _, err := opsmeta.RouteAlert(ctx, tx, otherAlert); return err })
	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		return opsmeta.AcknowledgeOperationalIncident(ctx, tx, tenant, base.IncidentID, "fallback", digestOf("forged"), fixedInstant.Add(time.Minute))
	}); !errors.Is(err, opsmeta.ErrAcknowledgementUnauthorized) {
		t.Fatalf("fallback acknowledged without primary authority: %v", err)
	}
}

func TestTodo_OBS_007_Integration_Contention(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "obs007-contention")
	other := insertTenant(t, db, "obs007-contention-other")
	const attempts = 8
	const limit = 2

	type outcome struct {
		result opsmeta.AlertIncidentResult
		err    error
	}
	routeConcurrent := func(tenantID uuid.UUID, prefix string, sameKey bool) []outcome {
		t.Helper()
		outcomes := make([]outcome, attempts)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for index := range attempts {
			wg.Add(1)
			go func() {
				defer wg.Done()
				conn := appConn(t, db)
				keyIndex := index
				if sameKey {
					keyIndex = 0
				}
				a := opsmeta.AlertIncident{
					TenantID: tenantID, IncidentID: uuid.New(), IncidentKey: fmt.Sprintf("alert:%s:%d", prefix, keyIndex),
					Severity: "SEV2", Scope: json.RawMessage(`{"service":"workflow"}`),
					CorrelationKey: fmt.Sprintf("corr:%s:%d", prefix, keyIndex), EvidenceDigest: digestOf(fmt.Sprintf("%s:%d", prefix, keyIndex)),
					DeclaredAt: fixedInstant, PrimaryOwner: "on-call", SecondaryRoute: "fallback", StormLimit: limit, StormWindow: time.Hour,
				}
				<-start
				outcomes[index].err = inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
					var err error
					outcomes[index].result, err = opsmeta.RouteAlert(ctx, tx, a)
					return err
				})
			}()
		}
		close(start)
		wg.Wait()
		return outcomes
	}
	countRows := func(tenantID uuid.UUID) int {
		t.Helper()
		conn := appConn(t, db)
		var count int
		inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM operational_incident WHERE tenant_id=$1`, tenantID).Scan(&count)
		})
		return count
	}

	distinct := routeConcurrent(tenant, "distinct", false)
	created, refused := 0, 0
	for _, got := range distinct {
		switch {
		case got.err == nil && got.result.Created:
			created++
		case errors.Is(got.err, opsmeta.ErrAlertStorm):
			refused++
		default:
			t.Fatalf("distinct contention outcome=%+v", got)
		}
	}
	if created != limit || refused != attempts-limit || countRows(tenant) != limit {
		t.Fatalf("distinct contention created=%d refused=%d rows=%d", created, refused, countRows(tenant))
	}

	// A separate tenant has an independent durable limit even while using the
	// same occurrence time and alert-key pattern.
	otherResults := routeConcurrent(other, "distinct", false)
	otherCreated := 0
	for _, got := range otherResults {
		if got.err == nil && got.result.Created {
			otherCreated++
		} else if !errors.Is(got.err, opsmeta.ErrAlertStorm) {
			t.Fatalf("other-tenant contention outcome=%+v", got)
		}
	}
	if otherCreated != limit || countRows(other) != limit || countRows(tenant) != limit {
		t.Fatalf("tenant-independent admission other_created=%d other_rows=%d first_rows=%d", otherCreated, countRows(other), countRows(tenant))
	}

	// Use a fresh tenant because prior distinct alerts intentionally consumed
	// its admission budget. Exact concurrent replays share one durable row and
	// all return its ID rather than consuming the limit.
	dedupeTenant := insertTenant(t, db, "obs007-contention-dedupe")
	replays := routeConcurrent(dedupeTenant, "same", true)
	var incidentID uuid.UUID
	replayCreated := 0
	for _, got := range replays {
		if got.err != nil {
			t.Fatalf("exact concurrent replay err=%v", got.err)
		}
		if got.result.Created {
			replayCreated++
		}
		if incidentID == uuid.Nil {
			incidentID = got.result.Incident.IncidentID
		} else if got.result.Incident.IncidentID != incidentID {
			t.Fatalf("exact replays returned different incident ids: %s and %s", incidentID, got.result.Incident.IncidentID)
		}
	}
	if replayCreated != 1 || countRows(dedupeTenant) != 1 {
		t.Fatalf("exact replay created=%d rows=%d", replayCreated, countRows(dedupeTenant))
	}
}

func TestTodo_OBS_007_AcknowledgementContention(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "obs007-ack-contention")
	setupConn := appConn(t, db)
	newAlert := func(key string) opsmeta.AlertIncident {
		return opsmeta.AlertIncident{
			TenantID: tenant, IncidentID: uuid.New(), IncidentKey: key, Severity: "SEV2",
			Scope: json.RawMessage(`{"service":"workflow","nested":{"kept":true},"exact":9007199254740993}`), CorrelationKey: key + ":corr", EvidenceDigest: digestOf(key),
			DeclaredAt: fixedInstant.Add(123456789 * time.Nanosecond), PrimaryOwner: "on-call", SecondaryRoute: "fallback", StormLimit: 10, StormWindow: time.Hour,
		}
	}
	first := newAlert("alert:ack-same")
	inTenantTx(t, setupConn, tenant, func(tx dbport.Tx) error { _, err := opsmeta.RouteAlert(ctx, tx, first); return err })

	runConcurrent := func(incident uuid.UUID, evidence []string, times []time.Time) []error {
		t.Helper()
		connections := make([]*pgxadapter.Conn, len(evidence))
		for i := range connections {
			connections[i] = appConn(t, db)
		}
		errs := make([]error, len(evidence))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range evidence {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs[i] = inTenantTxErr(connections[i], tenant, func(tx dbport.Tx) error {
					return opsmeta.AcknowledgeOperationalIncident(ctx, tx, tenant, incident, "on-call", evidence[i], times[i])
				})
			}()
		}
		close(start)
		wg.Wait()
		return errs
	}

	ackTime := fixedInstant.Add(time.Minute)
	receipt := digestOf("same-receipt")
	for index, err := range runConcurrent(first.IncidentID, []string{receipt, receipt}, []time.Time{ackTime, ackTime}) {
		if err != nil {
			t.Fatalf("exact acknowledgement retry %d: %v", index, err)
		}
	}
	var loaded opsmeta.OperationalIncident
	inTenantTx(t, setupConn, tenant, func(tx dbport.Tx) error {
		var err error
		loaded, err = opsmeta.LoadOperationalIncident(ctx, tx, tenant, first.IncidentID)
		return err
	})
	if loaded.ImpactRevision != 2 || !strings.Contains(string(loaded.Scope), `"kept": true`) || !strings.Contains(string(loaded.Scope), `9007199254740993`) {
		t.Fatalf("idempotent acknowledgement revision/scope=%d %s", loaded.ImpactRevision, loaded.Scope)
	}

	second := newAlert("alert:ack-conflict")
	inTenantTx(t, setupConn, tenant, func(tx dbport.Tx) error { _, err := opsmeta.RouteAlert(ctx, tx, second); return err })
	conflicting := runConcurrent(second.IncidentID, []string{digestOf("receipt-a"), digestOf("receipt-b")}, []time.Time{ackTime, ackTime.Add(time.Second)})
	succeeded, rejected := 0, 0
	for _, err := range conflicting {
		if err == nil {
			succeeded++
		} else if errors.Is(err, opsmeta.ErrAcknowledgementConflict) {
			rejected++
		} else {
			t.Fatalf("conflicting acknowledgement err=%v", err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("conflicting acknowledgements succeeded=%d rejected=%d", succeeded, rejected)
	}
	inTenantTx(t, setupConn, tenant, func(tx dbport.Tx) error {
		var err error
		loaded, err = opsmeta.LoadOperationalIncident(ctx, tx, tenant, second.IncidentID)
		return err
	})
	if loaded.ImpactRevision != 2 || !strings.Contains(string(loaded.Scope), `"kept": true`) || !strings.Contains(string(loaded.Scope), `9007199254740993`) {
		t.Fatalf("conflicting acknowledgement revision/scope=%d %s", loaded.ImpactRevision, loaded.Scope)
	}
}
