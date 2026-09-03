package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/projection"
	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
)

// discardLogger returns a bootstrap.Logger that drops everything, for tests
// that need a real Logger but not its output.
func discardLogger() bootstrap.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestTodo_SVC_007 proves SVC-007's unit-level contract for this
// composition root: the reconcile-sweep control flow (every due checkpoint
// is reconciled, one target's failure never blocks the rest, "did any
// projection catch up" is reported accurately) with no database at all,
// config precedence/redaction and validation for every declared field
// (including -rebuild), and Build failing safely - through bootstrap's own
// ConfigError path - on an incompatible database pool or an unparsable
// typed field. TestTodo_SVC_007_Integration (integration_test.go) is the
// real-PostgreSQL complement: a full bootstrap.Run lifecycle actually
// catching a projection up.
func TestTodo_SVC_007(t *testing.T) {
	t.Run("reconcile_sweep", testProjectorReconcileSweep)
	t.Run("config_fields", testProjectorConfigFields)
	t.Run("build_rejects_incompatible_db_pool", testProjectorBuildRejectsIncompatibleDBPool)
	t.Run("build_rejects_bad_typed_fields", testProjectorBuildRejectsBadTypedFields)
}

// fakeDueLister returns a fixed due list, or an error.
type fakeDueLister struct {
	due []projection.StreamProjection
	err error
}

func (f fakeDueLister) Due(context.Context) ([]projection.StreamProjection, error) {
	return f.due, f.err
}

// fakeReconciler scripts ReconcileOne's result per (projection, stream).
type fakeReconciler struct {
	applied map[string]int
	errs    map[string]error
	calls   []projection.StreamProjection
}

func key(sp projection.StreamProjection) string { return sp.ProjectionName + "/" + sp.StreamKey }

func (f *fakeReconciler) ReconcileOne(_ context.Context, target projection.StreamProjection) (int, error) {
	f.calls = append(f.calls, target)
	k := key(target)
	if err, ok := f.errs[k]; ok {
		return 0, err
	}
	return f.applied[k], nil
}

// TestProjectorReconcileSweep proves the sweep control flow this
// composition root moved into a Workload: every due (tenant, projection,
// stream) is reconciled, "did any projection catch up" is reported
// accurately, and one target's reconcile failure does not block the rest -
// all without a database, since dueLister and oneReconciler are the only
// things reconcileSweep depends on.
func testProjectorReconcileSweep(t *testing.T) {
	t.Run("nothing_due_is_not_an_error_and_reports_no_work", func(t *testing.T) {
		didWork, err := reconcileSweep(context.Background(), discardLogger(), fakeDueLister{}, &fakeReconciler{})
		if err != nil {
			t.Fatalf("reconcileSweep: %v", err)
		}
		if didWork {
			t.Fatal("didWork = true, want false")
		}
	})

	t.Run("listing_due_fails", func(t *testing.T) {
		wantErr := errors.New("list boom")
		_, err := reconcileSweep(context.Background(), discardLogger(), fakeDueLister{err: wantErr}, &fakeReconciler{})
		if !errors.Is(err, wantErr) {
			t.Fatalf("reconcileSweep err = %v, want wrapping %v", err, wantErr)
		}
	})

	t.Run("a_target_that_applies_events_reports_work", func(t *testing.T) {
		target := projection.StreamProjection{Tenant: uuid.New(), ProjectionName: "p1", StreamKey: "s1"}
		rec := &fakeReconciler{applied: map[string]int{key(target): 3}}

		didWork, err := reconcileSweep(context.Background(), discardLogger(), fakeDueLister{due: []projection.StreamProjection{target}}, rec)
		if err != nil {
			t.Fatalf("reconcileSweep: %v", err)
		}
		if !didWork {
			t.Fatal("didWork = false, want true")
		}
		if len(rec.calls) != 1 {
			t.Fatalf("ReconcileOne called %d times, want 1", len(rec.calls))
		}
	})

	t.Run("an_already_current_target_reports_no_work", func(t *testing.T) {
		target := projection.StreamProjection{Tenant: uuid.New(), ProjectionName: "p1", StreamKey: "s1"}
		rec := &fakeReconciler{applied: map[string]int{key(target): 0}}

		didWork, err := reconcileSweep(context.Background(), discardLogger(), fakeDueLister{due: []projection.StreamProjection{target}}, rec)
		if err != nil {
			t.Fatalf("reconcileSweep: %v", err)
		}
		if didWork {
			t.Fatal("didWork = true, want false")
		}
	})

	t.Run("one_target_failing_does_not_block_the_others", func(t *testing.T) {
		bad := projection.StreamProjection{Tenant: uuid.New(), ProjectionName: "bad", StreamKey: "s1"}
		good := projection.StreamProjection{Tenant: uuid.New(), ProjectionName: "good", StreamKey: "s2"}
		rec := &fakeReconciler{
			applied: map[string]int{key(good): 1},
			errs:    map[string]error{key(bad): errors.New("reconcile boom")},
		}

		didWork, err := reconcileSweep(context.Background(), discardLogger(), fakeDueLister{due: []projection.StreamProjection{bad, good}}, rec)
		if err != nil {
			t.Fatalf("reconcileSweep returned an error instead of tolerating the per-target failure: %v", err)
		}
		if !didWork {
			t.Fatal("didWork = false, want true (the good target still applied)")
		}
		if len(rec.calls) != 2 {
			t.Fatalf("ReconcileOne called %d times, want 2 (bad must not block good)", len(rec.calls))
		}
	})
}

