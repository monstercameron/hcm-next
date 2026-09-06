package temporal

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/bitemporal"
	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
)

// maxCorrectionDepth bounds the correction-chain walk [resolveTruthClass]
// performs. A well-formed ledger can never need it: a correction can only
// name a target that was already durably recorded, so the chain is acyclic
// and finite by construction (internal/data/ledger/lineage makes the same
// argument at length). The bound exists so a corrupted or adversarially
// constructed graph makes the walk fail closed rather than loop.
const maxCorrectionDepth = 1_000

// resolveTruthClass returns the truth class an assertion may be presented
// as. A non-correction carries its own class. A CORRECTION inherits the
// class of the assertion at the end of its correction chain, walked only
// through assertions present in visible - the caller's own authorized,
// coordinate-bounded set.
//
// It returns [TruthUnresolved] when the chain leaves that set, revisits a
// reference, or exceeds [maxCorrectionDepth]. Leaving the set happens for a
// correction whose target is withheld by the authorization decision, is
// effective or recorded outside the queried coordinate, or lies on another
// stream than the one being reconstructed. In every one of those cases the
// origin was not proven, and an unproven origin must never be promoted into
// domain truth - so the correction is returned as evidence instead.
func resolveTruthClass(a Assertion, visible map[datalogger.EventRef]Assertion) TruthClass {
	if a.AssertionClass != datalogger.Correction {
		return truthClassOf(a.AssertionClass)
	}
	seen := map[datalogger.EventRef]bool{a.Ref: true}
	current := a
	for depth := 0; depth < maxCorrectionDepth; depth++ {
		if current.Corrects == nil {
			return TruthUnresolved
		}
		next, ok := visible[*current.Corrects]
		if !ok || seen[next.Ref] {
			return TruthUnresolved
		}
		if next.AssertionClass != datalogger.Correction {
			return truthClassOf(next.AssertionClass)
		}
		seen[next.Ref] = true
		current = next
	}
	return TruthUnresolved
}

// beats reports whether candidate should replace incumbent as the winning
// assertion for a field. The rule is the ledger's deterministic resolution
// order and nothing else: the greatest effective_at, tie-broken by the
// greatest recorded_at, tie-broken by the greatest sequence. It is the same
// order internal/data/bitemporal resolves by, so both plans agree.
func beats(candidate, incumbent Assertion) bool {
	switch {
	case candidate.EffectiveAt.After(incumbent.EffectiveAt):
		return true
	case candidate.EffectiveAt.Before(incumbent.EffectiveAt):
		return false
	case candidate.RecordedAt.After(incumbent.RecordedAt):
		return true
	case candidate.RecordedAt.Before(incumbent.RecordedAt):
		return false
	default:
		return candidate.Ref.Sequence > incumbent.Ref.Sequence
	}
}

// Fold reconstructs one subject's state from the visible, authorized
// assertions at a coordinate. It is pure: it reads no database, consults no
// clock, and mutates nothing the caller passes in beyond the truth-class and
// supersession labels it writes onto its own copy.
//
// It performs three passes, in this order, and the order is the contract:
//
//  1. Index every assertion by its reference and mark every assertion that a
//     visible assertion corrects as Superseded. A superseded assertion can
//     never win, no matter how the temporal comparison would have gone.
//  2. Resolve each assertion's truth class through [resolveTruthClass],
//     failing closed to [TruthUnresolved] whenever the correction chain
//     leaves the visible set.
//  3. Pick a winner per schema_ref independently inside each promotable
//     truth class, and collect everything unpromotable as evidence.
//
// Because step 3 never compares across truth classes, an EXTERNAL_OBSERVATION
// recorded later than - and effective after - a DOMAIN_FACT for the same
// field does not displace it. The observation is returned in
// [State.Unpromoted]; the domain fact stays in [State.Domain].
func Fold(subject string, coord Coordinate, assertions []Assertion) State {
	visible := make(map[datalogger.EventRef]Assertion, len(assertions))
	for _, a := range assertions {
		visible[a.Ref] = a
	}
	superseded := make(map[datalogger.EventRef]bool, len(assertions))
	for _, a := range assertions {
		if a.Corrects != nil {
			if _, ok := visible[*a.Corrects]; ok {
				superseded[*a.Corrects] = true
			}
		}
	}

	state := State{Subject: subject, Coordinate: coord, Considered: len(assertions)}
	domain := map[string]Assertion{}
	transaction := map[string]Assertion{}

	for _, a := range assertions {
		a.TruthClass = resolveTruthClass(a, visible)
		a.Superseded = superseded[a.Ref]
		state.Tenant = a.Tenant

		if !a.TruthClass.Promotable() {
			state.Unpromoted = append(state.Unpromoted, a)
			continue
		}
		if a.Superseded {
			// Superseded facts are history, not state. They are reachable
			// through the listing modes and through the correction that
			// replaced them; they are not evidence about the present either,
			// so they are simply not part of the reconstructed answer.
			continue
		}

		into := domain
		if a.TruthClass == TruthTransaction {
			into = transaction
		}
		if incumbent, ok := into[a.SchemaRef]; !ok || beats(a, incumbent) {
			into[a.SchemaRef] = a
		}
	}

	state.Domain = sortedByField(domain)
	state.Transaction = sortedByField(transaction)
	sortChronologically(state.Unpromoted)
	return state
}

