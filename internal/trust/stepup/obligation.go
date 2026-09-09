package stepup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Risk is the risk tier a governance risk authority assigned to the action
// under evaluation. It is a governance coordinate, not an authentication
// concept: nothing here knows or names how a subject would raise assurance.
type Risk uint8

// Risk tiers, ordered. A higher tier satisfies a rule written for a lower
// one, which is what makes [ObligationRule.MinRisk] a floor.
const (
	RiskUnspecified Risk = iota
	RiskRoutine
	RiskElevated
	RiskCritical
)

// String returns the canonical wire spelling of the risk tier.
func (r Risk) String() string {
	switch r {
	case RiskRoutine:
		return "routine"
	case RiskElevated:
		return "elevated"
	case RiskCritical:
		return "critical"
	case RiskUnspecified:
		return "unspecified"
	default:
		return "invalid(" + strconv.Itoa(int(r)) + ")"
	}
}

func (r Risk) valid() bool { return r >= RiskRoutine && r <= RiskCritical }

// riskWire returns the wire value for a risk tier, omitting the unspecified
// tier so a proof written before risk binding existed round-trips unchanged.
func riskWire(r Risk) string {
	if r == RiskUnspecified {
		return ""
	}
	return r.String()
}

func parseRisk(s string) (Risk, error) {
	if s == "" {
		return RiskUnspecified, nil
	}
	for _, r := range []Risk{RiskRoutine, RiskElevated, RiskCritical} {
		if r.String() == s {
			return r, nil
		}
	}
	return RiskUnspecified, fmt.Errorf("unknown risk %q", s)
}

// Stage is the point in the request's life at which the obligation is being
// evaluated. The same obligation is evaluated at both stages: a decision-time
// pass is not carried forward, it is recomputed before the effect runs.
type Stage string

// Evaluation stages.
const (
	StageDecision  Stage = "decision"
	StageExecution Stage = "execution"
)

func (s Stage) valid() bool { return s == StageDecision || s == StageExecution }

// Obligation outcome codes. [CodeStepUpRequired] is the token a caller sees
// when a step-up obligation is unmet; it is stable and safe to surface.
const (
	CodeStepUpRequired    = "STEP_UP_REQUIRED"
	CodeStepUpSatisfied   = "STEP_UP_SATISFIED"
	CodeStepUpNotRequired = "STEP_UP_NOT_REQUIRED"
)

// Reason tokens. They are for operators and evidence, never a hint about
// which credential the subject should present.
const (
	ReasonSatisfied           = "satisfied"
	ReasonNotRequired         = "not_required"
	ReasonNoPolicy            = "no_policy"
	ReasonMalformedRequest    = "malformed_request"
	ReasonRiskUnspecified     = "risk_unspecified"
	ReasonPolicyGap           = "policy_gap"
	ReasonNoProof             = "no_step_up_proof"
	ReasonBindingMismatch     = "binding_mismatch"
	ReasonProofPremature      = "proof_premature"
	ReasonProofExpired        = "proof_expired"
	ReasonProofStale          = "proof_stale"
	ReasonProofAssuranceLow   = "proof_assurance_below_requirement"
	ReasonCurrentAssuranceLow = "current_assurance_below_requirement"
	ReasonPrincipalExpired    = "principal_credential_expired"
	ReasonSessionMismatch     = "session_not_bound"
)

// ErrStepUpRequired is the error a caller receives when it tried to act while
// a step-up obligation is unmet. Callers match it with errors.Is and map it
// onto [CodeStepUpRequired]; no proposal is approved and no effect runs.
var ErrStepUpRequired = errors.New("stepup: " + CodeStepUpRequired)

// ErrInvalidObligationPolicy is returned when a policy cannot be built.
var ErrInvalidObligationPolicy = errors.New("stepup: invalid step-up obligation policy")

