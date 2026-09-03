// Package bootstrap implements the SVC-002 shared Go process-bootstrap
// package (planning/todos.md `SVC-002`): one narrow entry point,
// `bootstrap.Run(ctx, Spec)`, that every composition-root command (cmd/
// hcmnext, cmd/worker, cmd/projector, cmd/migrate, and later cmd/scheduler,
// cmd/admin) calls instead of hand-rolling its own flag parsing, signal
// handling, health state and shutdown ordering.
//
// Run supplies:
//
//   - Typed flag/env configuration with explicit precedence
//     (flag > environment variable > default) and a printable effective
//     configuration with secret fields redacted (see Field, ParseConfig,
//     Values).
//   - A build-identity banner sourced from internal/platform/buildinfo,
//     plus a config fingerprint, so every process announces exactly what
//     code and configuration it is running (Values.Fingerprint).
//   - Structured startup/shutdown lifecycle events emitted through a small
//     Logger port (satisfied directly by *log/slog.Logger; no dependency on
//     internal/platform/logging, which is a sibling in-flight package).
//   - OS signal handling (SIGINT/SIGTERM by default) that triggers one
//     ordered, deadline-bounded graceful shutdown sequence — the same
//     sequence a workload failure or explicit context cancellation drives,
//     so tests exercise it without sending real OS signals.
//   - A STARTING -> READY -> DRAINING -> STOPPED health/readiness state
//     machine (Health), exposed as a plain value and, optionally, as a
//     loopback-only HTTP endpoint.
//   - A database pool factory port (DBPool/DBPoolFactory) with a pgxpool
//     default and an in-memory fake (FakeDBPool) for tests that never touch
//     a real PostgreSQL server.
//   - A run-group (Workload) that runs a role's concurrent goroutines,
//     cancels every sibling on the first error, and converts a panicking
//     workload into a returned error instead of a hung process or a crashed
//     binary with no exit code.
//   - Exit-code mapping (ExitCodeFor) so a command's main() can do exactly
//     `os.Exit(bootstrap.Run(ctx, spec))`.
//
// Scope note: bootstrap owns process lifecycle mechanics only. It never
// registers domain/business behavior, never decides what a role does, and
// never imports a business package; a command's Spec.Build hook supplies
// the actual workloads. internal/platform/{logging,config,timeauth,
// telemetry} are separate, concurrently developed packages; this package
// defines small ports (Logger, EnvLookup, Clock) instead of importing them,
// so they can be plugged in once they exist without changing this API.
//
// Refs: planning/specs/go-only-technology-constitution.md,
// planning/specs/data/models/operations-production.md,
// definitions/architecture/process-roles.yaml.
package bootstrap