// sortedByField flattens a winner-per-field map into a slice ordered by
// schema_ref, so a State is a deterministic value regardless of map order.
func sortedByField(byField map[string]Assertion) []Assertion {
	if len(byField) == 0 {
		return nil
	}
	out := make([]Assertion, 0, len(byField))
	for _, a := range byField {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SchemaRef < out[j].SchemaRef })
	return out
}

// sortChronologically orders evidence the way the ledger orders history:
// effective time, then recorded time, then sequence.
func sortChronologically(assertions []Assertion) {
	sort.Slice(assertions, func(i, j int) bool {
		a, b := assertions[i], assertions[j]
		if !a.EffectiveAt.Equal(b.EffectiveAt) {
			return a.EffectiveAt.Before(b.EffectiveAt)
		}
		if !a.RecordedAt.Equal(b.RecordedAt) {
			return a.RecordedAt.Before(b.RecordedAt)
		}
		if a.Ref.StreamKey != b.Ref.StreamKey {
			return a.Ref.StreamKey < b.Ref.StreamKey
		}
		return a.Ref.Sequence < b.Ref.Sequence
	})
}

// Plan is one way of answering a reconstruction. Every plan must produce the
// same [State] for the same request and decision; [VerifyPlanEquivalence]
// is what proves it for a given ledger rather than assuming it.
//
// A projection-backed plan - reading a maintained per-subject projection
// instead of the ledger - is a third implementation of this interface. It is
// only ever safe to serve from one because its answer can be checked against
// the ledger-derived answer through exactly this seam.
type Plan interface {
	// Name identifies the plan in an equivalence report.
	Name() string
	// Reconstruct answers one reconstruction request.
	Reconstruct(ctx context.Context, q Querier, req Request, dec Decision, opts ...Option) (State, error)
}

// LedgerPlan answers a reconstruction with one statement: the coordinate and
// every authorization boundary are pushed into SQL (see
// [BuildReconstructSQL]) and the rows are folded once in Go.
type LedgerPlan struct{}

// Name implements [Plan].
func (LedgerPlan) Name() string { return "ledger" }

// Reconstruct implements [Plan].
func (LedgerPlan) Reconstruct(ctx context.Context, q Querier, req Request, dec Decision, opts ...Option) (State, error) {
	coord, err := prepare(&req, dec, opts)
	if err != nil {
		return State{}, err
	}
	limit := reconstructLimit(req)

	sqlText, args, err := BuildReconstructSQL(req, dec, coord, limit)
	if err != nil {
		return State{}, err
	}
	rows, err := q.Query(ctx, sqlText, args...)
	if err != nil {
		return State{}, fmt.Errorf("temporal: reconstruct %s: %w", req.Subject, err)
	}
	defer rows.Close()

	assertions := make([]Assertion, 0, 16)
	for rows.Next() {
		a, scanErr := scanAssertion(rows)
		if scanErr != nil {
			return State{}, fmt.Errorf("temporal: scan assertion for %s: %w", req.Subject, scanErr)
		}
		assertions = append(assertions, a)
	}
	if err := rows.Err(); err != nil {
		return State{}, fmt.Errorf("temporal: reconstruct %s: %w", req.Subject, err)
	}
	if len(assertions) > limit {
		return State{}, ErrReconstructTooLarge{Subject: req.Subject, Limit: limit}
	}
	if err := loadAuthorityLabels(ctx, q, req.Tenant, assertions); err != nil {
		return State{}, err
	}

	state := Fold(req.Subject, coord, assertions)
	state.Tenant = req.Tenant
	return state, nil
}

