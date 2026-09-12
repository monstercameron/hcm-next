package edge

// EDGE-007: propagate overload, retry and circuit state end to end.
//
// internal/connectivity/edge had no overload, retry or circuit concept at
// all before this file (`grep -rn "CircuitState\|circuitBreaker\|Overload"
// internal/` returned nothing anywhere in the repository). It composes
// internal/operations/admission rather than forking a third retry ledger,
// exactly as EVENT-003's outbox.RetryAccount and INTG-015's
// connectivity/operation quota already do: admission.DecideBackpressure
// supplies the downstream health signal, admission.Provisioner is the one
// durable, replay-safe retry-budget owner, and admission.ConsumeRetry stays
// the only retry policy. This file adds only what neither of those already
// have: the RED vocabulary's ADMIT/QUEUE/DEFER/DEGRADE/REJECT outcome (a
// direct use of admission.Outcome, not a re-derivation of it), the
// explicit and total mapping from admission.BackpressureAction's different
// vocabulary onto it, and the circuit-breaker concept the repository was
// missing entirely.
//
// Nesting: an edge retry, a connector retry and a transport timeout for the
// SAME logical operation and the SAME physical attempt all derive the
// identical replay-safe attempt identity (attemptIdentity), so they collide
// on admission.Provisioner's one stored receipt instead of each minting a
// fresh token -- see RetryLedger.Consume and TestTodo_EDGE_007_Fault.
//
// Fail-closed: an unconfigured Coordinator.Breaker, an unrecognized
// admission.BackpressureAction, an unrecognized admission.RetryDisposition,
// and the zero value of CircuitState all resolve to REJECT, never ADMIT
// (see circuitOutcome, mapBackpressureAction, retryDispositionOutcome).

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
)

// ErrInvalidOverloadRequest reports a caller-supplied OverloadRequest that
// this package's pure policy cannot evaluate at all. Rejecting rather than
// guessing keeps an incomplete request from silently resolving to ADMIT.
var ErrInvalidOverloadRequest = errors.New("edge: invalid overload request")

// FailureSignal is one observed nested failure for a logical operation's
// attempt. Layers as different as an edge retry, a connector retry and a
// transport timeout all classify the SAME physical attempt using one of
// these three values.
type FailureSignal string

const (
	FailureRateLimited FailureSignal = "429"
	FailureUnavailable FailureSignal = "503"
	FailureTimeout     FailureSignal = "TIMEOUT"
)

// failureClass maps a FailureSignal onto the admission.FailureClass the
// shared retry budget understands. An unrecognized signal is explicitly
// not a class: callers must not guess one on its behalf.
func (f FailureSignal) failureClass() (admission.FailureClass, bool) {
	switch f {
	case FailureRateLimited:
		return admission.FailureThrottled, true
	case FailureUnavailable:
		return admission.FailureUnavailable, true
	case FailureTimeout:
		return admission.FailureTimeout, true
	default:
		return "", false
	}
}

func (f FailureSignal) valid() bool {
	_, ok := f.failureClass()
	return ok
}

// CircuitState is the edge-owned circuit-breaker state for one tenant's
// dependency. The zero value is deliberately not CLOSED: an uninitialized,
// unset or otherwise unrecognized state must fail closed rather than ever
// be treated as healthy.
type CircuitState string

const (
	CircuitUnknown  CircuitState = ""
	CircuitClosed   CircuitState = "CLOSED"
	CircuitHalfOpen CircuitState = "HALF_OPEN"
	CircuitOpen     CircuitState = "OPEN"
)

// circuitOutcome is the pure, total mapping from a CircuitState to its
// contribution toward the combined Outcome. Only the two states that mean
// "an attempt may proceed" (CLOSED, and the single admitted HALF_OPEN
// probe) resolve to ADMIT; OPEN, the zero value and any unrecognized state
// fail closed to REJECT.
func circuitOutcome(state CircuitState) admission.Outcome {
	switch state {
	case CircuitClosed, CircuitHalfOpen:
		return admission.Admit
	default:
		return admission.Reject
	}
}

// CircuitConfig bounds one circuit breaker's trip and recovery behavior.
type CircuitConfig struct {
	FailureThreshold int
	Cooldown         time.Duration
}

func (c CircuitConfig) normalized() CircuitConfig {
	if c.FailureThreshold <= 0 {
		c.FailureThreshold = 5
	}
	if c.Cooldown <= 0 {
		c.Cooldown = 30 * time.Second
	}
	return c
}

type circuitEntry struct {
	state    CircuitState
	failures int
	openedAt time.Time
	probing  bool
}

