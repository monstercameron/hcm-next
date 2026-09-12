package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// CICD-005 staged rollout execution and evidence-bound rollback.
//
// A Rollout advances one stage at a time and only while every one of its
// four independent health signals — SLO, telemetry, reconciliation and
// conformance — is both present and passing as of the evaluation time the
// caller supplies. A breach in any one of them pauses the rollout in a
// typed, distinguishable way: pausing is neither failing (the rollout is
// not abandoned) nor continuing (a paused rollout never silently advances
// on a plain Advance call; only Resume, re-evaluating every signal against
// fresh evidence, can move it forward again).
//
// Rollback moves the deployment back to an exact digest that Admit (see
// admission.go) both verifies and approves for the rollout's scope — never
// merely "the previous thing observed" — by calling Admit itself rather
// than re-implementing a weaker parallel check. A successful rollback
// immediately fences writers, and only fresh, present health AND
// reconciliation evidence can reopen; an absent observation is never
// treated as a passing one, on either side of that decision.
//
// Every transition — advance, pause, rollback, reopen — is appended to an
// immutable, append-only history. A rollback never erases or rewrites a
// StageRecord already recorded, and every record this package returns
// (StageRecord has no pointer or slice field) is a value copy a caller can
// never use to mutate the stored history.

// RolloutStatus is the lifecycle state of a staged rollout.
type RolloutStatus string

const (
	RolloutInProgress RolloutStatus = "IN_PROGRESS"
	RolloutPaused     RolloutStatus = "PAUSED"
	RolloutCompleted  RolloutStatus = "COMPLETED"
	RolloutFenced     RolloutStatus = "FENCED"
	RolloutRolledBack RolloutStatus = "ROLLED_BACK"
)

// Sentinel breach reasons, one per independent signal, so a caller can
// branch with errors.Is exactly as Admit's own sentinels allow.
var (
	// ErrRolloutSLOBreach means the stage's SLO evidence was present but
	// reported outside its budget.
	ErrRolloutSLOBreach = errors.New("release: rollout paused: SLO is outside its budget")
	// ErrRolloutTelemetryBreach means the stage's telemetry evidence was
	// present but reported unhealthy.
	ErrRolloutTelemetryBreach = errors.New("release: rollout paused: telemetry reports unhealthy")
	// ErrRolloutReconciliationBreach means the stage's reconciliation
	// evidence was present but reported inconsistent.
	ErrRolloutReconciliationBreach = errors.New("release: rollout paused: reconciliation is inconsistent")
	// ErrRolloutConformanceBreach means the stage's conformance evidence
	// was present but older than the policy's allowed window, or the
	// window itself has now elapsed since the evidence was generated; it
	// always wraps ErrConformanceStale as well so the same conformance
	// staleness check Admit performs is provably being reused here.
	ErrRolloutConformanceBreach = errors.New("release: rollout paused: conformance evidence is stale or absent")

	// ErrRolloutPaused is returned by Advance whenever the rollout is
	// already paused: a paused rollout never advances on a call that does
	// not go through Resume, no matter what evidence that call carries.
	ErrRolloutPaused = errors.New("release: rollout is paused and cannot advance")
	// ErrRolloutNotActive is returned by Advance/Resume/Rollback when the
	// rollout is not in a state that call can act on.
	ErrRolloutNotActive = errors.New("release: rollout is not in a state that can perform this action")
	// ErrRolloutAlreadyFenced guards Rollback against being called on a
	// rollout that is already fenced pending reopen.
	ErrRolloutAlreadyFenced = errors.New("release: rollout writers are already fenced pending reopen")
	// ErrRolloutNotFenced guards Reopen against being called on a rollout
	// that was never fenced by a rollback.
	ErrRolloutNotFenced = errors.New("release: rollout is not fenced; there is nothing to reopen")
	// ErrReopenEvidenceIncomplete means Reopen was attempted without both a
	// present health observation and a present reconciliation observation.
	// Absence of evidence is never treated as passing evidence: a Go zero
	// value HealthEvidence{} or ReconciliationEvidence{} — including one
	// whose boolean happens to be true but carries no observation time —
	// counts as absent, not as healthy.
	ErrReopenEvidenceIncomplete = errors.New("release: reopen refused: health and reconciliation evidence must both be present")
	// ErrReopenNotHealthy means both observations were present but at
	// least one of them reported an unhealthy or inconsistent state.
	ErrReopenNotHealthy = errors.New("release: reopen refused: health or reconciliation evidence reports an unhealthy state")
)

// SLOEvidence is one stage's SLO observation. MeasuredAt must be non-zero
// to count as an observation at all; a zero MeasuredAt is absence, never
// "within budget".
type SLOEvidence struct {
	WithinBudget bool
	MeasuredAt   time.Time
}

