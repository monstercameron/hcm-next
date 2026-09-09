package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
)

// noEnv isolates every configuration test from the real process environment.
func noEnv(string) (string, bool) { return "", false }

func parse(t *testing.T, args ...string) *bootstrap.Values {
	t.Helper()
	values, err := bootstrap.ParseConfig(args, noEnv, ConfigFields())
	if err != nil {
		t.Fatalf("ParseConfig(%v): %v", args, err)
	}
	return values
}

func minimalArgs(tenantID uuid.UUID) []string {
	return []string{"-database-url=postgres://localhost/hcmnext", "-tenant-id=" + tenantID.String()}
}

// TestConfigFieldsAreFreshAndComplete proves two things at once: the field
// list is a function so no two callers share a backing array, and every
// declared field carries the usage text and environment override an operator
// needs to configure the role without reading this source.
func TestConfigFieldsAreFreshAndComplete(t *testing.T) {
	first, second := ConfigFields(), ConfigFields()
	if len(first) == 0 {
		t.Fatal("ConfigFields declares nothing")
	}
	first[0].Name = "mutated"
	if second[0].Name == "mutated" {
		t.Fatal("two ConfigFields calls share one backing array")
	}

	want := map[string]bool{
		FieldDatabaseURL: true, FieldTenantID: true, FieldQueueKey: true, FieldWorkloadRef: true,
		FieldPollInterval: true, FieldQueueLeaseTTL: true, FieldInstanceLeaseTTL: true,
		FieldBatchSize: true, FieldMisfirePolicy: true, FieldMisfireGrace: true, FieldHealthAddr: true,
	}
	for _, field := range ConfigFields() {
		if !want[field.Name] {
			t.Errorf("undeclared configuration field %q", field.Name)
		}
		delete(want, field.Name)
		if field.Usage == "" {
			t.Errorf("field %q has no usage text", field.Name)
		}
		if field.Env == "" {
			t.Errorf("field %q has no environment override", field.Name)
		}
		if field.Name == FieldDatabaseURL && !field.Secret {
			t.Error("the database URL is not marked secret; it would be logged")
		}
	}
	for name := range want {
		t.Errorf("configuration field %q is missing", name)
	}
}

// TestValidateConfigAcceptsTheMinimalConfiguration proves a replica needs only
// a database and a tenant: every other setting has a working default.
func TestValidateConfigAcceptsTheMinimalConfiguration(t *testing.T) {
	values := parse(t, minimalArgs(uuid.New())...)
	if err := ValidateConfig(values); err != nil {
		t.Fatalf("ValidateConfig on the minimal configuration: %v", err)
	}
	if got := values.String(FieldQueueKey); got != DefaultQueueKey {
		t.Errorf("queue key default = %q, want %q", got, DefaultQueueKey)
	}
	if got := values.String(FieldWorkloadRef); got != DefaultWorkloadRef {
		t.Errorf("workload reference default = %q, want %q", got, DefaultWorkloadRef)
	}
	if !strings.HasPrefix(values.String(FieldWorkloadRef), "workload:") {
		t.Error("the default workload reference is not scheme-qualified; lease.Identity refuses a bare hostname")
	}
}

// TestValidateConfigRefusesAnUnusableConfiguration covers every refusal that
// happens before a listener or a tick starts, which is SVC-002's contract: an
// invalid configuration never reaches a running workload.
func TestValidateConfigRefusesAnUnusableConfiguration(t *testing.T) {
	tenantID := uuid.New()
	for name, args := range map[string][]string{
		"no database url":       {"-tenant-id=" + tenantID.String()},
		"no tenant":             {"-database-url=postgres://localhost/x"},
		"empty queue":           append(minimalArgs(tenantID), "-queue-key="),
		"empty workload":        append(minimalArgs(tenantID), "-workload-ref="),
		"zero poll interval":    append(minimalArgs(tenantID), "-poll-interval=0s"),
		"negative queue lease":  append(minimalArgs(tenantID), "-queue-lease=-1s"),
		"zero instance lease":   append(minimalArgs(tenantID), "-instance-lease=0s"),
		"unparsable duration":   append(minimalArgs(tenantID), "-misfire-grace=soon"),
		"zero batch size":       append(minimalArgs(tenantID), "-batch-size=0"),
		"unparsable batch size": append(minimalArgs(tenantID), "-batch-size=lots"),
		"undeclared misfire":    append(minimalArgs(tenantID), "-misfire-policy=WHENEVER"),
	} {
		t.Run(name, func(t *testing.T) {
			values, err := bootstrap.ParseConfig(args, noEnv, ConfigFields())
			if err != nil {
				return // the flag set refused it even earlier, which is also a refusal
			}
			if err := ValidateConfig(values); err == nil {
				t.Fatalf("ValidateConfig accepted %s", name)
			}
		})
	}
}