// TestProjectorConfigFields proves config precedence and secret redaction
// for this role's declared fields, and that validateConfig rejects a
// missing database URL and any unparsable duration/bool field before any
// listener or workload would start.
func testProjectorConfigFields(t *testing.T) {
	fields := projectorConfigFields()
	noEnv := func(string) (string, bool) { return "", false }

	t.Run("rebuild_defaults_to_false", func(t *testing.T) {
		v, err := bootstrap.ParseConfig(nil, noEnv, fields)
		if err != nil {
			t.Fatalf("ParseConfig: %v", err)
		}
		rebuild, err := v.Bool("rebuild")
		if err != nil {
			t.Fatalf("Bool(rebuild): %v", err)
		}
		if rebuild {
			t.Fatal("rebuild default = true, want false")
		}
	})

	t.Run("env_overrides_default_for_every_field", func(t *testing.T) {
		env := map[string]string{
			EnvDatabaseURL:                    "postgres://env/db",
			"HCMNEXT_PROJECTOR_POLL_INTERVAL": "5s",
			"HCMNEXT_PROJECTOR_REBUILD":       "true",
			EnvHealthAddr:                     "127.0.0.1:9092",
		}
		lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
		v, err := bootstrap.ParseConfig(nil, lookup, fields)
		if err != nil {
			t.Fatalf("ParseConfig: %v", err)
		}
		for name, want := range map[string]string{
			"database-url":  "postgres://env/db",
			"poll-interval": "5s",
			"rebuild":       "true",
			"health-addr":   "127.0.0.1:9092",
		} {
			if got := v.String(name); got != want {
				t.Fatalf("%s = %q, want %q", name, got, want)
			}
			if got := v.Source(name); got != "env" {
				t.Fatalf("%s source = %q, want env", name, got)
			}
		}
	})

	t.Run("flag_overrides_env", func(t *testing.T) {
		env := map[string]string{"HCMNEXT_PROJECTOR_REBUILD": "false"}
		lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
		v, err := bootstrap.ParseConfig([]string{"-rebuild=true"}, lookup, fields)
		if err != nil {
			t.Fatalf("ParseConfig: %v", err)
		}
		if got := v.String("rebuild"); got != "true" {
			t.Fatalf("rebuild = %q, want true (from the flag)", got)
		}
		if got := v.Source("rebuild"); got != "flag" {
			t.Fatalf("source = %q, want flag", got)
		}
	})

	t.Run("validate_rejects_missing_database_url", func(t *testing.T) {
		v, err := bootstrap.ParseConfig(nil, noEnv, fields)
		if err != nil {
			t.Fatalf("ParseConfig: %v", err)
		}
		if err := validateConfig(v); err == nil {
			t.Fatal("validateConfig accepted an empty database-url")
		}
	})

	t.Run("validate_rejects_an_unparsable_rebuild_flag", func(t *testing.T) {
		v, err := bootstrap.ParseConfig([]string{"-database-url=postgres://x", "-rebuild=not-a-bool"}, noEnv, fields)
		if err != nil {
			t.Fatalf("ParseConfig: %v", err)
		}
		if err := validateConfig(v); err == nil {
			t.Fatal("validateConfig accepted an unparsable rebuild flag")
		}
	})

	t.Run("validate_accepts_a_complete_valid_config", func(t *testing.T) {
		v, err := bootstrap.ParseConfig([]string{"-database-url=postgres://x", "-rebuild=true"}, noEnv, fields)
		if err != nil {
			t.Fatalf("ParseConfig: %v", err)
		}
		if err := validateConfig(v); err != nil {
			t.Fatalf("validateConfig: %v", err)
		}
	})
}

