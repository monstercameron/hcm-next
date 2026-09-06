package integrationmeta

import (
	"errors"
	"fmt"
)

var (
	// ErrNilTenant is returned when a row omits its tenant or its own
	// identifier: a tenant-scoped row with no tenant is never writable.
	ErrNilTenant = errors.New("integrationmeta: tenant or row id is nil")
	// ErrRawSecret is returned when a credential reference is not a
	// reference, or when a configuration object carries a secret-bearing
	// key (DB-014 RED).
	ErrRawSecret = errors.New("integrationmeta: raw secret rejected, expected a governed secret reference")
	// ErrMissingCorrelation is returned when a receipt, item or operation
	// omits the correlation, idempotency or dedupe key that makes it
	// replay-safe.
	ErrMissingCorrelation = errors.New("integrationmeta: correlation, dedupe or idempotency key missing")
	// ErrMissingVersion is returned when a versioned row omits its version.
	ErrMissingVersion = errors.New("integrationmeta: version missing or not positive")
	// ErrMissingDeadline is returned when an operation or observation omits
	// its deadline or freshness horizon.
	ErrMissingDeadline = errors.New("integrationmeta: deadline or freshness horizon missing")
	// ErrMissingDigest is returned when a payload digest is absent.
	ErrMissingDigest = errors.New("integrationmeta: content digest missing")
	// ErrMissingClassification is returned when a row omits the
	// classification its downstream handling depends on.
	ErrMissingClassification = errors.New("integrationmeta: classification missing")
	// ErrProviderAcceptanceNotCompletion is returned when a caller tries to
	// mark an operation business-complete without the observation that
	// confirmed the external state (DB-014 RED).
	ErrProviderAcceptanceNotCompletion = errors.New("integrationmeta: provider acceptance is not business completion")
	// ErrInvalidInterval is returned for a reversed or empty time interval.
	ErrInvalidInterval = errors.New("integrationmeta: time interval invalid")
	// ErrInvalidEnum is returned for a value outside its declared set.
	ErrInvalidEnum = errors.New("integrationmeta: value outside its declared set")
)

// ErrDetail carries the field or reason behind one of the sentinels above so
// a failure names what went wrong without inventing a new sentinel per field.
type ErrDetail struct {
	Sentinel error
	Detail   string
}

func (e ErrDetail) Error() string { return fmt.Sprintf("%v: %s", e.Sentinel, e.Detail) }

func (e ErrDetail) Unwrap() error { return e.Sentinel }

func detail(sentinel error, format string, args ...any) error {
	return ErrDetail{Sentinel: sentinel, Detail: fmt.Sprintf(format, args...)}
}

// secretKeys are the configuration keys a connection may never carry. The
// schema's connector_connection_configuration_has_no_secret constraint holds
// the identical list; this copy exists so the failure is a typed Go error
// before it is a constraint violation.
var secretKeys = []string{
	"secret", "password", "token", "api_key", "apiKey",
	"private_key", "privateKey", "client_secret", "clientSecret",
}
