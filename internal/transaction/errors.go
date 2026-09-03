package transaction

import "errors"

// Sentinel causes. Classify with [errors.Is]; never by matching strings.
var (
	// ErrInvalidBoundary reports a ConsistencyBoundary that cannot resolve
	// anything: no id, no tenant, no cell, no coordinator, no admission
	// selector, or an unspecified isolation level, commit protocol or
	// cross-boundary disposition.
	ErrInvalidBoundary = errors.New("transaction: invalid consistency boundary")

	// ErrInvalidResolution reports a resolution attempt over a plan that
	// cannot be resolved: no plan id, no participant, a participant with no
	// id/stream/storage class, a duplicate participant id, or a resolution
	// that admitted nothing into the local ACID set.
	ErrInvalidResolution = errors.New("transaction: invalid consistency boundary resolution")

	// ErrTenantMismatch reports a plan whose tenant is outside the
	// boundary's own tenant: the plan and boundary do not share a
	// tenant/cell/database boundary at all.
	ErrTenantMismatch = errors.New("transaction: plan tenant is outside the consistency boundary")

	// ErrStaleCoordinatorEpoch reports a resolution attempted under a
	// coordinator epoch older than the one the boundary is currently fenced
	// to. A stale epoch never resolves, even if every participant would
	// otherwise admit cleanly.
	ErrStaleCoordinatorEpoch = errors.New("transaction: coordinator epoch is stale")

	// ErrUnsupportedStream reports a participant the plan declared Local
	// whose stream/storage class matches no admission selector of the
	// boundary. It fails resolution outright rather than being silently
	// admitted or silently demoted to an effect.
	ErrUnsupportedStream = errors.New("transaction: participant stream is unsupported by this boundary")

	// ErrBoundaryOverCapacity reports a plan with more participants than the
	// boundary's declared admission cap.
	ErrBoundaryOverCapacity = errors.New("transaction: participant count exceeds the boundary's admission cap")
)