// ObligationRule is one declarative policy row. It selects an assurance level
// and a recency window for a capability, a set of purposes and a risk floor.
//
// A rule deliberately has no field naming an identity-provider method, factor
// or enrollment: policy selects assurance, and how a subject reaches that
// assurance is the identity provider's business.
type ObligationRule struct {
	// RuleID names the rule in evidence.
	RuleID string
	// Capability is the exact capability identifier the rule governs.
	Capability string
	// Purposes are the purposes of processing the rule applies to. An empty
	// set matches nothing: a rule must say what it is for.
	Purposes []string
	// MinRisk is the risk floor. The rule applies at this tier and above.
	MinRisk Risk
	// MinAssurance is the assurance the step-up must reach.
	MinAssurance trust.Assurance
	// Recency bounds how old a qualifying step-up may be at the moment of
	// evaluation. It is re-applied at execution, so a proof that was fresh at
	// decision time can still be refused at commit time.
	Recency time.Duration
}

func (r ObligationRule) validate() error {
	if strings.TrimSpace(r.RuleID) == "" || strings.TrimSpace(r.Capability) == "" {
		return fmt.Errorf("%w: rule id and capability are required", ErrInvalidObligationPolicy)
	}
	if len(r.Purposes) == 0 {
		return fmt.Errorf("%w: rule %q names no purpose", ErrInvalidObligationPolicy, r.RuleID)
	}
	for _, p := range r.Purposes {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("%w: rule %q has an empty purpose", ErrInvalidObligationPolicy, r.RuleID)
		}
	}
	if !r.MinRisk.valid() {
		return fmt.Errorf("%w: rule %q has risk floor %s", ErrInvalidObligationPolicy, r.RuleID, r.MinRisk)
	}
	if r.MinAssurance == trust.AssuranceUnspecified || r.MinAssurance > trust.AssuranceHigh {
		return fmt.Errorf("%w: rule %q selects no assurance", ErrInvalidObligationPolicy, r.RuleID)
	}
	if r.Recency <= 0 {
		return fmt.Errorf("%w: rule %q has no recency window", ErrInvalidObligationPolicy, r.RuleID)
	}
	return nil
}

func (r ObligationRule) matches(capability, purpose string, risk Risk) bool {
	if r.Capability != capability || risk < r.MinRisk {
		return false
	}
	for _, p := range r.Purposes {
		if p == purpose {
			return true
		}
	}
	return false
}

// ObligationPolicy is the immutable, declarative step-up policy. It is the
// only thing that decides whether a step-up is owed and at what assurance.
type ObligationPolicy struct {
	rules []ObligationRule
	// gap is the fail-closed requirement applied when an elevated or higher
	// risk action matches no rule. A policy hole must not read as "allowed".
	gap Requirement
	// assuranceTable and assuranceEvidence are populated only by
	// NewAssuranceObligationPolicy. Keeping them private preserves the
	// original pure policy constructor while making the assurance-aware path
	// explicit and auditable.
	assuranceTable    *CredentialAssuranceTable
	assuranceEvidence AssuranceEvidenceSink
}

// NewObligationPolicy validates and freezes rules into a policy. Rules are
// sorted by identifier so evidence is stable regardless of declaration order.
func NewObligationPolicy(rules ...ObligationRule) (*ObligationPolicy, error) {
	seen := make(map[string]bool, len(rules))
	out := make([]ObligationRule, 0, len(rules))
	for _, r := range rules {
		if err := r.validate(); err != nil {
			return nil, err
		}
		if seen[r.RuleID] {
			return nil, fmt.Errorf("%w: duplicate rule id %q", ErrInvalidObligationPolicy, r.RuleID)
		}
		seen[r.RuleID] = true
		copied := r
		copied.Purposes = sortedCopy(r.Purposes)
		out = append(out, copied)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RuleID < out[j].RuleID })
	return &ObligationPolicy{
		rules: out,
		gap:   Requirement{MinAssurance: trust.AssuranceHigh, Recency: time.Minute},
	}, nil
}