func (s SLOEvidence) present() bool { return !s.MeasuredAt.IsZero() }

// TelemetryEvidence is one stage's telemetry-health observation.
type TelemetryEvidence struct {
	Healthy    bool
	ObservedAt time.Time
}

func (t TelemetryEvidence) present() bool { return !t.ObservedAt.IsZero() }

// ReconciliationEvidence is one stage's reconciliation-consistency
// observation. The same shape serves both a stage-advance evaluation and a
// post-rollback Reopen decision.
type ReconciliationEvidence struct {
	Consistent bool
	CheckedAt  time.Time
}

func (r ReconciliationEvidence) present() bool { return !r.CheckedAt.IsZero() }

// HealthEvidence is the overall deployment-health observation Reopen
// requires alongside ReconciliationEvidence. It is kept distinct from
// TelemetryEvidence because reopening evaluates the deployment as a whole
// after a rollback, not one rollout stage's telemetry signal.
type HealthEvidence struct {
	Healthy    bool
	ObservedAt time.Time
}

func (h HealthEvidence) present() bool { return !h.ObservedAt.IsZero() }

// StageHealth bundles the four independent signals an Advance or Resume
// call evaluates for the rollout's current stage. Conformance reuses
// admission.go's ConformanceEvidence directly rather than a parallel type.
type StageHealth struct {
	SLO            SLOEvidence
	Telemetry      TelemetryEvidence
	Reconciliation ReconciliationEvidence
	Conformance    ConformanceEvidence
}

// RolloutPolicy configures how a rollout's health is evaluated.
// MaxConformanceAge is required; Advance/Resume reject with
// ErrEvidenceIncomplete before evaluating any signal if it is absent or
// its Go zero value, exactly as AdmissionPolicy does for Admit.
type RolloutPolicy struct {
	MaxConformanceAge time.Duration
}

// StageRecord is one immutable entry in a rollout's append-only history: a
// stage advance, a pause, a rollback (accepted or refused), or a reopen.
// Every StageRecord returned by this package is a value copy; it has no
// pointer or slice field, so a caller can never reach into the stored
// history through one.
type StageRecord struct {
	Sequence       int           `json:"sequence"`
	Kind           string        `json:"kind"`
	Stage          string        `json:"stage"`
	Status         RolloutStatus `json:"status"`
	EvaluatedAt    string        `json:"evaluated_at"`
	Reason         string        `json:"reason,omitempty"`
	ManifestDigest string        `json:"manifest_digest,omitempty"`
	Digest         string        `json:"digest"`
}

// Explain renders an audit-safe, deterministic description of the record.
func (rec StageRecord) Explain() string {
	if rec.Reason == "" {
		return fmt.Sprintf("rollout %s stage %q -> %s (record %s)", rec.Kind, rec.Stage, rec.Status, rec.Digest)
	}
	return fmt.Sprintf("rollout %s stage %q -> %s (record %s; reason: %s)", rec.Kind, rec.Stage, rec.Status, rec.Digest, rec.Reason)
}

// Snapshot is an immutable, JSON-safe view of a rollout: its identity,
// current status and complete history. It is the shape golden fixtures
// pin, and — like StageRecord — every field is a value, so it can be
// freely mutated by a caller without affecting the stored rollout.
type Snapshot struct {
	ID      string        `json:"id"`
	Status  RolloutStatus `json:"status"`
	History []StageRecord `json:"history"`
}

// Rollout is a staged rollout in progress. It is not safe to copy after
// first use; share a pointer. All mutation happens under mu, and every
// exported method that hands back state returns a copy so a caller can
// never mutate the rollout's stored history or status through it.
type Rollout struct {
	mu      sync.Mutex
	id      string
	stages  []string
	index   int
	status  RolloutStatus
	history []StageRecord
	seq     int
}

// NewRollout starts a fresh staged rollout at its first stage. Both id and
// a non-empty stage plan are required; an absent one is a caller error,
// not a rollout with implicit defaults.
func NewRollout(id string, stages []string) (*Rollout, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("%w: rollout id is required", ErrEvidenceIncomplete)
	}
	if len(stages) == 0 {
		return nil, fmt.Errorf("%w: rollout requires at least one stage", ErrEvidenceIncomplete)
	}
	cp := append([]string(nil), stages...)
	return &Rollout{id: id, stages: cp, status: RolloutInProgress}, nil
}

// ID returns the rollout's identity. It is fixed at construction.
func (r *Rollout) ID() string { return r.id }

// Status returns the rollout's current lifecycle state.
func (r *Rollout) Status() RolloutStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

