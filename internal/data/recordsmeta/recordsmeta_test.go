package recordsmeta_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/assurancemeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/opsmeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/privacymeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/recordsmeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// migrationFile is the migration DB-015's tables come from.
const migrationFile = "00032_privacy_records_operations_assurance.sql"

var fixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

// appendOnlyTables carry migration 00032's forbid_mutation trigger.
var appendOnlyTables = []string{
	"audit_package",
	"control_evidence",
	"data_copy_inventory",
	"slo_observation",
}

// allTables is every base table migration 00032 creates, across the four
// store packages that own them.
func allTables() []string {
	out := slices.Clone(privacymeta.PrivacyTables)
	out = append(out, recordsmeta.RecordsTables...)
	out = append(out, opsmeta.OperationsTables...)
	out = append(out, assurancemeta.AssuranceTables...)
	sort.Strings(out)
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root")
		}
		dir = parent
	}
}

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

// inTenantTxCommitting runs fn and commits whatever it wrote even when it
// returns an error, then hands the error back. ExecuteDisposition records a
// BLOCKED marker before returning its refusal; whether that marker is kept is
// the caller's decision, and this helper is the caller that keeps it.
func inTenantTxCommitting(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("set tenant: %v", err)
	}
	fnErr := fn(tx)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return fnErr
}

func digestOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// fixtures
// ---------------------------------------------------------------------------

func newDeclaration(tenant uuid.UUID) recordsmeta.RecordDeclaration {
	cutoff := fixedInstant
	eligible := fixedInstant.Add(24 * time.Hour)
	return recordsmeta.RecordDeclaration{
		TenantID:                 tenant,
		DeclarationID:            uuid.New(),
		SubjectEntityRef:         "person:" + uuid.NewString(),
		ArtifactRef:              "artifact://doc/" + uuid.NewString(),
		RecordSeries:             "HR-100",
		RecordClass:              "PERSONNEL",
		OwnerRef:                 "principal:hr",
		CustodianRef:             "principal:records",
		CutoffTrigger:            "TERMINATION",
		CutoffAt:                 &cutoff,
		RetentionScheduleKey:     "schedule:hr-100",
		RetentionScheduleVersion: 3,
		DispositionEligibleAt:    &eligible,
		Status:                   "ELIGIBLE",
		CreatedAt:                fixedInstant,
	}
}

func newHold(tenant uuid.UUID) recordsmeta.LegalHold {
	return recordsmeta.LegalHold{
		TenantID:       tenant,
		HoldID:         uuid.New(),
		MatterRef:      "matter:" + uuid.NewString(),
		AuthorityRef:   "principal:legal",
		HoldVersion:    1,
		Reason:         "pending litigation",
		ScopePredicate: json.RawMessage(`{"record_series":"HR-100"}`),
		ScopeDigest:    digestOf("scope-" + uuid.NewString()),
		PlacedBy:       "principal:legal",
		PlacedAt:       fixedInstant,
		Status:         "ACTIVE",
	}
}

func newIntersection(tenant, holdID, declarationID uuid.UUID) recordsmeta.HoldIntersection {
	return recordsmeta.HoldIntersection{
		TenantID:       tenant,
		IntersectionID: uuid.New(),
		HoldID:         holdID,
		DeclarationID:  declarationID,
		MatchedReason:  "record_series matched the hold predicate",
		MatchedAt:      fixedInstant,
		State:          "ACTIVE",
	}
}

func newDisposition(tenant, declarationID uuid.UUID) recordsmeta.RetentionDisposition {
	return recordsmeta.RetentionDisposition{
		TenantID:                 tenant,
		DispositionID:            uuid.New(),
		DeclarationID:            declarationID,
		Action:                   "DESTROY",
		RetentionScheduleKey:     "schedule:hr-100",
		RetentionScheduleVersion: 3,
		DueAt:                    fixedInstant.Add(24 * time.Hour),
		VerificationResult:       "PENDING",
		Status:                   "PLANNED",
		CreatedAt:                fixedInstant,
	}
}

// ---------------------------------------------------------------------------
// TestTodo_DB_015 -- the primary test
// ---------------------------------------------------------------------------

