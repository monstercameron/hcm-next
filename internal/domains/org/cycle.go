package org

// PROMOUX-005: "Include reporting-line and organization impact in management
// promotions."
//
// RED: a promotion into management can reach approval without a real cycle
// check on the reporting graph at all. Before this file, the only manager-
// cycle protections anywhere in the codebase (internal/domains/org's own
// ManagerMutation, internal/domains/promotion/localcommit's invariant
// evaluator and internal/domains/promotion/commit's Command) each compared
// a proposed manager against a caller-SUPPLIED slice of "ancestor" worker
// ids with slices.Contains -- a single-hop-shaped check dressed as a chain
// check, and one that only Detects a cycle if whoever built the slice
// happened to compute it correctly first. Nothing in the tree actually
// walks the reporting graph to decide whether a proposed edge closes a
// loop.
//
// GREEN, and REFACTOR's "the traversal belongs to internal/domains/org,
// and promotion asks it": this file is that traversal. It is Organization's
// own capability, reusing [ResolveManagerRelationships] -- the same
// bitemporal, authorization-aware walk ORG-002 already established -- to
// answer one new question of it: if worker Node's manager became
// ProposedManager, would Node appear somewhere in ProposedManager's own
// resolved ancestor chain, closing A -> ... -> Node -> ProposedManager -> ...
// -> Node? Depth is bounded only as a safety valve (the same MaxDepth
// ResolveManagerRelationships already takes), never assumed: a genuine
// multi-hop cycle several levels deep is found by walking the chain until it
// is found, and a genuinely deep, non-cycling chain is walked to its end and
// declared safe.
//
// An unresolved chain -- withheld, stale, ambiguous, disagreeing, or one
// that exceeds the declared depth bound -- is UNDETERMINED, never SAFE.
// "Not yet found" is not "not present": a promotion is never admitted on a
// safety check this function could not finish.
import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// CycleQuery asks whether setting Node's direct manager to ProposedManager
// would close a cycle in the reporting graph: Node becoming, directly or
// through any chain, its own manager.
type CycleQuery struct {
	Tenant values.TenantId
	// Node is the worker whose manager relationship is proposed to change.
	Node values.EntityRef
	// ProposedManager is the candidate new direct manager for Node.
	ProposedManager values.EntityRef
	AsOf            values.Instant
	KnownAt         values.KnownAt
	// MaxDepth bounds the walk above ProposedManager, exactly like
	// [ManagerResolutionRequest.MaxDepth]. It is a safety valve against
	// unbounded traversal, not a business assumption about how deep a
	// legitimate chain may be: a chain deeper than MaxDepth is reported
	// UNDETERMINED, never treated as safe.
	MaxDepth int
	// Authorize decides per-hop disclosure exactly as it does for
	// [ResolveManagerRelationships]. A cycle check run with a restrictive
	// authorizer can only ever under-resolve the chain (producing
	// UNDETERMINED where a fully authorized read would have found SAFE or
	// CYCLE); it can never manufacture a false SAFE.
	Authorize Authorizer
}

// Validate reports whether the query is well formed. It does not decide
// whether the proposed edge is safe; only [DetectManagerCycle] does that.
func (q CycleQuery) Validate() error {
	if err := q.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrInvalidRequest, err)
	}
	if err := q.Node.Validate(); err != nil {
		return fmt.Errorf("%w: node: %v", ErrInvalidRequest, err)
	}
	if q.Node.Tenant != q.Tenant || q.Node.Kind != people.KindWorker {
		return fmt.Errorf("%w: node is outside the requested tenant or is not a worker", ErrInvalidRequest)
	}
	if err := q.ProposedManager.Validate(); err != nil {
		return fmt.Errorf("%w: proposed manager: %v", ErrInvalidRequest, err)
	}
	if q.ProposedManager.Tenant != q.Tenant || q.ProposedManager.Kind != people.KindWorker {
		return fmt.Errorf("%w: proposed manager is outside the requested tenant or is not a worker", ErrInvalidRequest)
	}
	if err := q.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as-of: %v", ErrInvalidRequest, err)
	}
	if q.MaxDepth < 1 {
		return fmt.Errorf("%w: max depth must be positive", ErrInvalidRequest)
	}
	if q.Authorize == nil {
		return ErrAuthorizationMissing
	}
	return nil
}

// CycleStatus is the closed vocabulary [DetectManagerCycle] answers with.
type CycleStatus string

const (
	// CycleStatusUnspecified is the zero value and is never a legal answer.
	CycleStatusUnspecified CycleStatus = ""
	// CycleStatusSafe means the chain above ProposedManager was fully
	// resolved and Node does not appear in it: the proposed edge does not
	// close a cycle.
	CycleStatusSafe CycleStatus = "SAFE"
	// CycleStatusCycle means Node appears in ProposedManager's own resolved
	// ancestor chain (or ProposedManager is Node itself): the proposed edge
	// would close a cycle.
	CycleStatusCycle CycleStatus = "CYCLE"
	// CycleStatusUndetermined means the chain could not be fully resolved,
	// so safety cannot be certified either way.
	CycleStatusUndetermined CycleStatus = "UNDETERMINED"
)

