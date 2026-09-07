package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/monstercameron/hcm-next/internal/platform/buildinfo"
)

// Deps bundles the platform plumbing Run resolves before invoking
// Spec.Build, so a composition-root command builds its actual roles from
// these instead of constructing its own config/logger/health/database
// wiring.
type Deps struct {
	// Role is Spec.Role, echoed here for convenience.
	Role Role
	// Values is the parsed, precedence-resolved configuration.
	Values *Values
	// Logger is Spec.Logger, or the default if Spec left it nil.
	Logger Logger
	// Clock is Spec.Clock, or time.Now if Spec left it nil.
	Clock Clock
	// Health is the process's health/readiness state machine. Build may
	// read it but should not normally Set it directly; Run drives the
	// STARTING/READY/DRAINING/STOPPED transitions itself.
	Health *Health
	// DB is the database pool Run opened via Spec.DBPoolFactory, or nil if
	// Spec.DatabaseURLField was empty (this role has no database
	// dependency).
	DB DBPool
	// Identity is the resolved WorkloadIdentity reference for this process
	// instance.
	Identity string
}

// Runtime is what Spec.Build returns: the concurrent workloads Run should
// start under its run-group, plus any additional ordered shutdown steps
// beyond the ones Run appends automatically (draining those workloads,
// stopping the health endpoint, closing the database pool).
type Runtime struct {
	Workloads []Workload
	Shutdown  []ShutdownStep
}

// Spec declares everything Run needs to bring up, run and gracefully tear
// down one process role. A composition-root command (cmd/worker,
// cmd/projector, cmd/hcmnext, cmd/migrate, ...) constructs one Spec and
// calls `os.Exit(bootstrap.Run(ctx, spec))`; it owns no lifecycle mechanics
// of its own.
type Spec struct {
	// Role is this process's role, validated against the
	// process-roles.yaml vocabulary before anything else runs.
	Role Role

	// Args is the command-line arguments to parse (typically
	// os.Args[1:]).
	Args []string
	// Getenv is the environment-variable source. Nil means os.LookupEnv.
	Getenv EnvLookup
	// Stdout/Stderr receive the build-identity banner and any pre-logging
	// config error. Nil means os.Stdout/os.Stderr respectively.
	Stdout io.Writer
	Stderr io.Writer
	// Logger receives every structured lifecycle event Run emits. Nil
	// means slog.Default().
	Logger Logger
	// Clock is Run's time source. Nil means time.Now.
	Clock Clock
	// Identity resolves this process instance's opaque workload identity
	// reference. Nil means a role-name-plus-PID default.
	Identity WorkloadIdentity
	// Telemetry is called on every Health state transition. Nil means no
	// export beyond the structured log.
	Telemetry TelemetryHook

	// ConfigFields declares every flag/env-backed configuration value this
	// role accepts.
	ConfigFields []Field
	// Validate runs once, immediately after parsing, before any listener
	// or workload starts. A non-nil error is a config failure.
	Validate func(*Values) error

	// DatabaseURLField, if non-empty, names the ConfigFields entry holding
	// the PostgreSQL connection URL; Run resolves it and calls
	// DBPoolFactory before invoking Build. Empty means this role has no
	// database dependency.
	DatabaseURLField string
	// DBPoolFactory constructs the database pool. Nil means
	// PgxPoolFactory.
	DBPoolFactory DBPoolFactory

	// Build constructs the role's actual workloads and any
	// role-specific shutdown steps from the resolved Deps. Nil means an
	// empty Runtime (no workloads) — a degenerate but valid role that
	// starts, becomes ready, and waits only for a shutdown trigger.
	Build func(ctx context.Context, deps Deps) (Runtime, error)

	// ShutdownSignals are the OS signals that trigger graceful shutdown.
	// Nil means [os.Interrupt, syscall.SIGTERM].
	ShutdownSignals []os.Signal
	// ShutdownDeadline bounds the entire ordered shutdown sequence.
	// Non-positive means DefaultShutdownDeadline.
	ShutdownDeadline time.Duration

	// HealthAddr, if non-empty, is a loopback-only "host:port" (host must
	// be 127.0.0.1, localhost or ::1) Run serves the health/readiness
	// endpoint on. Empty disables the HTTP endpoint; Health is always
	// available as a value regardless.
	HealthAddr string
}

