package cryptoagile

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Kind names the cryptographic primitive an AlgorithmSuite provides.
type Kind string

// The three kinds cryptoagile knows about. A digest suite has no signing
// key of its own; it exists so a migration can rotate the hash algorithm
// underneath a content digest (e.g. the ledger hash chain's chain_algorithm,
// migrations/00014_ledger_hash_chain.sql) through the same registry as a
// signature or MAC suite, rather than a second, parallel mechanism.
const (
	KindSignature Kind = "signature"
	KindMAC       Kind = "mac"
	KindDigest    Kind = "digest"
)

// Status is where a suite sits in a migration's lifecycle.
type Status string

const (
	// StatusActive is the suite new material is produced under and the only
	// suite required for verification to succeed.
	StatusActive Status = "ACTIVE"
	// StatusDual is a suite that is also being produced (dual-sign) or still
	// accepted (dual-read) alongside the ACTIVE suite during a migration
	// window, but is not yet - or no longer - the default.
	StatusDual Status = "DUAL"
	// StatusRetired is a suite whose signatures are refused outright. A
	// RETIRED suite's own already-recorded signatures were verified before
	// retirement; retirement means no new trust decision may depend on it.
	StatusRetired Status = "RETIRED"
)

// AlgorithmSuite is one registry entry: an algorithm identity, never a
// scattered constant, with its own declared activation and retirement
// instants so "when did this stop being trustworthy" is answerable without
// grepping commit history.
type AlgorithmSuite struct {
	ID          string
	Kind        Kind
	Status      Status
	ActivatedAt time.Time
	RetiredAt   time.Time
}

// Suite validation failures.
var (
	ErrSuiteID         = errors.New("cryptoagile: suite id is required")
	ErrSuiteKind       = errors.New("cryptoagile: suite kind must be signature, mac or digest")
	ErrSuiteStatus     = errors.New("cryptoagile: suite status must be ACTIVE, DUAL or RETIRED")
	ErrSuiteActivation = errors.New("cryptoagile: an ACTIVE or DUAL suite must declare its activation instant")
	ErrSuiteRetirement = errors.New("cryptoagile: a RETIRED suite must declare a retirement instant after its activation instant")
)

// Validate reports whether s is internally consistent. It does not consult a
// [Registry]: duplicate-id and cross-suite checks live there.
func (s AlgorithmSuite) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return ErrSuiteID
	}
	switch s.Kind {
	case KindSignature, KindMAC, KindDigest:
	default:
		return fmt.Errorf("%w: %q", ErrSuiteKind, s.Kind)
	}
	switch s.Status {
	case StatusActive, StatusDual, StatusRetired:
	default:
		return fmt.Errorf("%w: %q", ErrSuiteStatus, s.Status)
	}
	if (s.Status == StatusActive || s.Status == StatusDual) && s.ActivatedAt.IsZero() {
		return ErrSuiteActivation
	}
	if s.Status == StatusRetired {
		if s.RetiredAt.IsZero() {
			return ErrSuiteRetirement
		}
		if !s.ActivatedAt.IsZero() && !s.RetiredAt.After(s.ActivatedAt) {
			return ErrSuiteRetirement
		}
	}
	return nil
}

// Registry is the crypto-agile algorithm catalog: the one place a suite id
// resolves to its kind, status and lifecycle instants. Callers never branch
// on a suite id string directly; they ask the registry.
type Registry struct {
	suites map[string]AlgorithmSuite
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{suites: make(map[string]AlgorithmSuite)}
}

// Registry failures.
var (
	ErrDuplicateSuite = errors.New("cryptoagile: suite id is already registered")
	ErrUnknownSuite   = errors.New("cryptoagile: suite id is not registered")
)

// Register adds s to the registry. It refuses an invalid suite and a suite
// id that already exists: a rotation changes an existing entry through
// [Registry.Transition], never by silently re-registering the id.
func (r *Registry) Register(s AlgorithmSuite) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if _, exists := r.suites[s.ID]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateSuite, s.ID)
	}
	r.suites[s.ID] = s
	return nil
}

// Transition moves the suite named id to status as of at, stamping
// ActivatedAt the first time a suite becomes ACTIVE or DUAL and RetiredAt
// when it becomes RETIRED. It returns the suite's state immediately before
// the transition, so a caller can build an [Evidence] record describing
// exactly what changed without re-deriving it from the registry's new state.
func (r *Registry) Transition(id string, status Status, at time.Time) (previous AlgorithmSuite, err error) {
	cur, ok := r.suites[id]
	if !ok {
		return AlgorithmSuite{}, fmt.Errorf("%w: %q", ErrUnknownSuite, id)
	}
	next := cur
	next.Status = status
	switch status {
	case StatusActive, StatusDual:
		if next.ActivatedAt.IsZero() {
			next.ActivatedAt = at
		}
	case StatusRetired:
		next.RetiredAt = at
	}
	if err := next.Validate(); err != nil {
		return AlgorithmSuite{}, err
	}
	r.suites[id] = next
	return cur, nil
}

// Get returns the suite named id and whether it is registered.
func (r *Registry) Get(id string) (AlgorithmSuite, bool) {
	s, ok := r.suites[id]
	return s, ok
}

// IsRetired reports whether id is a registered, RETIRED suite. An
// unregistered id is not "retired"; it is unknown, which callers must
// handle separately (see [ErrUnknownSuite]).
func (r *Registry) IsRetired(id string) bool {
	s, ok := r.suites[id]
	return ok && s.Status == StatusRetired
}
