package reconcile

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
)

// Executor is the database capability this package needs: a transaction the
// caller opened and has already scoped with internal/data/tenancy.WithTenant.
// It is exactly [runtimestate.Executor], restated so a caller of this package
// does not have to name that package to call it.
type Executor = runtimestate.Executor

// ObservationResult is what one [Observer] attempt found. It carries no
// business meaning of its own: Found and Freshness say whether and how
// reliably the external system was read, and CanonicalRef, when non-empty, is
// the reference a [Comparer] should treat as the current canonical state.
type ObservationResult struct {
	// Found reports whether the external system named a canonical state at
	// all. A false Found with a FreshnessUnavailable freshness is an object
	// that could not be read; a false Found with any other freshness is a
	// read that succeeded but found nothing to observe.
	Found bool
	// CanonicalRef is the observed reference, when Found. Left empty, a
	// job's stored CanonicalRef is unchanged.
	CanonicalRef string
	// Freshness is the observation's own reliability verdict.
	// internal/connectivity/observe.Classify or an adapter over it produces
	// this; this package never invents one.
	Freshness observe.Freshness
}

// Observer performs one observation attempt for a job's watched effect and
// reports what it found. It is the only way this package learns anything
// about an external system: [Coordinator] never reads a connector, a
// provider or domain state directly, only through this port.
type Observer interface {
	Observe(ctx context.Context, ex Executor, job Job, now time.Time) (ObservationResult, error)
}

// Verdict is what one [Comparer] call decided about a job's most recent
// observation. Status must be one of [StatusPass], [StatusMismatch],
// [StatusPartial] or [StatusUnknown] -- [validVerdictStatus] refuses any
// other value, because the job-lifecycle-only statuses (PENDING, OBSERVING,
// EXPIRED, REPAIR_REQUIRED) are this package's to assign, never a comparer's.
//
// NextCheckAt is read only when Status is StatusUnknown, which is a resting
// verdict rather than a final one: it names when the job should be polled
// again. Left zero, [Coordinator.Poll] applies its own backoff.
type Verdict struct {
	Status      Status
	NextCheckAt time.Time
}

// Comparer evaluates a job's most recent observation against what was
// intended and reports a verdict. RECON-002 owns the actual comparison
// policy -- expected population, authority, tolerance, coverage, effect
// criticality -- and this package only calls the port; it never evaluates
// policy itself and never mutates domain or provider state to do so.
type Comparer interface {
	Compare(ctx context.Context, ex Executor, job Job, obs ObservationResult, now time.Time) (Verdict, error)
}

// FenceVerifier is the lease-fence check every [Coordinator] write performs
// before touching the durable job row. Its signature matches
// [lease.Manager.Verify] exactly, so a production composition root can hand
// a [lease.Manager] value straight to [Coordinator.Fences] with no adapter:
// the port is declared here, beside the consumer, and [lease.Manager]
// satisfies it because its shape already matches, not because this package
// or that one declared a dependency on the other's concrete type.
type FenceVerifier interface {
	Verify(ctx context.Context, ex Executor, fence lease.Fence, now time.Time) (lease.Held, error)
}

var _ FenceVerifier = lease.Manager{}
