package runtime_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// targetPlanDigestForTest is a syntactically valid compiled-plan digest --
// migration 00002's content_digest domain requires exactly 64 lowercase hex
// characters -- used wherever a test needs one but its exact value is not
// under test.
const targetPlanDigestForTest = "0000000000000000000000000000000000000000000000000000000000000001"

// versionmigration.go's per-file suite (WF-RUN-018's one additive export on
// this package). It proves exactly the three things internal/workflow/migrate
// depends on and nothing else: the write is refused before touching storage
// when malformed, it is refused against anything but a PAUSED instance, and a
// well formed call against a PAUSED instance rewrites the version, digest and
// frontier atomically under the usual optimistic fence.

func TestRecordVersionMigration_ValidationRefusesBeforeAnyRead(t *testing.T) {
	t.Parallel()

	wellFormed := runtime.VersionMigration{
		TenantID: uuid.New(), InstanceID: uuid.New(),
		ExpectedVersion:     1,
		NewWorkflowVersion:  2,
		NewCompiledPlanHash: "sha256:target-plan",
		NewCurrentNodeIDs:   []string{"node-a"},
	}
	cases := []struct {
		name string
		with func(*runtime.VersionMigration)
	}{
		{"nil tenant", func(m *runtime.VersionMigration) { m.TenantID = uuid.Nil }},
		{"nil instance", func(m *runtime.VersionMigration) { m.InstanceID = uuid.Nil }},
		{"unfenced", func(m *runtime.VersionMigration) { m.ExpectedVersion = 0 }},
		{"no new workflow version", func(m *runtime.VersionMigration) { m.NewWorkflowVersion = 0 }},
		{"no new compiled plan hash", func(m *runtime.VersionMigration) { m.NewCompiledPlanHash = "" }},
		{"no new frontier", func(m *runtime.VersionMigration) { m.NewCurrentNodeIDs = nil }},
		{"empty frontier node id", func(m *runtime.VersionMigration) { m.NewCurrentNodeIDs = []string{""} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := wellFormed
			tc.with(&m)
			// A nil Executor: reaching a statement would panic, so passing it
			// is the proof that validation refused before any read.
			if _, err := (runtime.Store{}).RecordVersionMigration(context.Background(), nil, m); runtime.CodeOf(err) != runtime.CodeInvalidRecord {
				t.Fatalf("RecordVersionMigration: code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeInvalidRecord, err)
			}
		})
	}
}

// TestRecordVersionMigration_RequiresPaused proves the additive write refuses
// a running instance -- WF-RUN-018 requires a reviewed safe point, and a
// RUNNING instance is never one -- without mutating its version, digest or
// frontier.
func TestRecordVersionMigration_RequiresPaused(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "versionmigration-notpaused")
	pf := newPromotionFixture(t, values.TenantId("versionmigration-notpaused-tenant"), "intent:versionmigration-notpaused")
	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-vm-notpaused"))

	// A freshly started instance is CREATED, not PAUSED.
	err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		_, err := (runtime.Store{}).RecordVersionMigration(context.Background(), tx, runtime.VersionMigration{
			TenantID: tenantID, InstanceID: start.InstanceID,
			ExpectedVersion:     start.InstanceVersion,
			NewWorkflowVersion:  pf.Plan.Version + 1,
			NewCompiledPlanHash: targetPlanDigestForTest,
			NewCurrentNodeIDs:   start.Frontier,
		})
		return err
	})
	if runtime.CodeOf(err) != runtime.CodeIllegalTransition {
		t.Fatalf("RecordVersionMigration on a CREATED instance: code = %q, want %q (%v)",
			runtime.CodeOf(err), runtime.CodeIllegalTransition, err)
	}

	reloaded := loadInstanceForTest(t, conn, tenantID, start.InstanceID)
	if reloaded.WorkflowVersion != pf.Plan.Version || reloaded.CompiledPlanHash != pf.Plan.Digest() {
		t.Fatalf("a refused migration changed the pinned plan: version=%d hash=%s",
			reloaded.WorkflowVersion, reloaded.CompiledPlanHash)
	}
}

// TestRecordVersionMigration_HappyPathAndStaleFence proves the GREEN path: a
// PAUSED instance's version, digest and frontier move together in one write,
// the instance stays PAUSED, and a stale expected version changes nothing.
func TestRecordVersionMigration_HappyPathAndStaleFence(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "versionmigration-happy")
	pf := newPromotionFixture(t, values.TenantId("versionmigration-happy-tenant"), "intent:versionmigration-happy")
	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-vm-happy"))

	paused, err := requestPause(t, conn, tenantID,
		pauseRequestFor(tenantID, start.InstanceID, pf.Plan, start.InstanceVersion))
	if err != nil {
		t.Fatalf("RequestPause: %v", err)
	}
	if paused.Status != runtime.InstancePaused {
		t.Fatalf("status = %s, want PAUSED", paused.Status)
	}

	t.Run("a stale expected version changes nothing", func(t *testing.T) {
		err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			_, err := (runtime.Store{}).RecordVersionMigration(context.Background(), tx, runtime.VersionMigration{
				TenantID: tenantID, InstanceID: start.InstanceID,
				ExpectedVersion:     paused.InstanceVersion + 7,
				NewWorkflowVersion:  pf.Plan.Version + 1,
				NewCompiledPlanHash: targetPlanDigestForTest,
				NewCurrentNodeIDs:   []string{pf.Plan.StartNodeID},
			})
			return err
		})
		if runtime.CodeOf(err) != runtime.CodeStaleInstance {
			t.Fatalf("code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeStaleInstance, err)
		}
	})

	var migrated runtime.Instance
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		migrated, err = (runtime.Store{}).RecordVersionMigration(context.Background(), tx, runtime.VersionMigration{
			TenantID: tenantID, InstanceID: start.InstanceID,
			ExpectedVersion:     paused.InstanceVersion,
			NewWorkflowVersion:  pf.Plan.Version + 1,
			NewCompiledPlanHash: targetPlanDigestForTest,
			NewCurrentNodeIDs:   []string{"raise_threshold"},
		})
		return err
	})
	if migrated.RuntimeStatus != runtime.InstancePaused {
		t.Fatalf("status after migration = %s, want PAUSED (unchanged)", migrated.RuntimeStatus)
	}
	if migrated.WorkflowVersion != pf.Plan.Version+1 {
		t.Fatalf("workflow version = %d, want %d", migrated.WorkflowVersion, pf.Plan.Version+1)
	}
	if migrated.CompiledPlanHash != targetPlanDigestForTest {
		t.Fatalf("compiled plan hash = %q, want the new target digest", migrated.CompiledPlanHash)
	}
	if len(migrated.CurrentNodeIDs) != 1 || migrated.CurrentNodeIDs[0] != "raise_threshold" {
		t.Fatalf("frontier = %v, want [raise_threshold]", migrated.CurrentNodeIDs)
	}
	if migrated.InstanceVersion != paused.InstanceVersion+1 {
		t.Fatalf("instance version = %d, want %d", migrated.InstanceVersion, paused.InstanceVersion+1)
	}

	reloaded := loadInstanceForTest(t, conn, tenantID, start.InstanceID)
	if reloaded.InstanceVersion != migrated.InstanceVersion {
		t.Fatalf("reloaded instance version = %d, want %d", reloaded.InstanceVersion, migrated.InstanceVersion)
	}
}
