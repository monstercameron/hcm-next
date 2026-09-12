package outbox

// EVENT-003: cross-tenant, cross-resource admission and one shared retry
// budget.
//
// Consumer.Poll (consumer.go) already orders by criticality within one
// tenant's rows, but that ordering never sees another tenant's rows or a
// resource a poller shares with them (a rate-limited downstream API, a
// single outbound mail relay). Nothing before this file stopped a flood of
// P3/P4 background work - concentrated in one tenant or spread across many
// - from crowding out a P0 payroll/IAM item that happened to be polled a
// moment later, and no downstream slow-down signal ever reached the queue
// to change what gets admitted next. Schedule and ResourceLedger close
// that gap by arbitrating a combined candidate pool (drawn from one or more
// Consumer.Poll calls) against shared, backpressure-aware resource
// capacity. RetryAccount closes the companion gap: retry accounting lived
// in three unconnected places (outbox's own attempts/backoff, a
// transaction coordinator's retry callback, admission's budgets), so
// nested layers could each charge their own retry for what was really one
// logical attempt. Routing every layer through the same
// admission.Provisioner, keyed by the same attempt identity, makes a
// replayed attempt collide on one stored receipt instead of consuming a
// second token.

import (
	"fmt"
	"sort"
	"sync"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
)

// Candidate is one unit of scheduling input: an outbox record plus the
// shared downstream resource it competes for. The record already carries
// its own tenant, so ordering and fairness never need a side channel.
type Candidate struct {
	Record   Record
	Resource string
}

// criticalityRank mirrors the durable ordering Consumer.Poll already
// applies in SQL (P0 highest through P4 lowest, anything unrecognized
// last), so the in-memory scheduler and the queue's own ORDER BY never
// disagree about which record is more urgent.
func criticalityRank(c string) int {
	switch c {
	case CriticalityP0:
		return 0
	case CriticalityP1:
		return 1
	case CriticalityP2:
		return 2
	case CriticalityP3:
		return 3
	case CriticalityP4:
		return 4
	default:
		return 5
	}
}

// sortCandidates orders by criticality first - globally, across every
// tenant and resource in the pool - then FIFO by AvailableAt and finally
// OutboxID so equal inputs always produce the same order. This is the part
// Consumer.Poll cannot provide alone: its ORDER BY only ever sees one
// tenant's rows, so nothing upstream of Schedule stops a low-priority
// flood in one tenant (or many) from being interleaved ahead of a P0 item
// from another.
func sortCandidates(candidates []Candidate) []Candidate {
	sorted := append([]Candidate(nil), candidates...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i].Record, sorted[j].Record
		if ra, rb := criticalityRank(a.Criticality), criticalityRank(b.Criticality); ra != rb {
			return ra < rb
		}
		if !a.AvailableAt.Equal(b.AvailableAt) {
			return a.AvailableAt.Before(b.AvailableAt)
		}
		return a.OutboxID.String() < b.OutboxID.String()
	})
	return sorted
}

// ResourcePolicy bounds admission for one shared resource. Capacity is the
// total number of candidates any single Schedule pass may admit for it.
// PerTenantShare additionally caps how much of that capacity one tenant may
// occupy, so a single tenant's flood cannot claim every slot a resource
// also owes to other tenants; zero means unbounded (still capped by
// Capacity).
type ResourcePolicy struct {
	Capacity       int
	PerTenantShare int
}

// ResourceLedger is the admission tracker for a set of shared resources. It
// is safe for concurrent use: multiple goroutines standing in for
// concurrent pollers or dispatchers share one ledger so a resource's
// capacity is enforced across all of them, not per caller.
type ResourceLedger struct {
	mu       sync.Mutex
	policy   map[string]ResourcePolicy
	ceiling  map[string]int // effective capacity for the current pass, after ApplyBackpressure
	inFlight map[string]int
	byTenant map[string]map[uuid.UUID]int
	claimed  map[uuid.UUID]string // outboxID -> resource, fences double-admission of one row
}

