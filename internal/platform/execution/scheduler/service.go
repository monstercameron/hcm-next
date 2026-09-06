package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/schedule"
	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
	"github.com/monstercameron/hcm-next/internal/workflow/lease"
)

// The environment variables this role's configuration is sourced from. They
// follow cmd/worker's and cmd/projector's convention exactly: the shared
// database URL, and one HCMNEXT_SCHEDULER_* variable per role-specific
// setting.
const (
	EnvDatabaseURL      = "HCMNEXT_DATABASE_URL"
	EnvHealthAddr       = "HCMNEXT_SCHEDULER_HEALTH_ADDR"
	EnvTenantID         = "HCMNEXT_SCHEDULER_TENANT_ID"
	EnvQueueKey         = "HCMNEXT_SCHEDULER_QUEUE_KEY"
	EnvWorkloadRef      = "HCMNEXT_SCHEDULER_WORKLOAD_REF"
	EnvPollInterval     = "HCMNEXT_SCHEDULER_POLL_INTERVAL"
	EnvQueueLeaseTTL    = "HCMNEXT_SCHEDULER_QUEUE_LEASE"
	EnvInstanceLeaseTTL = "HCMNEXT_SCHEDULER_INSTANCE_LEASE"
	EnvBatchSize        = "HCMNEXT_SCHEDULER_BATCH_SIZE"
	EnvMisfirePolicy    = "HCMNEXT_SCHEDULER_MISFIRE_POLICY"
	EnvMisfireGrace     = "HCMNEXT_SCHEDULER_MISFIRE_GRACE"
)

// The configuration field names, so a command names them once.
const (
	FieldDatabaseURL      = "database-url"
	FieldHealthAddr       = "health-addr"
	FieldTenantID         = "tenant-id"
	FieldQueueKey         = "queue-key"
	FieldWorkloadRef      = "workload-ref"
	FieldPollInterval     = "poll-interval"
	FieldQueueLeaseTTL    = "queue-lease"
	FieldInstanceLeaseTTL = "instance-lease"
	FieldBatchSize        = "batch-size"
	FieldMisfirePolicy    = "misfire-policy"
	FieldMisfireGrace     = "misfire-grace"
)

// DefaultQueueKey is the queue resource a scheduler replica leases when no
// other is configured. It matches the resource id
// internal/workflow/timer's own end-to-end fixture leases, so a single-queue
// deployment needs no configuration at all beyond the tenant.
const DefaultQueueKey = "queue:workflow-runtime"

// DefaultWorkloadRef is the scheme-qualified workload this process's leases
// are held by. internal/workflow/lease refuses a bare hostname: a lease holder
// names the deployed workload, not the machine it landed on.
const DefaultWorkloadRef = "workload:hcmnext-scheduler"

// ConfigFields declares every flag/env-backed value the scheduler role
// accepts. It is a function rather than a package variable so two callers
// never share (and risk mutating) one backing array.
func ConfigFields() []bootstrap.Field {
	return []bootstrap.Field{
		{
			Name:   FieldDatabaseURL,
			Env:    EnvDatabaseURL,
			Usage:  "PostgreSQL connection URL (" + EnvDatabaseURL + " if unset)",
			Kind:   bootstrap.KindString,
			Secret: true,
		},
		{
			Name:  FieldTenantID,
			Env:   EnvTenantID,
			Usage: "UUID of the tenant whose durable workflow work this replica dispatches",
			Kind:  bootstrap.KindString,
		},
		{
			Name:    FieldQueueKey,
			Env:     EnvQueueKey,
			Usage:   "queue resource this replica leases; replicas of one fleet shard by giving different values",
			Default: DefaultQueueKey,
			Kind:    bootstrap.KindString,
		},
		{
			Name:    FieldWorkloadRef,
			Env:     EnvWorkloadRef,
			Usage:   "scheme-qualified workload reference every lease this replica takes is held by",
			Default: DefaultWorkloadRef,
			Kind:    bootstrap.KindString,
		},
		{
			Name:    FieldPollInterval,
			Env:     EnvPollInterval,
			Usage:   "how long to sleep between ticks that found no due work",
			Default: DefaultPollInterval.String(),
			Kind:    bootstrap.KindDuration,
		},
		{
			Name:    FieldQueueLeaseTTL,
			Env:     EnvQueueLeaseTTL,
			Usage:   "how long this replica's queue lease is good for before another replica may take it over",
			Default: DefaultQueueTTL.String(),
			Kind:    bootstrap.KindDuration,
		},
		{
			Name:    FieldInstanceLeaseTTL,
			Env:     EnvInstanceLeaseTTL,
			Usage:   "how long a claimed instance stays leased before an abandoned claim may be recovered",
			Default: DefaultInstanceTTL.String(),
			Kind:    bootstrap.KindDuration,
		},
		{
			Name:    FieldBatchSize,
			Env:     EnvBatchSize,
			Usage:   "maximum units of ready work one tick claims per queue",
			Default: strconv.Itoa(DefaultBatchSize),
			Kind:    bootstrap.KindInt,
		},
		{
			Name:    FieldMisfirePolicy,
			Env:     EnvMisfirePolicy,
			Usage:   "what to do with an overdue timer: FIRE_NOW, SKIP, CATCH_UP, CATCH_UP_ONCE, CATCH_UP_ALL or REVIEW",
			Default: string(schedule.MisfireCatchUpOnce),
			Kind:    bootstrap.KindString,
		},
		{
			Name:    FieldMisfireGrace,
			Env:     EnvMisfireGrace,
			Usage:   "how late a timer may be before the misfire policy applies",
			Default: time.Hour.String(),
			Kind:    bootstrap.KindDuration,
		},
		{
			Name:  FieldHealthAddr,
			Env:   EnvHealthAddr,
			Usage: "loopback host:port (127.0.0.1, localhost or ::1) to serve the health/readiness endpoint on; empty disables it",
			Kind:  bootstrap.KindString,
		},
	}
}

