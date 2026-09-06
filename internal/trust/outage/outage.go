// Package outage is a pure decision policy over federation health signals.
// It never calls the identity provider, never reads a JWKS cache and never
// consults a clock source itself: every signal the policy needs -- whether
// the IdP answered, how stale the last successful JWKS refresh is, and the
// measured clock skew -- is handed in as a value, so the same inputs always
// produce the same decision for any caller, in tests or in production.
//
// The policy discriminates by request class rather than by a single global
// "IdP is down" switch: a brand-new ordinary session is never created from a
// stale federation assertion, an existing session may keep working within a
// declared bounded staleness window, a write degrades to a read-only
// fallback under the same window, and a privileged operation is refused
// unless a separately provisioned, approved emergency grant is presented.
// [Reconcile] is the corresponding after-recovery step: every decision this
// package made under degraded health is revisited once health returns, and
// none of them is silently upgraded to full trust.
package outage

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Class is the closed set of request classes the outage policy tells apart.
// A caller cannot invent a fifth class: [Evaluate] refuses anything that is
// not one of these four.
type Class string

// The four request classes this policy discriminates between.
const (
	// ClassNewSession is a request to establish a brand-new authenticated
	// session from a federation assertion. It is never permitted while
	// federation health is degraded, no matter how short the staleness.
	ClassNewSession Class = "NEW_SESSION"
	// ClassExistingRead is a read using a session that was already
	// established before the outage began.
	ClassExistingRead Class = "EXISTING_SESSION_READ"
	// ClassExistingWrite is a write using a session that was already
	// established before the outage began. It degrades to a read-only
	// fallback rather than being treated the same as a fresh read.
	ClassExistingWrite Class = "EXISTING_SESSION_WRITE"
	// ClassPrivileged is any operation policy has flagged as privileged
	// (administrative, cross-tenant, or otherwise sensitive). During a
	// healthy federation it is decided by ordinary authorization; during an
	// outage it can only proceed under a separately approved emergency
	// grant.
	ClassPrivileged Class = "PRIVILEGED_OPERATION"
)

func (c Class) valid() bool {
	switch c {
	case ClassNewSession, ClassExistingRead, ClassExistingWrite, ClassPrivileged:
		return true
	default:
		return false
	}
}

// Outcome is the closed set of decisions [Evaluate] can return.
type Outcome string

// The outcomes this policy can produce.
const (
	OutcomeDeny            Outcome = "DENY"
	OutcomePermit          Outcome = "PERMIT"
	OutcomePermitCached    Outcome = "PERMIT_CACHED_WITHIN_WINDOW"
	OutcomePermitReadOnly  Outcome = "PERMIT_READ_ONLY_FALLBACK"
	OutcomePermitEmergency Outcome = "PERMIT_EMERGENCY_GRANT"
)

// Reason tokens. They are stable and safe for evidence and operator display.
const (
	ReasonHealthy                     = "federation_healthy"
	ReasonInvalidPolicy               = "invalid_policy"
	ReasonInvalidRequest              = "invalid_request"
	ReasonNewSessionDuringOutage      = "new_session_denied_during_outage"
	ReasonCachedWithinWindow          = "cached_session_within_bounded_window"
	ReasonCachedWindowExceeded        = "cached_session_exceeds_bounded_window"
	ReasonPrivilegedRequiresEmergency = "privileged_operation_requires_emergency_grant"
	ReasonEmergencyGrantAccepted      = "emergency_grant_accepted"
)

// Health is the federation health signal snapshot the policy evaluates. It
// carries no interpretation of its own -- turning it into a decision is
// entirely [Evaluate]'s job, so a health prober can be swapped out without
// this package changing.
type Health struct {
	// IdPReachable reports whether the last attempted call to the identity
	// provider succeeded.
	IdPReachable bool
	// JWKSAge is the time elapsed since the signing-key set was last
	// successfully refreshed from the IdP. A negative value is treated as
	// malformed input.
	JWKSAge time.Duration
	// ClockSkew is the measured skew between local time and the IdP's
	// reported time. Callers pass the magnitude or the signed difference;
	// [Evaluate] compares the absolute value against the policy bound.
	ClockSkew time.Duration
}