// CircuitBreaker tracks circuit state per tenant/dependency pair. It is safe
// for concurrent use, and every tenant's entry is independent: one tenant's
// trip can never be observed, consumed or reset by another tenant's calls.
type CircuitBreaker struct {
	mu      sync.Mutex
	cfg     CircuitConfig
	entries map[string]*circuitEntry
}

// NewCircuitBreaker builds a breaker with normalized, always-positive
// trip/recovery bounds.
func NewCircuitBreaker(cfg CircuitConfig) *CircuitBreaker {
	return &CircuitBreaker{cfg: cfg.normalized(), entries: make(map[string]*circuitEntry)}
}

func circuitKey(tenant, dependency string) string { return tenant + "\x1f" + dependency }

// Admit reports whether the circuit for tenant/dependency currently allows
// an attempt at now, and the resulting CircuitState. An OPEN circuit is not
// re-probed as healthy until its cooldown has fully elapsed; once it has,
// exactly one HALF_OPEN probe is admitted at a time -- a second caller
// arriving while a probe is already outstanding is refused rather than
// allowed to race it.
func (b *CircuitBreaker) Admit(tenant, dependency string, now time.Time) (bool, CircuitState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := circuitKey(tenant, dependency)
	e, ok := b.entries[key]
	if !ok {
		e = &circuitEntry{state: CircuitClosed}
		b.entries[key] = e
	}
	switch e.state {
	case CircuitClosed:
		return true, CircuitClosed
	case CircuitOpen:
		if !now.Before(e.openedAt.Add(b.cfg.Cooldown)) {
			e.state, e.probing = CircuitHalfOpen, true
			return true, CircuitHalfOpen
		}
		return false, CircuitOpen
	case CircuitHalfOpen:
		if e.probing {
			return false, CircuitHalfOpen
		}
		e.probing = true
		return true, CircuitHalfOpen
	default:
		// An unrecognized stored state must never be treated as healthy.
		e.state, e.openedAt, e.probing = CircuitOpen, now, false
		return false, CircuitOpen
	}
}

// Report records the real outcome of an attempt Admit allowed: success
// closes the circuit and clears the failure count; failure increments it
// and trips the circuit open once the threshold is reached, or immediately
// if the failing attempt was itself the HALF_OPEN probe. It always clears
// the in-flight probe marker so a later attempt is never wedged behind a
// probe that will never report back.
func (b *CircuitBreaker) Report(tenant, dependency string, now time.Time, success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := circuitKey(tenant, dependency)
	e, ok := b.entries[key]
	if !ok {
		e = &circuitEntry{state: CircuitClosed}
		b.entries[key] = e
	}
	wasProbe := e.probing
	e.probing = false
	if success {
		e.state, e.failures = CircuitClosed, 0
		return
	}
	e.failures++
	if wasProbe || e.failures >= b.cfg.FailureThreshold {
		e.state, e.openedAt, e.failures = CircuitOpen, now, 0
	}
}

// ReleaseProbe clears an outstanding HALF_OPEN probe marker without
// recording a success or failure. It is used when a probe was admitted by
// the circuit but the overall decision still refused to perform any effect
// for an unrelated reason (backpressure, an exhausted retry budget): no
// dependency call was actually attempted, so the probe slot must not stay
// wedged waiting for a Report that will never come.
func (b *CircuitBreaker) ReleaseProbe(tenant, dependency string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if e, ok := b.entries[circuitKey(tenant, dependency)]; ok {
		e.probing = false
	}
}

// mapBackpressureAction is the total, explicit RED<->admission bridge. RED's
// vocabulary (ADMIT/QUEUE/DEFER/DEGRADE/REJECT) is admission.Outcome;
// admission.BackpressureAction is a different, overlapping-but-not-identical
// vocabulary (CONTINUE/SLOW/QUEUE/DEFER/STOP). Every declared action maps
// somewhere explicit below; an unrecognized action fails closed to REJECT,
// never silently to ADMIT.
func mapBackpressureAction(action admission.BackpressureAction) admission.Outcome {
	switch action {
	case admission.BackpressureContinue:
		return admission.Admit
	case admission.BackpressureSlowUpstream:
		return admission.Degrade
	case admission.BackpressureQueue:
		return admission.Queue
	case admission.BackpressureDefer:
		return admission.Defer
	case admission.BackpressureStop:
		return admission.Reject
	default:
		return admission.Reject
	}
}

