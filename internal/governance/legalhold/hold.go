// Package legalhold owns scoped legal holds and their disposition decisions.
// A hold is governance state: it freezes disposition for matching records and
// dependent artifacts, while leaving unrelated records eligible to proceed.
package legalhold

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidHold       = errors.New("legalhold: invalid hold")
	ErrHoldBlocked       = errors.New("legalhold: HOLD_BLOCKED")
	ErrUnauthorized      = errors.New("legalhold: unauthorized hold query")
	ErrCompartmentDenied = errors.New("legalhold: compartment denied")
	ErrUnknownHold       = errors.New("legalhold: unknown hold")
)

// Scope identifies the records to which a hold applies. Empty RecordRefs and
// ClassRefs are wildcards; Tenant and Compartment are always required.
type Scope struct {
	Tenant      string
	Compartment string
	RecordRefs  []string
	ClassRefs   []string
}

func (s Scope) Validate() error {
	if s.Tenant == "" || s.Compartment == "" {
		return fmt.Errorf("%w: tenant and compartment are required", ErrInvalidHold)
	}
	return nil
}

func (s Scope) matches(r Record) bool {
	if s.Tenant != r.Tenant || s.Compartment != r.Compartment {
		return false
	}
	if len(s.RecordRefs) == 0 && len(s.ClassRefs) == 0 {
		return true
	}
	for _, x := range s.RecordRefs {
		if x == r.Ref {
			return true
		}
	}
	for _, x := range s.ClassRefs {
		if x == r.ClassRef {
			return true
		}
	}
	return false
}

// Hold is append-only lifecycle state. ReleasedAt is set only by Release and
// never changes the original scope or creation evidence.
type Hold struct {
	ID          string
	Tenant      string
	Compartment string
	Scope       Scope
	Reason      string
	Authority   string
	CreatedAt   values.Instant
	ReleasedAt  values.Instant
}

func (h Hold) Validate() error {
	if h.ID == "" || h.Reason == "" || h.Authority == "" || !h.CreatedAt.IsSet() {
		return fmt.Errorf("%w: id, reason, authority and created_at are required", ErrInvalidHold)
	}
	if err := h.Scope.Validate(); err != nil {
		return err
	}
	if h.Tenant != h.Scope.Tenant || h.Compartment != h.Scope.Compartment {
		return fmt.Errorf("%w: hold and scope tenant/compartment differ", ErrInvalidHold)
	}
	return nil
}
func (h Hold) Active() bool { return !h.ReleasedAt.IsSet() }

// Record is the minimal retention subject needed to make a disposition
// decision. ParentRef links dependent artifacts to their held record.
type Record struct {
	Tenant, Compartment, Ref, ParentRef, ClassRef string
	ParentClassRef                                string
	// Ancestors contains any further parents between this record and its
	// root.  It is optional for callers that only have a direct parent; when
	// present, every ancestor is checked against active holds.
	Ancestors []Record
}

// Evidence is emitted for every blocked disposition and every lifecycle
// transition. It intentionally contains no record payload or sensitive reason.
type Evidence struct {
	ID, Action, RecordRef, HoldID, Code, Tenant, Compartment string
	At                                                       values.Instant
}

type Decision struct {
	Allowed  bool
	Code     string
	HoldID   string
	Evidence Evidence
}

// Err returns the protocol error represented by the decision. Callers that
// use error-returning command handlers can preserve HOLD_BLOCKED with
// errors.Is while still retaining the evidence-bearing decision.
func (d Decision) Err() error {
	if d.Code == "HOLD_BLOCKED" {
		return ErrHoldBlocked
	}
	return nil
}

// Store is a concurrency-safe in-memory semantic adapter suitable for unit
// and conformance tests. Production persistence can implement the same model.
type Store struct {
	mu       sync.RWMutex
	holds    map[string]Hold
	evidence []Evidence
}

func NewStore() *Store { return &Store{holds: make(map[string]Hold)} }

func (s *Store) Create(h Hold) error {
	if err := h.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.holds[h.ID]; ok {
		return fmt.Errorf("%w: duplicate id", ErrInvalidHold)
	}
	s.holds[h.ID] = h
	s.evidence = append(s.evidence, Evidence{ID: "hold-created/" + h.ID, Action: "CREATE", HoldID: h.ID, Code: "HOLD_CREATED", Tenant: h.Tenant, Compartment: h.Compartment, At: h.CreatedAt})
	return nil
}

// Release ends a hold. The caller supplies the release time to keep decisions
// deterministic and permit a schedule recalculation by the retention owner.
func (s *Store) Release(id string, at values.Instant) error {
	if !at.IsSet() {
		return fmt.Errorf("%w: release time required", ErrInvalidHold)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.holds[id]
	if !ok {
		return ErrUnknownHold
	}
	if !h.Active() {
		return nil
	}
	h.ReleasedAt = at
	s.holds[id] = h
	s.evidence = append(s.evidence, Evidence{ID: "hold-released/" + id, Action: "RELEASE", HoldID: id, Code: "HOLD_RELEASED", Tenant: h.Tenant, Compartment: h.Compartment, At: at})
	return nil
}

// DecideDisposition checks record and all of its ancestors against active
// holds. A dependent artifact is blocked when its parent is held.
func (s *Store) DecideDisposition(r Record, evidenceID string, at values.Instant) Decision {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range s.holds {
		if h.Active() && h.Scope.matches(r) {
			return s.blockedLocked(r, evidenceID, at, h)
		}
		if h.Active() && r.ParentRef != "" && h.Scope.matches(Record{Tenant: r.Tenant, Compartment: r.Compartment, Ref: r.ParentRef, ClassRef: r.ParentClassRef}) {
			return s.blockedLocked(r, evidenceID, at, h)
		}
		for _, ancestor := range r.Ancestors {
			if h.Active() && h.Scope.matches(ancestor) {
				return s.blockedLocked(r, evidenceID, at, h)
			}
		}
	}
	return Decision{Allowed: true, Code: "DISPOSITION_ALLOWED"}
}

func (s *Store) blockedLocked(r Record, evidenceID string, at values.Instant, h Hold) Decision {
	e := Evidence{ID: evidenceID, Action: "DISPOSITION", RecordRef: r.Ref, HoldID: h.ID, Code: "HOLD_BLOCKED", Tenant: r.Tenant, Compartment: r.Compartment, At: at}
	s.evidence = append(s.evidence, e)
	return Decision{Code: "HOLD_BLOCKED", HoldID: h.ID, Evidence: e}
}

// EvaluateDisposition is the error-returning form of DecideDisposition.
func (s *Store) EvaluateDisposition(r Record, evidenceID string, at values.Instant) (Decision, error) {
	d := s.DecideDisposition(r, evidenceID, at)
	return d, d.Err()
}

// Holds returns only holds visible to the caller's tenant and compartments.
// Both authorization and compartment are explicit; empty values fail closed.
func (s *Store) Holds(tenant, compartment string, authorized bool) ([]Hold, error) {
	if !authorized {
		return nil, ErrUnauthorized
	}
	if tenant == "" || compartment == "" {
		return nil, ErrCompartmentDenied
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Hold, 0)
	for _, h := range s.holds {
		if h.Tenant == tenant && h.Compartment == compartment {
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) Evidence() []Evidence {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Evidence(nil), s.evidence...)
}