// Policy is the declarative, bounded-staleness outage policy. Every bound
// must be strictly positive; a zero or negative policy is refused rather
// than silently interpreted as "unbounded" or "always healthy".
type Policy struct {
	// MaxJWKSStaleness is the longest a JWKS refresh may be overdue before
	// federation health is considered degraded.
	MaxJWKSStaleness time.Duration
	// MaxClockSkew is the largest tolerated absolute skew between local and
	// IdP time before federation health is considered degraded.
	MaxClockSkew time.Duration
	// MaxCachedSessionAge is the bounded staleness window within which an
	// existing session may keep operating against cached keys once
	// federation is degraded.
	MaxCachedSessionAge time.Duration
}

// ErrInvalidPolicy is returned when a [Policy] has a non-positive bound.
var ErrInvalidPolicy = errors.New("outage: invalid policy")

// ErrInvalidRequest is returned when a [Request] names an unrecognized class
// or a malformed session age.
var ErrInvalidRequest = errors.New("outage: invalid request")

func (p Policy) validate() error {
	if p.MaxJWKSStaleness <= 0 {
		return fmt.Errorf("%w: max jwks staleness must be positive", ErrInvalidPolicy)
	}
	if p.MaxClockSkew <= 0 {
		return fmt.Errorf("%w: max clock skew must be positive", ErrInvalidPolicy)
	}
	if p.MaxCachedSessionAge <= 0 {
		return fmt.Errorf("%w: max cached session age must be positive", ErrInvalidPolicy)
	}
	return nil
}

// EmergencyGrant is the separate, out-of-band P0 emergency access grant this
// policy checks the presence and validity of. This package never issues or
// stores one -- [internal/trust/jit] is the source of truth for that
// lifecycle; a caller of [Evaluate] presents whatever grant it already holds.
type EmergencyGrant struct {
	// Approved reports whether a distinct approver signed off on this grant.
	Approved bool
	// StepUpSatisfied reports whether the acting principal presented the
	// step-up assurance the emergency path requires.
	StepUpSatisfied bool
	// TicketRef names the incident or ticket the grant is bound to. An empty
	// ticket reference is never accepted, emergency or not.
	TicketRef string
	// ExpiresAt is when the grant stops being usable.
	ExpiresAt time.Time
}

// validAt reports whether g is a fully approved, still-live emergency grant
// at the instant now. A nil grant, or one missing any one of approval,
// step-up, ticket reference or remaining lifetime, is not valid -- there is
// no partial credit.
func (g *EmergencyGrant) validAt(now time.Time) bool {
	if g == nil {
		return false
	}
	if !g.Approved || !g.StepUpSatisfied {
		return false
	}
	if strings.TrimSpace(g.TicketRef) == "" {
		return false
	}
	return now.Before(g.ExpiresAt)
}

// Request is one access decision request presented to [Evaluate].
type Request struct {
	// Class is the request class being decided.
	Class Class
	// SessionAge is how long the existing session has been alive. It is
	// only consulted for [ClassExistingRead] and [ClassExistingWrite]; a
	// negative value is malformed.
	SessionAge time.Duration
	// Emergency is the emergency grant presented for [ClassPrivileged]. It
	// is ignored for every other class.
	Emergency *EmergencyGrant
}

// Decision is the pure, immutable result of [Evaluate]. Every field a caller
// or an evidence store needs is a value here; nothing is looked up again.
type Decision struct {
	Class       Class
	Outcome     Outcome
	Reason      string
	Healthy     bool
	EvaluatedAt time.Time
}