// Run resolves configuration, brings the role up, waits for a shutdown
// trigger (an OS signal, every workload finishing, or a workload failing),
// runs the ordered deadline-bounded shutdown sequence, and returns the
// process exit code. It never calls os.Exit itself; callers do
// `os.Exit(bootstrap.Run(ctx, spec))`.
func Run(ctx context.Context, spec Spec) int {
	stdout := spec.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := spec.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	logger := spec.Logger
	if logger == nil {
		logger = defaultLogger()
	}
	clock := spec.Clock
	if clock == nil {
		clock = defaultClock()
	}
	telemetry := spec.Telemetry
	if telemetry == nil {
		telemetry = func(Role, HealthState, map[string]any) {}
	}

	info := buildinfo.Current()
	fmt.Fprintf(stdout, "%s %s revision=%s modified=%t go=%s\n", spec.Role, info.Module, info.Revision, info.Modified, info.GoVersion)

	if err := spec.Role.Validate(); err != nil {
		fmt.Fprintf(stderr, "bootstrap: %v\n", err)
		return ExitCodeFor(&ConfigError{Err: err})
	}

	values, err := ParseConfig(spec.Args, spec.Getenv, spec.ConfigFields)
	if err != nil {
		fmt.Fprintf(stderr, "bootstrap: %v\n", err)
		return ExitCodeFor(&ConfigError{Err: err})
	}
	if spec.Validate != nil {
		if err := spec.Validate(values); err != nil {
			fmt.Fprintf(stderr, "bootstrap: config validation: %v\n", err)
			return ExitCodeFor(&ConfigError{Err: err})
		}
	}

	identity, err := resolveIdentity(spec)
	if err != nil {
		fmt.Fprintf(stderr, "bootstrap: workload identity: %v\n", err)
		return ExitCodeFor(&ConfigError{Err: err})
	}

	health := NewHealth()
	startAttrs := append([]any{
		"role", string(spec.Role),
		"identity", identity,
		"revision", info.Revision,
		"config_fingerprint", values.Fingerprint(),
	}, values.LogAttrs()...)
	logger.Info("bootstrap.starting", startAttrs...)
	telemetry(spec.Role, StateStarting, map[string]any{"identity": identity})

	var healthServer *http.Server
	if spec.HealthAddr != "" {
		if err := validateLoopbackAddr(spec.HealthAddr); err != nil {
			logger.Error("bootstrap.health_addr_invalid", "error", err.Error())
			return ExitCodeFor(&ConfigError{Err: err})
		}
		ln, listenErr := net.Listen("tcp", spec.HealthAddr)
		if listenErr != nil {
			logger.Error("bootstrap.health_listen_failed", "error", listenErr.Error())
			return ExitCodeFor(&ConfigError{Err: fmt.Errorf("health endpoint: %w", listenErr)})
		}
		healthServer = &http.Server{Handler: health.EndpointHandler()}
		go func() {
			if serveErr := healthServer.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				logger.Error("bootstrap.health_server_failed", "error", serveErr.Error())
			}
		}()
	}

	var db DBPool
	if spec.DatabaseURLField != "" {
		factory := spec.DBPoolFactory
		if factory == nil {
			factory = PgxPoolFactory
		}
		pool, dbErr := factory(ctx, values.String(spec.DatabaseURLField))
		if dbErr != nil {
			logger.Error("bootstrap.database_unavailable", "error", dbErr.Error())
			stopHealthServer(healthServer)
			return ExitCodeFor(dbErr)
		}
		db = pool
	}

	deps := Deps{Role: spec.Role, Values: values, Logger: logger, Clock: clock, Health: health, DB: db, Identity: identity}

	var rt Runtime
	if spec.Build != nil {
		rt, err = spec.Build(ctx, deps)
		if err != nil {
			logger.Error("bootstrap.build_failed", "error", err.Error())
			if db != nil {
				db.Close()
			}
			stopHealthServer(healthServer)
			return ExitCodeFor(&ConfigError{Err: err})
		}
	}

	g := newGroup(ctx)
	for _, w := range rt.Workloads {
		g.goRun(w)
	}

	if setErr := health.Set(StateReady); setErr != nil {
		logger.Error("bootstrap.health_transition_failed", "error", setErr.Error())
	}
	telemetry(spec.Role, StateReady, nil)
	logger.Info("bootstrap.ready", "role", string(spec.Role))

	signals := spec.ShutdownSignals
	if signals == nil {
		signals = []os.Signal{os.Interrupt, syscall.SIGTERM}
	}
	sigCtx, stopSignals := signal.NotifyContext(ctx, signals...)
	defer stopSignals()

	groupDone := make(chan error, 1)
	go func() { groupDone <- g.wait() }()

	var triggerErr error
	groupFinished := false
	select {
	case <-sigCtx.Done():
		logger.Info("bootstrap.signal_received", "role", string(spec.Role))
	case triggerErr = <-groupDone:
		groupFinished = true
		if triggerErr != nil {
			logger.Error("bootstrap.role_failed", "error", triggerErr.Error())
		} else {
			logger.Info("bootstrap.workloads_completed", "role", string(spec.Role))
		}
	}

	if setErr := health.Set(StateDraining); setErr != nil {
		logger.Error("bootstrap.health_transition_failed", "error", setErr.Error())
	}
	telemetry(spec.Role, StateDraining, nil)
	logger.Info("bootstrap.draining", "role", string(spec.Role))

	// Ensure every still-running workload observes cancellation, whether
	// shutdown was triggered by a signal or by a sibling's failure.
	g.cancel()

	steps := make([]ShutdownStep, 0, len(rt.Shutdown)+3)
	steps = append(steps, ShutdownStep{
		Name: "drain-workloads",
		Run: func(stepCtx context.Context) error {
			if groupFinished {
				return triggerErr
			}
			select {
			case err := <-groupDone:
				return err
			case <-stepCtx.Done():
				return stepCtx.Err()
			}
		},
	})
	steps = append(steps, rt.Shutdown...)
	if healthServer != nil {
		steps = append(steps, ShutdownStep{Name: "stop-health-endpoint", Run: healthServer.Shutdown})
	}
	if db != nil {
		steps = append(steps, ShutdownStep{Name: "close-database-pool", Run: func(context.Context) error {
			db.Close()
			return nil
		}})
	}

	shutdownErr := runShutdown(context.WithoutCancel(ctx), logger, steps, spec.ShutdownDeadline)

	if setErr := health.Set(StateStopped); setErr != nil {
		logger.Error("bootstrap.health_transition_failed", "error", setErr.Error())
	}
	telemetry(spec.Role, StateStopped, nil)
	logger.Info("bootstrap.stopped", "role", string(spec.Role))

	switch {
	case triggerErr != nil:
		return ExitCodeFor(triggerErr)
	case shutdownErr != nil:
		return ExitCodeFor(shutdownErr)
	default:
		return ExitOK
	}
}

// resolveIdentity applies Spec.Identity, or the role-plus-PID default when
// Spec left it nil.
func resolveIdentity(spec Spec) (string, error) {
	if spec.Identity == nil {
		return fmt.Sprintf("role:%s:pid:%d", spec.Role, os.Getpid()), nil
	}
	return spec.Identity()
}

// validateLoopbackAddr rejects any HealthAddr that is not explicitly bound
// to a loopback host, so bootstrap never exposes the health endpoint beyond
// the local machine.
func validateLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("health address %q: %w", addr, err)
	}
	switch host {
	case "127.0.0.1", "localhost", "::1":
		return nil
	default:
		return fmt.Errorf("health address %q must bind a loopback host (127.0.0.1, localhost or ::1), got %q", addr, host)
	}
}

// stopHealthServer is used on early-exit paths (after the health endpoint
// started but before workloads did) to avoid leaking the listener.
func stopHealthServer(s *http.Server) {
	if s == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.Shutdown(ctx)
}
