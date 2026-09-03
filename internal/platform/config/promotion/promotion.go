// Package promotion implements the pure configuration promotion lifecycle.
//
// It owns the validate -> simulate -> approve -> activate state machine, but
// deliberately has no runtime, database, or transport dependencies.  A
// Registry is an in-memory reference implementation of the control-plane
// contract; callers can use its immutable records to build another adapter.
package promotion

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/platform/config"
)

type Status string

const (
	StatusDraft     Status = "DRAFT"
	StatusValidated Status = "VALIDATED"
	StatusSimulated Status = "SIMULATED"
	StatusApproved  Status = "APPROVED"
	StatusActive    Status = "ACTIVE"
)

var (
	ErrInvalidPackage    = errors.New("config promotion: invalid package")
	ErrNotValidated      = errors.New("config promotion: package is not validated")
	ErrNotSimulated      = errors.New("config promotion: package is not simulated")
	ErrNotApproved       = errors.New("config promotion: package is not approved")
	ErrStale             = errors.New("config promotion: package changed after review")
	ErrIncompatible      = errors.New("config promotion: package is incompatible")
	ErrAlreadyActive     = errors.New("config promotion: another package is active")
	ErrNotFound          = errors.New("config promotion: package not found")
	ErrInvalidTransition = errors.New("config promotion: invalid lifecycle transition")
)

// Package is an immutable candidate. PublicKey is used only to verify the
// detached signature and is never modified by the registry.
type Package struct {
	ID          string
	Environment string
	Bundle      config.SignedBundle
	PublicKey   ed25519.PublicKey
}

// Validation records the exact package digest checked by Validate.
type Validation struct {
	PackageID  string
	Digest     string
	Compatible bool
	CheckedAt  time.Time
}

// Simulation records a side-effect-free result bound to a validated digest.
type Simulation struct {
	PackageID  string
	Digest     string
	Compatible bool
	Passed     bool
	CheckedAt  time.Time
}

// Approval is a review over one exact simulation. Approver must differ from
// the package signer, preventing self-approval.
type Approval struct {
	PackageID  string
	Digest     string
	Approver   string
	ApprovedAt time.Time
}

// Record is the immutable publication evidence plus mutable lifecycle status.
// Bundle, validation, simulation, and approval are copied on ingress/egress.
type Record struct {
	Package     Package
	Status      Status
	Validation  Validation
	Simulation  Simulation
	Approval    Approval
	ActivatedAt time.Time
	RollbackTo  string
}

func clonePackage(p Package) Package {
	p.PublicKey = append(ed25519.PublicKey(nil), p.PublicKey...)
	p.Bundle.Bundle.Dependencies = append([]config.Dependency(nil), p.Bundle.Bundle.Dependencies...)
	p.Bundle.Bundle.CredentialRefs = append([]string(nil), p.Bundle.Bundle.CredentialRefs...)
	return p
}
func cloneRecord(r Record) Record { r.Package = clonePackage(r.Package); return r }

func validatePackage(p Package) (Validation, error) {
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Environment) == "" {
		return Validation{}, fmt.Errorf("%w: id and environment are required", ErrInvalidPackage)
	}
	if p.Bundle.Bundle.BundleID != p.ID {
		return Validation{}, fmt.Errorf("%w: package id does not match bundle id", ErrInvalidPackage)
	}
	if len(p.PublicKey) != ed25519.PublicKeySize {
		return Validation{}, fmt.Errorf("%w: public key is required", ErrInvalidPackage)
	}
	if st, err := config.VerifyBundle(p.Bundle, p.PublicKey); err != nil || st != config.VerifyStatusValid {
		if err == nil {
			err = ErrInvalidPackage
		}
		return Validation{}, fmt.Errorf("%w: %v", ErrInvalidPackage, err)
	}
	return Validation{PackageID: p.ID, Digest: p.Bundle.Digest, Compatible: true, CheckedAt: time.Now().UTC()}, nil
}

// Validate verifies signatures, content address, identity and environment.
func Validate(p Package) (Validation, error) { return validatePackage(clonePackage(p)) }

// ValidatePackage is the explicit-name alias for Validate.
func ValidatePackage(p Package) (Validation, error) { return Validate(p) }