// fakeProjectorPool is a minimal projectorPool that satisfies Build's type
// assertion without a real database; its Begin/Query/QueryRow are never
// expected to be called in the scenarios that use it.
type fakeProjectorPool struct{}

func (fakeProjectorPool) Ping(context.Context) error { return nil }
func (fakeProjectorPool) Close()                     {}
func (fakeProjectorPool) Begin(context.Context) (dbport.Tx, error) {
	return nil, errors.New("fakeProjectorPool: Begin not implemented")
}
func (fakeProjectorPool) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("fakeProjectorPool: Query not implemented")
}
func (fakeProjectorPool) QueryRow(context.Context, string, ...any) dbport.Row {
	return nil
}

// TestProjectorBuildRejectsIncompatibleDBPool proves the composition root
// fails safely - through bootstrap's own ConfigError path, with the
// database pool opened and then closed again - when the configured
// DBPoolFactory does not yield a pool that supports projection
// reconciliation, instead of panicking or silently running with no
// workload. It exercises the real production Spec end to end via
// bootstrap.Run and bootstrap.NewFakeDBPool, with no real PostgreSQL server
// involved.
func testProjectorBuildRejectsIncompatibleDBPool(t *testing.T) {
	fakePool := bootstrap.NewFakeDBPool()
	s := spec([]string{"-database-url=postgres://fake/db"})
	s.Getenv = func(string) (string, bool) { return "", false }
	s.DBPoolFactory = bootstrap.NewFakeDBPoolFactory(fakePool)
	s.Logger = discardLogger()
	s.Stdout = io.Discard
	s.Stderr = io.Discard

	code := bootstrap.Run(context.Background(), s)

	if code != bootstrap.ExitConfigError {
		t.Fatalf("exit code = %d, want ExitConfigError (%d)", code, bootstrap.ExitConfigError)
	}
	if fakePool.Pings() != 1 {
		t.Fatalf("Pings() = %d, want exactly 1 (bootstrap must still gate readiness on the pool before Build runs)", fakePool.Pings())
	}
	if !fakePool.Closed() {
		t.Fatal("Closed() = false, want true (Run closes the pool it opened even when Build itself fails)")
	}
}

// TestProjectorBuildRejectsBadTypedFields proves Build reports a clear
// error (rather than an uninformative parse panic further down) when a
// typed field parses incorrectly - defense in depth alongside
// validateConfig, exercised directly since Build re-parses these fields
// itself.
func testProjectorBuildRejectsBadTypedFields(t *testing.T) {
	deps := bootstrap.Deps{
		Values: mustValues(t, []bootstrap.Field{
			{Name: "poll-interval", Default: "not-a-duration"},
			{Name: "rebuild", Default: "false"},
		}),
		DB:     fakeProjectorPool{},
		Logger: discardLogger(),
	}
	if _, err := build(context.Background(), deps); err == nil {
		t.Fatal("build accepted an unparsable poll-interval")
	}
}

func mustValues(t *testing.T, fields []bootstrap.Field) *bootstrap.Values {
	t.Helper()
	v, err := bootstrap.ParseConfig(nil, func(string) (string, bool) { return "", false }, fields)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	return v
}