// retryDispositionOutcome maps admission.RetryDisposition onto the shared
// Outcome vocabulary. Only a granted token permits the attempt; every other
// disposition -- including one this package does not yet know about --
// fails closed to REJECT.
func retryDispositionOutcome(d admission.RetryDisposition) admission.Outcome {
	switch d {
	case admission.RetryAllowed:
		return admission.Admit
	case admission.RetryNotAllowed, admission.RetryBudgetExhausted, admission.RetryRepairRequired:
		return admission.Reject
	default:
		return admission.Reject
	}
}

// outcomeSeverity orders the five outcomes from most permissive to most
// restrictive so several independent signals can be combined by keeping
// whichever is most restrictive.
func outcomeSeverity(o admission.Outcome) int {
	switch o {
	case admission.Admit:
		return 0
	case admission.Queue:
		return 1
	case admission.Degrade:
		return 2
	case admission.Defer:
		return 3
	default:
		return 4 // REJECT, and anything unrecognized, is the most restrictive.
	}
}

// attemptIdentity derives the stable, replay-safe identity every layer must
// compute the same way to actually share one retry budget: the logical
// operation id joined with the physical attempt number, mirroring EVENT-003
// outbox.AttemptIdentity. An edge retry, a connector retry and a transport
// timeout observing the SAME physical attempt of the SAME logical operation
// compute the same identity and therefore collide on the same stored
// receipt in admission.Provisioner.Consume instead of each minting a fresh
// token.
func attemptIdentity(logicalOperationID string, attempt int) string {
	return fmt.Sprintf("%s#%d", logicalOperationID, attempt)
}

// RetryLedger bridges edge's nested failure observations to admission's
// durable, replay-safe retry budget so a logical operation retried at
// several unconnected layers is charged once per physical attempt, never
// once per layer that happens to notice the failure.
type RetryLedger struct {
	Provisioner *admission.Provisioner
	Allowed     int
	Version     string
}

func (l RetryLedger) allowed() int {
	if l.Allowed <= 0 {
		return 3
	}
	return l.Allowed
}

func (l RetryLedger) version() string {
	if strings.TrimSpace(l.Version) == "" {
		return "edge-007-v1"
	}
	return l.Version
}

// Consume provisions (idempotently) the named logical operation's budget
// and consumes one token for the attempt identity derived from
// logicalOperationID and attempt. A replayed identity -- the same physical
// attempt seen again by this layer or another sharing this Provisioner --
// returns the stored receipt rather than a second token, because
// admission.Provisioner.Consume is already replay-safe by AttemptID.
func (l RetryLedger) Consume(tenant, dependency, logicalOperationID, operationKind string, attempt int, failure FailureSignal) (admission.RetryReceipt, error) {
	if l.Provisioner == nil {
		return admission.RetryReceipt{}, fmt.Errorf("edge: retry ledger requires a provisioner")
	}
	class, ok := failure.failureClass()
	if !ok {
		return admission.RetryReceipt{}, fmt.Errorf("edge: unrecognized failure signal %q", failure)
	}
	spec := admission.ProvisionSpec{
		TenantID: tenant, Service: "edge", Dependency: dependency,
		LogicalOperationID: logicalOperationID, OperationKind: operationKind,
		Allowed:   l.allowed(),
		Retryable: []admission.FailureClass{admission.FailureUnavailable, admission.FailureThrottled, admission.FailureTimeout},
		Version:   l.version(),
	}
	budget, err := l.Provisioner.Provision(spec)
	if err != nil {
		return admission.RetryReceipt{}, err
	}
	return l.Provisioner.Consume(budget.ID, admission.AttemptInput{
		AttemptID: attemptIdentity(logicalOperationID, attempt),
		Attempt: admission.RetryAttempt{
			LogicalOperationID: logicalOperationID, OperationKind: operationKind,
			TenantID: tenant, Dependency: dependency, Failure: class, Attempt: attempt,
		},
	})
}

// OverloadRequest is one caller's request to evaluate the shared
// overload/retry/circuit contract for one logical operation. Attempt is the
// physical attempt number: 0 for the first, optimistic try (Failure must
// then be empty); a positive attempt number means a previous attempt
// observed Failure and this call is asking whether a retry is permitted.
type OverloadRequest struct {
	TenantID           string
	Dependency         string
	LogicalOperationID string
	OperationKind      string
	Attempt            int
	Failure            FailureSignal
	Signal             admission.BackpressureSignal
	Targets            []string
}

func (r OverloadRequest) valid() bool {
	if strings.TrimSpace(r.TenantID) == "" || strings.TrimSpace(r.Dependency) == "" ||
		strings.TrimSpace(r.LogicalOperationID) == "" || strings.TrimSpace(r.OperationKind) == "" ||
		r.Attempt < 0 {
		return false
	}
	if r.Attempt > 0 {
		return r.Failure.valid()
	}
	return r.Failure == ""
}

