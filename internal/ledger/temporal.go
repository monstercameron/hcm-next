package ledger

import (
	"context"

	temporaladapter "github.com/monstercameron/human-capital-management-suite/internal/data/ledger/temporal"
)

// Temporal query value types, re-exported so a business package needs only
// this port import to ask the ledger a bitemporal question. They are plain
// data - a request, an authorization decision, an answer - with no behaviour
// that depends on PostgreSQL, so aliasing rather than redeclaring them
// avoids a parallel, driftable copy (the same reasoning as the append-path
// aliases in port.go).
type (
	TemporalRequest    = temporaladapter.Request
	TemporalDecision   = temporaladapter.Decision
	TemporalResult     = temporaladapter.Result
	TemporalMode       = temporaladapter.Mode
	TemporalOption     = temporaladapter.Option
	Assertion          = temporaladapter.Assertion
	AuthorityLabel     = temporaladapter.AuthorityLabel
	Coordinate         = temporaladapter.Coordinate
	ReconstructedState = temporaladapter.State
	TruthClass         = temporaladapter.TruthClass
)

// The six query modes, re-exported. There is no seventh.
const (
	ModeCurrent       = temporaladapter.ModeCurrent
	ModeEffectiveAsOf = temporaladapter.ModeEffectiveAsOf
	ModeKnownAsOf     = temporaladapter.ModeKnownAsOf
	ModeBetween       = temporaladapter.ModeBetween
	ModeHistory       = temporaladapter.ModeHistory
	ModeReconstruct   = temporaladapter.ModeReconstruct
)

// The truth classes an answer may be labelled with. Only [TruthDomain] and
// [TruthTransaction] are ever resolved into reconstructed state; see
// internal/data/ledger/temporal's package doc for why an external
// observation is not one of them.
const (
	TruthDomain      = temporaladapter.TruthDomain
	TruthTransaction = temporaladapter.TruthTransaction
	TruthObserved    = temporaladapter.TruthObserved
	TruthClaimed     = temporaladapter.TruthClaimed
	TruthUnresolved  = temporaladapter.TruthUnresolved
)

// TemporalReader answers bitemporal questions about recorded assertions.
//
// It is a distinct port from [Reader] on purpose. [Reader] is the replay and
// verification surface: it hands back a whole stream, unfiltered and
// unresolved, and it is what an integrity check or a projection rebuild
// needs. TemporalReader is the question-answering surface: every call is
// scoped by an authorization decision and a bitemporal coordinate, and every
// answer is labelled with its truth class, its authority and the exact
// source event it came from. A caller that only needs to ask "what is true
// now, that I am allowed to see" should depend on this and never on
// [Reader], which enforces no authorization at all.
//
// internal/data/ledger/temporal implements it: Query and Reconstruct there
// are ordinary functions, so [TemporalAdapter] binds them into this method
// set at a composition root.
type TemporalReader interface {
	// Query answers any of the five listing and point-resolution modes.
	// RECONSTRUCT is refused here because it returns a state rather than a
	// page; call Reconstruct for it.
	Query(ctx context.Context, q Querier, req TemporalRequest, dec TemporalDecision, opts ...TemporalOption) (TemporalResult, error)
	// Reconstruct rebuilds one subject's whole state at one coordinate.
	Reconstruct(ctx context.Context, q Querier, req TemporalRequest, dec TemporalDecision, opts ...TemporalOption) (ReconstructedState, error)
}

// TemporalAdapter binds internal/data/ledger/temporal's package-level
// functions to [TemporalReader]. It holds no state; the value exists only so
// the free functions can satisfy an interface.
type TemporalAdapter struct{}

// NewTemporalReader returns the default [TemporalReader].
func NewTemporalReader() TemporalAdapter { return TemporalAdapter{} }

// Query implements [TemporalReader].
func (TemporalAdapter) Query(ctx context.Context, q Querier, req TemporalRequest, dec TemporalDecision, opts ...TemporalOption) (TemporalResult, error) {
	return temporaladapter.Query(ctx, q, req, dec, opts...)
}

// Reconstruct implements [TemporalReader].
func (TemporalAdapter) Reconstruct(ctx context.Context, q Querier, req TemporalRequest, dec TemporalDecision, opts ...TemporalOption) (ReconstructedState, error) {
	return temporaladapter.Reconstruct(ctx, q, req, dec, opts...)
}