// ValidateConfig fails configuration resolution -- before any listener or tick
// starts -- on anything this package can judge without naming an identifier
// type. The tenant id's own syntax is the command's to check, because parsing
// it needs github.com/google/uuid, which this root may not import; the check
// here is only that one was supplied at all.
func ValidateConfig(v *bootstrap.Values) error {
	if v.String(FieldDatabaseURL) == "" {
		return fmt.Errorf("%s is not set; pass -%s or set the environment variable", EnvDatabaseURL, FieldDatabaseURL)
	}
	if v.String(FieldTenantID) == "" {
		return fmt.Errorf("%s is not set; a scheduler replica serves a named tenant", EnvTenantID)
	}
	if v.String(FieldQueueKey) == "" {
		return fmt.Errorf("-%s must name the queue resource this replica leases", FieldQueueKey)
	}
	if v.String(FieldWorkloadRef) == "" {
		return fmt.Errorf("-%s must name the workload this replica's leases are held by", FieldWorkloadRef)
	}
	for _, name := range []string{FieldPollInterval, FieldQueueLeaseTTL, FieldInstanceLeaseTTL, FieldMisfireGrace} {
		d, err := v.Duration(name)
		if err != nil {
			return err
		}
		if d <= 0 {
			return fmt.Errorf("-%s must be positive", name)
		}
	}
	size, err := v.Int(FieldBatchSize)
	if err != nil {
		return err
	}
	if size <= 0 {
		return fmt.Errorf("-%s must be positive", FieldBatchSize)
	}
	if _, err := MisfireFrom(v); err != nil {
		return err
	}
	return nil
}

// MisfireFrom builds the declared misfire policy from resolved configuration.
func MisfireFrom(v *bootstrap.Values) (schedule.MisfireConfig, error) {
	grace, err := v.Duration(FieldMisfireGrace)
	if err != nil {
		return schedule.MisfireConfig{}, err
	}
	cfg := schedule.MisfireConfig{
		Policy: schedule.MisfirePolicy(v.String(FieldMisfirePolicy)),
		Grace:  grace,
	}
	if err := cfg.Validate(); err != nil {
		return schedule.MisfireConfig{}, fmt.Errorf("-%s: %w", FieldMisfirePolicy, err)
	}
	return cfg, nil
}

// Identity is the lease holder identity one replica takes every lease under:
// the configured workload reference plus the process instance reference
// bootstrap already resolved for this process.
func Identity(deps bootstrap.Deps) lease.Identity {
	return lease.Identity{
		WorkloadRef: deps.Values.String(FieldWorkloadRef),
		InstanceRef: deps.Identity,
	}
}

// BuildRuntime is the scheduler role's whole composition: it turns resolved
// bootstrap dependencies plus the claims the command resolved (each naming a
// tenant, a queue and this replica's holder identity) into the single Workload
// bootstrap.Run supervises.
//
// It is here rather than in cmd/scheduler so that every function the command
// calls is exercised by this package's own tests; the command's remaining job
// is parsing its flags and choosing this role.
func BuildRuntime(deps bootstrap.Deps, dispatcher Dispatcher, claims ...lease.AcquireRequest) (bootstrap.Runtime, error) {
	db, ok := deps.DB.(Beginner)
	if !ok {
		return bootstrap.Runtime{}, fmt.Errorf(
			"%w: database pool %T cannot open transactions (Begin)", ErrConfig, deps.DB)
	}
	poll, err := deps.Values.Duration(FieldPollInterval)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	queueTTL, err := deps.Values.Duration(FieldQueueLeaseTTL)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	instanceTTL, err := deps.Values.Duration(FieldInstanceLeaseTTL)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	batch, err := deps.Values.Int(FieldBatchSize)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	misfire, err := MisfireFrom(deps.Values)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	s, err := New(Config{
		DB:          db,
		Claims:      claims,
		Misfire:     misfire,
		Dispatcher:  dispatcher,
		Clock:       func() time.Time { return deps.Clock().UTC() },
		BatchSize:   batch,
		QueueTTL:    queueTTL,
		InstanceTTL: instanceTTL,
		Logger:      deps.Logger,
	})
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	deps.Logger.Info("scheduler.configured",
		"queues", len(claims), "poll_interval", poll.String(), "queue_lease", queueTTL.String(),
		"instance_lease", instanceTTL.String(), "batch_size", batch,
		"misfire_policy", string(misfire.Policy), "dispatch", dispatcher != nil)

	return bootstrap.Runtime{Workloads: []bootstrap.Workload{{
		Name: "workflow-scheduler",
		Run:  func(ctx context.Context) error { return s.Run(ctx, poll) },
	}}}, nil
}
