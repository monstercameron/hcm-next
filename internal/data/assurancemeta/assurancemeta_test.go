package assurancemeta_test

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

	"github.com/monstercameron/hcm-next/internal/data/assurancemeta"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
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

func newSLO(tenant uuid.UUID) assurancemeta.SLODefinition {
	return assurancemeta.SLODefinition{
		TenantID:      tenant,
		SLOID:         uuid.New(),
		SLOKey:        "slo:" + uuid.NewString(),
		SLOVersion:    2,
		Service:       "payroll",
		Capability:    "run-payroll",
		ObjectiveKind: "AVAILABILITY",
		TargetRatio:   0.999,
		WindowSeconds: 2592000,
		OwnerRef:      "principal:sre",
		EffectiveFrom: fixedInstant,
	}
}

func newObservation(tenant, sloID uuid.UUID, version int64) assurancemeta.SLOObservation {
	return assurancemeta.SLOObservation{
		TenantID:             tenant,
		ObservationID:        uuid.New(),
		SLOID:                sloID,
		SLOVersion:           version,
		WindowStart:          fixedInstant,
		WindowEnd:            fixedInstant.Add(time.Hour),
		GoodEvents:           998,
		TotalEvents:          1000,
		ErrorBudgetRemaining: 0.33,
		SourceQuality:        "COMPLETE",
		ObservedAt:           fixedInstant.Add(time.Hour),
	}
}

func newEvidence(tenant uuid.UUID) assurancemeta.ControlEvidence {
	return assurancemeta.ControlEvidence{
		TenantID:          tenant,
		EvidenceID:        uuid.New(),
		ControlKey:        "ctl:" + uuid.NewString(),
		ControlVersion:    4,
		ImplementationRef: "impl:rls",
		Scope:             json.RawMessage(`{"plane":"DATA"}`),
		WindowStart:       fixedInstant,
		WindowEnd:         fixedInstant.Add(24 * time.Hour),
		SourceRef:         "src:pg_policy",
		ArtifactDigest:    digestOf("evidence-" + uuid.NewString()),
		Collector:         "collector:nightly",
		Completeness:      "COMPLETE",
		Result:            "PASS",
		FreshnessDeadline: fixedInstant.Add(72 * time.Hour),
		RetentionClass:    "PERMANENT",
		CollectedAt:       fixedInstant.Add(24 * time.Hour),
	}
}

func newPackage(tenant uuid.UUID, evidenceIDs ...uuid.UUID) assurancemeta.AuditPackage {
	return assurancemeta.AuditPackage{
		TenantID:       tenant,
		PackageID:      uuid.New(),
		Purpose:        "SOC2-TYPE-II",
		Scope:          json.RawMessage(`{"period":"2026-Q3"}`),
		EvidenceIDs:    evidenceIDs,
		ManifestDigest: digestOf("manifest-" + uuid.NewString()),
		SignatureRef:   "artifact://sig/" + uuid.NewString(),
		Completeness:   "COMPLETE",
		GeneratedAt:    fixedInstant,
		ExpiresAt:      fixedInstant.Add(720 * time.Hour),
		Status:         "GENERATED",
	}
}

