package reconcile

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/effectgraph"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
)

// Coordinator triggers and advances reconciliation jobs.
//
// It holds no state and starts nothing: every method takes the caller's
// transaction and the caller's instant, and every write presents the
// caller's lease fence. There is no ticker, no goroutine and no ambient
// clock anywhere in this package, matching the discipline
// internal/workflow/timer and internal/workflow/lease already hold.
type Coordinator struct {
	// Store persists job rows. Required.
	Store Store
	// Observer reads the external system for a job's watched effect.
	// Required for [Coordinator.Poll]; unused by [Coordinator.Trigger].
	Observer Observer
	// Comparer evaluates an observation against what was intended. Required
	// for [Coordinator.Poll]; unused by [Coordinator.Trigger].
	Comparer Comparer
	// Fences verifies a presented lease fence before any write. Required. A
	// [lease.Manager] value satisfies it directly.
	Fences FenceVerifier
	// Backoff computes how long to wait before the next poll, given the
	// attempt number just recorded. Nil uses [DefaultBackoff].
	Backoff func(attempt int) time.Duration
}

func (c Coordinator) backoffFor(attempt int) time.Duration {
	if c.Backoff != nil {
		return c.Backoff(attempt)
	}
	return DefaultBackoff(attempt)
}

// DefaultBackoff is the reconciliation poll backoff used when a [Coordinator]
// declares none: one minute, doubling per attempt, capped at 24 hours. It is
// a pure function of the attempt number, never of a clock.
func DefaultBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	const maxShift = 10 // 2^10 minutes is already past the 24h cap below
	if shift > maxShift {
		shift = maxShift
	}
	d := time.Minute * time.Duration(uint64(1)<<uint(shift))
	const cap = 24 * time.Hour
	if d > cap {
		d = cap
	}
	return d
}

// TriggerRequest asks for one reconciliation job on a committed mandatory
// effect, under one comparison policy.
type TriggerRequest struct {
	TenantID uuid.UUID

	// Effect is the committed effect this job will watch. It must declare a
	// required observation contract (Effect.Observation.Required): this
	// package refuses to create a job for an effect that made no such
	// promise. Effect.IdempotencyKey and Effect.RepairPolicy are carried onto
	// the job as EffectRef and RepairPolicy respectively.
	Effect effectgraph.EffectNode
	// PolicyRef names the comparison policy this job is evaluated under.
	PolicyRef string

	IntendedRef       string
	RequiredFreshness observe.Freshness

	// SLARef names the service-level commitment this job is measured
	// against.
	SLARef string
	// Deadline is the instant beyond which this job is exhausted if it has
	// not reached a determinate verdict. It must be strictly after Now.
	Deadline time.Time

	// Fence is the lease the caller holds; its Holder becomes the job's
	// initial Owner. Verified before anything is written.
	Fence lease.Fence
	// Now is the caller's own clock reading.
	Now time.Time
}

func (r TriggerRequest) validate() error {
	switch {
	case r.TenantID == uuid.Nil:
		return invalid(uuid.Nil, uuid.Nil, "tenant id must not be the nil UUID")
	case r.Effect.ID == "" || r.Effect.IdempotencyKey == "":
		return invalid(uuid.Nil, uuid.Nil,
			"triggering reconciliation needs a compiled effect with an id and an idempotency key")
	case !r.Effect.Observation.Required:
		return refuse(CodeNotMandatory, ErrInvalid, uuid.Nil, uuid.Nil,
			"effect %s declares no mandatory observation contract; reconciliation exists for mandatory effects only",
			r.Effect.ID)
	case r.PolicyRef == "":
		return invalid(uuid.Nil, uuid.Nil, "a comparison policy reference is required")
	case r.IntendedRef == "":
		return invalid(uuid.Nil, uuid.Nil, "an intended-state reference is required")
	case !r.RequiredFreshness.Valid():
		return invalid(uuid.Nil, uuid.Nil, "required freshness %q is not a declared value", string(r.RequiredFreshness))
	case r.SLARef == "":
		return invalid(uuid.Nil, uuid.Nil, "an SLA reference is required")
	case r.Now.IsZero():
		return invalid(uuid.Nil, uuid.Nil, "triggering reconciliation needs the caller's own clock reading")
	case r.Deadline.IsZero() || !r.Deadline.After(r.Now):
		return invalid(uuid.Nil, uuid.Nil, "deadline must be strictly after the trigger instant")
	case r.Fence.TenantID != r.TenantID:
		return invalid(uuid.Nil, uuid.Nil, "the presented fence names a different tenant than the request")
	}
	if err := r.Fence.Validate(); err != nil {
		return wrapErr(CodeInvalid, ErrInvalid, uuid.Nil, uuid.Nil, err, "the presented lease fence is malformed")
	}
	return nil
}

