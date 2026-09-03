package bootstrap

import "errors"

// Process exit codes Run returns. A command's main() does exactly
// `os.Exit(bootstrap.Run(ctx, spec))`.
const (
	// ExitOK means the process ran and shut down cleanly.
	ExitOK = 0
	// ExitRuntimeError means a workload failed, panicked, or a dependency
	// (for example the database pool) could not be established.
	ExitRuntimeError = 1
	// ExitConfigError means Spec/flag/env validation failed before any
	// listener or workload started.
	ExitConfigError = 2
	// ExitShutdownTimeout means the shutdown sequence did not finish
	// within its deadline.
	ExitShutdownTimeout = 3
)

// ConfigError wraps an error discovered while resolving configuration
// (flag parsing, Spec.Validate, an invalid Role, an invalid HealthAddr).
// ExitCodeFor maps it to ExitConfigError. Run guarantees a ConfigError is
// always returned before any listener or workload starts.
type ConfigError struct{ Err error }

func (e *ConfigError) Error() string { return e.Err.Error() }
func (e *ConfigError) Unwrap() error { return e.Err }

// ExitCodeFor classifies err into one of the Exit* codes above. nil maps to
// ExitOK.
func ExitCodeFor(err error) int {
	if err == nil {
		return ExitOK
	}
	var cfgErr *ConfigError
	if errors.As(err, &cfgErr) {
		return ExitConfigError
	}
	var deadlineErr *ErrShutdownDeadlineExceeded
	if errors.As(err, &deadlineErr) {
		return ExitShutdownTimeout
	}
	return ExitRuntimeError
}