// NewResourceLedger builds a ledger from the configured per-resource
// policy. A resource absent from policy is unbounded until a policy or a
// backpressure decision names it.
func NewResourceLedger(policy map[string]ResourcePolicy) *ResourceLedger {
	p := make(map[string]ResourcePolicy, len(policy))
	ceiling := make(map[string]int, len(policy))
	for resource, rp := range policy {
		p[resource] = rp
		ceiling[resource] = rp.Capacity
	}
	return &ResourceLedger{
		policy:   p,
		ceiling:  ceiling,
		inFlight: make(map[string]int),
		byTenant: make(map[string]map[uuid.UUID]int),
		claimed:  make(map[uuid.UUID]string),
	}
}

// ApplyBackpressure folds one admission.BackpressureDecision into the
// resource's ceiling for the current pass: DEFER and STOP admit nothing
// until the caller observes health again, QUEUE and SLOW halve the
// baseline, and anything else (CONTINUE, an unrecognized action) restores
// the configured baseline. This is what makes a downstream slow-down
// signal actually change what the scheduler admits next instead of only
// being logged: a DEFER decision for a resource takes effect on the very
// next Schedule call over it.
func (l *ResourceLedger) ApplyBackpressure(resource string, decision admission.BackpressureDecision) {
	l.mu.Lock()
	defer l.mu.Unlock()
	policy, configured := l.policy[resource]
	switch decision.Action {
	case admission.BackpressureDefer, admission.BackpressureStop:
		l.ceiling[resource] = 0
	case admission.BackpressureQueue, admission.BackpressureSlowUpstream:
		if !configured {
			// An unbounded resource has no baseline to halve. Inventing one
			// would be a number nobody declared, so the slow-down leaves it
			// unbounded: bounding a resource is what ResourcePolicy is for.
			delete(l.ceiling, resource)
			return
		}
		// Never round a slow-down down into a full stop: a resource with a
		// capacity of one still admits one under QUEUE/SLOW, where integer
		// division alone would have said zero and silently made SLOW mean
		// STOP.
		halved := policy.Capacity / 2
		if halved < 1 {
			halved = 1
		}
		l.ceiling[resource] = halved
	default:
		// Restoring health must return an unpoliced resource to unbounded,
		// not to the zero value of a policy that was never configured --
		// that would let the signal meaning "everything is fine" wedge the
		// resource shut permanently.
		if !configured {
			delete(l.ceiling, resource)
			return
		}
		l.ceiling[resource] = policy.Capacity
	}
}

func (l *ResourceLedger) capacity(resource string) int {
	if ceiling, ok := l.ceiling[resource]; ok {
		return ceiling
	}
	if policy, ok := l.policy[resource]; ok {
		return policy.Capacity
	}
	return -1 // unbounded: no policy and no backpressure decision named this resource
}

// TryAdmit reserves one slot for resource on behalf of tenant/outboxID. It
// returns false when the resource (or the tenant's configured share of it)
// is already at capacity, or when outboxID was already admitted by a
// concurrent caller: the same physical row can never be reserved twice out
// of one ledger, no matter how many goroutines race to claim it.
func (l *ResourceLedger) TryAdmit(resource string, tenant uuid.UUID, outboxID uuid.UUID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, already := l.claimed[outboxID]; already {
		return false
	}
	if capacity := l.capacity(resource); capacity >= 0 && l.inFlight[resource] >= capacity {
		return false
	}
	if share := l.policy[resource].PerTenantShare; share > 0 {
		if l.byTenant[resource] == nil {
			l.byTenant[resource] = make(map[uuid.UUID]int)
		}
		if l.byTenant[resource][tenant] >= share {
			return false
		}
		l.byTenant[resource][tenant]++
	}
	l.inFlight[resource]++
	l.claimed[outboxID] = resource
	return true
}