// TestTodo_DB_015_Assurance is DB-015's assurance-side test. The matrix names
// live in internal/data/recordsmeta (the lane's primary package for DB-015);
// this suite proves the same clauses for the four assurance tables migration
// 00032 creates.
func TestTodo_DB_015_Assurance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "db015-assurance")
	other := insertTenant(t, db, "db015-assurance-other")

	t.Run("the assurance table set is exactly what 00032 declares", func(t *testing.T) {
		want := []string{"audit_package", "control_evidence", "slo_definition", "slo_observation"}
		if !slices.Equal(assurancemeta.AssuranceTables, want) {
			t.Fatalf("AssuranceTables=%v, want %v", assurancemeta.AssuranceTables, want)
		}
		for _, table := range want {
			var found string
			if err := db.Conn.QueryRow(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=$1`, table).Scan(&found); err != nil {
				t.Errorf("table %s missing from the live schema: %v", table, err)
			}
		}
	})

	t.Run("an observation is measured against a version that exists", func(t *testing.T) {
		slo := newSLO(tenant)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return assurancemeta.InsertSLODefinition(ctx, tx, slo)
		})
		wrong := newObservation(tenant, slo.SLOID, 1)
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return assurancemeta.InsertSLOObservation(ctx, tx, wrong)
		}); !errors.Is(err, assurancemeta.ErrMissingVersion) {
			t.Fatalf("an observation against version 1 of a version-2 SLO returned %v", err)
		}
		right := newObservation(tenant, slo.SLOID, slo.SLOVersion)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return assurancemeta.InsertSLOObservation(ctx, tx, right)
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			loaded, err := assurancemeta.LoadSLOObservation(ctx, tx, tenant, right.ObservationID)
			if err != nil {
				return err
			}
			if loaded.SLOVersion != slo.SLOVersion || loaded.GoodEvents != 998 || loaded.SourceQuality != "COMPLETE" {
				return fmt.Errorf("observation %+v lost its version, counts or source quality", loaded)
			}
			return nil
		})
	})

	t.Run("an observation cannot report more good events than total", func(t *testing.T) {
		slo := newSLO(tenant)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return assurancemeta.InsertSLODefinition(ctx, tx, slo)
		})
		impossible := newObservation(tenant, slo.SLOID, slo.SLOVersion)
		impossible.GoodEvents = 1001
		if err := impossible.Validate(); !errors.Is(err, assurancemeta.ErrImpossibleRatio) {
			t.Fatalf("1001 good of 1000 total validated as %v", err)
		}
		if err := db.ExecErr(`INSERT INTO slo_observation (tenant_id, observation_id, slo_id, slo_version, window_start, window_end, good_events, total_events, error_budget_remaining, source_quality, observed_at) VALUES ($1,$2,$3,$4,now(),now()+interval '1 hour',1001,1000,0.1,'COMPLETE',now())`,
			tenant, uuid.New(), slo.SLOID, slo.SLOVersion); err == nil {
			t.Fatal("the schema accepted 1001 good events of 1000")
		}
		reversed := newObservation(tenant, slo.SLOID, slo.SLOVersion)
		reversed.WindowEnd = reversed.WindowStart.Add(-time.Hour)
		if err := reversed.Validate(); !errors.Is(err, assurancemeta.ErrMissingWindow) {
			t.Fatalf("a reversed window validated as %v", err)
		}
	})

	t.Run("one window is measured once per SLO", func(t *testing.T) {
		slo := newSLO(tenant)
		first := newObservation(tenant, slo.SLOID, slo.SLOVersion)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			if err := assurancemeta.InsertSLODefinition(ctx, tx, slo); err != nil {
				return err
			}
			return assurancemeta.InsertSLOObservation(ctx, tx, first)
		})
		duplicate := newObservation(tenant, slo.SLOID, slo.SLOVersion)
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return assurancemeta.InsertSLOObservation(ctx, tx, duplicate)
		}); err == nil {
			t.Fatal("the same window was measured twice")
		}
	})

	t.Run("control evidence keeps its version, window and freshness horizon", func(t *testing.T) {
		evidence := newEvidence(tenant)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return assurancemeta.InsertControlEvidence(ctx, tx, evidence)
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			loaded, err := assurancemeta.LoadControlEvidence(ctx, tx, tenant, evidence.EvidenceID)
			if err != nil {
				return err
			}
			if loaded.ControlVersion != 4 || !loaded.FreshnessDeadline.After(loaded.WindowEnd) || loaded.ArtifactDigest == "" {
				return fmt.Errorf("evidence %+v lost its version, freshness horizon or artifact digest", loaded)
			}
			return nil
		})
		stale := newEvidence(tenant)
		stale.FreshnessDeadline = stale.WindowEnd
		if err := stale.Validate(); !errors.Is(err, assurancemeta.ErrMissingFreshness) {
			t.Errorf("evidence with no freshness horizon validated as %v", err)
		}
		silentFailure := newEvidence(tenant)
		silentFailure.Result = "FAIL"
		if err := silentFailure.Validate(); !errors.Is(err, assurancemeta.ErrInvalidEnum) {
			t.Errorf("a FAIL with no deficiency reference validated as %v", err)
		}
		if err := db.ExecErr(`INSERT INTO control_evidence (tenant_id, evidence_id, control_key, control_version, implementation_ref, window_start, window_end, source_ref, artifact_digest, collector, completeness, result, freshness_deadline, retention_class, collected_at) VALUES ($1,$2,$3,1,'impl',now(),now()+interval '1 hour','src',$4,'collector','COMPLETE','FAIL',now()+interval '2 hours','PERMANENT',now())`,
			tenant, uuid.New(), "ctl:"+uuid.NewString(), digestOf("e")); err == nil {
			t.Fatal("the schema accepted a FAIL with no deficiency reference")
		}
	})

	t.Run("a COMPLETE audit package carries evidence and a signature reference", func(t *testing.T) {
		evidence := newEvidence(tenant)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return assurancemeta.InsertControlEvidence(ctx, tx, evidence)
		})
		empty := newPackage(tenant)
		if err := empty.Validate(); !errors.Is(err, assurancemeta.ErrEmptyPackage) {
			t.Fatalf("an empty COMPLETE package validated as %v", err)
		}
		if err := db.ExecErr(`INSERT INTO audit_package (tenant_id, package_id, purpose, manifest_digest, signature_ref, completeness, generated_at, expires_at, status) VALUES ($1,$2,'SOC2',$3,'artifact://sig/x','COMPLETE',now(),now()+interval '30 days','GENERATED')`,
			tenant, uuid.New(), digestOf("m")); err == nil {
			t.Fatal("the schema accepted a COMPLETE package with no evidence")
		}
		for _, ref := range []string{"", "MEUCIQD", "https://x/sig", "artifact:/x"} {
			p := newPackage(tenant, evidence.EvidenceID)
			p.SignatureRef = ref
			if err := p.Validate(); !errors.Is(err, assurancemeta.ErrRawSignature) {
				t.Errorf("signature ref %q validated as %v, want ErrRawSignature", ref, err)
			}
		}
		good := newPackage(tenant, evidence.EvidenceID)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return assurancemeta.InsertAuditPackage(ctx, tx, good)
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			loaded, err := assurancemeta.LoadAuditPackage(ctx, tx, tenant, good.PackageID)
			if err != nil {
				return err
			}
			if !slices.Equal(loaded.EvidenceIDs, []uuid.UUID{evidence.EvidenceID}) {
				return fmt.Errorf("package %+v lost the evidence it bundled", loaded)
			}
			if !loaded.ExpiresAt.After(loaded.GeneratedAt) {
				return fmt.Errorf("package expiry did not survive the round trip")
			}
			return nil
		})
	})

	t.Run("assurance evidence is append-only", func(t *testing.T) {
		evidence := newEvidence(tenant)
		pkg := newPackage(tenant, evidence.EvidenceID)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			if err := assurancemeta.InsertControlEvidence(ctx, tx, evidence); err != nil {
				return err
			}
			return assurancemeta.InsertAuditPackage(ctx, tx, pkg)
		})
		if err := db.ExecErr(`UPDATE control_evidence SET result='PASS' WHERE tenant_id=$1 AND evidence_id=$2`, tenant, evidence.EvidenceID); err == nil {
			t.Fatal("control evidence was rewritten")
		}
		if err := db.ExecErr(`DELETE FROM audit_package WHERE tenant_id=$1 AND package_id=$2`, tenant, pkg.PackageID); err == nil {
			t.Fatal("an audit package was deleted")
		}
	})

	t.Run("no telemetry series lands in these tables", func(t *testing.T) {
		// DB-015 REFACTOR: an observation is the aggregate a decision is
		// made from, never the series behind it.
		for _, table := range assurancemeta.AssuranceTables {
			var byteColumns int
			if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND data_type='bytea'`, table).Scan(&byteColumns); err != nil {
				t.Fatalf("inspect %s: %v", table, err)
			}
			if byteColumns != 0 {
				t.Errorf("table %s holds %d byte-bearing columns, want 0", table, byteColumns)
			}
		}
	})

	t.Run("one tenant cannot reach another tenant's assurance evidence", func(t *testing.T) {
		evidence := newEvidence(tenant)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return assurancemeta.InsertControlEvidence(ctx, tx, evidence)
		})
		if err := inTenantTxErr(conn, other, func(tx dbport.Tx) error {
			_, err := assurancemeta.LoadControlEvidence(ctx, tx, tenant, evidence.EvidenceID)
			return err
		}); !errors.Is(err, dbport.ErrNoRows) {
			t.Fatalf("another tenant read this tenant's control evidence: %v", err)
		}
		slo := newSLO(tenant)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return assurancemeta.InsertSLODefinition(ctx, tx, slo)
		})
		crossTenant := newObservation(other, slo.SLOID, slo.SLOVersion)
		if err := inTenantTxErr(conn, other, func(tx dbport.Tx) error {
			return assurancemeta.InsertSLOObservation(ctx, tx, crossTenant)
		}); err == nil {
			t.Fatal("another tenant measured this tenant's SLO")
		}
	})
}
