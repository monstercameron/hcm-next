package recover

import "context"

// Phase names one persistence boundary of a recovery. There are exactly
// four, and together they cover every point at which a worker's death
// changes what the durable rows say -- which is WF-RUN-003's REFACTOR
// clause read literally.
type Phase string

// The four declared persistence boundaries. Each one is the gap immediately
// after the transaction (or dispatch) named before it, so a crash at the
// phase leaves exactly the state that phase's name describes.
const (
	// PhaseBeforeNodeStateCommit is inside TX1, after the lease takeover and
	// the attempt rows have been written and before they are committed. A
	// crash here leaves nothing at all: the takeover and the new attempt roll
	// back together.
	PhaseBeforeNodeStateCommit Phase = "BEFORE_NODE_STATE_COMMIT"
	// PhaseAfterNodeStateCommitBeforeDispatch is after TX1 committed and
	// before TX2 begins. A crash here leaves the new fence and the scheduled
	// attempt durable, and no effect dispatched.
	PhaseAfterNodeStateCommitBeforeDispatch Phase = "AFTER_NODE_STATE_COMMIT_BEFORE_DISPATCH"
	// PhaseAfterDispatchBeforeResultCommit is after TX2 committed and before
	// TX3 begins. A crash here leaves the business effect and its TX-006
	// record durable and the advancement not applied -- the case that makes
	// replay rather than re-execution the only correct recovery.
	PhaseAfterDispatchBeforeResultCommit Phase = "AFTER_DISPATCH_BEFORE_RESULT_COMMIT"
	// PhaseAfterResultCommit is after TX3 committed. A crash here has lost
	// nothing: the next recovery finds the node finished and has nothing to
	// do.
	PhaseAfterResultCommit Phase = "AFTER_RESULT_COMMIT"
)

// phaseOrder is the declared order the boundaries occur in. It is the single
// place that order is stated, so [Phases] and [Phase.Valid] cannot disagree
// with each other or with [Recoverer.Recover].
var phaseOrder = []Phase{
	PhaseBeforeNodeStateCommit,
	PhaseAfterNodeStateCommitBeforeDispatch,
	PhaseAfterDispatchBeforeResultCommit,
	PhaseAfterResultCommit,
}

// Phases returns the four declared boundaries in the order a recovery
// crosses them. A test that walks every failpoint walks this slice, so a
// fifth boundary added later is covered without editing the test.
func Phases() []Phase { return append([]Phase(nil), phaseOrder...) }

// Valid reports whether p is a declared boundary.
func (p Phase) Valid() bool {
	for _, known := range phaseOrder {
		if known == p {
			return true
		}
	}
	return false
}

// Failpoint decides, deterministically, whether the worker dies at a
// boundary. It is a port rather than a build tag so that the FAULT case is
// an ordinary test with ordinary control flow: returning a non-nil error is
// the process death, and [Recoverer.Recover] treats it exactly as it treats
// a machine that stopped -- it abandons the recovery without committing
// anything further.
//
// An implementation must be deterministic: WF-RUN-003 asks for crash
// failpoints at every persistence boundary, not for random fault injection,
// and a boundary that only sometimes crashes proves nothing about the
// boundary next to it.
type Failpoint interface {
	Check(ctx context.Context, phase Phase) error
}

// NoFailpoint never crashes. It is the default when [Options.Failpoints] is
// nil, so an ordinary recovery needs no failpoint wiring at all.
type NoFailpoint struct{}

// Check implements [Failpoint].
func (NoFailpoint) Check(context.Context, Phase) error { return nil }

var _ Failpoint = NoFailpoint{}

// CrashAt is the deterministic single-shot failpoint the FAULT case drives:
// it crashes the first time the recovery reaches Phase and never again, so
// the same value can be handed to a retry loop and the retry proceeds.
//
// It is a pointer receiver because "the first time" is state. It is not safe
// for concurrent use, and it does not need to be: one recovery is one
// sequential call.
type CrashAt struct {
	// Phase is the boundary to crash at.
	Phase Phase
	// fired records that the crash already happened once.
	fired bool
}

// Check implements [Failpoint].
func (c *CrashAt) Check(_ context.Context, phase Phase) error {
	if c == nil || c.Phase != phase || c.fired {
		return nil
	}
	c.fired = true
	return errCrashInjected
}

// Fired reports whether this failpoint has already crashed once.
func (c *CrashAt) Fired() bool { return c != nil && c.fired }

var _ Failpoint = (*CrashAt)(nil)

// errCrashInjected is the marker [CrashAt] returns. [Recoverer] wraps it
// into a typed [Error] carrying the boundary, so a caller reads the phase
// off the error rather than off the failpoint it configured.
var errCrashInjected = errCrash{}

type errCrash struct{}

func (errCrash) Error() string { return "injected crash" }