// HistoryFoldPlan answers a reconstruction by paging
// internal/data/bitemporal's HISTORY mode and folding the result in Go.
//
// It is the reference plan: it reuses DATA-005's own authorized SQL
// unchanged, so an answer it produces is by construction the answer that
// adapter would give. It reads more rows than [LedgerPlan] (HISTORY has no
// effective-time bound, so future-effective assertions are fetched and then
// discarded), which is exactly why it is the reference and not the default.
type HistoryFoldPlan struct{}

// Name implements [Plan].
func (HistoryFoldPlan) Name() string { return "history-fold" }

// Reconstruct implements [Plan].
func (HistoryFoldPlan) Reconstruct(ctx context.Context, q Querier, req Request, dec Decision, opts ...Option) (State, error) {
	coord, err := prepare(&req, dec, opts)
	if err != nil {
		return State{}, err
	}
	limit := reconstructLimit(req)

	history := bitemporal.Request{
		Tenant:  req.Tenant,
		Mode:    bitemporal.ModeHistory,
		Subject: req.Subject,
		Field:   req.Field,
		KnownAt: coord.KnownAt,
		Limit:   bitemporal.MaxPageSize,
	}

	var assertions []Assertion
	for {
		page, pageErr := bitemporal.Query(ctx, q, history, dec, bitemporal.WithClock(func() time.Time { return coord.KnownAt }))
		if pageErr != nil {
			return State{}, fmt.Errorf("temporal: reconstruct %s from history: %w", req.Subject, pageErr)
		}
		for _, fact := range page.Facts {
			// HISTORY carries no effective-time bound, so the coordinate's
			// business-time edge is applied here. It is a temporal bound, not
			// an authorization bound: every authorization boundary was already
			// applied by the statement that produced these rows.
			if fact.EffectiveAt.After(coord.EffectiveAt) {
				continue
			}
			assertions = append(assertions, fromFact(fact))
			if len(assertions) > limit {
				return State{}, ErrReconstructTooLarge{Subject: req.Subject, Limit: limit}
			}
		}
		if page.NextCursor == "" {
			break
		}
		history.Cursor = page.NextCursor
	}

	if err := loadAuthorityLabels(ctx, q, req.Tenant, assertions); err != nil {
		return State{}, err
	}
	state := Fold(req.Subject, coord, assertions)
	state.Tenant = req.Tenant
	return state, nil
}

// reconstructLimit is how many assertions a reconstruction is willing to
// fold: the request's own Limit when it sets one, otherwise
// [MaxReconstructEvents].
func reconstructLimit(req Request) int {
	if req.Limit > 0 && req.Limit < MaxReconstructEvents {
		return req.Limit
	}
	return MaxReconstructEvents
}

// Reconstruct answers a RECONSTRUCT request with the default plan.
func Reconstruct(ctx context.Context, q Querier, req Request, dec Decision, opts ...Option) (State, error) {
	req.Mode = ModeReconstruct
	return LedgerPlan{}.Reconstruct(ctx, q, req, dec, opts...)
}

// VerifyPlanEquivalence runs every plan over the same request and decision
// and proves they produced the same answer, by comparing [State.Digest]
// rather than by comparing structs field by field.
//
// It returns the first plan's state on success and [ErrPlansDisagree] naming
// the exact pair that differs on failure. Fewer than two plans is not an
// error: a single plan trivially agrees with itself, and callers that build
// their plan list from configuration should not have to special-case that.
func VerifyPlanEquivalence(ctx context.Context, q Querier, req Request, dec Decision, plans []Plan, opts ...Option) (State, error) {
	if len(plans) == 0 {
		return State{}, ErrRequestInvalid{Field: "plans", Reason: "at least one plan is required"}
	}

	var (
		first       State
		firstDigest string
	)
	for i, plan := range plans {
		state, err := plan.Reconstruct(ctx, q, req, dec, opts...)
		if err != nil {
			return State{}, fmt.Errorf("temporal: plan %q: %w", plan.Name(), err)
		}
		digest, err := state.Digest()
		if err != nil {
			return State{}, fmt.Errorf("temporal: plan %q: %w", plan.Name(), err)
		}
		if i == 0 {
			first, firstDigest = state, digest
			continue
		}
		if digest != firstDigest {
			return State{}, ErrPlansDisagree{
				Subject: req.Subject,
				PlanA:   plans[0].Name(), DigestA: firstDigest,
				PlanB: plan.Name(), DigestB: digest,
			}
		}
	}
	return first, nil
}