// DefaultObligationPolicy is the P1A step-up policy: the sensitive verbs the
// gate knows require a substantial step-up at elevated risk and a high one at
// critical risk, within a short recency window.
func DefaultObligationPolicy() *ObligationPolicy {
	rules := []ObligationRule{
		{RuleID: "p1a.approve.elevated", Capability: ActionApprove, Purposes: []string{PurposeHCMOperations}, MinRisk: RiskElevated, MinAssurance: trust.AssuranceSubstantial, Recency: 5 * time.Minute},
		{RuleID: "p1a.approve.critical", Capability: ActionApprove, Purposes: []string{PurposeHCMOperations}, MinRisk: RiskCritical, MinAssurance: trust.AssuranceHigh, Recency: 2 * time.Minute},
		{RuleID: "p1a.repair.elevated", Capability: ActionRepair, Purposes: []string{PurposeHCMOperations}, MinRisk: RiskElevated, MinAssurance: trust.AssuranceHigh, Recency: 2 * time.Minute},
		{RuleID: "p1a.export.elevated", Capability: ActionExport, Purposes: []string{PurposeHCMOperations}, MinRisk: RiskElevated, MinAssurance: trust.AssuranceSubstantial, Recency: 5 * time.Minute},
	}
	p, err := NewObligationPolicy(rules...)
	if err != nil {
		// The literal above is fixed; a failure here is a programming error.
		panic("stepup: default obligation policy is invalid: " + err.Error())
	}
	return p
}

// PurposeHCMOperations is the purpose of processing the default policy rules
// are written for.
const PurposeHCMOperations = "hcm_operations"

// Select returns the requirement policy imposes for one capability, purpose
// and risk tier, the identifier of the rule that produced it, and whether a
// step-up is owed at all. When several rules match, the strictest wins:
// the highest assurance, and the tightest recency at that assurance.
func (p *ObligationPolicy) Select(capability, purpose string, risk Risk) (Requirement, string, bool) {
	if p == nil || !risk.valid() {
		return Requirement{}, "", false
	}
	var (
		best   Requirement
		ruleID string
		found  bool
	)
	for _, r := range p.rules {
		if !r.matches(capability, purpose, risk) {
			continue
		}
		if !found || r.MinAssurance > best.MinAssurance ||
			(r.MinAssurance == best.MinAssurance && r.Recency < best.Recency) {
			best, ruleID, found = Requirement{MinAssurance: r.MinAssurance, Recency: r.Recency}, r.RuleID, true
		}
	}
	if found {
		return best, ruleID, true
	}
	if risk >= RiskElevated {
		// A policy hole at elevated or higher risk fails closed rather than
		// silently allowing the action.
		return p.gap, "policy.gap", true
	}
	return Requirement{}, "", false
}

// ObligationRequest is the decision or execution point being evaluated.
type ObligationRequest struct {
	// Operation carries the action, proposal, scopes, tenant and the
	// governance coordinates (purpose, capability, risk).
	Operation Operation
	// Stage is the point being evaluated. The same request is evaluated at
	// [StageDecision] and again at [StageExecution].
	Stage Stage
	// At is the evaluation instant.
	At time.Time
}

// Obligation is the result of evaluating a step-up obligation. It is a value:
// nothing was consumed, executed or recorded to produce it.
type Obligation struct {
	// Code is [CodeStepUpRequired], [CodeStepUpSatisfied] or
	// [CodeStepUpNotRequired].
	Code string
	// Required reports whether policy owes a step-up here at all.
	Required bool
	// Satisfied reports whether the presented step-up meets the requirement.
	Satisfied bool
	// Reason is a stable operator token. It never names a credential type.
	Reason string
	// RuleID names the policy rule that selected the requirement.
	RuleID string
	// Requirement is the assurance and recency policy selected.
	Requirement Requirement
	// Stage, EvaluatedAt and the binding coordinates make the decision
	// reproducible from evidence alone.
	Stage       Stage
	EvaluatedAt time.Time
	Tenant      values.TenantId
	Subject     string
	SessionRef  string
	Purpose     string
	Capability  string
	Risk        Risk
	Action      string
	ProposalID  string
	// ProofID, IssuedAt and ExpiresAt describe the step-up that satisfied the
	// obligation. They are empty when nothing satisfied it.
	ProofID   string
	IssuedAt  time.Time
	ExpiresAt time.Time
	// BindingDigest is the framed digest over every bound coordinate. It is
	// empty unless the obligation is satisfied.
	BindingDigest string
}