// OverloadDecision is the stable, payload-free evaluation result. Outcome is
// always one of admission.Admit/Queue/Defer/Degrade/Reject; CircuitState is
// always populated so an open circuit is visible to the caller as a typed
// value, never an opaque failure.
type OverloadDecision struct {
	Outcome        admission.Outcome
	Reason         string
	PerformEffect  bool
	RetryAfter     int
	CircuitState   CircuitState
	RetryRemaining int
	AttemptID      string
}

// Coordinator composes a CircuitBreaker and a RetryLedger into the single
// end-to-end overload/retry/circuit decision this package owns. It performs
// no I/O itself: the circuit breaker and retry ledger are the only state,
// and both are safe for concurrent use.
type Coordinator struct {
	Breaker *CircuitBreaker
	Retry   RetryLedger
}

// Decide evaluates one logical operation's attempt against circuit state,
// downstream backpressure and the shared nested retry budget, in that
// order. An open (or still-probing) circuit short-circuits everything else
// so a blocked dependency is never re-probed as healthy and never charges
// the retry budget for an attempt that will not be made. Every code path
// that ends without PerformEffect true is guaranteed to have performed no
// side effect: this function only decides, it never dispatches.
func (c *Coordinator) Decide(now time.Time, req OverloadRequest) (OverloadDecision, error) {
	if !req.valid() {
		return OverloadDecision{Outcome: admission.Reject, Reason: "INVALID_REQUEST", CircuitState: CircuitUnknown}, ErrInvalidOverloadRequest
	}
	if c.Breaker == nil {
		return OverloadDecision{Outcome: admission.Reject, Reason: "CIRCUIT_BREAKER_UNCONFIGURED", CircuitState: CircuitUnknown}, nil
	}

	allow, cstate := c.Breaker.Admit(req.TenantID, req.Dependency, now)
	if !allow {
		outcome, reason := admission.Reject, "CIRCUIT_OPEN"
		if cstate == CircuitHalfOpen {
			outcome, reason = admission.Defer, "CIRCUIT_PROBE_IN_FLIGHT"
		}
		return OverloadDecision{Outcome: outcome, Reason: reason, CircuitState: cstate}, nil
	}

	outcome := circuitOutcome(cstate)
	reason := "CIRCUIT_" + string(cstate)

	bp := admission.DecideBackpressure(req.Signal, req.Targets)
	bpOutcome := mapBackpressureAction(bp.Action)
	if outcomeSeverity(bpOutcome) > outcomeSeverity(outcome) {
		outcome, reason = bpOutcome, "BACKPRESSURE_"+string(bp.Action)
	}

	var attemptID string
	retryRemaining := -1
	if req.Attempt > 0 {
		receipt, err := c.Retry.Consume(req.TenantID, req.Dependency, req.LogicalOperationID, req.OperationKind, req.Attempt, req.Failure)
		if err != nil {
			if cstate == CircuitHalfOpen {
				c.Breaker.ReleaseProbe(req.TenantID, req.Dependency)
			}
			return OverloadDecision{Outcome: admission.Reject, Reason: "RETRY_LEDGER_ERROR", CircuitState: cstate}, err
		}
		attemptID = attemptIdentity(req.LogicalOperationID, req.Attempt)
		retryRemaining = receipt.Remaining
		retryOutcome := retryDispositionOutcome(receipt.Disposition)
		if outcomeSeverity(retryOutcome) > outcomeSeverity(outcome) {
			outcome, reason = retryOutcome, "RETRY_"+string(receipt.Disposition)
		}
	}

	performEffect := outcome == admission.Admit || outcome == admission.Degrade
	if cstate == CircuitHalfOpen && !performEffect {
		c.Breaker.ReleaseProbe(req.TenantID, req.Dependency)
	}
	return OverloadDecision{
		Outcome: outcome, Reason: reason, PerformEffect: performEffect,
		RetryAfter: bp.RetryAfter, CircuitState: cstate, RetryRemaining: retryRemaining, AttemptID: attemptID,
	}, nil
}

// ExplainOverload describes the EDGE-007 contract without including tenant
// or request data.
func ExplainOverload() string {
	return "EDGE-007 v1: composed admission backpressure/retry, edge-owned circuit state, one shared nested retry budget, and total outcome mapping to ADMIT/QUEUE/DEFER/DEGRADE/REJECT"
}
