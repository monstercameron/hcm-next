// Package draftflow contains the presentation-layer mechanics for moving a
// participant's requested values to an exact, confirmable proposal.  It does
// not resolve authority or perform domain writes; those remain server/domain
// contracts supplied by callers.
package draftflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

var (
	ErrInvalid             = errors.New("draftflow: invalid request")
	ErrConflict            = errors.New("draftflow: revision conflict")
	ErrNotFound            = errors.New("draftflow: draft not found")
	ErrDigestMismatch      = errors.New("draftflow: proposal digest mismatch")
	ErrIdempotencyConflict = errors.New("draftflow: idempotency key reused for another proposal")
)

// Draft stores requested values only. Current truth is deliberately absent.
type Draft struct {
	ID        string
	FlowID    string
	Owner     string
	Revision  uint64
	Version   uint64
	Requested map[string]string
	UpdatedAt time.Time
}

type SaveRequest struct {
	ID, FlowID, Owner string
	ExpectedRevision  uint64
	ExpectedVersion   uint64
	Requested         map[string]string
	At                time.Time
}

type Store struct {
	mu     sync.RWMutex
	drafts map[string]Draft
	now    func() time.Time
}

func NewStore(now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{drafts: make(map[string]Draft), now: now}
}

func (s *Store) Save(req SaveRequest) (Draft, error) {
	if s == nil || req.ID == "" || req.FlowID == "" || req.Owner == "" {
		return Draft{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.drafts[req.ID]
	expected := req.ExpectedRevision
	if expected == 0 && req.ExpectedVersion != 0 {
		expected = req.ExpectedVersion
	}
	if exists {
		if old.FlowID != req.FlowID || old.Owner != req.Owner {
			return Draft{}, ErrInvalid
		}
		if old.Revision != expected {
			return Draft{}, ErrConflict
		}
	} else if expected != 0 {
		return Draft{}, ErrConflict
	}
	rev := uint64(1)
	if exists {
		rev = old.Revision + 1
	}
	at := req.At
	if at.IsZero() {
		at = s.now()
	}
	d := Draft{ID: req.ID, FlowID: req.FlowID, Owner: req.Owner, Revision: rev, Requested: clone(req.Requested), UpdatedAt: at}
	d.Version = rev
	s.drafts[req.ID] = d
	return cloneDraft(d), nil
}

// Autosave is the named autosave entry point. It uses the same atomic CAS
// semantics as Save and is always effect-free.
func (s *Store) Autosave(req SaveRequest) (Draft, error) { return s.Save(req) }

func (s *Store) Get(id string) (Draft, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.drafts[id]
	return cloneDraft(d), ok
}

// ValidationError is associated with a requested field, preserving the input
// value so an accessible form can retain both focus and what was typed.
type ValidationError struct{ Field, Message string }
type ValidationResult struct {
	Draft         Draft
	Requested     map[string]string
	FieldErrors   []ValidationError
	SummaryErrors []string
	FocusField    string
	Valid         bool
	Errors        []ValidationError
}
type Validator func(map[string]string) []ValidationError

func Validate(d Draft, validator Validator) ValidationResult {
	r := ValidationResult{Draft: cloneDraft(d), Requested: clone(d.Requested)}
	if validator != nil {
		r.FieldErrors = append([]ValidationError(nil), validator(clone(d.Requested))...)
	}
	for _, e := range r.FieldErrors {
		if e.Message != "" {
			r.SummaryErrors = append(r.SummaryErrors, e.Message)
		}
		if r.FocusField == "" && e.Field != "" {
			r.FocusField = e.Field
		}
	}
	r.Valid = len(r.FieldErrors) == 0
	r.Errors = append([]ValidationError(nil), r.FieldErrors...)
	return r
}

// TruthSnapshot is server-resolved state; it must not be copied into Draft.
type TruthSnapshot struct {
	Values      map[string]string
	Provenance  map[string]string
	Uncertainty []string
	Revision    string
}
type ResolveFunc func(Draft) TruthSnapshot
type Resolved struct {
	Requested map[string]string
	Truth     TruthSnapshot
}

func Resolve(d Draft, resolver ResolveFunc) Resolved {
	r := Resolved{Requested: clone(d.Requested)}
	if resolver != nil {
		r.Truth = resolver(d)
		r.Truth.Values = clone(r.Truth.Values)
		r.Truth.Provenance = clone(r.Truth.Provenance)
		r.Truth.Uncertainty = append([]string(nil), r.Truth.Uncertainty...)
	}
	return r
}

type Effects struct{ Writes, Events, Signals, ProviderRequests int }

func (e Effects) IsZero() bool { return e == (Effects{}) }

type Simulation struct {
	Requested map[string]string
	Truth     TruthSnapshot
	Changes   []string
	Warnings  []string
	Proposal  Proposal
	Effects   Effects
}
type Simulator func(Resolved) (changes []string, warnings []string)

func Simulate(d Draft, truth TruthSnapshot, simulator Simulator) Simulation {
	r := Resolved{Requested: clone(d.Requested), Truth: truth}
	r.Truth.Values = clone(truth.Values)
	r.Truth.Provenance = clone(truth.Provenance)
	r.Truth.Uncertainty = append([]string(nil), truth.Uncertainty...)
	s := Simulation{Requested: clone(d.Requested), Truth: r.Truth, Effects: Effects{}}
	if simulator != nil {
		s.Changes, s.Warnings = simulator(r)
	}
	s.Proposal = Proposal{Requested: clone(d.Requested), Truth: r.Truth, Changes: append([]string(nil), s.Changes...), Warnings: append([]string(nil), s.Warnings...)}
	s.Proposal.Digest = Digest(s.Proposal)
	return s
}

type Proposal struct {
	Requested         map[string]string
	Truth             TruthSnapshot
	Changes, Warnings []string
	Digest            string
}

func Digest(p Proposal) string {
	b, _ := json.Marshal(canonicalProposal{Requested: sorted(p.Requested), Truth: canonicalTruth{Values: sorted(p.Truth.Values), Provenance: sorted(p.Truth.Provenance), Uncertainty: sortedStrings(p.Truth.Uncertainty), Revision: p.Truth.Revision}, Changes: sortedStrings(p.Changes), Warnings: sortedStrings(p.Warnings)})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type canonicalProposal struct {
	Requested map[string]string `json:"requested"`
	Truth     canonicalTruth    `json:"truth"`
	Changes   []string          `json:"changes,omitempty"`
	Warnings  []string          `json:"warnings,omitempty"`
}
type canonicalTruth struct {
	Values      map[string]string `json:"values,omitempty"`
	Provenance  map[string]string `json:"provenance,omitempty"`
	Uncertainty []string          `json:"uncertainty,omitempty"`
	Revision    string            `json:"revision,omitempty"`
}

func sorted(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	o := make(map[string]string, len(m))
	for _, k := range keys {
		o[k] = m[k]
	}
	return o
}
func sortedStrings(v []string) []string { o := append([]string(nil), v...); sort.Strings(o); return o }

type CompareResult struct {
	Requested, Current map[string]string
	Provenance         map[string]string
	Uncertainty        []string
	Material           bool
}

func Compare(requested, current map[string]string, provenance map[string]string, uncertainty []string, material func(string) bool) CompareResult {
	r := CompareResult{Requested: clone(requested), Current: clone(current), Provenance: clone(provenance), Uncertainty: append([]string(nil), uncertainty...)}
	for k, v := range requested {
		if current[k] != v && (material == nil || material(k)) {
			r.Material = true
		}
	}
	return r
}

type Confirmation struct {
	ID, IdempotencyKey, ProposalDigest string
	ConfirmedAt                        time.Time
	Idempotent                         bool
}
type ConfirmRequest struct {
	ID, IdempotencyKey, ProposalDigest string
	At                                 time.Time
}
type Confirmer struct {
	mu    sync.Mutex
	byKey map[string]Confirmation
	now   func() time.Time
}

func NewConfirmer(now func() time.Time) *Confirmer {
	if now == nil {
		now = time.Now
	}
	return &Confirmer{byKey: make(map[string]Confirmation), now: now}
}
func (c *Confirmer) Confirm(req ConfirmRequest) (Confirmation, error) {
	if c == nil || req.ID == "" || req.IdempotencyKey == "" || req.ProposalDigest == "" {
		return Confirmation{}, ErrInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.byKey[req.IdempotencyKey]; ok {
		if old.ProposalDigest != req.ProposalDigest || old.ID != req.ID {
			return Confirmation{}, ErrIdempotencyConflict
		}
		old.Idempotent = true
		return old, nil
	}
	at := req.At
	if at.IsZero() {
		at = c.now()
	}
	x := Confirmation{ID: req.ID, IdempotencyKey: req.IdempotencyKey, ProposalDigest: req.ProposalDigest, ConfirmedAt: at}
	c.byKey[req.IdempotencyKey] = x
	return x, nil
}
func ConfirmExact(c *Confirmer, req ConfirmRequest, p Proposal) (Confirmation, error) {
	if req.ProposalDigest != Digest(p) {
		return Confirmation{}, fmt.Errorf("%w: expected %s", ErrDigestMismatch, Digest(p))
	}
	return c.Confirm(req)
}

func clone(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	o := make(map[string]string, len(m))
	for k, v := range m {
		o[k] = v
	}
	return o
}
func cloneDraft(d Draft) Draft { d.Requested = clone(d.Requested); return d }