func TestMisfireFromBuildsTheDeclaredPolicy(t *testing.T) {
	values := parse(t, append(minimalArgs(uuid.New()), "-misfire-policy=SKIP", "-misfire-grace=90m")...)
	cfg, err := MisfireFrom(values)
	if err != nil {
		t.Fatalf("MisfireFrom: %v", err)
	}
	if cfg.Policy != schedule.MisfireSkip || cfg.Grace != 90*time.Minute {
		t.Fatalf("misfire = %+v, want SKIP within 90m", cfg)
	}
}

// TestMisfireFromDefaultsToCatchUpOnce records the default an operator gets by
// saying nothing: an overdue promise is kept exactly once rather than replayed
// for every occurrence it slept through or dropped silently.
func TestMisfireFromDefaultsToCatchUpOnce(t *testing.T) {
	cfg, err := MisfireFrom(parse(t, minimalArgs(uuid.New())...))
	if err != nil {
		t.Fatalf("MisfireFrom: %v", err)
	}
	if cfg.Policy != schedule.MisfireCatchUpOnce {
		t.Fatalf("default misfire policy = %q, want CATCH_UP_ONCE", cfg.Policy)
	}
}

func TestIdentityNamesTheWorkloadAndTheReplica(t *testing.T) {
	deps := bootstrap.Deps{
		Values:   parse(t, append(minimalArgs(uuid.New()), "-workload-ref=workload:hcmnext-scheduler-canary")...),
		Identity: "role:scheduler:pid:4242",
	}
	id := Identity(deps)
	if id.WorkloadRef != "workload:hcmnext-scheduler-canary" || id.InstanceRef != "role:scheduler:pid:4242" {
		t.Fatalf("identity = %+v, want the configured workload and this process instance", id)
	}
}

// fakePool satisfies bootstrap.DBPool and this package's Beginner, so
// BuildRuntime can be exercised without a database server.
type fakePool struct{ failingBeginner }

func (fakePool) Ping(context.Context) error { return nil }
func (fakePool) Close()                     {}

// pingOnlyPool satisfies bootstrap.DBPool but cannot open transactions, which
// is the wiring mistake BuildRuntime has to refuse.
type pingOnlyPool struct{}

func (pingOnlyPool) Ping(context.Context) error { return nil }
func (pingOnlyPool) Close()                     {}

func depsFor(t *testing.T, pool bootstrap.DBPool, args ...string) bootstrap.Deps {
	t.Helper()
	return bootstrap.Deps{
		Role:     bootstrap.RoleScheduler,
		Values:   parse(t, args...),
		Logger:   &recordingLogger{},
		Clock:    func() time.Time { return fixtureAt },
		DB:       pool,
		Identity: "role:scheduler:pid:1",
	}
}

// TestBuildRuntimeComposesTheOneWorkload proves the command's whole remaining
// job is choosing the role: everything from the resolved configuration to the
// running loop happens here, and the loop stops when bootstrap cancels it.
func TestBuildRuntimeComposesTheOneWorkload(t *testing.T) {
	tenantID := uuid.New()
	logger := &recordingLogger{}
	deps := depsFor(t, fakePool{failingBeginner{err: errors.New("no database in this test")}},
		append(minimalArgs(tenantID), "-poll-interval=5ms")...)
	deps.Logger = logger

	runtime, err := BuildRuntime(deps, nil, claimFixture(tenantID, "replica:a"))
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	if len(runtime.Workloads) != 1 || runtime.Workloads[0].Name != "workflow-scheduler" {
		t.Fatalf("runtime = %+v, want exactly one named workflow-scheduler workload", runtime.Workloads)
	}
	if logger.count("scheduler.configured") != 1 {
		t.Fatal("BuildRuntime reported no effective configuration")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Workloads[0].Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("workload returned %v, want nil on cancellation", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the workload did not return after its context was canceled")
	}
}

// TestBuildRuntimeRefusesAPoolThatCannotBeginATransaction is the wiring
// mistake worth naming: bootstrap's default pool exposes only Ping/Close, and
// a scheduler that cannot open a transaction cannot claim anything.
func TestBuildRuntimeRefusesAPoolThatCannotBeginATransaction(t *testing.T) {
	tenantID := uuid.New()
	deps := depsFor(t, pingOnlyPool{}, minimalArgs(tenantID)...)
	_, err := BuildRuntime(deps, nil, claimFixture(tenantID, "replica:a"))
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("err = %v, want ErrConfig", err)
	}
}

// TestBuildRuntimeRefusesAClaimItCannotServe proves the validation in New is
// not bypassed by the bootstrap path.
func TestBuildRuntimeRefusesAClaimItCannotServe(t *testing.T) {
	tenantID := uuid.New()
	deps := depsFor(t, fakePool{failingBeginner{err: errors.New("unused")}}, minimalArgs(tenantID)...)
	bad := claimFixture(tenantID, "replica:a")
	bad.Resource = lease.Resource{Kind: lease.ResourceWorkItem, ID: "item:1"}
	if _, err := BuildRuntime(deps, nil, bad); !errors.Is(err, ErrConfig) {
		t.Fatalf("err = %v, want ErrConfig", err)
	}
}
