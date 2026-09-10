package cycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// LifecycleState is the governed state of a cycle revision. UNOPENED exists
// only to make OPEN an explicit transition; NewOpenCycle is available when a
// caller is restoring a cycle that was already opened by an earlier ledger.
type LifecycleState string

const (
	StateUnopened LifecycleState = "UNOPENED"
	StateOpen     LifecycleState = "OPEN"
	StateClosed   LifecycleState = "CLOSED"
	StateLocked   LifecycleState = "LOCKED"
)

// TransitionOperation is both the typed event kind and the phase capability
// token required for that operation.
type TransitionOperation string

const (
	OperationOpen   TransitionOperation = "OPEN"
	OperationClose  TransitionOperation = "CLOSE"
	OperationLock   TransitionOperation = "LOCK"
	OperationReopen TransitionOperation = "REOPEN"

	TransitionOpen   = OperationOpen
	TransitionClose  = OperationClose
	TransitionLock   = OperationLock
	TransitionReopen = OperationReopen
)

var (
	ErrTransitionState     = errors.New("cycle: transition is not allowed from the current state")
	ErrTransitionReason    = errors.New("cycle: transition requires a reason")
	ErrTransitionRequester = errors.New("cycle: transition requires a requester")
	ErrTransitionApprover  = errors.New("cycle: reopen requires a distinct approver")
	ErrTransitionPhase     = errors.New("cycle: operation is not allowed by the active phase")
	ErrTransitionTime      = errors.New("cycle: transition requires an instant")
	ErrStaleTransition     = errors.New("cycle: transition CAS expectation is stale")
	ErrTransitionRevision  = errors.New("cycle: reopen revision could not be created")
)

// TransitionRequest is the authority-bearing input for one lifecycle change.
// Expected fields are optional for the value API and enforced by Ledger when
// supplied; a caller using shared state should provide them for CAS safety.
type TransitionRequest struct {
	Operation              TransitionOperation
	Requester              string
	Approver               string
	Reason                 string
	At                     time.Time
	ActivePhase            CompiledPhase
	ExpectedSequence       int
	ExpectedDigest         string
	ExpectedRevisionDigest string
	Counts                 map[string]int
	Exceptions             []string
	SnapshotVersions       map[string]string
	NewRevisionID          string
	NewRevisionVersion     string
}

// TransitionEvent is the append-only evidence for one accepted transition.
// Digest covers every field except Digest itself and is therefore stable for
// equal inputs across processes.
type TransitionEvent struct {
	Sequence              int
	Operation             TransitionOperation
	FromState             LifecycleState
	ToState               LifecycleState
	Requester             string
	Approver              string
	Reason                string
	At                    time.Time
	PhaseID               string
	BeforeRevisionDigest  string
	AfterRevisionDigest   string
	BeforeRevisionVersion string
	AfterRevisionVersion  string
	Counts                map[string]int
	Exceptions            []string
	SnapshotVersions      map[string]string
	Digest                string
}

// Validate checks the event's typed minimum evidence.
func (e TransitionEvent) Validate() error {
	if e.Sequence <= 0 || e.Operation == "" || e.FromState == "" || e.ToState == "" {
		return ErrTransitionState
	}
	if strings.TrimSpace(e.Requester) == "" || strings.TrimSpace(e.Reason) == "" {
		return ErrTransitionReason
	}
	if e.At.IsZero() || e.BeforeRevisionDigest == "" || e.AfterRevisionDigest == "" || e.Digest == "" {
		return ErrTransitionTime
	}
	if e.Operation == OperationReopen && (strings.TrimSpace(e.Approver) == "" || e.Approver == e.Requester) {
		return ErrTransitionApprover
	}
	if e.Digest != digestEvent(e) {
		return ErrTransitionState
	}
	return nil
}

// CanonicalDigest returns the event digest.
func (e TransitionEvent) CanonicalDigest() string { return e.Digest }

// GovernedCycle is an immutable point-in-time lifecycle view. Transition
// returns a new value and never rewrites the receiver.
type GovernedCycle struct {
	Revision        Revision
	State           LifecycleState
	Sequence        int
	LastEventDigest string
}

// NewGovernedCycle creates an explicit UNOPENED lifecycle for a valid cycle
// revision.
func NewGovernedCycle(revision Revision) (GovernedCycle, error) {
	if revision.Digest == "" || revision.ID == "" || revision.Version == "" {
		return GovernedCycle{}, ErrRevision
	}
	return GovernedCycle{Revision: revision, State: StateUnopened}, nil
}

