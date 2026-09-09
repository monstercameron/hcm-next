// Package outage owns the identity-continuity contract used while a tenant's
// identity provider is unavailable. It is deliberately pure: callers provide
// health, phase, session and revocation observations, and receive a decision
// plus the evidence needed to persist it.
package outage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	incidentstate "github.com/monstercameron/human-capital-management-suite/internal/operations/incidentstate"
	trustoutage "github.com/monstercameron/human-capital-management-suite/internal/trust/outage"
)

const contractVersion = 1

// Version returns the version of the identity-continuity contract.
func Version() int { return contractVersion }

// Phase is the tenant-scoped identity-continuity lifecycle.
type Phase string

const (
	PhaseNormal           Phase = "NORMAL"
	PhaseSuspected        Phase = "SUSPECTED"
	PhaseDeclared         Phase = "DECLARED"
	PhaseDegradedIdentity Phase = "DEGRADED_IDENTITY"
	PhaseRecovering       Phase = "RECOVERING"
	PhaseReconciling      Phase = "RECONCILING"
	PhaseContained        Phase = "CONTAINED"
	PhaseRepairRequired   Phase = "REPAIR_REQUIRED"
)

func (p Phase) valid() bool {
	switch p {
	case PhaseNormal, PhaseSuspected, PhaseDeclared, PhaseDegradedIdentity,
		PhaseRecovering, PhaseReconciling, PhaseContained, PhaseRepairRequired:
		return true
	default:
		return false
	}
}

var phaseTransitions = map[Phase]map[Phase]bool{
	PhaseNormal:           {PhaseSuspected: true},
	PhaseSuspected:        {PhaseDeclared: true, PhaseNormal: true},
	PhaseDeclared:         {PhaseDegradedIdentity: true, PhaseContained: true, PhaseRepairRequired: true},
	PhaseDegradedIdentity: {PhaseRecovering: true, PhaseContained: true, PhaseRepairRequired: true},
	PhaseRecovering:       {PhaseReconciling: true, PhaseRepairRequired: true},
	PhaseReconciling:      {PhaseNormal: true, PhaseRepairRequired: true},
	PhaseContained:        {PhaseRecovering: true, PhaseRepairRequired: true},
	PhaseRepairRequired:   {PhaseRecovering: true, PhaseReconciling: true},
}

// Mode describes the authority posture used for one request.
type Mode string

const (
	ModeNormal         Mode = "NORMAL"
	ModeCached         Mode = "CACHED"
	ModeReadOnly       Mode = "READ_ONLY"
	ModeManual         Mode = "MANUAL"
	ModeEmergency      Mode = "EMERGENCY"
	ModeReconciliation Mode = "RECONCILIATION"
	ModeDenied         Mode = "DENIED"
)

// RevocationStatus is the revocation observation available to the decision.
// Unknown is intentionally a first-class value and fails closed.
type RevocationStatus string

const (
	RevocationUnknown   RevocationStatus = "UNKNOWN"
	RevocationConfirmed RevocationStatus = "CONFIRMED"
	RevocationRevoked   RevocationStatus = "REVOKED"
)

// Profile declares the bounded tenant-specific continuity policy.
type Profile struct {
	TenantID      string
	IncidentID    string
	Policy        trustoutage.Policy
	ManualEnabled bool
	MaxManualAge  time.Duration
}

// ManualApproval is the explicit, incident-bound approval for a manual
// continuity decision. It never creates a new ordinary session or grants a
// privileged operation.
type ManualApproval struct {
	Approved        bool
	StepUpSatisfied bool
	Actor           string
	Approver        string
	Reason          string
	IncidentID      string
	ExpiresAt       time.Time
}

// Request is one tenant-scoped continuity decision.
type Request struct {
	SessionRef string
	Class      trustoutage.Class
	SessionAge time.Duration
	Revocation RevocationStatus
	Manual     *ManualApproval
	Emergency  *trustoutage.EmergencyGrant
}

// Evidence is an immutable, metadata-only record for an outage decision. It
// contains no assertion, token, key or workforce payload.
type Evidence struct {
	ID         string
	IncidentID string
	TenantID   string
	SessionRef string
	Class      trustoutage.Class
	Phase      Phase
	Mode       Mode
	Outcome    string
	Reason     string
	At         time.Time
}

// Decision is the pure result of Decide.
type Decision struct {
	Phase             Phase
	Mode              Mode
	Outcome           trustoutage.Outcome
	Reason            string
	Allowed           bool
	ReadOnly          bool
	RequiresReconcile bool
	Evidence          Evidence
}