// Err returns [ErrStepUpRequired] when the obligation is owed and unmet, and
// nil otherwise.
func (o Obligation) Err() error {
	if o.Required && !o.Satisfied {
		return fmt.Errorf("%w: %s", ErrStepUpRequired, o.Reason)
	}
	return nil
}

// String returns a redacted, log-safe description.
func (o Obligation) String() string {
	return fmt.Sprintf("stepup obligation code=%s stage=%s capability=%s purpose=%s risk=%s assurance=%s rule=%s reason=%s",
		o.Code, o.Stage, o.Capability, o.Purpose, o.Risk, o.Requirement.MinAssurance, o.RuleID, o.Reason)
}

// EvaluateObligation decides whether a step-up is owed for req and, if one is,
// whether proof satisfies it for principal p at req.At. It is pure: it reads
// no store, mutates nothing, consumes nothing and executes nothing, so a
// transport, a decision coordinator and an execution path can all call it and
// reach the same conclusion from the same inputs.
//
// Everything unrecognized fails closed. A nil policy, a malformed request, an
// unspecified risk tier or an absent proof for a governed action all produce
// [CodeStepUpRequired].
func EvaluateObligation(pol *ObligationPolicy, req ObligationRequest, p *trust.Principal, proof *Proof) Obligation {
	ob := Obligation{
		Code:        CodeStepUpRequired,
		Required:    true,
		Stage:       req.Stage,
		EvaluatedAt: req.At.UTC(),
		Tenant:      req.Operation.Tenant,
		Purpose:     req.Operation.Purpose,
		Capability:  req.Operation.Capability,
		Risk:        req.Operation.Risk,
		Action:      req.Operation.Action,
		ProposalID:  req.Operation.ProposalID,
	}
	if p != nil {
		ob.Subject, ob.SessionRef = p.Subject(), p.SessionRef()
	}

	if pol == nil {
		ob.Reason = ReasonNoPolicy
		return ob
	}
	if p == nil || !req.Stage.valid() || req.At.IsZero() ||
		strings.TrimSpace(req.Operation.Capability) == "" || strings.TrimSpace(req.Operation.Purpose) == "" ||
		req.Operation.Tenant.Validate() != nil {
		ob.Reason = ReasonMalformedRequest
		return ob
	}
	if !req.Operation.Risk.valid() {
		ob.Reason = ReasonRiskUnspecified
		return ob
	}

	requirement, ruleID, required := pol.Select(req.Operation.Capability, req.Operation.Purpose, req.Operation.Risk)
	ob.Requirement, ob.RuleID = requirement, ruleID
	if !required {
		ob.Code, ob.Required, ob.Satisfied, ob.Reason = CodeStepUpNotRequired, false, true, ReasonNotRequired
		return ob
	}
	if ruleID == "policy.gap" {
		// Record the hole explicitly so it is visible in evidence rather
		// than looking like an ordinary refusal.
		ob.Reason = ReasonPolicyGap
	}

	if proof == nil {
		if ob.Reason == "" {
			ob.Reason = ReasonNoProof
		}
		return ob
	}
	ob.ProofID, ob.IssuedAt, ob.ExpiresAt = proof.ID, proof.IssuedAt.UTC(), proof.ExpiresAt.UTC()

	// The proof must bind this exact principal, session, tenant, action,
	// proposal, purpose, capability, risk and scope set.
	if proof.Subject != p.Subject() || proof.SessionRef != p.SessionRef() {
		ob.Reason = ReasonSessionMismatch
		return ob
	}
	if proof.Tenant != req.Operation.Tenant || proof.Action != req.Operation.Action ||
		proof.ProposalID != req.Operation.ProposalID || proof.Purpose != req.Operation.Purpose ||
		proof.Capability != req.Operation.Capability || proof.Risk != req.Operation.Risk ||
		!equalScopes(proof.Scopes, req.Operation.Scopes) {
		ob.Reason = ReasonBindingMismatch
		return ob
	}

	now := req.At.UTC()
	switch {
	case now.Before(proof.IssuedAt):
		ob.Reason = ReasonProofPremature
		return ob
	case !now.Before(proof.ExpiresAt):
		ob.Reason = ReasonProofExpired
		return ob
	case now.Sub(proof.IssuedAt) > requirement.Recency:
		ob.Reason = ReasonProofStale
		return ob
	case !proof.Assurance.AtLeast(requirement.MinAssurance):
		ob.Reason = ReasonProofAssuranceLow
		return ob
	case !p.Assurance().AtLeast(requirement.MinAssurance):
		ob.Reason = ReasonCurrentAssuranceLow
		return ob
	case !p.ExpiresAt().After(now):
		ob.Reason = ReasonPrincipalExpired
		return ob
	}

	ob.Code, ob.Satisfied, ob.Reason = CodeStepUpSatisfied, true, ReasonSatisfied
	ob.BindingDigest = ob.bindingDigest()
	return ob
}