// String returns the wire token, or "CYCLE_STATUS_UNSPECIFIED".
func (s CycleStatus) String() string {
	switch s {
	case CycleStatusSafe, CycleStatusCycle, CycleStatusUndetermined:
		return string(s)
	default:
		return "CYCLE_STATUS_UNSPECIFIED"
	}
}

// CycleFinding is [DetectManagerCycle]'s typed answer.
type CycleFinding struct {
	Status CycleStatus
	// Depth is how many hops above ProposedManager Node was found. It is
	// meaningful only when Status is CycleStatusCycle; zero means
	// ProposedManager is Node itself.
	Depth int
	// Resolution is the full resolved chain above ProposedManager, carried
	// for evidence and explanation. It is the zero value for the
	// self-management short-circuit, which never reads.
	Resolution ManagerResolution
}

// WouldCycle reports whether the proposed edge is a confirmed cycle. It is
// false both when the edge is confirmed safe and when safety could not be
// certified -- callers that must fail closed on an unresolved answer check
// [CycleFinding.Certain] as well, never WouldCycle alone.
func (f CycleFinding) WouldCycle() bool { return f.Status == CycleStatusCycle }

// Certain reports whether Status is a definitive answer (SAFE or CYCLE)
// rather than CycleStatusUndetermined.
func (f CycleFinding) Certain() bool {
	return f.Status == CycleStatusSafe || f.Status == CycleStatusCycle
}

// DetectManagerCycle reports whether making Node report to ProposedManager
// would close a cycle in the reporting graph.
//
// It is real reachability, not a shortcut: the only case decided without a
// read is Node == ProposedManager (a cycle of length one). Every other case
// resolves ProposedManager's own ancestor chain with
// [ResolveManagerRelationships] -- walking as many hops as MaxDepth allows,
// exactly the same bitemporal, authorization-checked traversal ORG-002 uses
// for every other manager-chain question -- and asks whether Node appears in
// it. A genuine multi-hop cycle several levels deep is found this way, and a
// genuinely deep, non-cycling chain is walked to its end and declared SAFE;
// neither answer depends on how deep the chain happens to be, only on
// whether the walk reaches Node before it reaches the top.
//
// The only error DetectManagerCycle returns is a contract failure (a nil
// reader, a malformed query, or the reader itself failing); every business
// outcome, including "the chain could not be resolved", is a [CycleFinding],
// never an error.
func DetectManagerCycle(ctx context.Context, reader WorkerFacts, q CycleQuery) (CycleFinding, error) {
	if reader == nil {
		return CycleFinding{}, fmt.Errorf("%w: no worker facts reader", ErrInvalidRequest)
	}
	if err := q.Validate(); err != nil {
		return CycleFinding{}, err
	}
	if q.Node == q.ProposedManager {
		return CycleFinding{Status: CycleStatusCycle, Depth: 0}, nil
	}

	resolution, err := ResolveManagerRelationships(ctx, reader, ManagerResolutionRequest{
		Tenant: q.Tenant, Worker: q.ProposedManager, AsOf: q.AsOf, KnownAt: q.KnownAt,
		MaxDepth: q.MaxDepth, Authorize: q.Authorize,
	})
	switch {
	case errors.Is(err, ErrDepthExceeded), errors.Is(err, ErrRelationshipCycle):
		// The chain above the proposed manager is either deeper than the
		// declared bound or the pre-existing graph already loops before it
		// settles. Either way this walk cannot certify that Node is absent
		// from it, so the answer is UNDETERMINED rather than a guess.
		return CycleFinding{Status: CycleStatusUndetermined}, nil
	case err != nil:
		return CycleFinding{}, err
	}

	if resolution.Disclosure == people.DisclosureWithheld {
		return CycleFinding{Status: CycleStatusUndetermined, Resolution: resolution}, nil
	}
	switch resolution.Status {
	case StatusStale, StatusDisagreeing, StatusAmbiguous:
		return CycleFinding{Status: CycleStatusUndetermined, Resolution: resolution}, nil
	}
	for _, hop := range resolution.Chain {
		if hop.Disclosure != people.DisclosureFull || hop.Manager.Access != people.AccessAuthorized {
			// A hop this read cannot see could hide Node further up the
			// chain. "Not found yet" is not "not present": the walk cannot
			// continue past an opaque hop, so it cannot certify safety.
			return CycleFinding{Status: CycleStatusUndetermined, Resolution: resolution}, nil
		}
		if hop.Manager.Value == q.Node {
			return CycleFinding{Status: CycleStatusCycle, Depth: hop.Level + 1, Resolution: resolution}, nil
		}
	}
	return CycleFinding{Status: CycleStatusSafe, Resolution: resolution}, nil
}
