package configbundle

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// CanaryOutcome is the only result a canary decision can carry.
type CanaryOutcome string

const (
	CanaryPaused   CanaryOutcome = "PAUSED"
	CanaryPromoted CanaryOutcome = "PROMOTED"
	// DecisionPause and DecisionPromote are concise compatibility spellings.
	DecisionPause   = CanaryPaused
	DecisionPromote = CanaryPromoted
)

const (
	CanaryHealthy  = "HEALTHY"
	CanaryAtRisk   = "AT_RISK"
	CanaryBreached = "BREACHED"
	CanaryUnknown  = "UNKNOWN"
)

var (
	ErrCanaryInvalid         = errors.New("configbundle: invalid canary plan")
	ErrCanarySigner          = errors.New("configbundle: canary decision signer is required")
	ErrCanaryDecision        = errors.New("configbundle: canary decision is invalid")
	ErrCanaryDecisionMutated = errors.New("configbundle: canary decision was mutated")
)

// CanaryObservation is one immutable health measurement for the declared
// canary window. EvidenceDigest is a reference, never a payload.
type CanaryObservation struct {
	ID             string
	Status         any
	ObservedAt     time.Time
	WindowFrom     time.Time
	WindowTo       time.Time
	Good           int64
	Total          int64
	Complete       bool
	EvidenceDigest string
}

// HealthObservation is a descriptive alias for CanaryObservation.
type HealthObservation = CanaryObservation

// CanaryEvidence records an independently produced reconciliation or
// conformance result. Unknown evidence is never treated as a pass.
type CanaryEvidence struct {
	Available      bool
	Passed         bool
	EvidenceDigest string
}

// CanaryPlan declares the bounded cohort and all evidence needed to decide
// whether it may be promoted. It contains no runtime or network callback.
type CanaryPlan struct {
	Scope          Scope
	BundleDigest   string
	Epoch          uint64
	Cohort         []string
	CohortID       string
	MaxCohort      int
	CohortLimit    int
	WindowFrom     time.Time
	WindowTo       time.Time
	Health         []CanaryObservation
	Observations   []CanaryObservation
	Reconciliation CanaryEvidence
	Conformance    CanaryEvidence
}

// CanaryRequest is a compatibility alias for CanaryPlan.
type CanaryRequest = CanaryPlan

// CanaryDecision is signed evidence of a deterministic pause or promotion.
// It contains identities and digests only, not telemetry payloads.
type CanaryDecision struct {
	Scope          Scope
	BundleDigest   string
	Epoch          uint64
	CohortID       string
	CohortSize     int
	WindowFrom     time.Time
	WindowTo       time.Time
	Outcome        CanaryOutcome
	Reason         string
	EvidenceDigest []string
	DecidedAt      time.Time
	Digest         string
	Signature      BundleSignature
}

// Decision is the concise spelling used by rollout callers.
type Decision = CanaryDecision

// CanaryController evaluates canaries with an injected clock and signer.
// Evaluation is pure apart from retaining an append-only decision history.
type CanaryController struct {
	signer  ReceiptSigner
	now     func() time.Time
	mu      sync.RWMutex
	history []CanaryDecision
}

// NewCanaryController creates a controller. The signer is mandatory because
// an unsigned rollout decision is not safe operational evidence.
func NewCanaryController(signer ReceiptSigner, clocks ...func() time.Time) *CanaryController {
	now := func() time.Time { return time.Now().UTC() }
	if len(clocks) > 0 && clocks[0] != nil {
		now = clocks[0]
	}
	return &CanaryController{signer: signer, now: now}
}