// History returns a defensive copy of every record appended so far, oldest
// first. Mutating the returned slice never affects the stored rollout.
func (r *Rollout) History() []StageRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]StageRecord, len(r.history))
	copy(out, r.history)
	return out
}

// Snapshot returns a defensive copy of the rollout's identity, status and
// complete history in one value.
func (r *Rollout) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	history := make([]StageRecord, len(r.history))
	copy(history, r.history)
	return Snapshot{ID: r.id, Status: r.status, History: history}
}

func (r *Rollout) currentStageLocked() string {
	if r.index < len(r.stages) {
		return r.stages[r.index]
	}
	return r.stages[len(r.stages)-1]
}

// evaluateStageHealth is the pure decision function behind Advance and
// Resume: given one stage's evidence and the policy, decide whether the
// stage is healthy. Absence of any one signal's evidence is checked before
// that signal's pass/fail value is trusted, so a Go zero value never reads
// as "healthy" by accident. The four signals are evaluated in a fixed
// order (SLO, telemetry, reconciliation, conformance) so each is
// independently provable with the other three held healthy.
func evaluateStageHealth(health StageHealth, policy RolloutPolicy, now time.Time) error {
	switch {
	case policy.MaxConformanceAge <= 0:
		return fmt.Errorf("%w: rollout policy has no conformance staleness window configured", ErrEvidenceIncomplete)
	case !health.SLO.present():
		return fmt.Errorf("%w: no SLO evidence for this stage", ErrEvidenceIncomplete)
	case !health.SLO.WithinBudget:
		return fmt.Errorf("%w", ErrRolloutSLOBreach)
	case !health.Telemetry.present():
		return fmt.Errorf("%w: no telemetry evidence for this stage", ErrEvidenceIncomplete)
	case !health.Telemetry.Healthy:
		return fmt.Errorf("%w", ErrRolloutTelemetryBreach)
	case !health.Reconciliation.present():
		return fmt.Errorf("%w: no reconciliation evidence for this stage", ErrEvidenceIncomplete)
	case !health.Reconciliation.Consistent:
		return fmt.Errorf("%w", ErrRolloutReconciliationBreach)
	case health.Conformance.GeneratedAt.IsZero():
		return fmt.Errorf("%w: conformance evidence has no timestamp", ErrEvidenceIncomplete)
	}
	age := now.Sub(health.Conformance.GeneratedAt)
	if age < 0 || age > policy.MaxConformanceAge {
		return fmt.Errorf("%w: %w: evidence age %s exceeds the allowed window %s", ErrRolloutConformanceBreach, ErrConformanceStale, age, policy.MaxConformanceAge)
	}
	return nil
}

// Advance evaluates the rollout's current stage against health as of now
// and, only if every signal passes, moves it to the next stage (or to
// RolloutCompleted if that was the last one). now is a parameter, never
// time.Now(), so every evaluation is deterministic and testable against a
// pinned clock, exactly as Admit requires.
//
// Advance never acts on a paused rollout: it returns ErrRolloutPaused
// immediately, without evaluating health at all, so a paused rollout can
// never be advanced by a caller that simply retries the same call. Resume
// is the only path back to RolloutInProgress.
func (r *Rollout) Advance(health StageHealth, policy RolloutPolicy, now time.Time) (StageRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch r.status {
	case RolloutPaused:
		return StageRecord{}, fmt.Errorf("%w: stage %q; call Resume with fresh evidence to retry", ErrRolloutPaused, r.currentStageLocked())
	case RolloutInProgress:
		return r.evaluateAndTransitionLocked(health, policy, now)
	default:
		return StageRecord{}, fmt.Errorf("%w: rollout status is %s", ErrRolloutNotActive, r.status)
	}
}

// Resume re-evaluates a paused rollout's current stage against fresh
// evidence as of now. It only acts on a rollout that is currently paused;
// on success it both clears the pause and advances the stage in the same
// call. A breach on Resume pauses again with its own typed reason, which
// may differ from the one that originally paused it.
func (r *Rollout) Resume(health StageHealth, policy RolloutPolicy, now time.Time) (StageRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status != RolloutPaused {
		return StageRecord{}, fmt.Errorf("%w: rollout status is %s, not paused", ErrRolloutNotActive, r.status)
	}
	return r.evaluateAndTransitionLocked(health, policy, now)
}

func (r *Rollout) evaluateAndTransitionLocked(health StageHealth, policy RolloutPolicy, now time.Time) (StageRecord, error) {
	stage := r.currentStageLocked()
	if err := evaluateStageHealth(health, policy, now); err != nil {
		r.status = RolloutPaused
		return r.recordLocked("pause", stage, RolloutPaused, now, "", err), err
	}
	r.index++
	status := RolloutInProgress
	if r.index >= len(r.stages) {
		status = RolloutCompleted
	}
	r.status = status
	return r.recordLocked("advance", stage, status, now, "", nil), nil
}