func TestTodo_DB_015(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "db015-primary")

	t.Run("migration 00032 creates exactly its declared tables", func(t *testing.T) {
		want := []string{
			"audit_package",
			"backup_run",
			"control_evidence",
			"data_copy",
			"data_copy_inventory",
			"hold_intersection",
			"legal_hold",
			"operational_incident",
			"processing_purpose_declaration",
			"record_declaration",
			"recovery_run",
			"retention_disposition",
			"slo_definition",
			"slo_observation",
		}
		if got := allTables(); !slices.Equal(got, want) {
			t.Fatalf("DB-015 table set is %v, want %v", got, want)
		}
		for _, table := range want {
			var found string
			if err := db.Conn.QueryRow(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=$1`, table).Scan(&found); err != nil {
				t.Errorf("table %s missing from the live schema: %v", table, err)
			}
		}
	})

	t.Run("every table declares its tenant and forces row level security", func(t *testing.T) {
		for _, table := range allTables() {
			var nullable string
			if err := db.Conn.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name='tenant_id'`, table).Scan(&nullable); err != nil {
				t.Errorf("table %s has no tenant_id column: %v", table, err)
				continue
			}
			if nullable != "NO" {
				t.Errorf("table %s tenant_id is nullable", table)
			}
			var enabled, forced bool
			if err := db.Conn.QueryRow(ctx, `SELECT relrowsecurity, relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname=$1`, table).Scan(&enabled, &forced); err != nil {
				t.Errorf("pg_class for %s: %v", table, err)
				continue
			}
			if !enabled || !forced {
				t.Errorf("table %s row level security enabled=%v forced=%v, want both true", table, enabled, forced)
			}
			var policy string
			if err := db.Conn.QueryRow(ctx, `SELECT polname FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname=$1`, table).Scan(&policy); err != nil || policy != "tenant_isolation" {
				t.Errorf("table %s policy is %q (%v), want tenant_isolation", table, policy, err)
			}
		}
	})

	t.Run("a hold blocks a disposition and the block names the hold", func(t *testing.T) {
		declaration := newDeclaration(tenant)
		hold := newHold(tenant)
		intersection := newIntersection(tenant, hold.HoldID, declaration.DeclarationID)
		disposition := newDisposition(tenant, declaration.DeclarationID)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declaration); err != nil {
				return err
			}
			if err := recordsmeta.InsertLegalHold(ctx, tx, hold); err != nil {
				return err
			}
			if err := recordsmeta.InsertHoldIntersection(ctx, tx, intersection); err != nil {
				return err
			}
			return recordsmeta.InsertRetentionDisposition(ctx, tx, disposition)
		})

		err := inTenantTxCommitting(t, conn, tenant, func(tx dbport.Tx) error {
			return recordsmeta.ExecuteDisposition(ctx, tx, tenant, disposition.DispositionID, "CRYPTO_ERASE", digestOf("receipt"), fixedInstant.Add(48*time.Hour))
		})
		if !errors.Is(err, recordsmeta.ErrHoldBlocksDisposition) {
			t.Fatalf("executing under a hold returned %v, want ErrHoldBlocksDisposition", err)
		}

		// The refusal is recorded, not merely returned: the disposition is
		// BLOCKED and names the hold that blocked it.
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			d, err := recordsmeta.LoadRetentionDisposition(ctx, tx, tenant, disposition.DispositionID)
			if err != nil {
				return err
			}
			if d.Status != "BLOCKED" {
				return fmt.Errorf("disposition status is %s, want BLOCKED", d.Status)
			}
			if d.BlockingHoldID == nil || *d.BlockingHoldID != hold.HoldID {
				return fmt.Errorf("disposition %+v does not name the hold that blocked it", d)
			}
			if d.ExecutedAt != nil {
				return fmt.Errorf("a blocked disposition recorded an execution")
			}
			return nil
		})

		// Releasing the intersection unblocks it.
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return recordsmeta.ReleaseIntersection(ctx, tx, tenant, intersection.IntersectionID, fixedInstant.Add(36*time.Hour))
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return recordsmeta.ExecuteDisposition(ctx, tx, tenant, disposition.DispositionID, "CRYPTO_ERASE", digestOf("receipt"), fixedInstant.Add(48*time.Hour))
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			d, err := recordsmeta.LoadRetentionDisposition(ctx, tx, tenant, disposition.DispositionID)
			if err != nil {
				return err
			}
			if d.Status != "EXECUTED" || d.ExecutedAt == nil || d.ReceiptDigest == nil || d.BlockingHoldID != nil {
				return fmt.Errorf("disposition %+v did not execute cleanly after the release", d)
			}
			return nil
		})
	})

	t.Run("the schema refuses an execution under a hold even without the Go layer", func(t *testing.T) {
		declaration := newDeclaration(tenant)
		hold := newHold(tenant)
		disposition := newDisposition(tenant, declaration.DeclarationID)
		disposition.BlockingHoldID = &hold.HoldID
		disposition.Status = "BLOCKED"
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declaration); err != nil {
				return err
			}
			if err := recordsmeta.InsertLegalHold(ctx, tx, hold); err != nil {
				return err
			}
			return recordsmeta.InsertRetentionDisposition(ctx, tx, disposition)
		})
		if err := db.ExecErr(`UPDATE retention_disposition SET executed_at=now(), method='SHRED', receipt_digest=$3, status='EXECUTED' WHERE tenant_id=$1 AND disposition_id=$2`,
			tenant, disposition.DispositionID, digestOf("receipt")); err == nil {
			t.Fatal("a raw UPDATE executed a disposition that a hold blocks")
		}
	})

	t.Run("nothing loses its retention schedule version", func(t *testing.T) {
		noVersion := newDeclaration(tenant)
		noVersion.RetentionScheduleVersion = 0
		if err := noVersion.Validate(); !errors.Is(err, recordsmeta.ErrMissingRetention) {
			t.Errorf("a declaration with no schedule version validated as %v", err)
		}
		noKey := newDisposition(tenant, uuid.New())
		noKey.RetentionScheduleKey = ""
		if err := noKey.Validate(); !errors.Is(err, recordsmeta.ErrMissingRetention) {
			t.Errorf("a disposition with no schedule key validated as %v", err)
		}
		if err := db.ExecErr(`INSERT INTO record_declaration (tenant_id, declaration_id, subject_entity_ref, record_series, record_class, owner_ref, custodian_ref, cutoff_trigger, retention_schedule_key, retention_schedule_version, status, created_at) VALUES ($1,$2,'p','HR-100','PERSONNEL','o','c','CREATION','schedule:x',0,'DECLARED',now())`,
			tenant, uuid.New()); err == nil {
			t.Fatal("the schema accepted a declaration with retention_schedule_version 0")
		}
	})

	t.Run("a correction names its target and never itself", func(t *testing.T) {
		original := newDeclaration(tenant)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return recordsmeta.InsertRecordDeclaration(ctx, tx, original)
		})
		correction := newDeclaration(tenant)
		correction.CorrectsDeclarationID = &original.DeclarationID
		correction.RetentionScheduleVersion = 4
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return recordsmeta.InsertRecordDeclaration(ctx, tx, correction)
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			// The original survives the correction: lineage, not overwrite.
			d, err := recordsmeta.LoadRecordDeclaration(ctx, tx, tenant, original.DeclarationID)
			if err != nil {
				return err
			}
			if d.RetentionScheduleVersion != 3 {
				return fmt.Errorf("the corrected declaration was rewritten to version %d", d.RetentionScheduleVersion)
			}
			c, err := recordsmeta.LoadRecordDeclaration(ctx, tx, tenant, correction.DeclarationID)
			if err != nil {
				return err
			}
			if c.CorrectsDeclarationID == nil || *c.CorrectsDeclarationID != original.DeclarationID {
				return fmt.Errorf("correction %+v lost its lineage", c)
			}
			return nil
		})
		selfCorrecting := newDeclaration(tenant)
		selfCorrecting.CorrectsDeclarationID = &selfCorrecting.DeclarationID
		if err := selfCorrecting.Validate(); !errors.Is(err, recordsmeta.ErrSelfCorrection) {
			t.Fatalf("a self-correcting declaration validated as %v", err)
		}
	})

	t.Run("a hold release is always attributed", func(t *testing.T) {
		h := newHold(tenant)
		h.Status = "RELEASED"
		if err := h.Validate(); !errors.Is(err, recordsmeta.ErrUnattributedRelease) {
			t.Fatalf("an unattributed release validated as %v", err)
		}
		if err := db.ExecErr(`INSERT INTO legal_hold (tenant_id, hold_id, matter_ref, authority_ref, hold_version, reason, scope_digest, placed_by, placed_at, status) VALUES ($1,$2,$3,'a',1,'r',$4,'p',now(),'RELEASED')`,
			tenant, uuid.New(), "matter:"+uuid.NewString(), digestOf("scope")); err == nil {
			t.Fatal("the schema accepted a RELEASED hold with no releasing actor")
		}
	})

	t.Run("one hold grips one record once", func(t *testing.T) {
		declaration := newDeclaration(tenant)
		hold := newHold(tenant)
		first := newIntersection(tenant, hold.HoldID, declaration.DeclarationID)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declaration); err != nil {
				return err
			}
			if err := recordsmeta.InsertLegalHold(ctx, tx, hold); err != nil {
				return err
			}
			return recordsmeta.InsertHoldIntersection(ctx, tx, first)
		})
		duplicate := newIntersection(tenant, hold.HoldID, declaration.DeclarationID)
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return recordsmeta.InsertHoldIntersection(ctx, tx, duplicate)
		}); err == nil {
			t.Fatal("one hold gripped the same record twice")
		}
	})
}