// Triggered is the result of [Coordinator.Trigger].
type Triggered struct {
	Job Job
	// Replay reports that this exact effect and policy already had a job --
	// nothing was written this time, and Job is the one already on the
	// table.
	Replay   bool
	Evidence Evidence
}

// Trigger creates one reconciliation job for a committed mandatory effect
// under one comparison policy, or returns the job that already exists.
//
// The job's identity is derived from (tenant, effect, policy) by [JobID], so
// a duplicate trigger for the same effect and policy addresses the promise
// already made: the second call reports Replay and writes nothing.
func (c Coordinator) Trigger(ctx context.Context, ex Executor, req TriggerRequest) (Triggered, error) {
	if c.Store == nil {
		return Triggered{}, invalid(uuid.Nil, uuid.Nil, "coordinator has no store")
	}
	if c.Fences == nil {
		return Triggered{}, invalid(uuid.Nil, uuid.Nil, "coordinator has no fence verifier")
	}
	if err := req.validate(); err != nil {
		return Triggered{}, err
	}
	if _, err := c.Fences.Verify(ctx, ex, req.Fence, req.Now); err != nil {
		return Triggered{}, wrapErr(CodeFenceRefused, ErrFenceRefused, req.TenantID, uuid.Nil, err,
			"the fence presented for triggering reconciliation was refused")
	}

	id := JobID(req.TenantID, req.Effect.IdempotencyKey, req.PolicyRef)
	job := Job{
		TenantID: req.TenantID, JobID: id,
		EffectRef: req.Effect.IdempotencyKey, EffectID: req.Effect.ID, PolicyRef: req.PolicyRef,
		IntendedRef: req.IntendedRef, CanonicalRef: "",
		RequiredFreshness:   req.RequiredFreshness,
		ObservationAttempts: 0,
		NextCheckAt:         req.Now,
		Deadline:            req.Deadline,
		Owner:               req.Fence.Holder.HolderID(),
		SLARef:              req.SLARef,
		RepairPolicy:        req.Effect.RepairPolicy,
		Status:              StatusPending,
		Version:             1,
		CreatedAt:           req.Now,
		UpdatedAt:           req.Now,
	}
	stored, existing, err := c.Store.Create(ctx, ex, job)
	if err != nil {
		return Triggered{}, err
	}
	kind := EventTriggered
	reason := ""
	if existing {
		kind = EventReplayed
		reason = "a reconciliation job for this effect and comparison policy was already triggered"
	}
	return Triggered{
		Job: stored, Replay: existing,
		Evidence: newEvidence(kind, stored, req.Now, req.Fence.Token, reason),
	}, nil
}

// PollRequest asks for one reconciliation attempt on one already-triggered
// job.
type PollRequest struct {
	TenantID uuid.UUID
	JobID    uuid.UUID

	// Fence is the lease the caller holds. Verified before anything is
	// written; the job's Owner is updated to the fence's holder.
	Fence lease.Fence
	// Now is the caller's own clock reading.
	Now time.Time
}

func (r PollRequest) validate() error {
	switch {
	case r.TenantID == uuid.Nil:
		return invalid(uuid.Nil, uuid.Nil, "tenant id must not be the nil UUID")
	case r.JobID == uuid.Nil:
		return invalid(r.TenantID, uuid.Nil, "job id must not be the nil UUID")
	case r.Now.IsZero():
		return invalid(r.TenantID, r.JobID, "polling reconciliation needs the caller's own clock reading")
	case r.Fence.TenantID != r.TenantID:
		return invalid(r.TenantID, r.JobID, "the presented fence names a different tenant than the request")
	}
	if err := r.Fence.Validate(); err != nil {
		return wrapErr(CodeInvalid, ErrInvalid, r.TenantID, r.JobID, err, "the presented lease fence is malformed")
	}
	return nil
}

// Polled is the result of [Coordinator.Poll].
type Polled struct {
	Job Job
	// NoOp reports that the job was already terminal: nothing was observed,
	// compared or written.
	NoOp     bool
	Evidence Evidence
}