// PhaseEvent records a valid phase transition and its evidence reference.
type PhaseEvent struct {
	From        Phase
	To          Phase
	Actor       string
	Reason      string
	EvidenceRef string
	At          time.Time
}

// RecordedUse is the minimal metadata retained for post-recovery work.
type RecordedUse struct {
	SessionRef string
	Mode       Mode
	Outcome    trustoutage.Outcome
	Revocation RevocationStatus
	EvidenceID string
	DecidedAt  time.Time
}

// ReconciliationAction is the required post-recovery action for one use.
type ReconciliationAction string

const (
	ReconcileNoActionNeeded     ReconciliationAction = "NO_ACTION_NEEDED"
	ReconcileRevalidateRequired ReconciliationAction = "REVALIDATE_REQUIRED"
	ReconcileRevokeRequired     ReconciliationAction = "REVOKE_REQUIRED"
	ReconcileReviewRequired     ReconciliationAction = "REVIEW_REQUIRED"
	ReconcileBlocked            ReconciliationAction = "RECONCILIATION_BLOCKED"
)

// ReconciliationResult is the pure result for one recorded use.
type ReconciliationResult struct {
	SessionRef string
	Action     ReconciliationAction
	Reason     string
	EvidenceID string
}

var (
	ErrInvalidProfile   = errors.New("identity outage: invalid profile")
	ErrInvalidRequest   = errors.New("identity outage: invalid request")
	ErrPhaseTransition  = errors.New("identity outage: invalid phase transition")
	ErrOutageUndeclared = errors.New("identity outage: outage is not declared")
	ErrRecoveryNotReady = errors.New("identity outage: recovery is not ready")
)

func (p Profile) validate() error {
	if strings.TrimSpace(p.TenantID) == "" || strings.TrimSpace(p.IncidentID) == "" {
		return fmt.Errorf("%w: tenant and incident are required", ErrInvalidProfile)
	}
	if p.Policy.MaxJWKSStaleness <= 0 || p.Policy.MaxClockSkew <= 0 || p.Policy.MaxCachedSessionAge <= 0 {
		return fmt.Errorf("%w: trust bounds must be positive", ErrInvalidProfile)
	}
	if p.ManualEnabled && p.MaxManualAge <= 0 {
		return fmt.Errorf("%w: manual age must be positive when manual mode is enabled", ErrInvalidProfile)
	}
	return nil
}

func validRevocation(s RevocationStatus) bool {
	return s == RevocationUnknown || s == RevocationConfirmed || s == RevocationRevoked
}

func (r Request) validate() error {
	if !r.ClassValid() {
		return fmt.Errorf("%w: unknown request class", ErrInvalidRequest)
	}
	if r.Class != trustoutage.ClassNewSession && strings.TrimSpace(r.SessionRef) == "" {
		return fmt.Errorf("%w: session reference is required", ErrInvalidRequest)
	}
	if r.Class != trustoutage.ClassNewSession && r.SessionAge < 0 {
		return fmt.Errorf("%w: session age cannot be negative", ErrInvalidRequest)
	}
	if !validRevocation(r.Revocation) {
		return fmt.Errorf("%w: unknown revocation status", ErrInvalidRequest)
	}
	return nil
}

// ClassValid reports whether the request class belongs to TRUST-020's closed
// request-class set.
func (r Request) ClassValid() bool {
	switch r.Class {
	case trustoutage.ClassNewSession, trustoutage.ClassExistingRead,
		trustoutage.ClassExistingWrite, trustoutage.ClassPrivileged:
		return true
	default:
		return false
	}
}

// Advance moves the continuity phase when the transition is allowed. It does
// not mutate caller state and requires an actor, reason, evidence reference
// and trusted timestamp for every move.
func Advance(from, to Phase, actor, reason, evidenceRef string, at time.Time) (PhaseEvent, error) {
	if !from.valid() || !to.valid() || !phaseTransitions[from][to] {
		return PhaseEvent{}, fmt.Errorf("%w: %s -> %s", ErrPhaseTransition, from, to)
	}
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" || strings.TrimSpace(evidenceRef) == "" || at.IsZero() {
		return PhaseEvent{}, fmt.Errorf("%w: transition evidence is incomplete", ErrPhaseTransition)
	}
	return PhaseEvent{From: from, To: to, Actor: actor, Reason: reason, EvidenceRef: evidenceRef, At: at.UTC()}, nil
}