// ---------------------------------------------------------------------------
// TestTodo_DB_015_Security
// ---------------------------------------------------------------------------

func TestTodo_DB_015_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	alpha := insertTenant(t, db, "db015-alpha")
	beta := insertTenant(t, db, "db015-beta")

	declaration := newDeclaration(alpha)
	hold := newHold(alpha)
	inTenantTx(t, conn, alpha, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declaration); err != nil {
			return err
		}
		return recordsmeta.InsertLegalHold(ctx, tx, hold)
	})

	t.Run("another tenant cannot read a record declaration or its hold", func(t *testing.T) {
		if err := inTenantTxErr(conn, beta, func(tx dbport.Tx) error {
			_, err := recordsmeta.LoadRecordDeclaration(ctx, tx, alpha, declaration.DeclarationID)
			return err
		}); !errors.Is(err, dbport.ErrNoRows) {
			t.Fatalf("beta read alpha's record declaration: %v", err)
		}
		if err := inTenantTxErr(conn, beta, func(tx dbport.Tx) error {
			_, err := recordsmeta.LoadLegalHold(ctx, tx, alpha, hold.HoldID)
			return err
		}); !errors.Is(err, dbport.ErrNoRows) {
			t.Fatalf("beta read alpha's legal hold: %v", err)
		}
	})

	t.Run("another tenant cannot place a hold over this tenant's record", func(t *testing.T) {
		crossTenant := newIntersection(beta, hold.HoldID, declaration.DeclarationID)
		if err := inTenantTxErr(conn, beta, func(tx dbport.Tx) error {
			return recordsmeta.InsertHoldIntersection(ctx, tx, crossTenant)
		}); err == nil {
			t.Fatal("beta placed a hold intersection over alpha's record")
		}
	})

	t.Run("another tenant cannot dispose of this tenant's record", func(t *testing.T) {
		crossTenant := newDisposition(beta, declaration.DeclarationID)
		if err := inTenantTxErr(conn, beta, func(tx dbport.Tx) error {
			return recordsmeta.InsertRetentionDisposition(ctx, tx, crossTenant)
		}); err == nil {
			t.Fatal("beta planned a disposition over alpha's record")
		}
	})

	t.Run("a hold that another tenant's session cannot see still blocks", func(t *testing.T) {
		// The block is evaluated inside the owning tenant's session, so an
		// invisible hold is not an absent one.
		intersection := newIntersection(alpha, hold.HoldID, declaration.DeclarationID)
		disposition := newDisposition(alpha, declaration.DeclarationID)
		inTenantTx(t, conn, alpha, func(tx dbport.Tx) error {
			if err := recordsmeta.InsertHoldIntersection(ctx, tx, intersection); err != nil {
				return err
			}
			return recordsmeta.InsertRetentionDisposition(ctx, tx, disposition)
		})
		err := inTenantTxErr(conn, alpha, func(tx dbport.Tx) error {
			return recordsmeta.ExecuteDisposition(ctx, tx, alpha, disposition.DispositionID, "SHRED", digestOf("r"), fixedInstant.Add(48*time.Hour))
		})
		if !errors.Is(err, recordsmeta.ErrHoldBlocksDisposition) {
			t.Fatalf("the hold did not block: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// TestTodo_DB_015_Mutation
// ---------------------------------------------------------------------------

func TestTodo_DB_015_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "db015-mutation")

	inventory := privacymeta.DataCopyInventory{
		TenantID: tenant, InventoryID: uuid.New(),
		CanonicalAssetKey: "asset:" + uuid.NewString(), AsOf: fixedInstant,
		SourceWatermark: "wm-1", ExpectedSources: json.RawMessage(`["catalog"]`), SourceWatermarks: json.RawMessage(`{"catalog":"wm-1"}`), Completeness: "COMPLETE", ContentDigest: digestOf("inv"),
		CreatedAt: fixedInstant,
	}
	slo := assurancemeta.SLODefinition{
		TenantID: tenant, SLOID: uuid.New(), SLOKey: "slo:" + uuid.NewString(), SLOVersion: 1,
		Service: "payroll", Capability: "run", ObjectiveKind: "AVAILABILITY",
		TargetRatio: 0.999, WindowSeconds: 2592000, OwnerRef: "principal:sre",
		EffectiveFrom: fixedInstant,
	}
	observation := assurancemeta.SLOObservation{
		TenantID: tenant, ObservationID: uuid.New(), SLOID: slo.SLOID, SLOVersion: 1,
		WindowStart: fixedInstant, WindowEnd: fixedInstant.Add(time.Hour),
		GoodEvents: 999, TotalEvents: 1000, ErrorBudgetRemaining: 0.5,
		SourceQuality: "COMPLETE", ObservedAt: fixedInstant.Add(time.Hour),
	}
	evidence := assurancemeta.ControlEvidence{
		TenantID: tenant, EvidenceID: uuid.New(), ControlKey: "ctl:" + uuid.NewString(),
		ControlVersion: 2, ImplementationRef: "impl:1", WindowStart: fixedInstant,
		WindowEnd: fixedInstant.Add(time.Hour), SourceRef: "src:1",
		ArtifactDigest: digestOf("ev"), Collector: "collector:1", Completeness: "COMPLETE",
		Result: "PASS", FreshnessDeadline: fixedInstant.Add(48 * time.Hour),
		RetentionClass: "PERMANENT", CollectedAt: fixedInstant.Add(time.Hour),
	}
	pkg := assurancemeta.AuditPackage{
		TenantID: tenant, PackageID: uuid.New(), Purpose: "SOC2",
		EvidenceIDs: []uuid.UUID{evidence.EvidenceID}, ManifestDigest: digestOf("manifest"),
		SignatureRef: "artifact://sig/" + uuid.NewString(), Completeness: "COMPLETE",
		GeneratedAt: fixedInstant, ExpiresAt: fixedInstant.Add(720 * time.Hour), Status: "GENERATED",
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := privacymeta.InsertDataCopyInventory(ctx, tx, inventory); err != nil {
			return err
		}
		if err := assurancemeta.InsertSLODefinition(ctx, tx, slo); err != nil {
			return err
		}
		if err := assurancemeta.InsertSLOObservation(ctx, tx, observation); err != nil {
			return err
		}
		if err := assurancemeta.InsertControlEvidence(ctx, tx, evidence); err != nil {
			return err
		}
		return assurancemeta.InsertAuditPackage(ctx, tx, pkg)
	})

	cases := []struct {
		table  string
		column string
		key    string
		id     uuid.UUID
	}{
		{"data_copy_inventory", "source_watermark", "inventory_id", inventory.InventoryID},
		{"slo_observation", "source_quality", "observation_id", observation.ObservationID},
		{"control_evidence", "collector", "evidence_id", evidence.EvidenceID},
		{"audit_package", "purpose", "package_id", pkg.PackageID},
	}
	for _, tc := range cases {
		t.Run(tc.table+" rejects UPDATE", func(t *testing.T) {
			sql := fmt.Sprintf(`UPDATE %s SET %s='rewritten' WHERE tenant_id=$1 AND %s=$2`, tc.table, tc.column, tc.key)
			if err := db.ExecErr(sql, tenant, tc.id); err == nil {
				t.Fatalf("an append-only %s row was rewritten", tc.table)
			}
		})
		t.Run(tc.table+" rejects DELETE", func(t *testing.T) {
			sql := fmt.Sprintf(`DELETE FROM %s WHERE tenant_id=$1 AND %s=$2`, tc.table, tc.key)
			if err := db.ExecErr(sql, tenant, tc.id); err == nil {
				t.Fatalf("an append-only %s row was deleted", tc.table)
			}
		})
	}

	t.Run("every append-only table carries the forbid_mutation trigger", func(t *testing.T) {
		for _, table := range appendOnlyTables {
			var name string
			err := db.Conn.QueryRow(ctx, `SELECT t.tgname FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname=$1 AND NOT t.tgisinternal`, table).Scan(&name)
			if err != nil {
				t.Errorf("no append-only trigger on %s: %v", table, err)
				continue
			}
			if name != table+"_append_only" {
				t.Errorf("table %s carries trigger %s, want %s_append_only", table, name, table)
			}
		}
	})

	t.Run("live records state stays updatable", func(t *testing.T) {
		declaration := newDeclaration(tenant)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return recordsmeta.InsertRecordDeclaration(ctx, tx, declaration)
		})
		if err := db.ExecErr(`UPDATE record_declaration SET status='HELD' WHERE tenant_id=$1 AND declaration_id=$2`, tenant, declaration.DeclarationID); err != nil {
			t.Fatalf("record_declaration is live serving state and must stay updatable: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// TestTodo_DB_015_Fault
// ---------------------------------------------------------------------------

// TestTodo_DB_015_Fault proves a failure part-way through a records
// transaction leaves no half-recorded lifecycle behind: the hold, its
// intersection and the disposition either all land or none do, and a
// disposition refused by a hold never leaves an execution behind.
func TestTodo_DB_015_Fault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "db015-fault")

	declaration := newDeclaration(tenant)
	hold := newHold(tenant)
	intersection := newIntersection(tenant, hold.HoldID, declaration.DeclarationID)

	sentinel := errors.New("sink failed after the writes")
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declaration); err != nil {
			return err
		}
		if err := recordsmeta.InsertLegalHold(ctx, tx, hold); err != nil {
			return err
		}
		if err := recordsmeta.InsertHoldIntersection(ctx, tx, intersection); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("transaction returned %v, want the sink failure", err)
	}

	reader := appConn(t, db)
	for _, probe := range []struct {
		name string
		load func(tx dbport.Tx) error
	}{
		{"record_declaration", func(tx dbport.Tx) error {
			_, e := recordsmeta.LoadRecordDeclaration(ctx, tx, tenant, declaration.DeclarationID)
			return e
		}},
		{"legal_hold", func(tx dbport.Tx) error {
			_, e := recordsmeta.LoadLegalHold(ctx, tx, tenant, hold.HoldID)
			return e
		}},
		{"hold_intersection", func(tx dbport.Tx) error {
			_, e := recordsmeta.LoadHoldIntersection(ctx, tx, tenant, intersection.IntersectionID)
			return e
		}},
	} {
		if got := inTenantTxErr(reader, tenant, probe.load); !errors.Is(got, dbport.ErrNoRows) {
			t.Errorf("%s survived a rolled back transaction: %v", probe.name, got)
		}
	}

	t.Run("a refused execution leaves no partial disposition", func(t *testing.T) {
		d := newDeclaration(tenant)
		h := newHold(tenant)
		i := newIntersection(tenant, h.HoldID, d.DeclarationID)
		disposition := newDisposition(tenant, d.DeclarationID)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			if err := recordsmeta.InsertRecordDeclaration(ctx, tx, d); err != nil {
				return err
			}
			if err := recordsmeta.InsertLegalHold(ctx, tx, h); err != nil {
				return err
			}
			if err := recordsmeta.InsertHoldIntersection(ctx, tx, i); err != nil {
				return err
			}
			return recordsmeta.InsertRetentionDisposition(ctx, tx, disposition)
		})
		// ExecuteDisposition writes the BLOCKED marker and then returns the
		// refusal; the caller rolls back, so not even the marker survives.
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return recordsmeta.ExecuteDisposition(ctx, tx, tenant, disposition.DispositionID, "SHRED", digestOf("r"), fixedInstant.Add(48*time.Hour))
		})
		if !errors.Is(err, recordsmeta.ErrHoldBlocksDisposition) {
			t.Fatalf("execution returned %v, want ErrHoldBlocksDisposition", err)
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			loaded, err := recordsmeta.LoadRetentionDisposition(ctx, tx, tenant, disposition.DispositionID)
			if err != nil {
				return err
			}
			if loaded.Status != "PLANNED" || loaded.ExecutedAt != nil {
				return fmt.Errorf("a rolled back refusal left the disposition as %+v", loaded)
			}
			return nil
		})
	})

	t.Run("executing before the due date is refused", func(t *testing.T) {
		d := newDeclaration(tenant)
		disposition := newDisposition(tenant, d.DeclarationID)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			if err := recordsmeta.InsertRecordDeclaration(ctx, tx, d); err != nil {
				return err
			}
			return recordsmeta.InsertRetentionDisposition(ctx, tx, disposition)
		})
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return recordsmeta.ExecuteDisposition(ctx, tx, tenant, disposition.DispositionID, "SHRED", digestOf("r"), fixedInstant)
		})
		if !errors.Is(err, recordsmeta.ErrInvalidInterval) {
			t.Fatalf("executing early returned %v, want ErrInvalidInterval", err)
		}
	})
}

