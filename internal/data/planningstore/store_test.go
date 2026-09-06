package planningstore_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/planningstore"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/demand"
	"github.com/monstercameron/hcm-next/internal/domains/scenario"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var planningNow = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func planningDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 110); err != nil {
		t.Fatalf("apply migrations through 00110: %v", err)
	}
	return db
}

func planningTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}

func planningConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("set role: %v", err)
	}
	return conn
}

func demandFixture(t *testing.T, id, version string) demand.DemandSignal {
	t.Helper()
	start, err := values.NewInstantFromUnix(planningNow.Unix(), int32(planningNow.Nanosecond()))
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.NewInstantFromUnix(planningNow.Add(time.Hour).Unix(), int32(planningNow.Add(time.Hour).Nanosecond()))
	if err != nil {
		t.Fatal(err)
	}
	work, err := values.NewInstantInterval(start, end)
	if err != nil {
		t.Fatal(err)
	}
	quantity, err := values.NewQuantity("2.00", "FTE", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	confidence, err := values.NewDecimal("0.90", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	value, err := demand.NewDemandSignal(demand.DemandSignal{
		SignalID: id, Work: work, OrgUnit: "org:clinical", Quantity: quantity, Unit: "FTE",
		Role: "role:nurse", Priority: 1, Source: "forecast", SourceRef: "forecast:1",
		Confidence: confidence, Scenario: "base", Version: version,
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func coverageFixture(t *testing.T, signal demand.DemandSignal, version string) demand.CoverageRequirement {
	t.Helper()
	value, err := demand.NewCoverageRequirement("coverage-1", signal.Scenario, version, []demand.DemandSignal{signal})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func scenarioFixture(t *testing.T, id string, revision uint64, parent *scenario.ScenarioRevision) scenario.ScenarioRevision {
	t.Helper()
	start, err := values.NewLocalDate(2026, time.January, 1)
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.NewLocalDate(2026, time.February, 1)
	if err != nil {
		t.Fatal(err)
	}
	horizon, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "us-federal", Version: "2026"})
	if err != nil {
		t.Fatal(err)
	}
	value := scenario.ScenarioRevision{
		ScenarioID: id, Revision: revision, Owner: "workforce-planning", Scope: "north-america",
		Horizon: horizon, BaselineSnapshotRef: "snapshot:2026-01", Author: "planner-1",
		AuthorityDisclaimer: "simulation only; does not mutate authoritative facts", Lifecycle: scenario.LifecycleDraft,
		Assumptions: []scenario.Assumption{{Key: "headcount.target", Value: scenario.DecimalValue(values.MustDecimal("12", 0, values.RoundingHalfEven)), Unit: "HEAD", ProvenanceRefs: []string{"forecast:2026"}}},
	}
	if parent != nil {
		value.ParentRevision = parent.Revision
		value.ParentDigest = parent.CanonicalDigest
		value.Assumptions[0].Value = scenario.DecimalValue(values.MustDecimal("14", 0, values.RoundingHalfEven))
	}
	value, err = scenario.NewScenarioRevision(value)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestTodo_PERSIST_PLANNING_001(t *testing.T) {
	db := planningDB(t)
	tenant := planningTenant(t, db, "planning-primary")
	store := planningstore.New(planningConn(t, db), tenant)
	signal := demandFixture(t, "signal-primary", "v1")
	coverage := coverageFixture(t, signal, "v1")
	scenarioRevision := scenarioFixture(t, "scenario-primary", 1, nil)

	if err := store.SaveSignal(context.Background(), tenant.String(), signal, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCoverageRequirement(context.Background(), tenant.String(), coverage, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveScenarioRevision(context.Background(), tenant.String(), scenarioRevision, 0); err != nil {
		t.Fatal(err)
	}
	loadedSignal, err := store.LoadSignal(context.Background(), tenant.String(), signal.SignalID, signal.Version)
	if err != nil || loadedSignal.CanonicalDigest != signal.CanonicalDigest {
		t.Fatalf("signal reload = %v, %v", loadedSignal, err)
	}
	loadedCoverage, err := store.LoadCoverageRequirement(context.Background(), tenant.String(), coverage.RequirementID, coverage.Version)
	if err != nil || loadedCoverage.CanonicalDigest != coverage.CanonicalDigest {
		t.Fatalf("coverage reload = %v, %v", loadedCoverage, err)
	}
	loadedScenario, err := store.LoadScenarioRevision(context.Background(), tenant.String(), scenarioRevision.ScenarioID, scenarioRevision.Revision)
	if err != nil || loadedScenario.CanonicalDigest != scenarioRevision.CanonicalDigest {
		t.Fatalf("scenario reload = %v, %v", loadedScenario, err)
	}
}

func TestTodo_PERSIST_PLANNING_001_Fault(t *testing.T) {
	db := planningDB(t)
	tenant := planningTenant(t, db, "planning-fault")
	store := planningstore.New(planningConn(t, db), tenant)
	signal := demandFixture(t, "signal-fault", "v1")
	if err := store.SaveSignal(context.Background(), tenant.String(), signal, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSignal(context.Background(), tenant.String(), signal, ""); planningstore.CodeOf(err) != planningstore.CodeDuplicateRevision {
		t.Fatalf("duplicate signal = %v, code=%s", err, planningstore.CodeOf(err))
	}
	if err := store.SaveSignal(context.Background(), tenant.String(), demandFixture(t, "signal-fault", "v2"), "stale-v0"); planningstore.CodeOf(err) != planningstore.CodeStaleCAS {
		t.Fatalf("stale signal = %v, code=%s", err, planningstore.CodeOf(err))
	}
	first := scenarioFixture(t, "scenario-fault", 1, nil)
	if err := store.SaveScenarioRevision(context.Background(), tenant.String(), first, 0); err != nil {
		t.Fatal(err)
	}
	second := scenarioFixture(t, "scenario-fault", 2, &first)
	if err := store.SaveScenarioRevision(context.Background(), tenant.String(), second, 0); planningstore.CodeOf(err) != planningstore.CodeStaleCAS {
		t.Fatalf("stale scenario = %v, code=%s", err, planningstore.CodeOf(err))
	}
}

func TestTodo_PERSIST_PLANNING_001_Integration(t *testing.T) {
	db := planningDB(t)
	tenant := planningTenant(t, db, "planning-integration")
	store := planningstore.New(planningConn(t, db), tenant)
	first := demandFixture(t, "signal-integration", "v1")
	second := demandFixture(t, "signal-integration", "v2")
	if err := store.SaveSignal(context.Background(), tenant.String(), first, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSignal(context.Background(), tenant.String(), second, "v1"); err != nil {
		t.Fatal(err)
	}
	versions, err := store.ListSignalVersions(context.Background(), tenant.String(), first.SignalID)
	if err != nil || len(versions) != 2 || versions[0].Version != "v1" || versions[1].Version != "v2" {
		t.Fatalf("signal history = %v, %v", versions, err)
	}
	base := scenarioFixture(t, "scenario-integration", 1, nil)
	next := scenarioFixture(t, "scenario-integration", 2, &base)
	if err := store.SaveScenarioRevision(context.Background(), tenant.String(), base, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveScenarioRevision(context.Background(), tenant.String(), next, 1); err != nil {
		t.Fatal(err)
	}
	history, err := store.ListScenarioRevisions(context.Background(), tenant.String(), base.ScenarioID)
	if err != nil || len(history) != 2 || history[1].ParentRevision != 1 {
		t.Fatalf("scenario history = %v, %v", history, err)
	}
}

func TestTodo_PERSIST_PLANNING_001_Security(t *testing.T) {
	db := planningDB(t)
	alpha, beta := planningTenant(t, db, "planning-alpha"), planningTenant(t, db, "planning-beta")
	alphaStore := planningstore.New(planningConn(t, db), alpha)
	signal := demandFixture(t, "signal-security", "v1")
	if err := alphaStore.SaveSignal(context.Background(), alpha.String(), signal, ""); err != nil {
		t.Fatal(err)
	}
	betaStore := planningstore.New(planningConn(t, db), beta)
	if _, err := betaStore.LoadSignal(context.Background(), beta.String(), signal.SignalID, signal.Version); !errors.Is(err, planningstore.ErrNotFound) {
		t.Fatalf("cross-tenant load = %v, want ErrNotFound", err)
	}
	conn := planningConn(t, db)
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, beta); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"demand_signal", "coverage_requirement", "scenario_revision"} {
		var count int
		if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("beta saw %d rows in %s", count, table)
		}
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_PERSIST_PLANNING_001_Recovery(t *testing.T) {
	db := planningDB(t)
	tenant := planningTenant(t, db, "planning-recovery")
	signal := demandFixture(t, "signal-recovery", "v1")
	first := planningstore.New(planningConn(t, db), tenant)
	if err := first.SaveSignal(context.Background(), tenant.String(), signal, ""); err != nil {
		t.Fatal(err)
	}
	fresh := planningstore.New(planningConn(t, db), tenant)
	loaded, err := fresh.LoadSignal(context.Background(), tenant.String(), signal.SignalID, signal.Version)
	if err != nil || loaded.CanonicalDigest != signal.CanonicalDigest {
		t.Fatalf("fresh connection signal = %v, %v", loaded, err)
	}
}

func TestTodo_PERSIST_PLANNING_001_Mutation(t *testing.T) {
	db := planningDB(t)
	tenant := planningTenant(t, db, "planning-mutation")
	store := planningstore.New(planningConn(t, db), tenant)
	signal := demandFixture(t, "signal-mutation", "v1")
	coverage := coverageFixture(t, signal, "v1")
	base := scenarioFixture(t, "scenario-mutation", 1, nil)
	if err := store.SaveSignal(context.Background(), tenant.String(), signal, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCoverageRequirement(context.Background(), tenant.String(), coverage, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveScenarioRevision(context.Background(), tenant.String(), base, 0); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"demand_signal", "coverage_requirement", "scenario_revision"} {
		if err := db.ExecErr("UPDATE "+table+" SET tenant_id=$1 WHERE tenant_id=$1", tenant); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("UPDATE %s = %v, want append-only refusal", table, err)
		}
		if err := db.ExecErr("DELETE FROM "+table+" WHERE tenant_id=$1", tenant); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("DELETE %s = %v, want append-only refusal", table, err)
		}
	}
}

func TestPlanningStoreImplementsDomainPort(t *testing.T) {
	var _ demand.Store = planningstore.New(nil, uuid.New())
}
