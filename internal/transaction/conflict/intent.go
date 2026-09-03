package conflict

import (
	"fmt"
	"strings"
	"sync"
)

// IntentStatus is the lifecycle of a registered write intent.  The registry
// deliberately keeps this small: an intent is either active, committed, or
// terminal.  Terminal transitions are never overwritten by a retry.
type IntentStatus string

const (
	IntentDraft      IntentStatus = "DRAFT"
	IntentSubmitted  IntentStatus = "SUBMITTED"
	IntentReserved   IntentStatus = "RESERVED"
	IntentCommitted  IntentStatus = "COMMITTED"
	IntentReleased   IntentStatus = "RELEASED"
	IntentCancelled  IntentStatus = "CANCELLED"
	IntentConflicted IntentStatus = "CONFLICTED"
)

// Short aliases used by ports that model the lifecycle as Status values.
const (
	StatusDraft      = IntentDraft
	StatusSubmitted  = IntentSubmitted
	StatusReserved   = IntentReserved
	StatusCommitted  = IntentCommitted
	StatusReleased   = IntentReleased
	StatusCancelled  = IntentCancelled
	StatusConflicted = IntentConflicted
)

func (s IntentStatus) terminal() bool {
	return s == IntentCommitted || s == IntentReleased || s == IntentCancelled || s == IntentConflicted
}

// WriteIntent is the durable shape owned by the conflict registry.  Its
// footprints contain the complete normalized write set and therefore also
// carry the expected stream revisions used by commit-time validation.
type WriteIntent struct {
	ID             string
	ProposalID     string
	Footprints     []WriteFootprint
	SnapshotDigest string
	Status         IntentStatus
	Fence          uint64
}

func (i WriteIntent) Validate() error {
	if strings.TrimSpace(i.ID) == "" {
		return fmt.Errorf("%w: intent id is empty", ErrInvalidIntent)
	}
	if len(i.Footprints) == 0 {
		return fmt.Errorf("%w: intent %s has no footprints", ErrInvalidIntent, i.ID)
	}
	for n, f := range i.Footprints {
		if err := f.Validate(); err != nil {
			return fmt.Errorf("%w: intent %s footprint %d: %v", ErrInvalidIntent, i.ID, n, err)
		}
	}
	if i.SnapshotDigest == "" {
		return fmt.Errorf("%w: intent %s has no conflict snapshot", ErrInvalidIntent, i.ID)
	}
	return nil
}

// Registry is a concurrency-safe in-memory reference implementation.  It is
// intentionally a fake/port implementation: production persistence belongs
// to the transaction coordinator and must make the same compare-and-swap
// decisions in its database transaction.
type Registry struct {
	mu     sync.Mutex
	next   uint64
	items  map[string]*WriteIntent
	active map[string]string // footprint digest -> owning intent
}

func NewRegistry() *Registry {
	return &Registry{items: make(map[string]*WriteIntent), active: make(map[string]string)}
}

// Register persists an intent as SUBMITTED. Repeating an identical request
// is idempotent; reusing an id for another write set is a typed conflict.
func (r *Registry) Register(in WriteIntent) (WriteIntent, error) {
	if err := in.Validate(); err != nil {
		return WriteIntent{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.items == nil {
		r.items = make(map[string]*WriteIntent)
		r.active = make(map[string]string)
	}
	if old, ok := r.items[in.ID]; ok {
		if intentDigest(*old) != intentDigest(in) {
			return WriteIntent{}, ErrIntentConflict
		}
		return *old, nil
	}
	in.Status = IntentSubmitted
	in.Footprints = append([]WriteFootprint(nil), in.Footprints...)
	r.items[in.ID] = &in
	return in, nil
}

func intentDigest(i WriteIntent) string {
	var b strings.Builder
	for _, f := range i.Footprints {
		b.WriteString(f.Digest())
		b.WriteByte(0)
	}
	return i.SnapshotDigest + "\x00" + b.String()
}