func manualValid(p Profile, r Request, phase Phase, now time.Time) bool {
	if !p.ManualEnabled || r.Manual == nil || (phase != PhaseDeclared && phase != PhaseDegradedIdentity) {
		return false
	}
	m := r.Manual
	if !m.Approved || !m.StepUpSatisfied || strings.TrimSpace(m.Actor) == "" || strings.TrimSpace(m.Approver) == "" || m.Actor == m.Approver || strings.TrimSpace(m.Reason) == "" {
		return false
	}
	if m.IncidentID != p.IncidentID || m.ExpiresAt.IsZero() || !now.Before(m.ExpiresAt) || m.ExpiresAt.Sub(now) >= p.MaxManualAge {
		return false
	}
	return r.Class == trustoutage.ClassExistingRead || r.Class == trustoutage.ClassExistingWrite
}

func outagePhase(phase Phase) bool {
	return phase == PhaseDeclared || phase == PhaseDegradedIdentity || phase == PhaseContained || phase == PhaseRepairRequired
}

func makeEvidence(p Profile, req Request, phase Phase, mode Mode, outcome trustoutage.Outcome, reason string, now time.Time) Evidence {
	parts := []string{p.IncidentID, p.TenantID, req.SessionRef, string(req.Class), string(phase), string(mode), string(outcome), reason, now.UTC().Format(time.RFC3339Nano)}
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return Evidence{
		ID: "idp-outage-" + hex.EncodeToString(h[:12]), IncidentID: p.IncidentID, TenantID: p.TenantID,
		SessionRef: req.SessionRef, Class: req.Class, Phase: phase, Mode: mode,
		Outcome: string(outcome), Reason: reason, At: now.UTC(),
	}
}

// Decide evaluates one request. Ordinary new sessions never succeed while
// identity continuity is declared or recovering. Existing sessions may only
// use TRUST-020's bounded cached/read-only result; manual approval is an
// explicit, incident-bound fallback for existing sessions and remains
// reconcilable. Unknown or revoked revocation state fails closed.
func Decide(p Profile, phase Phase, health trustoutage.Health, req Request, now time.Time) (Decision, error) {
	if err := p.validate(); err != nil {
		return Decision{}, err
	}
	if !phase.valid() {
		return Decision{}, fmt.Errorf("%w: unknown phase", ErrInvalidRequest)
	}
	if now.IsZero() {
		return Decision{}, fmt.Errorf("%w: decision time is required", ErrInvalidRequest)
	}
	if err := req.validate(); err != nil {
		return Decision{}, err
	}

	d := Decision{Phase: phase, Mode: ModeDenied, Outcome: trustoutage.OutcomeDeny}
	deny := func(reason string) (Decision, error) {
		d.Reason = reason
		d.Evidence = makeEvidence(p, req, phase, d.Mode, d.Outcome, reason, now)
		return d, nil
	}
	if req.Revocation == RevocationRevoked {
		return deny("revocation_confirmed")
	}
	if req.Revocation != RevocationConfirmed {
		return deny("revocation_uncertain")
	}
	if phase != PhaseNormal && req.Class == trustoutage.ClassNewSession {
		return deny("identity_continuity_not_normal")
	}
	if (phase == PhaseSuspected || phase == PhaseNormal) && !healthHealthy(p.Policy, health, now) {
		return deny(ErrOutageUndeclared.Error())
	}
	if phase == PhaseSuspected {
		return deny("identity_outage_suspected")
	}
	if phase == PhaseRecovering || phase == PhaseReconciling {
		if req.Class == trustoutage.ClassNewSession {
			return deny("recovery_reconciliation_required")
		}
	}

	trustDecision := trustoutage.Evaluate(p.Policy, health, trustoutage.Request{
		Class: req.Class, SessionAge: req.SessionAge, Emergency: req.Emergency,
	}, now)
	d.Outcome, d.Reason = trustDecision.Outcome, trustDecision.Reason
	d.Allowed = trustDecision.Outcome != trustoutage.OutcomeDeny
	d.ReadOnly = trustDecision.Outcome == trustoutage.OutcomePermitReadOnly
	if manualValid(p, req, phase, now) && !d.Allowed {
		d.Allowed = true
		d.Reason = "manual_continuity_approval"
		if req.Class == trustoutage.ClassExistingWrite {
			d.Outcome, d.ReadOnly = trustoutage.OutcomePermitReadOnly, true
		} else {
			d.Outcome = trustoutage.OutcomePermitCached
		}
		d.Mode = ModeManual
		d.RequiresReconcile = true
	}
	if d.Allowed && d.Mode != ModeManual {
		switch d.Outcome {
		case trustoutage.OutcomePermit:
			d.Mode = ModeNormal
		case trustoutage.OutcomePermitCached:
			d.Mode = ModeCached
			d.RequiresReconcile = true
		case trustoutage.OutcomePermitReadOnly:
			d.Mode = ModeReadOnly
			d.RequiresReconcile = true
		case trustoutage.OutcomePermitEmergency:
			d.Mode = ModeEmergency
			d.RequiresReconcile = true
		}
	}
	if (phase == PhaseRecovering || phase == PhaseReconciling) && d.Allowed {
		d.Mode = ModeReconciliation
		d.RequiresReconcile = true
	}
	if !d.Allowed {
		d.Mode = ModeDenied
	}
	if outagePhase(phase) || d.RequiresReconcile || !d.Allowed {
		d.Evidence = makeEvidence(p, req, phase, d.Mode, d.Outcome, d.Reason, now)
	}
	return d, nil
}

