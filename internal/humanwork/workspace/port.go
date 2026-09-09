package workspace

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Refusals a [Cell] reports. They are sentinels rather than typed envelopes
// because this package must not depend on the transport error model to know
// what happened; the handler projects them onto HTTP status codes and the
// application layer projects the same conditions onto its own envelope for
// the RPC surface.
var (
	// ErrDenied means the evaluated authorization policy refuses this caller
	// the subject entirely. It is distinct from a masked field: a masked
	// field still yields a page.
	ErrDenied = errors.New("workspace: the authorization policy denies this read")
	// ErrWorkerUnknown means the reference does not resolve to a worker this
	// cell can read.
	ErrWorkerUnknown = errors.New("workspace: no such worker")
)

// Reading is everything one live cell answered for one [Request].
//
// It carries domain results, not rendering decisions: which of them becomes a
// visible field, a finding or a timeline entry is this package's business,
// and what they mean is the owning domain's. The one judgement the cell does
// make is CompensationDisclosed, because only the cell evaluated the policy.
type Reading struct {
	// Tenant and Worker are the server-resolved identity the read ran under.
	Tenant string
	Worker values.EntityRef

	// Explanation is the governed worker-state read every other answer rests
	// on. Its per-field Access is the authoritative statement of what this
	// caller may see, and it is what [Visibility] is derived from.
	Explanation people.Explanation

	// CompensationDisclosed reports whether the pay-bearing capabilities ran.
	// When it is false the caller's purpose carries no compensation grant,
	// PayBand/Compensation/Preflight/Simulation are zero, and
	// CompensationDenial names the policy's own reason.
	CompensationDisclosed bool
	CompensationDenial    string

	PayBand      rewards.PayBandEvaluation
	Compensation rewards.SimulateCompensationResult
	Preflight    promotion.PreflightResult
	Simulation   promotion.SimulationResult

	// EvidenceIDs are the capability-gateway invocation evidence identifiers
	// this reading produced, in invocation order.
	EvidenceIDs []string

	// PolicyVersion is the authorization policy the read was decided under.
	PolicyVersion string
	// CapabilityID and CapabilityVersion identify the governed capability
	// that produced the promotion answer.
	CapabilityID      string
	CapabilityVersion string
	// AsOf is the instant the cell stamped the answer at.
	AsOf time.Time
}

// Cell is the live P1A cell this workspace reads through.
//
// It is one method rather than five because the five governed calls are
// ordered and interdependent: the worker read pins the baseline every later
// answer is computed against, and the authorization decision that gates the
// compensation half is made once and applied to all of it. Exposing them
// separately would let a caller assemble a page from answers that were
// authorized under different decisions.
//
// The implementation is internal/intent/app, which owns the capability
// gateway, the authorization evaluator and the corpus ports. This package
// never imports it: the port is declared here, in the consumer, so that the
// workspace can be rendered and tested against a stub without composing a
// cell.
type Cell interface {
	// ReadPromotion runs the governed read/preflight/simulate chain for one
	// promotion question, on behalf of the principal in ctx, and writes
	// nothing.
	ReadPromotion(ctx context.Context, req Request) (Reading, error)
}
