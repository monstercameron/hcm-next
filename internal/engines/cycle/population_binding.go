// CYCLE-005: bind a cycle revision to exactly one immutable population
// snapshot, by reference and digest, never by copying members. The
// population engine (internal/engines/population) already owns freezing an
// auditable Snapshot of "which subjects"; this package only cites it. A
// binding is permanent once made -- rebinding to a different snapshot is
// refused unless the cycle's currently active phase declares the
// OperationRebindPopulation operation (see Phase.AllowedOperations /
// CompiledPhase.AllowedOperations in phase_windows.go), so "who may change
// which population a cycle runs against" is governed by the same declared
// phase graph as every other cycle operation, not by an out-of-band rule.
package cycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// OperationRebindPopulation is the declared-operation token a phase must
// list in AllowedOperations for BindPopulation.Rebind to accept a new
// snapshot while the cycle is in that phase.
const OperationRebindPopulation = "REBIND_POPULATION"

var (
	// ErrBindingSnapshot is returned when a snapshot reference is missing
	// its definition id, revision version, or digest.
	ErrBindingSnapshot = errors.New("cycle: population binding requires a definition id, revision version and digest")
	// ErrBindingBoundAt is returned when a binding has no bound-at instant.
	ErrBindingBoundAt = errors.New("cycle: population binding requires a bound-at instant")
	// ErrRebindNotAllowed is returned when Rebind is attempted while the
	// active phase does not declare OperationRebindPopulation.
	ErrRebindNotAllowed = errors.New("cycle: rebinding the population snapshot is not allowed in the current phase")
)

// PopulationSnapshotRef is an immutable, by-reference citation of exactly
// one population.Snapshot: the definition it was resolved from, the
// revision it was published as, and the snapshot's own canonical digest.
// A cycle never holds a copy of a population's members -- it holds this
// reference plus the digest that proves which frozen snapshot it means.
type PopulationSnapshotRef struct {
	DefinitionID    string
	RevisionVersion string
	Digest          string
}

func (r PopulationSnapshotRef) valid() bool {
	return strings.TrimSpace(r.DefinitionID) != "" &&
		strings.TrimSpace(r.RevisionVersion) != "" &&
		strings.TrimSpace(r.Digest) != ""
}

// PopulationBinding binds one cycle revision (identified by its own
// CanonicalDigest) to one population snapshot reference, at the instant the
// binding was made. Its zero value is not a legal binding; construct one
// with BindPopulation. A PopulationBinding value is immutable: every field
// is set once at construction and there is no exported mutator except
// Rebind, which returns a distinct value rather than modifying the
// receiver.
type PopulationBinding struct {
	CycleRevisionDigest string
	Snapshot            PopulationSnapshotRef
	BoundAt             time.Time
	Digest              string
}

// BindPopulation binds cycleRevisionDigest (a cycle Revision's
// CanonicalDigest) to snapshot at boundAt, producing a new, immutable
// PopulationBinding. Two calls with byte-identical inputs produce
// byte-identical digests; any change to which cycle revision, which
// snapshot, or when it was bound changes the digest.
func BindPopulation(cycleRevisionDigest string, snapshot PopulationSnapshotRef, boundAt time.Time) (PopulationBinding, error) {
	if strings.TrimSpace(cycleRevisionDigest) == "" {
		return PopulationBinding{}, ErrRevision
	}
	if !snapshot.valid() {
		return PopulationBinding{}, ErrBindingSnapshot
	}
	if boundAt.IsZero() {
		return PopulationBinding{}, ErrBindingBoundAt
	}
	b := PopulationBinding{
		CycleRevisionDigest: cycleRevisionDigest,
		Snapshot:            snapshot,
		BoundAt:             boundAt.UTC(),
	}
	b.Digest = b.computeDigest()
	return b, nil
}

func (b PopulationBinding) computeDigest() string {
	raw, _ := json.Marshal(struct {
		CycleRevisionDigest string
		Snapshot            PopulationSnapshotRef
		BoundAt             time.Time
	}{b.CycleRevisionDigest, b.Snapshot, b.BoundAt})
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// CanonicalDigest returns the binding's own digest.
func (b PopulationBinding) CanonicalDigest() string { return b.Digest }

// phaseAllows reports whether phase declares operation among its allowed
// operations.
func phaseAllows(phase CompiledPhase, operation string) bool {
	for _, op := range phase.AllowedOperations {
		if op == operation {
			return true
		}
	}
	return false
}

// Rebind replaces b's snapshot reference with a new one, bound at boundAt,
// but only when activePhase declares OperationRebindPopulation among its
// AllowedOperations -- i.e. the cycle is in a declared phase that allows
// changing which population it is bound to. Outside such a phase, a bound
// population is permanent: Rebind returns ErrRebindNotAllowed and leaves b
// untouched (Rebind never mutates the receiver).
func (b PopulationBinding) Rebind(activePhase CompiledPhase, snapshot PopulationSnapshotRef, boundAt time.Time) (PopulationBinding, error) {
	if !phaseAllows(activePhase, OperationRebindPopulation) {
		return PopulationBinding{}, fmt.Errorf("%w: phase %q does not declare %s", ErrRebindNotAllowed, activePhase.ID, OperationRebindPopulation)
	}
	return BindPopulation(b.CycleRevisionDigest, snapshot, boundAt)
}
