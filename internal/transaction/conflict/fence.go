package conflict

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidIntent   = errors.New("conflict: invalid write intent")
	ErrIntentConflict  = errors.New("conflict: write intent conflict")
	ErrStaleBaseline   = errors.New("conflict: stale baseline")
	ErrFence           = errors.New("conflict: stale fence")
	ErrAlreadyTerminal = errors.New("conflict: intent already terminal")
)

// Error is a stable machine-readable commit refusal.
type Error struct {
	Code     string
	IntentID string
	Err      error
}

func (e *Error) Error() string { return fmt.Sprintf("conflict: %s for %s", e.Code, e.IntentID) }
func (e *Error) Unwrap() error { return e.Err }
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

const (
	CodeConflictStaleBaseline = "CONFLICT_STALE_BASELINE"
	CodeConflictOrdering      = "CONFLICT_ORDERING"
	CodeConflictFence         = "CONFLICT_STALE_FENCE"
)

// CommitRequest supplies the observation made by the coordinator immediately
// before commit. Current footprints use the observed revision in
// ExpectedRevision; no database or transaction is opened by this package.
type CommitRequest struct {
	IntentID string
	Current  []WriteFootprint
}

type CommitResult struct {
	Intent   WriteIntent
	Fence    uint64
	Decision Decision
}

// ValidateAtCommit closes the preflight race and reserves the intent's
// normalized scope. The first overlapping intent with the same baseline wins;
// every later contender receives CONFLICT_STALE_BASELINE. The critical section
// is only the fake's atomic CAS, never a worker/business lock.
func (r *Registry) ValidateAtCommit(req CommitRequest) (CommitResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	in, ok := r.items[req.IntentID]
	if !ok {
		return CommitResult{}, fmt.Errorf("%w: %s", ErrInvalidIntent, req.IntentID)
	}
	if in.Status.terminal() {
		return CommitResult{}, ErrAlreadyTerminal
	}
	if len(req.Current) > 0 {
		for _, want := range in.Footprints {
			matched := false
			for _, got := range req.Current {
				overlap, err := want.Overlaps(got)
				if err != nil {
					return CommitResult{}, err
				}
				if overlap {
					matched = true
					if want.ExpectedRevision.Canonical() == nil || got.ExpectedRevision.Canonical() == nil || string(want.ExpectedRevision.Canonical()) != string(got.ExpectedRevision.Canonical()) {
						return r.stale(in)
					}
					break
				}
			}
			if !matched {
				return r.stale(in)
			}
		}
	}
	for _, f := range in.Footprints {
		for _, otherID := range r.active {
			if otherID == in.ID {
				continue
			}
			other := r.items[otherID]
			for _, of := range other.Footprints {
				overlap, err := f.Overlaps(of)
				if err != nil {
					return CommitResult{}, err
				}
				if overlap {
					return r.stale(in)
				}
			}
		}
	}
	r.next++
	in.Fence = r.next
	in.Status = IntentCommitted
	for _, f := range in.Footprints {
		r.active[f.Digest()] = in.ID
	}
	return CommitResult{Intent: *in, Fence: in.Fence, Decision: DecisionHardConflict}, nil
}

func (r *Registry) stale(in *WriteIntent) (CommitResult, error) {
	in.Status = IntentConflicted
	return CommitResult{}, &Error{Code: CodeConflictStaleBaseline, IntentID: in.ID, Err: ErrStaleBaseline}
}

// Release closes a committed or reserved intent exactly once. Replaying the
// same fence is idempotent; a different fence cannot release another writer.
func (r *Registry) Release(id string, fence uint64) (WriteIntent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	in, ok := r.items[id]
	if !ok {
		return WriteIntent{}, ErrInvalidIntent
	}
	if in.Fence != fence || fence == 0 {
		return WriteIntent{}, ErrFence
	}
	if in.Status == IntentReleased {
		return *in, nil
	}
	if in.Status != IntentCommitted && in.Status != IntentReserved {
		return WriteIntent{}, ErrAlreadyTerminal
	}
	in.Status = IntentReleased
	for _, f := range in.Footprints {
		if r.active[f.Digest()] == id {
			delete(r.active, f.Digest())
		}
	}
	return *in, nil
}

func (r *Registry) Lookup(id string) (WriteIntent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	in, ok := r.items[id]
	if !ok {
		return WriteIntent{}, false
	}
	return *in, true
}

func NewIntentRegistry() *Registry                                 { return NewRegistry() }
func (r *Registry) Persist(in WriteIntent) (WriteIntent, error)    { return r.Register(in) }
func (r *Registry) Commit(req CommitRequest) (CommitResult, error) { return r.ValidateAtCommit(req) }