// bindingDigest frames every bound coordinate with an explicit length so no
// two distinct bindings can collide by field-boundary ambiguity.
func (o Obligation) bindingDigest() string {
	h := sha256.New()
	for _, kv := range [][2]string{
		{"tenant", o.Tenant.String()}, {"subject", o.Subject}, {"session", o.SessionRef},
		{"purpose", o.Purpose}, {"capability", o.Capability}, {"risk", o.Risk.String()},
		{"action", o.Action}, {"proposal", o.ProposalID}, {"proof", o.ProofID},
		{"rule", o.RuleID}, {"assurance", o.Requirement.MinAssurance.String()},
		{"recency", o.Requirement.Recency.String()},
		{"iat", strconv.FormatInt(o.IssuedAt.UnixNano(), 10)},
		{"exp", strconv.FormatInt(o.ExpiresAt.UnixNano(), 10)},
	} {
		fmt.Fprintf(h, "%s=%d:%s;", kv[0], len(kv[1]), kv[1])
	}
	return "ev:stepup:" + hex.EncodeToString(h.Sum(nil))[:32]
}

// PresentUnderObligation re-evaluates the step-up obligation at execution
// time and, only when it is satisfied, presents the proof to the gate. When
// the obligation is unmet it returns [ErrStepUpRequired] with no consumption
// and no execution: no proposal is approved and no effect runs.
//
// This is the recheck the contract requires. A decision-time evaluation that
// passed is never carried forward as a token; the obligation is computed
// again here, against the clock and the principal as they are at commit.
func (g *Gate) PresentUnderObligation(ctx context.Context, pol *ObligationPolicy, proof Proof, op Operation, p *trust.Principal, req ObligationRequest) (Outcome, Obligation, error) {
	req.Operation, req.Stage = op, StageExecution
	if req.At.IsZero() {
		req.At = g.now()
	}
	ob := EvaluateObligation(pol, req, p, &proof)
	if err := ob.Err(); err != nil {
		return "", ob, err
	}
	if !ob.Required {
		// Nothing is owed, but the caller still asked for a gated execution:
		// hold it to the gate's own single-use contract.
		ob.Requirement = Requirement{MinAssurance: p.Assurance(), Recency: defaultLifetime}
	}
	out, err := g.Present(ctx, proof, op, p, ob.Requirement)
	return out, ob, err
}