// EvaluateCanary returns a signed PAUSED or PROMOTED decision. A valid plan
// with incomplete/unknown evidence is deliberately a successful PAUSED
// decision, allowing operators to act without treating telemetry loss as an
// application error.
func (c *CanaryController) EvaluateCanary(plan CanaryPlan) (CanaryDecision, error) {
	if c == nil || c.signer == nil {
		return CanaryDecision{}, ErrCanarySigner
	}
	if err := validateCanaryPlan(plan); err != nil {
		return CanaryDecision{}, err
	}
	now := c.now().UTC()
	observations := plan.Health
	if len(observations) == 0 {
		observations = plan.Observations
	}
	decision := CanaryDecision{Scope: plan.Scope, BundleDigest: plan.BundleDigest, Epoch: plan.Epoch, CohortID: plan.CohortID, CohortSize: len(plan.Cohort), WindowFrom: plan.WindowFrom.UTC(), WindowTo: plan.WindowTo.UTC(), Outcome: CanaryPromoted, Reason: "HEALTHY_EVIDENCE", DecidedAt: now}
	decision.EvidenceDigest = observationDigests(observations, plan.Reconciliation, plan.Conformance)
	if now.Before(plan.WindowTo) {
		decision.Outcome, decision.Reason = CanaryPaused, "EVIDENCE_WINDOW_OPEN"
	} else if len(observations) == 0 {
		decision.Outcome, decision.Reason = CanaryPaused, "HEALTH_EVIDENCE_UNKNOWN"
	} else if reason := unhealthyObservationReason(observations, plan); reason != "" {
		decision.Outcome, decision.Reason = CanaryPaused, reason
	} else if !plan.Reconciliation.Available || !plan.Reconciliation.Passed {
		decision.Outcome, decision.Reason = CanaryPaused, "RECONCILIATION_NOT_CONFORMANT"
	} else if !plan.Conformance.Available || !plan.Conformance.Passed {
		decision.Outcome, decision.Reason = CanaryPaused, "CONFORMANCE_NOT_PASSED"
	}
	digest, err := decision.DigestValue()
	if err != nil {
		return CanaryDecision{}, err
	}
	decision.Digest = digest
	signature, err := c.signer.SignDigest(digest)
	if err != nil {
		return CanaryDecision{}, fmt.Errorf("%w: %v", ErrCanarySigner, err)
	}
	if err := validateSignatureIdentity(signature); err != nil {
		return CanaryDecision{}, err
	}
	decision.Signature = signature
	c.mu.Lock()
	c.history = append(c.history, cloneCanaryDecision(decision))
	c.mu.Unlock()
	return cloneCanaryDecision(decision), nil
}

// Evaluate is an alias for EvaluateCanary.
func (c *CanaryController) Evaluate(plan CanaryPlan) (CanaryDecision, error) {
	return c.EvaluateCanary(plan)
}

// Activate evaluates one canary activation request.
func (c *CanaryController) Activate(plan CanaryPlan) (CanaryDecision, error) {
	return c.EvaluateCanary(plan)
}

// EvaluateCanary is the functional form for callers that do not need a
// retained decision history.
func EvaluateCanary(plan CanaryPlan, signer ReceiptSigner, now time.Time) (CanaryDecision, error) {
	controller := NewCanaryController(signer, func() time.Time { return now })
	return controller.EvaluateCanary(plan)
}

// Decisions returns a defensive, chronological copy of signed decisions.
func (c *CanaryController) Decisions() []CanaryDecision {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]CanaryDecision, len(c.history))
	for i := range c.history {
		out[i] = cloneCanaryDecision(c.history[i])
	}
	return out
}

// DigestValue computes the decision digest without trusting Digest or
// Signature.
func (d CanaryDecision) DigestValue() (string, error) {
	if d.Outcome != CanaryPaused && d.Outcome != CanaryPromoted {
		return "", ErrCanaryDecision
	}
	w := canonicalbytes.New("hcmnext.platform.configbundle.CanaryDecision", 1).
		String("tenant_id", d.Scope.TenantID).
		String("cell_id", d.Scope.CellID).
		String("bundle_digest", d.BundleDigest).
		Int("epoch", int64(d.Epoch)).
		String("cohort_id", d.CohortID).
		Int("cohort_size", int64(d.CohortSize)).
		String("window_from", d.WindowFrom.UTC().Format(time.RFC3339Nano)).
		String("window_to", d.WindowTo.UTC().Format(time.RFC3339Nano)).
		String("outcome", string(d.Outcome)).
		String("reason", d.Reason).
		SortedStrings("evidence_digest", d.EvidenceDigest).
		String("decided_at", d.DecidedAt.UTC().Format(time.RFC3339Nano))
	return w.Digest()
}