// NewOpenCycle creates a view for a revision that is already open.
func NewOpenCycle(revision Revision) (GovernedCycle, error) {
	c, err := NewGovernedCycle(revision)
	if err != nil {
		return GovernedCycle{}, err
	}
	c.State = StateOpen
	return c, nil
}

// CanonicalDigest identifies the lifecycle view, including state and event
// sequence, while retaining the underlying cycle revision digest.
func (c GovernedCycle) CanonicalDigest() string {
	b, _ := json.Marshal(struct {
		Revision, State string
		Sequence        int
		Last            string
	}{c.Revision.Digest, string(c.State), c.Sequence, c.LastEventDigest})
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Transition validates and applies one operation to this immutable view.
func (c GovernedCycle) Transition(req TransitionRequest) (GovernedCycle, TransitionEvent, error) {
	if req.Operation == "" {
		return GovernedCycle{}, TransitionEvent{}, ErrTransitionState
	}
	if strings.TrimSpace(req.Requester) == "" {
		return GovernedCycle{}, TransitionEvent{}, ErrTransitionRequester
	}
	if strings.TrimSpace(req.Reason) == "" {
		return GovernedCycle{}, TransitionEvent{}, ErrTransitionReason
	}
	if req.At.IsZero() {
		return GovernedCycle{}, TransitionEvent{}, ErrTransitionTime
	}
	if req.ExpectedSequence > 0 && req.ExpectedSequence != c.Sequence {
		return GovernedCycle{}, TransitionEvent{}, ErrStaleTransition
	}
	if req.ExpectedDigest != "" && req.ExpectedDigest != c.CanonicalDigest() {
		return GovernedCycle{}, TransitionEvent{}, ErrStaleTransition
	}
	if req.ExpectedRevisionDigest != "" && req.ExpectedRevisionDigest != c.Revision.Digest {
		return GovernedCycle{}, TransitionEvent{}, ErrStaleTransition
	}
	if !phaseAllows(req.ActivePhase, string(req.Operation)) {
		return GovernedCycle{}, TransitionEvent{}, fmt.Errorf("%w: phase %q does not declare %s", ErrTransitionPhase, req.ActivePhase.ID, req.Operation)
	}

	// Every operation arm assigns the target state, so there is no
	// meaningful initializer; the default arm returns.
	var to LifecycleState
	switch req.Operation {
	case OperationOpen:
		if c.State != StateUnopened {
			return GovernedCycle{}, TransitionEvent{}, fmt.Errorf("%w: OPEN from %s", ErrTransitionState, c.State)
		}
		to = StateOpen
	case OperationClose:
		if c.State != StateOpen {
			return GovernedCycle{}, TransitionEvent{}, fmt.Errorf("%w: CLOSE from %s", ErrTransitionState, c.State)
		}
		to = StateClosed
	case OperationLock:
		if c.State != StateClosed {
			return GovernedCycle{}, TransitionEvent{}, fmt.Errorf("%w: LOCK from %s", ErrTransitionState, c.State)
		}
		to = StateLocked
	case OperationReopen:
		if c.State != StateClosed {
			return GovernedCycle{}, TransitionEvent{}, fmt.Errorf("%w: REOPEN from %s", ErrTransitionState, c.State)
		}
		if strings.TrimSpace(req.Approver) == "" || req.Approver == req.Requester {
			return GovernedCycle{}, TransitionEvent{}, ErrTransitionApprover
		}
		to = StateOpen
	default:
		return GovernedCycle{}, TransitionEvent{}, fmt.Errorf("%w: unknown operation %q", ErrTransitionState, req.Operation)
	}

	nextRevision := c.Revision
	if req.Operation == OperationReopen {
		var err error
		nextRevision, err = reopenRevision(c.Revision, req, c.Sequence+1)
		if err != nil {
			return GovernedCycle{}, TransitionEvent{}, err
		}
	}
	event := TransitionEvent{
		Sequence: c.Sequence + 1, Operation: req.Operation, FromState: c.State, ToState: to,
		Requester: req.Requester, Approver: req.Approver, Reason: req.Reason, At: req.At.UTC(),
		PhaseID: req.ActivePhase.ID, BeforeRevisionDigest: c.Revision.Digest, AfterRevisionDigest: nextRevision.Digest,
		BeforeRevisionVersion: c.Revision.Version, AfterRevisionVersion: nextRevision.Version,
		Counts: cloneCounts(req.Counts), Exceptions: append([]string(nil), req.Exceptions...), SnapshotVersions: cloneStrings(req.SnapshotVersions),
	}
	event.Digest = digestEvent(event)
	if err := event.Validate(); err != nil {
		return GovernedCycle{}, TransitionEvent{}, err
	}
	return GovernedCycle{Revision: nextRevision, State: to, Sequence: event.Sequence, LastEventDigest: event.Digest}, event, nil
}

// Ledger is the in-memory CAS port for governed transitions. It is not a
// persistence implementation; it models the atomic state and append-only
// event boundary for pure engine tests and conformance adapters.
type Ledger struct {
	mu      sync.RWMutex
	current GovernedCycle
	events  []TransitionEvent
}

// NewLedger returns a ledger in the UNOPENED state.
func NewLedger(revision Revision) (*Ledger, error) {
	c, err := NewGovernedCycle(revision)
	if err != nil {
		return nil, err
	}
	return &Ledger{current: c}, nil
}

// NewOpenLedger returns a ledger whose current revision is already OPEN.
func NewOpenLedger(revision Revision) (*Ledger, error) {
	c, err := NewOpenCycle(revision)
	if err != nil {
		return nil, err
	}
	return &Ledger{current: c}, nil
}

// Current returns a detached lifecycle view.
func (l *Ledger) Current() GovernedCycle { l.mu.RLock(); defer l.mu.RUnlock(); return l.current }

// Events returns a detached append-only event history.
func (l *Ledger) Events() []TransitionEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]TransitionEvent, len(l.events))
	for i, event := range l.events {
		out[i] = cloneEvent(event)
	}
	return out
}