// Release returns a slot after a dispatch completes or is abandoned, so a
// later Schedule pass can reuse it.
func (l *ResourceLedger) Release(outboxID uuid.UUID, tenant uuid.UUID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	resource, ok := l.claimed[outboxID]
	if !ok {
		return
	}
	delete(l.claimed, outboxID)
	if l.inFlight[resource] > 0 {
		l.inFlight[resource]--
	}
	if byTenant := l.byTenant[resource]; byTenant != nil && byTenant[tenant] > 0 {
		byTenant[tenant]--
	}
}

// InFlight reports how many slots resource currently holds admitted.
func (l *ResourceLedger) InFlight(resource string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.inFlight[resource]
}

// ScheduleResult is one scheduling pass's outcome. Admitted candidates
// should be dispatched now; Deferred candidates should be reconsidered on
// a later pass (their resource or tenant share was at capacity, or
// backpressure zeroed the resource for this pass).
type ScheduleResult struct {
	Admitted []Candidate
	Deferred []Candidate
}

// Schedule orders candidates by criticality first - so a P0 item is always
// considered before any P3/P4 backlog regardless of which tenant either
// belongs to - and admits them against ledger's shared, resource-scoped
// capacity. It performs no I/O: it is the deterministic policy a caller
// applies to whatever candidates one or more Consumer.Poll calls already
// claimed, or to records fed back in after a prior pass deferred them.
func Schedule(candidates []Candidate, ledger *ResourceLedger) ScheduleResult {
	var result ScheduleResult
	for _, candidate := range sortCandidates(candidates) {
		if ledger.TryAdmit(candidate.Resource, candidate.Record.Tenant, candidate.Record.OutboxID) {
			result.Admitted = append(result.Admitted, candidate)
		} else {
			result.Deferred = append(result.Deferred, candidate)
		}
	}
	return result
}

// AttemptIdentity derives the stable, replay-safe attempt identity every
// layer must compute the same way to actually share one retry budget: the
// envelope's LogicalOperationID (immutable across redelivery, unlike
// AttemptID which changes on every claim) when known, otherwise the row's
// own OutboxID, joined with the physical attempt number. Two layers
// observing the same physical attempt of the same logical operation - one
// inside outbox's own failure handling, another in a transaction
// coordinator's retry callback - compute the same identity and therefore
// collide on the same stored receipt in admission.Provisioner.Consume
// instead of each minting a fresh token.
func AttemptIdentity(rec Record, attempt int) string {
	logicalOp := rec.OutboxID.String()
	if rec.Causal != nil && rec.Causal.LogicalOperationID != "" {
		logicalOp = rec.Causal.LogicalOperationID
	}
	return fmt.Sprintf("%s#%d", logicalOp, attempt)
}

// RetryAccount bridges outbox's own attempt bookkeeping to admission's
// durable retry budget so a logical operation retried at several
// unconnected layers is charged once per logical attempt rather than once
// per layer that happens to notice the failure.
type RetryAccount struct {
	Provisioner *admission.Provisioner
}

// Consume provisions (idempotently: re-provisioning never resets or grants
// fresh allowance) the named operation's budget and consumes one token for
// attemptID. A replayed identity - the same logical attempt seen again by
// this layer or another sharing this Provisioner - returns the stored
// receipt rather than a second token: admission.Provisioner.Consume is
// already replay-safe by AttemptID, so accounting here never double-charges.
func (a RetryAccount) Consume(spec admission.ProvisionSpec, attemptID string, attempt admission.RetryAttempt) (admission.RetryReceipt, error) {
	if a.Provisioner == nil {
		return admission.RetryReceipt{}, fmt.Errorf("outbox: retry account requires a provisioner")
	}
	budget, err := a.Provisioner.Provision(spec)
	if err != nil {
		return admission.RetryReceipt{}, err
	}
	return a.Provisioner.Consume(budget.ID, admission.AttemptInput{AttemptID: attemptID, Attempt: attempt})
}
