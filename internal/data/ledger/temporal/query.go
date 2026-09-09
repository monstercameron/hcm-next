package temporal

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/bitemporal"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
)

// Option configures one query.
type Option func(*options)

type options struct {
	now func() time.Time
}

// WithClock replaces the source of "now" used to default an unspecified
// EffectiveAt or KnownAt. Tests use it to make defaulting deterministic and
// to prove replay stability without racing the wall clock.
func WithClock(now func() time.Time) Option {
	return func(o *options) { o.now = now }
}

func resolveOptions(opts []Option) options {
	o := options{now: func() time.Time { return time.Now().UTC() }}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// clampKnownAt applies the decision's knowledge ceiling to a requested
// horizon, returning the earlier of the two. A caller can never see anything
// recorded after the moment their own authorization was established merely
// by asking for a later KnownAt. A zero MaxKnownAt is "no ceiling".
//
// internal/data/bitemporal applies the identical clamp to the five delegated
// modes from its own unexported copy; this one exists because RECONSTRUCT
// resolves its coordinate before any statement is built.
func clampKnownAt(dec Decision, requested time.Time) time.Time {
	if dec.MaxKnownAt.IsZero() {
		return requested
	}
	if requested.IsZero() || dec.MaxKnownAt.Before(requested) {
		return dec.MaxKnownAt
	}
	return requested
}

// prepare validates a RECONSTRUCT request against its decision and resolves
// the bitemporal coordinate it will be answered at, defaulting either axis
// to the injected clock and clamping the knowledge horizon to the decision's
// ceiling. It normalizes req.Mode so a caller that reached a plan directly
// cannot ask it to build the wrong statement.
func prepare(req *Request, dec Decision, opts []Option) (Coordinate, error) {
	req.Mode = ModeReconstruct
	if err := req.Validate(); err != nil {
		return Coordinate{}, err
	}
	if err := dec.Validate(); err != nil {
		return Coordinate{}, err
	}
	if req.Tenant != dec.Tenant {
		return Coordinate{}, ErrTenantMismatch{RequestTenant: req.Tenant, DecisionTenant: dec.Tenant}
	}

	now := resolveOptions(opts).now()
	coord := Coordinate{EffectiveAt: req.EffectiveAt, KnownAt: req.KnownAt}
	if coord.EffectiveAt.IsZero() {
		coord.EffectiveAt = now
	}
	if coord.KnownAt.IsZero() {
		coord.KnownAt = now
	}
	coord.KnownAt = clampKnownAt(dec, coord.KnownAt)
	return coord, nil
}

// Query answers any of the six modes.
//
// The five DATA-005 modes are delegated to internal/data/bitemporal, whose
// SQL already compiles every authorization boundary into the statement; this
// function relabels the result with the truth class, the resolved authority
// label and the source event ID that LEDGER-006 owes a caller, and reports
// the exact coordinate the answer was resolved at.
//
// RECONSTRUCT is answered by this package's own [Reconstruct]; because a
// reconstruction is a [State] rather than a page of assertions, Query
// refuses it with a typed error instead of flattening it into a shape that
// would lose the domain/evidence separation.
func Query(ctx context.Context, q Querier, req Request, dec Decision, opts ...Option) (Result, error) {
	if err := req.Validate(); err != nil {
		return Result{}, err
	}
	if err := dec.Validate(); err != nil {
		return Result{}, err
	}
	if req.Tenant != dec.Tenant {
		return Result{}, ErrTenantMismatch{RequestTenant: req.Tenant, DecisionTenant: dec.Tenant}
	}
	if !req.Mode.delegated() {
		return Result{}, ErrRequestInvalid{
			Field:  "Mode",
			Reason: "RECONSTRUCT returns a State; call Reconstruct instead of Query",
		}
	}

	now := resolveOptions(opts).now()
	page, err := bitemporal.Query(ctx, q, req.delegate(), dec, bitemporal.WithClock(func() time.Time { return now }))
	if err != nil {
		return Result{}, err
	}

	assertions := make([]Assertion, 0, len(page.Facts))
	for _, fact := range page.Facts {
		assertions = append(assertions, fromFact(fact))
	}
	if err := loadAuthorityLabels(ctx, q, req.Tenant, assertions); err != nil {
		return Result{}, err
	}
	labelWithinPage(assertions)

	return Result{
		Mode:       req.Mode,
		Coordinate: coordinateOf(req, dec, now),
		Assertions: assertions,
		NextCursor: page.NextCursor,
	}, nil
}

// coordinateOf reports the exact coordinate a delegated mode resolved at,
// applying the same defaulting internal/data/bitemporal.computeBounds does
// so that a returned Result can be replayed without knowing what "now" was.
// HISTORY has no effective-time edge; its EffectiveAt is reported as the
// zero time rather than as a bound it did not apply.
func coordinateOf(req Request, dec Decision, now time.Time) Coordinate {
	knownAt := req.KnownAt
	if knownAt.IsZero() {
		knownAt = now
	}
	coord := Coordinate{KnownAt: clampKnownAt(dec, knownAt)}
	switch req.Mode {
	case ModeCurrent:
		coord.EffectiveAt = now
		coord.KnownAt = clampKnownAt(dec, now)
	case ModeEffectiveAsOf:
		coord.EffectiveAt = req.EffectiveAt
	case ModeKnownAsOf:
		coord.EffectiveAt = req.EffectiveAt
		if coord.EffectiveAt.IsZero() {
			coord.EffectiveAt = now
		}
	case ModeBetween:
		coord.EffectiveAt = req.EffectiveTo
	}
	return coord
}

// labelWithinPage resolves truth class and supersession using only the
// assertions on this page.
//
// A listing mode returns history, and history is paged: a correction whose
// target sits on another page is labelled [TruthUnresolved] here even though
// a reconstruction over the same subject would resolve it. That is the
// deliberate direction of the error. Truth class is resolved only from
// assertions the caller is authorized to see and is actually holding;
// widening the resolution to rows outside the page would mean reading -
// and leaking the class of - assertions the decision may withhold. The
// promotion decision that actually matters is made by [Fold] over a whole
// coordinate-bounded set, never by this labelling.
func labelWithinPage(assertions []Assertion) {
	visible := make(map[datalogger.EventRef]Assertion, len(assertions))
	for _, a := range assertions {
		visible[a.Ref] = a
	}
	corrected := make(map[datalogger.EventRef]bool, len(assertions))
	for _, a := range assertions {
		if a.Corrects != nil {
			if _, ok := visible[*a.Corrects]; ok {
				corrected[*a.Corrects] = true
			}
		}
	}
	for i := range assertions {
		assertions[i].TruthClass = resolveTruthClass(assertions[i], visible)
		assertions[i].Superseded = corrected[assertions[i].Ref]
	}
}

// fromFact projects one internal/data/bitemporal.Fact onto this package's
// answer shape. The authority label starts out as the bare reference the
// event carries; [loadAuthorityLabels] resolves it. Truth class starts out
// empty and is written by [labelWithinPage] or [Fold], never guessed here
// from a single row.
func fromFact(fact bitemporal.Fact) Assertion {
	return Assertion{
		Tenant:          fact.Tenant,
		Ref:             datalogger.EventRef{StreamKey: fact.StreamKey, Sequence: fact.Sequence},
		SourceEventID:   fact.EventID,
		SchemaRef:       fact.SchemaRef,
		AssertionClass:  fact.AssertionClass,
		CorrectionKind:  fact.CorrectionKind,
		Authority:       AuthorityLabel{Present: fact.Authority != "", Ref: fact.Authority},
		SourceRef:       fact.SourceRef,
		Payload:         fact.Payload,
		ArtifactRef:     fact.ArtifactRef,
		Digest:          fact.Digest,
		DigestAlgorithm: fact.DigestAlgorithm,
		OccurredAt:      fact.OccurredAt,
		EffectiveAt:     fact.EffectiveAt,
		RecordedAt:      fact.RecordedAt,
		CorrelationID:   fact.CorrelationID,
		CausationID:     fact.CausationID,
		Corrects:        fact.Corrects,
	}
}

// AsOf resolves what was effective at a business instant for one subject and
// field. It is [Query] with Mode = EFFECTIVE_AS_OF.
func AsOf(ctx context.Context, q Querier, req Request, dec Decision, opts ...Option) (Result, error) {
	req.Mode = ModeEffectiveAsOf
	return Query(ctx, q, req, dec, opts...)
}

// KnownAsOf resolves what the ledger had recorded by a system instant. It is
// [Query] with Mode = KNOWN_AS_OF: a correction recorded after that instant
// never appears, because the horizon is a SQL predicate.
func KnownAsOf(ctx context.Context, q Querier, req Request, dec Decision, opts ...Option) (Result, error) {
	req.Mode = ModeKnownAsOf
	return Query(ctx, q, req, dec, opts...)
}

// History returns every visible assertion for a subject and field, with no
// effective-time bound.
func History(ctx context.Context, q Querier, req Request, dec Decision, opts ...Option) (Result, error) {
	req.Mode = ModeHistory
	return Query(ctx, q, req, dec, opts...)
}

// Between returns every assertion effective in the half-open window
// [EffectiveFrom, EffectiveTo). A fact effective exactly at a boundary
// belongs to exactly one of two adjacent windows, so paging a timeline by
// adjacent windows can never return it twice.
func Between(ctx context.Context, q Querier, req Request, dec Decision, opts ...Option) (Result, error) {
	req.Mode = ModeBetween
	return Query(ctx, q, req, dec, opts...)
}