// Evaluate is pure: it performs no I/O, reads no store and mutates nothing.
// The same (p, h, req, now) always produces the same [Decision]. This is
// deliberate -- a session gateway, a write path and a privileged-operation
// gate all call the identical function and must reach the identical
// conclusion from the identical inputs.
//
// Everything unrecognized fails closed: an invalid policy, an invalid
// request class, or a negative session age all deny.
func Evaluate(p Policy, h Health, req Request, now time.Time) Decision {
	d := Decision{Class: req.Class, EvaluatedAt: now.UTC()}

	if err := p.validate(); err != nil {
		d.Outcome, d.Reason = OutcomeDeny, ReasonInvalidPolicy
		return d
	}
	if !req.Class.valid() || (req.Class != ClassNewSession && req.SessionAge < 0) {
		d.Outcome, d.Reason = OutcomeDeny, ReasonInvalidRequest
		return d
	}

	d.Healthy = healthyAt(p, h)
	if d.Healthy {
		d.Outcome, d.Reason = OutcomePermit, ReasonHealthy
		return d
	}

	switch req.Class {
	case ClassNewSession:
		// No new ordinary session is ever created from a stale federation
		// assertion, regardless of how small the staleness is.
		d.Outcome, d.Reason = OutcomeDeny, ReasonNewSessionDuringOutage
	case ClassExistingRead:
		if req.SessionAge <= p.MaxCachedSessionAge {
			d.Outcome, d.Reason = OutcomePermitCached, ReasonCachedWithinWindow
		} else {
			d.Outcome, d.Reason = OutcomeDeny, ReasonCachedWindowExceeded
		}
	case ClassExistingWrite:
		// A write degrades to a read-only fallback under the same bounded
		// window rather than being denied outright, and never proceeds as
		// an ordinary write while federation is degraded.
		if req.SessionAge <= p.MaxCachedSessionAge {
			d.Outcome, d.Reason = OutcomePermitReadOnly, ReasonCachedWithinWindow
		} else {
			d.Outcome, d.Reason = OutcomeDeny, ReasonCachedWindowExceeded
		}
	case ClassPrivileged:
		if req.Emergency.validAt(now) {
			d.Outcome, d.Reason = OutcomePermitEmergency, ReasonEmergencyGrantAccepted
		} else {
			d.Outcome, d.Reason = OutcomeDeny, ReasonPrivilegedRequiresEmergency
		}
	}
	return d
}

// healthyAt reports whether the federation health signal is fully within
// policy bounds. Any single degraded dimension -- unreachable IdP, an
// overdue JWKS refresh, or clock skew beyond tolerance -- makes the whole
// signal unhealthy.
func healthyAt(p Policy, h Health) bool {
	if !h.IdPReachable {
		return false
	}
	if h.JWKSAge < 0 || h.JWKSAge > p.MaxJWKSStaleness {
		return false
	}
	skew := h.ClockSkew
	if skew < 0 {
		skew = -skew
	}
	if skew > p.MaxClockSkew {
		return false
	}
	return true
}

// CachedUse is one decision this package produced while federation was
// degraded, recorded by the caller for later reconciliation.
type CachedUse struct {
	SessionRef string
	Outcome    Outcome
	DecidedAt  time.Time
}

// ReconciliationAction is the closed set of outcomes [Reconcile] can assign
// to a recorded use.
type ReconciliationAction string

// Reconciliation actions.
const (
	ReconcileRevalidateRequired ReconciliationAction = "REVALIDATE_REQUIRED"
	ReconcileNoActionNeeded     ReconciliationAction = "NO_ACTION_NEEDED"
)

// ReconciliationResult is what [Reconcile] decided for one recorded use.
type ReconciliationResult struct {
	SessionRef string
	Action     ReconciliationAction
	Reason     string
}

// Reconcile is evaluated once federation health has returned to normal. It
// is pure and mutates nothing; the caller is responsible for acting on the
// results (re-validating a session against a live key, or routing an
// emergency use to after-action review).
//
// Every use that was only ever permitted as a cached or read-only fallback
// must be revalidated against a live signing key before it is trusted beyond
// the original bounded window: recovery does not retroactively bless what
// was only provisionally trusted. Every emergency-granted use is likewise
// flagged, because emergency access is reconciled by evidence review, not by
// silent re-trust once the outage ends. A use that was never degraded needs
// no action.
func Reconcile(uses []CachedUse) []ReconciliationResult {
	out := make([]ReconciliationResult, 0, len(uses))
	for _, u := range uses {
		r := ReconciliationResult{SessionRef: u.SessionRef}
		switch u.Outcome {
		case OutcomePermitCached, OutcomePermitReadOnly:
			r.Action, r.Reason = ReconcileRevalidateRequired, "cached_or_fallback_use_must_revalidate"
		case OutcomePermitEmergency:
			r.Action, r.Reason = ReconcileRevalidateRequired, "emergency_use_requires_post_recovery_review"
		default:
			r.Action, r.Reason = ReconcileNoActionNeeded, "not_degraded"
		}
		out = append(out, r)
	}
	return out
}