// Simulate validates p and returns a deterministic, side-effect-free result.
// An optional prior Validation must refer to the same package and digest.
func Simulate(p Package, v Validation) (Simulation, error) {
	current, err := validatePackage(clonePackage(p))
	if err != nil {
		return Simulation{}, err
	}
	if v.PackageID != current.PackageID || !strings.EqualFold(v.Digest, current.Digest) {
		return Simulation{}, ErrStale
	}
	return Simulation{PackageID: p.ID, Digest: current.Digest, Compatible: true, Passed: true, CheckedAt: time.Now().UTC()}, nil
}

// SimulatePackage is the explicit-name alias for Simulate.
func SimulatePackage(p Package, v Validation) (Simulation, error) { return Simulate(p, v) }

// Approve approves exactly one successful simulation. The approver cannot be
// the signer, and all supplied records must still match the package digest.
func Approve(p Package, v Validation, s Simulation, approver string, at time.Time) (Approval, error) {
	if strings.TrimSpace(approver) == "" || approver == p.Bundle.SignerKeyID {
		return Approval{}, ErrInvalidPackage
	}
	if v.PackageID != p.ID || s.PackageID != p.ID || !strings.EqualFold(v.Digest, p.Bundle.Digest) || !strings.EqualFold(s.Digest, p.Bundle.Digest) {
		return Approval{}, ErrStale
	}
	if !s.Passed || !s.Compatible {
		return Approval{}, ErrNotSimulated
	}
	if at.IsZero() {
		return Approval{}, ErrInvalidPackage
	}
	return Approval{PackageID: p.ID, Digest: p.Bundle.Digest, Approver: approver, ApprovedAt: at.UTC()}, nil
}

// ApprovePackage is the explicit-name alias for Approve.
func ApprovePackage(p Package, v Validation, s Simulation, approver string, at time.Time) (Approval, error) {
	return Approve(p, v, s, approver, at)
}

// Registry is a concurrency-safe in-memory promotion control plane.
type Registry struct {
	mu      sync.RWMutex
	records map[string]Record
	active  map[string]string
}

func NewRegistry() *Registry {
	return &Registry{records: make(map[string]Record), active: make(map[string]string)}
}

func (r *Registry) Put(p Package) error {
	v, err := Validate(p)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.records[p.ID]; ok {
		return ErrInvalidTransition
	}
	r.records[p.ID] = Record{Package: clonePackage(p), Status: StatusValidated, Validation: v}
	return nil
}

func (r *Registry) Simulate(id string) (Simulation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[id]
	if !ok {
		return Simulation{}, ErrNotFound
	}
	s, err := Simulate(rec.Package, rec.Validation)
	if err != nil {
		return Simulation{}, err
	}
	rec.Simulation, rec.Status = s, StatusSimulated
	r.records[id] = rec
	return s, nil
}

func (r *Registry) Approve(id, approver string, at time.Time) (Approval, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[id]
	if !ok {
		return Approval{}, ErrNotFound
	}
	a, err := Approve(rec.Package, rec.Validation, rec.Simulation, approver, at)
	if err != nil {
		return Approval{}, err
	}
	rec.Approval, rec.Status = a, StatusApproved
	r.records[id] = rec
	return a, nil
}

// Activate atomically makes the approved package current for its environment.
// The previous active package is retained as RollbackTo and no record history
// is overwritten. Existing callers can retain a Record returned by Get as a
// pinned version while a later activation occurs.
func (r *Registry) Activate(id string, at time.Time) (Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	if rec.Status != StatusApproved {
		return Record{}, ErrNotApproved
	}
	if at.IsZero() || rec.Approval.Digest != rec.Package.Bundle.Digest || rec.Simulation.Digest != rec.Package.Bundle.Digest {
		return Record{}, ErrStale
	}
	env := rec.Package.Environment
	prior := r.active[env]
	rec.RollbackTo = prior
	rec.ActivatedAt = at.UTC()
	rec.Status = StatusActive
	r.records[id] = rec
	r.active[env] = id
	return cloneRecord(rec), nil
}

func (r *Registry) Get(id string) (Record, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.records[id]
	if !ok {
		return Record{}, false
	}
	return cloneRecord(rec), true
}
func (r *Registry) Active(environment string) (Record, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.active[environment]
	if !ok {
		return Record{}, false
	}
	rec, ok := r.records[id]
	if !ok {
		return Record{}, false
	}
	return cloneRecord(rec), true
}