// Rollback moves the rollout's target back to an exact digest that Admit
// both verifies and approves for policy.Target's scope, by calling Admit
// itself rather than a weaker, parallel check: a rollback target that is
// unsigned, untrusted, vulnerable, stale, or simply not the one exact
// approved digest/scope is refused exactly as Admit would refuse it as a
// fresh deployment candidate, via the same errors.Is-distinguishable
// sentinels. The rollout's prior history is never altered by a rollback,
// accepted or refused; a refusal is itself appended as a record, and an
// accepted rollback immediately fences writers (RolloutFenced) rather than
// resuming or completing the rollout.
func (r *Rollout) Rollback(candidate DeploymentCandidate, policy AdmissionPolicy, now time.Time) (StageRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch r.status {
	case RolloutFenced:
		return StageRecord{}, fmt.Errorf("%w", ErrRolloutAlreadyFenced)
	case RolloutRolledBack:
		return StageRecord{}, fmt.Errorf("%w: rollout has already completed rollback", ErrRolloutNotActive)
	}
	stage := r.currentStageLocked()
	decision, err := Admit(candidate, policy, now)
	if err != nil {
		wrapped := fmt.Errorf("release: rollback target refused: %w", err)
		return r.recordLocked("rollback_refused", stage, r.status, now, decision.ManifestDigest, wrapped), wrapped
	}
	r.status = RolloutFenced
	return r.recordLocked("rollback", stage, RolloutFenced, now, decision.ManifestDigest, nil), nil
}

// Reopen reverses a rollback's write fence, but only once fresh health AND
// reconciliation evidence both actually exist: an absent observation
// (including a HealthEvidence or ReconciliationEvidence whose boolean
// happens to be true but whose timestamp is the Go zero value) is refused
// with ErrReopenEvidenceIncomplete before either boolean is even read, and
// a present-but-unhealthy pair is separately refused with
// ErrReopenNotHealthy. Reopen only acts on a rollout that Rollback has
// fenced; on success it moves the rollout to its terminal RolloutRolledBack
// state.
func (r *Rollout) Reopen(health HealthEvidence, reconciliation ReconciliationEvidence, now time.Time) (StageRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status != RolloutFenced {
		return StageRecord{}, fmt.Errorf("%w", ErrRolloutNotFenced)
	}
	stage := r.currentStageLocked()
	if !health.present() || !reconciliation.present() {
		err := fmt.Errorf("%w", ErrReopenEvidenceIncomplete)
		return r.recordLocked("reopen_refused", stage, r.status, now, "", err), err
	}
	if !health.Healthy || !reconciliation.Consistent {
		err := fmt.Errorf("%w", ErrReopenNotHealthy)
		return r.recordLocked("reopen_refused", stage, r.status, now, "", err), err
	}
	r.status = RolloutRolledBack
	return r.recordLocked("reopen", stage, RolloutRolledBack, now, "", nil), nil
}

// recordLocked appends an immutable StageRecord to the history and returns
// a copy of it. Callers hold mu. It never rewrites or removes a prior
// entry; it only ever appends.
func (r *Rollout) recordLocked(kind, stage string, status RolloutStatus, now time.Time, manifestDigest string, cause error) StageRecord {
	r.seq++
	rec := StageRecord{
		Sequence:       r.seq,
		Kind:           kind,
		Stage:          stage,
		Status:         status,
		EvaluatedAt:    now.UTC().Format(time.RFC3339),
		ManifestDigest: manifestDigest,
	}
	if cause != nil {
		rec.Reason = cause.Error()
	}
	rec.Digest = recordDigest(rec)
	r.history = append(r.history, rec)
	return rec
}

// recordDigest computes a stable digest over a StageRecord's canonical
// fields (excluding Digest itself, so the computation is never circular),
// mirroring decisionDigest in admission.go.
func recordDigest(rec StageRecord) string {
	payload := struct {
		Sequence       int           `json:"sequence"`
		Kind           string        `json:"kind"`
		Stage          string        `json:"stage"`
		Status         RolloutStatus `json:"status"`
		EvaluatedAt    string        `json:"evaluated_at"`
		Reason         string        `json:"reason,omitempty"`
		ManifestDigest string        `json:"manifest_digest,omitempty"`
	}{rec.Sequence, rec.Kind, rec.Stage, rec.Status, rec.EvaluatedAt, rec.Reason, rec.ManifestDigest}
	b, err := json.Marshal(payload)
	if err != nil {
		// payload is entirely plain strings/ints; Marshal cannot fail.
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