// Transition performs a compare-and-swap transition while holding the ledger
// lock. A stale expected sequence/digest cannot mutate state or append an
// event.
func (l *Ledger) Transition(req TransitionRequest) (GovernedCycle, TransitionEvent, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	next, event, err := l.current.Transition(req)
	if err != nil {
		return GovernedCycle{}, TransitionEvent{}, err
	}
	l.current = next
	l.events = append(l.events, cloneEvent(event))
	return next, cloneEvent(event), nil
}

func reopenRevision(previous Revision, req TransitionRequest, sequence int) (Revision, error) {
	id, version := req.NewRevisionID, req.NewRevisionVersion
	if id == "" || version == "" {
		seed, _ := json.Marshal(struct {
			Previous, Reason, Requester string
			At                          time.Time
			Sequence                    int
		}{previous.Digest, req.Reason, req.Requester, req.At.UTC(), sequence})
		sum := sha256.Sum256(seed)
		short := hex.EncodeToString(sum[:8])
		if id == "" {
			id = previous.ID + ":reopen:" + short
		}
		if version == "" {
			version = previous.Version + ".reopen." + short
		}
	}
	from := req.At.UTC()
	if !previous.EffectiveTo.IsZero() && !from.Before(previous.EffectiveTo) {
		return Revision{}, fmt.Errorf("%w: reopen at %s is outside the prior effective interval", ErrTransitionRevision, from)
	}
	r, err := NewRevision(previous.Cycle(), id, version, from, previous.EffectiveTo)
	if err != nil {
		return Revision{}, fmt.Errorf("%w: %v", ErrTransitionRevision, err)
	}
	return r, nil
}

func digestEvent(event TransitionEvent) string {
	b, _ := json.Marshal(struct {
		Sequence                                                                                        int
		Operation                                                                                       TransitionOperation
		FromState, ToState                                                                              LifecycleState
		Requester, Approver, Reason                                                                     string
		At                                                                                              time.Time
		PhaseID, BeforeRevisionDigest, AfterRevisionDigest, BeforeRevisionVersion, AfterRevisionVersion string
		Counts                                                                                          map[string]int
		Exceptions                                                                                      []string
		SnapshotVersions                                                                                map[string]string
	}{event.Sequence, event.Operation, event.FromState, event.ToState, event.Requester, event.Approver, event.Reason, event.At, event.PhaseID, event.BeforeRevisionDigest, event.AfterRevisionDigest, event.BeforeRevisionVersion, event.AfterRevisionVersion, event.Counts, event.Exceptions, event.SnapshotVersions})
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func cloneCounts(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func cloneStrings(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func cloneEvent(in TransitionEvent) TransitionEvent {
	out := in
	out.Counts = cloneCounts(in.Counts)
	out.Exceptions = append([]string(nil), in.Exceptions...)
	out.SnapshotVersions = cloneStrings(in.SnapshotVersions)
	return out
}