func healthHealthy(p trustoutage.Policy, h trustoutage.Health, now time.Time) bool {
	d := trustoutage.Evaluate(p, h, trustoutage.Request{Class: trustoutage.ClassExistingRead}, now)
	return d.Healthy
}

// Reconcile requires healthy federation and returns a deterministic result
// for every recorded degraded use. Revoked observations take precedence over
// revalidation; unknown observations remain blocked; emergency/manual uses
// require review in addition to revalidation.
func Reconcile(p Profile, health trustoutage.Health, uses []RecordedUse, now time.Time) ([]ReconciliationResult, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	if now.IsZero() || !healthHealthy(p.Policy, health, now) {
		return nil, ErrRecoveryNotReady
	}
	ordered := append([]RecordedUse(nil), uses...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].SessionRef != ordered[j].SessionRef {
			return ordered[i].SessionRef < ordered[j].SessionRef
		}
		return ordered[i].DecidedAt.Before(ordered[j].DecidedAt)
	})
	out := make([]ReconciliationResult, 0, len(ordered))
	for _, use := range ordered {
		if strings.TrimSpace(use.SessionRef) == "" {
			return nil, fmt.Errorf("%w: recorded use has no session reference", ErrInvalidRequest)
		}
		result := ReconciliationResult{SessionRef: use.SessionRef, EvidenceID: use.EvidenceID}
		switch {
		case use.Revocation == RevocationRevoked:
			result.Action, result.Reason = ReconcileRevokeRequired, "revocation_observed_after_recovery"
		case use.Revocation != RevocationConfirmed:
			result.Action, result.Reason = ReconcileBlocked, "revocation_reconciliation_unknown"
		case use.Mode == ModeManual || use.Mode == ModeEmergency:
			result.Action, result.Reason = ReconcileReviewRequired, "manual_or_emergency_use_requires_review"
		case use.Mode == ModeCached || use.Mode == ModeReadOnly || use.Mode == ModeReconciliation:
			result.Action, result.Reason = ReconcileRevalidateRequired, "degraded_use_requires_live_revalidation"
		default:
			result.Action, result.Reason = ReconcileNoActionNeeded, "not_a_degraded_use"
		}
		out = append(out, result)
	}
	return out, nil
}

// Explain returns a bounded explanation suitable for evidence and operator
// display. It intentionally omits identity assertions and payloads.
func Explain(d Decision) string {
	return fmt.Sprintf("identity outage phase=%s mode=%s outcome=%s allowed=%t reconcile=%t reason=%s", d.Phase, d.Mode, d.Outcome, d.Allowed, d.RequiresReconcile, d.Reason)
}

// IncidentEvidence confirms that a continuity evidence record is bound to an
// incident whose tenant matches the profile. The incident itself remains
// owned by internal/operations/incidentstate.
func IncidentEvidence(p Profile, incident incidentstate.Incident, e Evidence) error {
	if err := p.validate(); err != nil {
		return err
	}
	if incident.ID != p.IncidentID || incident.TenantID != p.TenantID || e.ID == "" || e.IncidentID != p.IncidentID || e.TenantID != p.TenantID {
		return fmt.Errorf("%w: evidence is not bound to the declared incident", ErrInvalidRequest)
	}
	return nil
}