// Poll performs one reconciliation attempt on an already-triggered job:
// apply deadline exhaustion, then observe, then -- only for an observation
// that meets the job's required freshness -- compare, and settle the result.
//
// The sequence and its guards are RECON-001's whole RED clause made
// executable. A job that is already terminal is left untouched
// ([Polled.NoOp]): a settled promise is not re-settled. A job past its
// deadline settles to EXPIRED or REPAIR_REQUIRED rather than being polled
// again. An observation that does not meet the job's required freshness
// still counts as an attempt but leaves the job at StatusObserving, due
// again after backoff -- a stale result never ends polling. Only a
// [Comparer] verdict on a sufficiently fresh observation can settle the job
// to PASS, MISMATCH, PARTIAL or the resting StatusUnknown.
func (c Coordinator) Poll(ctx context.Context, ex Executor, req PollRequest) (Polled, error) {
	if c.Store == nil {
		return Polled{}, invalid(uuid.Nil, uuid.Nil, "coordinator has no store")
	}
	if c.Fences == nil {
		return Polled{}, invalid(uuid.Nil, uuid.Nil, "coordinator has no fence verifier")
	}
	if err := req.validate(); err != nil {
		return Polled{}, err
	}
	if _, err := c.Fences.Verify(ctx, ex, req.Fence, req.Now); err != nil {
		return Polled{}, wrapErr(CodeFenceRefused, ErrFenceRefused, req.TenantID, req.JobID, err,
			"the fence presented for polling reconciliation was refused")
	}

	current, err := c.Store.LoadForUpdate(ctx, ex, req.TenantID, req.JobID)
	if err != nil {
		return Polled{}, err
	}
	if current.Status.Terminal() {
		return Polled{Job: current, NoOp: true}, nil
	}
	holder := req.Fence.Holder.HolderID()

	// Deadline exhaustion is checked before another observation is spent: a
	// job past its deadline is settled, not polled once more.
	if !req.Now.Before(current.Deadline) {
		next := current
		next.Status = exhaustStatus(current.RepairPolicy)
		next.Owner = holder
		next.UpdatedAt = req.Now
		if err := c.Store.Advance(ctx, ex, next, current.Version); err != nil {
			return Polled{}, err
		}
		return Polled{
			Job: next,
			Evidence: newEvidence(EventExhausted, next, req.Now, req.Fence.Token,
				"deadline reached with no determinate reconciliation verdict"),
		}, nil
	}

	if c.Observer == nil {
		return Polled{}, invalid(req.TenantID, req.JobID, "coordinator has no observer")
	}
	obs, err := c.Observer.Observe(ctx, ex, current, req.Now)
	if err != nil {
		return Polled{}, wrapErr(CodeObserverFailed, ErrObserverFailed, req.TenantID, req.JobID, err,
			"observe the effect's canonical state")
	}

	next := current
	next.ObservationAttempts = current.ObservationAttempts + 1
	next.Owner = holder
	next.UpdatedAt = req.Now
	if obs.CanonicalRef != "" {
		next.CanonicalRef = obs.CanonicalRef
	}

	if !meetsFreshness(obs.Freshness, current.RequiredFreshness) {
		// RED: a stale observation result never ends polling.
		next.Status = StatusObserving
		next.NextCheckAt = req.Now.Add(c.backoffFor(next.ObservationAttempts))
		if err := c.Store.Advance(ctx, ex, next, current.Version); err != nil {
			return Polled{}, err
		}
		return Polled{
			Job: next,
			Evidence: newEvidence(EventObserved, next, req.Now, req.Fence.Token,
				"observation freshness "+string(obs.Freshness)+" does not meet the required "+string(current.RequiredFreshness)),
		}, nil
	}

	if c.Comparer == nil {
		return Polled{}, invalid(req.TenantID, req.JobID, "coordinator has no comparer")
	}
	verdict, err := c.Comparer.Compare(ctx, ex, current, obs, req.Now)
	if err != nil {
		return Polled{}, wrapErr(CodeComparerFailed, ErrComparerFailed, req.TenantID, req.JobID, err,
			"compare the observation against the intended state")
	}
	if !validVerdictStatus(verdict.Status) {
		return Polled{}, refuse(CodeUnexpectedStatus, ErrComparerFailed, req.TenantID, req.JobID,
			"comparer returned status %q, which is not one of PASS, MISMATCH, PARTIAL, UNKNOWN", string(verdict.Status))
	}
	next.Status = verdict.Status
	if !verdict.Status.Terminal() {
		// StatusUnknown: a resting verdict, still due.
		next.NextCheckAt = verdict.NextCheckAt
		if next.NextCheckAt.IsZero() {
			next.NextCheckAt = req.Now.Add(c.backoffFor(next.ObservationAttempts))
		}
	}
	if err := c.Store.Advance(ctx, ex, next, current.Version); err != nil {
		return Polled{}, err
	}
	return Polled{Job: next, Evidence: newEvidence(EventSettled, next, req.Now, req.Fence.Token, "")}, nil
}