// ---------------------------------------------------------------------------
// TestTodo_DB_015_Recovery
// ---------------------------------------------------------------------------

// TestTodo_DB_015_Recovery covers the backup and recovery lifecycle that
// internal/data/opsmeta owns: a recovery run never closes on an unvalidated
// claim, a RESTORE always names the backup it restored from, and a completed
// backup is immutable for a stated window.
func TestTodo_DB_015_Recovery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "db015-recovery")

	backup := opsmeta.BackupRun{
		TenantID: tenant, RunID: uuid.New(), PolicyKey: "policy:nightly", Plane: "DATA",
		StoreRef: "store:primary", BackupMode: "FULL", PointInTime: fixedInstant,
		Watermark: "lsn-1", LocationRef: "s3://vault/1", KeyVersion: 1,
		ManifestDigest: digestOf("manifest"), StartedAt: fixedInstant,
		VerificationResult: "PENDING", Status: "RUNNING",
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		return opsmeta.InsertBackupRun(ctx, tx, backup)
	})

	t.Run("a completed backup is immutable for a stated window", func(t *testing.T) {
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return opsmeta.CompleteBackupRun(ctx, tx, tenant, backup.RunID, fixedInstant.Add(time.Hour), fixedInstant.Add(time.Hour), "PASS")
		})
		if !errors.Is(err, opsmeta.ErrMutableBackup) {
			t.Fatalf("completing with no immutability window returned %v", err)
		}
		if err := db.ExecErr(`UPDATE backup_run SET status='COMPLETED', completed_at=now() WHERE tenant_id=$1 AND run_id=$2`, tenant, backup.RunID); err == nil {
			t.Fatal("the schema completed a backup with no immutability horizon")
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return opsmeta.CompleteBackupRun(ctx, tx, tenant, backup.RunID, fixedInstant.Add(time.Hour), fixedInstant.Add(720*time.Hour), "PASS")
		})
	})

	t.Run("a RESTORE always names the backup it restored from", func(t *testing.T) {
		orphan := opsmeta.RecoveryRun{
			TenantID: tenant, RunID: uuid.New(), ScenarioKey: "scenario:cell-loss",
			PlanRef: "plan:1", RecoveryMode: "RESTORE", IsolatedEnvironment: "recovery-cell",
			TargetRPOSeconds: 900, TargetRTOSeconds: 3600, ValidationResult: "PENDING",
			StartedAt: fixedInstant, Status: "RUNNING",
		}
		if err := orphan.Validate(); !errors.Is(err, opsmeta.ErrMissingLineage) {
			t.Fatalf("a RESTORE with no backup validated as %v, want ErrMissingLineage", err)
		}
		if err := db.ExecErr(`INSERT INTO recovery_run (tenant_id, run_id, scenario_key, plan_ref, recovery_mode, isolated_environment, target_rpo_seconds, target_rto_seconds, started_at, status) VALUES ($1,$2,'s','p','RESTORE','env',900,3600,now(),'RUNNING')`,
			tenant, uuid.New()); err == nil {
			t.Fatal("the schema accepted a RESTORE naming no backup")
		}
	})

	t.Run("a recovery run never closes on an unvalidated claim", func(t *testing.T) {
		run := opsmeta.RecoveryRun{
			TenantID: tenant, RunID: uuid.New(), ScenarioKey: "scenario:cell-loss",
			PlanRef: "plan:1", BackupRunID: &backup.RunID, RecoveryMode: "RESTORE",
			IsolatedEnvironment: "recovery-cell", TargetRPOSeconds: 900, TargetRTOSeconds: 3600,
			ValidationResult: "PENDING", StartedAt: fixedInstant, Status: "RUNNING",
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return opsmeta.InsertRecoveryRun(ctx, tx, run)
		})
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return opsmeta.CompleteRecoveryRun(ctx, tx, tenant, run.RunID, fixedInstant.Add(time.Hour), 600, 3000, "FAIL")
		})
		if !errors.Is(err, opsmeta.ErrUnvalidatedCompletion) {
			t.Fatalf("completing with a FAIL validation returned %v", err)
		}
		if err := db.ExecErr(`UPDATE recovery_run SET status='COMPLETED', completed_at=now() WHERE tenant_id=$1 AND run_id=$2`, tenant, run.RunID); err == nil {
			t.Fatal("the schema completed a recovery run with no actuals")
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return opsmeta.CompleteRecoveryRun(ctx, tx, tenant, run.RunID, fixedInstant.Add(time.Hour), 600, 3000, "PASS")
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			loaded, err := opsmeta.LoadRecoveryRun(ctx, tx, tenant, run.RunID)
			if err != nil {
				return err
			}
			if loaded.Status != "COMPLETED" || loaded.ActualRPOSeconds == nil || *loaded.ActualRPOSeconds != 600 {
				return fmt.Errorf("recovery run %+v did not record its actuals", loaded)
			}
			if loaded.BackupRunID == nil || *loaded.BackupRunID != backup.RunID {
				return fmt.Errorf("recovery run lost its backup lineage")
			}
			return nil
		})
	})
}