// Verify checks the canonical digest and detached signature.
func (d CanaryDecision) Verify(publicKey ed25519.PublicKey) error {
	if err := validateSignatureIdentity(d.Signature); err != nil {
		return err
	}
	digest, err := d.DigestValue()
	if err != nil {
		return err
	}
	if digest != d.Digest {
		return ErrCanaryDecisionMutated
	}
	raw, err := digestBytes(d.Digest)
	if err != nil {
		return err
	}
	signature, err := decodeSignature(d.Signature.Value)
	if err != nil || !ed25519.Verify(publicKey, raw, signature) {
		return ErrCanaryDecision
	}
	return nil
}

// Explain returns audit-safe rollout facts.
func (d CanaryDecision) Explain() string {
	return fmt.Sprintf("canary decision=%s reason=%s tenant=%s cell=%s epoch=%d bundle=%s", d.Outcome, d.Reason, d.Scope.TenantID, d.Scope.CellID, d.Epoch, d.BundleDigest)
}

func validateCanaryPlan(plan CanaryPlan) error {
	if strings.TrimSpace(plan.Scope.TenantID) == "" || strings.TrimSpace(plan.Scope.CellID) == "" || plan.Epoch == 0 {
		return ErrCanaryInvalid
	}
	if _, err := digestBytes(plan.BundleDigest); err != nil {
		return fmt.Errorf("%w: bundle digest: %v", ErrCanaryInvalid, err)
	}
	if plan.WindowFrom.IsZero() || plan.WindowTo.IsZero() || !plan.WindowTo.After(plan.WindowFrom) {
		return fmt.Errorf("%w: evidence window must be non-empty", ErrCanaryInvalid)
	}
	limit := plan.MaxCohort
	if plan.CohortLimit != 0 {
		if limit != 0 && limit != plan.CohortLimit {
			return fmt.Errorf("%w: cohort limits disagree", ErrCanaryInvalid)
		}
		limit = plan.CohortLimit
	}
	if limit <= 0 || len(plan.Cohort) == 0 || len(plan.Cohort) > limit {
		return fmt.Errorf("%w: cohort must be non-empty and bounded", ErrCanaryInvalid)
	}
	seen := make(map[string]struct{}, len(plan.Cohort))
	for _, member := range plan.Cohort {
		if strings.TrimSpace(member) == "" || strings.TrimSpace(member) != member {
			return fmt.Errorf("%w: cohort member is not exact", ErrCanaryInvalid)
		}
		if _, ok := seen[member]; ok {
			return fmt.Errorf("%w: cohort contains duplicate member", ErrCanaryInvalid)
		}
		seen[member] = struct{}{}
	}
	return nil
}

func unhealthyObservationReason(observations []CanaryObservation, plan CanaryPlan) string {
	ordered := append([]CanaryObservation(nil), observations...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	for _, observation := range ordered {
		if strings.TrimSpace(observation.ID) == "" || observation.EvidenceDigest == "" || !observation.Complete || observation.Total <= 0 || observation.Good < 0 || observation.Good > observation.Total || observation.ObservedAt.IsZero() || observation.WindowFrom.UTC() != plan.WindowFrom.UTC() || observation.WindowTo.UTC() != plan.WindowTo.UTC() || observation.ObservedAt.Before(plan.WindowFrom) || observation.ObservedAt.After(plan.WindowTo) {
			return "HEALTH_EVIDENCE_UNKNOWN"
		}
		switch strings.ToUpper(fmt.Sprint(observation.Status)) {
		case CanaryHealthy:
		default:
			return "HEALTH_" + strings.ToUpper(fmt.Sprint(observation.Status))
		}
	}
	return ""
}

func observationDigests(observations []CanaryObservation, reconciliation, conformance CanaryEvidence) []string {
	var out []string
	for _, observation := range observations {
		if observation.EvidenceDigest != "" {
			out = append(out, observation.ID+":"+observation.EvidenceDigest)
		}
	}
	if reconciliation.EvidenceDigest != "" {
		out = append(out, "reconciliation:"+reconciliation.EvidenceDigest)
	}
	if conformance.EvidenceDigest != "" {
		out = append(out, "conformance:"+conformance.EvidenceDigest)
	}
	sort.Strings(out)
	return out
}

func cloneCanaryDecision(d CanaryDecision) CanaryDecision {
	d.EvidenceDigest = append([]string(nil), d.EvidenceDigest...)
	return d
}
