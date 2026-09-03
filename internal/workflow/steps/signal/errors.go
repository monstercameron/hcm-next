package signal

import "errors"

// Sentinel errors. All are matchable with errors.Is. These mark malformed
// inputs — a caller/compiler bug — as distinct from the typed [Status]
// refusals Accept returns for a well-formed but non-conforming signal.
var (
	// ErrSubscriptionIdentityRequired says a SignalSubscription did not name
	// the tenant/instance/node it belongs to.
	ErrSubscriptionIdentityRequired = errors.New("signal: tenant, workflow instance and node id are required")
	// ErrSubscriptionCorrelationRequired says a SignalSubscription declared
	// no event type or correlation key.
	ErrSubscriptionCorrelationRequired = errors.New("signal: event type and correlation key are required")
	// ErrSubscriptionSchemaRequired says a SignalSubscription declared no
	// expected schema ref.
	ErrSubscriptionSchemaRequired = errors.New("signal: expected schema ref is required")
	// ErrSubscriptionSourcesRequired says a SignalSubscription declared no
	// accepted sources; a subscription that accepts anyone is not a
	// subscription.
	ErrSubscriptionSourcesRequired = errors.New("signal: at least one accepted source is required")
	// ErrSubscriptionOrderingInvalid says a SignalSubscription declared an
	// unrecognized ordering expectation.
	ErrSubscriptionOrderingInvalid = errors.New("signal: unknown ordering expectation")
	// ErrVerifierRequired says Accept was called with no Verifier.
	ErrVerifierRequired = errors.New("signal: a signature verifier is required")
	// ErrNowRequired says Accept was called without a valid caller-supplied
	// "now".
	ErrNowRequired = errors.New("signal: a valid caller-supplied instant is required")
	// ErrPriorEntryWrongSubscription says a prior log entry supplied for
	// dedupe/ordering belongs to a different subscription than the one being
	// checked.
	ErrPriorEntryWrongSubscription = errors.New("signal: a prior log entry belongs to a different subscription")
)