// ---------------------------------------------------------------------------
// TestTodo_DB_015_Race
// ---------------------------------------------------------------------------

func TestTodo_DB_015_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	setup := appConn(t, db)
	tenant := insertTenant(t, db, "db015-race")

	declaration := newDeclaration(tenant)
	hold := newHold(tenant)
	inTenantTx(t, setup, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declaration); err != nil {
			return err
		}
		return recordsmeta.InsertLegalHold(ctx, tx, hold)
	})

	const writers = 6
	var wins, losses atomic.Int64
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for range writers {
		done.Add(1)
		go func() {
			defer done.Done()
			conn := appConn(t, db)
			intersection := newIntersection(tenant, hold.HoldID, declaration.DeclarationID)
			start.Wait()
			err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
				return recordsmeta.InsertHoldIntersection(ctx, tx, intersection)
			})
			if err == nil {
				wins.Add(1)
			} else {
				losses.Add(1)
			}
		}()
	}
	start.Done()
	done.Wait()

	if wins.Load() != 1 {
		t.Fatalf("%d of %d concurrent writers placed the same hold, want exactly 1", wins.Load(), writers)
	}
	if losses.Load() != writers-1 {
		t.Fatalf("%d writers were refused, want %d", losses.Load(), writers-1)
	}
	var count int64
	if err := db.QueryRow(ctx, `SELECT count(*) FROM hold_intersection WHERE tenant_id=$1 AND hold_id=$2 AND declaration_id=$3`, tenant, hold.HoldID, declaration.DeclarationID).Scan(&count); err != nil {
		t.Fatalf("count intersections: %v", err)
	}
	if count != 1 {
		t.Fatalf("%d intersections exist, want 1", count)
	}
}

