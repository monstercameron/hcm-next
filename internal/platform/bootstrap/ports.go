package bootstrap

import (
	"log/slog"
	"os"
	"time"
)

// Logger is the narrow structured-logging port bootstrap depends on. It is
// satisfied directly by *log/slog.Logger (Info/Error already have this
// exact signature), so a command wires the real
// internal/platform/logging.Handler in through slog.New(...) without any
// adapter, and tests pass a small fake. Bootstrap never imports
// internal/platform/logging itself.
type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}

// defaultLogger returns the package-level slog logger used when a Spec
// leaves Logger nil. It is stdlib-only so bootstrap has no dependency on
// any sibling platform package.
func defaultLogger() Logger { return slog.Default() }

// Clock returns the current time. Production code leaves Spec.Clock nil
// (time.Now); tests inject a fixed or controllable Clock so timing-sensitive
// assertions (deadlines, timestamps) are deterministic.
type Clock func() time.Time

func defaultClock() Clock { return time.Now }

// EnvLookup reads one environment variable, reporting whether it was set.
// Production code leaves Spec.Getenv nil (os.LookupEnv); tests inject a map
// -backed lookup so config-precedence assertions never depend on the real
// process environment.
type EnvLookup func(key string) (string, bool)

func defaultEnvLookup() EnvLookup { return os.LookupEnv }

// WorkloadIdentity returns an opaque reference identifying this running
// process instance (for example "principal:worker:<uuid>"), attached as an
// attribute to every lifecycle event bootstrap logs. It is a hook: issuing a
// real, attested workload identity belongs to internal/platform/trust once
// that package exists. Spec.Identity may be nil, in which case bootstrap
// derives a reference from the role name and process ID.
type WorkloadIdentity func() (string, error)

// TelemetryHook is invoked on every Health state transition bootstrap
// drives, so a future internal/platform/telemetry exporter can turn process
// lifecycle into spans/metrics without bootstrap importing it directly.
// Spec.Telemetry may be nil, in which case transitions are simply not
// exported anywhere but the structured log.
type TelemetryHook func(role Role, state HealthState, attrs map[string]any)