// ---------------------------------------------------------------------------
// TestTodo_DB_015_Integration
// ---------------------------------------------------------------------------

func TestTodo_DB_015_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	writer := appConn(t, db)
	tenant := insertTenant(t, db, "db015-integration")

	purpose := privacymeta.ProcessingPurposeDeclaration{
		TenantID: tenant, DeclarationID: uuid.New(), PurposeKey: "purpose:payroll",
		DeclarationVersion: 1, LawfulBasis: "CONTRACT",
		DataCategories:       json.RawMessage(`{"identity":true}`),
		AllowedOperations:    json.RawMessage(`{"read":true}`),
		Recipients:           json.RawMessage(`{"processor:adp":true}`),
		RetentionScheduleKey: "schedule:hr-100", ContentDigest: digestOf("purpose"),
		EffectiveFrom: fixedInstant, Status: "ACTIVE", CreatedAt: fixedInstant,
	}
	inventory := privacymeta.DataCopyInventory{
		TenantID: tenant, InventoryID: uuid.New(),
		CanonicalAssetKey: "asset:person-file", AsOf: fixedInstant,
		SourceWatermark: "wm-1", ExpectedSources: json.RawMessage(`["provider-catalog"]`), SourceWatermarks: json.RawMessage(`{"provider-catalog":"wm-1"}`), Completeness: "PARTIAL", UnknownCount: 2,
		ContentDigest: digestOf("inv"), CreatedAt: fixedInstant,
	}
	copyRow := privacymeta.DataCopy{
		TenantID: tenant, CopyID: uuid.New(), InventoryID: inventory.InventoryID,
		CanonicalAssetKey: "asset:person-file", CopyType: "PROVIDER",
		StoreRef: "store:adp", DiscoverySource: "provider-catalog", SubjectRef: "worker:1", DataCategory: "HR", ProcessorRef: "processor:adp", Region: "us-east", FieldScope: json.RawMessage(`{"fields":["name"]}`), EncryptionKeyRef: "key:tenant",
		RetentionScheduleKey: "schedule:hr-100", HoldState: "NONE",
		DeletionCapability: "DELETE", RestorePolicy: "REAPPLY_TOMBSTONES", CreatedAt: fixedInstant,
	}
	declaration := newDeclaration(tenant)
	hold := newHold(tenant)
	intersection := newIntersection(tenant, hold.HoldID, declaration.DeclarationID)
	intersection.CopyID = &copyRow.CopyID
	disposition := newDisposition(tenant, declaration.DeclarationID)
	incident := opsmeta.OperationalIncident{
		TenantID: tenant, IncidentID: uuid.New(), IncidentKey: "inc:" + uuid.NewString(),
		Severity: "SEV2", ImpactRevision: 1, Scope: json.RawMessage(`{"cell":"local"}`),
		CorrelationKey: "corr:" + uuid.NewString(), EvidenceDigest: digestOf("inc"),
		DeclaredAt: fixedInstant, Status: "OPEN",
	}
	slo := assurancemeta.SLODefinition{
		TenantID: tenant, SLOID: uuid.New(), SLOKey: "slo:" + uuid.NewString(), SLOVersion: 1,
		Service: "payroll", Capability: "run", ObjectiveKind: "FRESHNESS",
		TargetRatio: 0.99, WindowSeconds: 86400, OwnerRef: "principal:sre",
		EffectiveFrom: fixedInstant,
	}

	inTenantTx(t, writer, tenant, func(tx dbport.Tx) error {
		if err := privacymeta.InsertProcessingPurposeDeclaration(ctx, tx, purpose); err != nil {
			return err
		}
		if err := privacymeta.InsertDataCopyInventory(ctx, tx, inventory); err != nil {
			return err
		}
		if err := privacymeta.InsertDataCopy(ctx, tx, copyRow); err != nil {
			return err
		}
		if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declaration); err != nil {
			return err
		}
		if err := recordsmeta.InsertLegalHold(ctx, tx, hold); err != nil {
			return err
		}
		if err := recordsmeta.InsertHoldIntersection(ctx, tx, intersection); err != nil {
			return err
		}
		if err := recordsmeta.InsertRetentionDisposition(ctx, tx, disposition); err != nil {
			return err
		}
		if err := opsmeta.InsertOperationalIncident(ctx, tx, incident); err != nil {
			return err
		}
		return assurancemeta.InsertSLODefinition(ctx, tx, slo)
	})

	// Everything above is committed. A fresh connection must reconstruct the
	// whole privacy/records/operations/assurance picture from durable state.
	reader := appConn(t, db)
	inTenantTx(t, reader, tenant, func(tx dbport.Tx) error {
		if _, err := privacymeta.LoadProcessingPurposeDeclaration(ctx, tx, tenant, purpose.DeclarationID); err != nil {
			return fmt.Errorf("processing_purpose_declaration: %w", err)
		}
		inv, err := privacymeta.LoadDataCopyInventory(ctx, tx, tenant, inventory.InventoryID)
		if err != nil {
			return fmt.Errorf("data_copy_inventory: %w", err)
		}
		if inv.Completeness != "PARTIAL" || inv.UnknownCount != 2 {
			return fmt.Errorf("the census rounded itself up to %s with %d unknowns", inv.Completeness, inv.UnknownCount)
		}
		kinds, err := privacymeta.ListCopyTypes(ctx, tx, tenant, inventory.InventoryID)
		if err != nil {
			return fmt.Errorf("list copy types: %w", err)
		}
		if !slices.Equal(kinds, []string{"PROVIDER"}) {
			return fmt.Errorf("copy inventory holds %v, want [PROVIDER]", kinds)
		}
		d, err := recordsmeta.LoadRecordDeclaration(ctx, tx, tenant, declaration.DeclarationID)
		if err != nil {
			return fmt.Errorf("record_declaration: %w", err)
		}
		if d.RetentionScheduleVersion != 3 {
			return fmt.Errorf("declaration lost its retention schedule version")
		}
		if _, err := recordsmeta.LoadLegalHold(ctx, tx, tenant, hold.HoldID); err != nil {
			return fmt.Errorf("legal_hold: %w", err)
		}
		i, err := recordsmeta.LoadHoldIntersection(ctx, tx, tenant, intersection.IntersectionID)
		if err != nil {
			return fmt.Errorf("hold_intersection: %w", err)
		}
		if i.CopyID == nil || *i.CopyID != copyRow.CopyID {
			return fmt.Errorf("the intersection lost the copy it grips")
		}
		if _, err := recordsmeta.LoadRetentionDisposition(ctx, tx, tenant, disposition.DispositionID); err != nil {
			return fmt.Errorf("retention_disposition: %w", err)
		}
		if _, err := opsmeta.LoadOperationalIncident(ctx, tx, tenant, incident.IncidentID); err != nil {
			return fmt.Errorf("operational_incident: %w", err)
		}
		if _, err := assurancemeta.LoadSLODefinition(ctx, tx, tenant, slo.SLOID); err != nil {
			return fmt.Errorf("slo_definition: %w", err)
		}
		// The hold survived the commit, so the disposition is still blocked.
		holdID, held, err := recordsmeta.ActiveHold(ctx, tx, tenant, declaration.DeclarationID)
		if err != nil {
			return fmt.Errorf("active hold: %w", err)
		}
		if !held || holdID != hold.HoldID {
			return fmt.Errorf("the hold did not survive the commit")
		}
		return nil
	})

	t.Run("storage disposition rows, once registered, name migration 00032", func(t *testing.T) {
		// definitions/ is outside this lane's file roots, so DB-015's rows
		// are reported rather than written. This assertion says nothing
		// while they are absent and pins their migration once they land.
		root := repoRoot(t)
		reg, err := storagedisposition.Load(filepath.Join(root, "definitions", "storage", "storage-disposition.yaml"))
		if err != nil {
			t.Fatalf("load storage-disposition registry: %v", err)
		}
		registered := 0
		for _, table := range allTables() {
			entry, ok := reg.Lookup(table)
			if !ok {
				continue
			}
			registered++
			if entry.Migration != migrationFile {
				t.Errorf("table %s is registered against %s, want %s", table, entry.Migration, migrationFile)
			}
			if !entry.TenantScoped() {
				t.Errorf("table %s is registered without a tenant scoping column", table)
			}
			wantAppendOnly := slices.Contains(appendOnlyTables, table)
			if entry.AppendOnly != wantAppendOnly {
				t.Errorf("table %s registered append_only=%v, want %v", table, entry.AppendOnly, wantAppendOnly)
			}
		}
		t.Logf("%d of %d DB-015 tables are registered in storage-disposition.yaml", registered, len(allTables()))
	})
}
