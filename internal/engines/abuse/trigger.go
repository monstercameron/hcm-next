package abuse

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var (
	ErrInvalidTriggerPolicy  = errors.New("abuse: invalid trigger policy")
	ErrInvalidTriggerRequest = errors.New("abuse: invalid trigger request")
	ErrTriggerAuthority      = errors.New("abuse: trigger authority is insufficient")
	ErrTerminationTarget     = errors.New("abuse: containment target cannot terminate employment or payroll")
)

// TriggerAction is the deliberately small set of reversible responses to a
// risk assessment. A risk engine never exposes a termination or employment
// outcome as an action.
type TriggerAction string

const (
	TriggerNoAction             TriggerAction = "NO_ACTION"
	TriggerStepUp               TriggerAction = "STEP_UP"
	TriggerHumanReview          TriggerAction = "HUMAN_REVIEW"
	TriggerTemporaryContainment TriggerAction = "TEMPORARY_CONTAINMENT"
)

func (a TriggerAction) Valid() bool {
	switch a {
	case TriggerNoAction, TriggerStepUp, TriggerHumanReview, TriggerTemporaryContainment:
		return true
	default:
		return false
	}
}

// ReviewRoute is the owned route for a human decision. It is present in every
// non-empty trigger decision, including step-up and containment decisions, so
// a false positive always has a human route without becoming an accusation.
type ReviewRoute struct {
	Queue       string
	CaseType    string
	SLA         time.Duration
	ReviewerSoD bool
}

func (r ReviewRoute) Validate() error {
	if strings.TrimSpace(r.Queue) == "" || strings.TrimSpace(r.CaseType) == "" || r.SLA <= 0 || !r.ReviewerSoD {
		return fmt.Errorf("%w: review route", ErrInvalidTriggerPolicy)
	}
	return nil
}

// TriggerRule binds a minimum risk score and confidence floor to one
// response. Rules are evaluated from highest score to lowest score; duplicate
// thresholds are rejected so the policy cannot depend on construction order.
type TriggerRule struct {
	ID                string
	MinimumScore      int
	MinimumConfidence int
	Action            TriggerAction
	Duration          time.Duration
}

func (r TriggerRule) Validate(maxDuration time.Duration) error {
	if strings.TrimSpace(r.ID) == "" || r.MinimumScore < 0 || r.MinimumScore > 100 ||
		r.MinimumConfidence < 0 || r.MinimumConfidence > 100 || !r.Action.Valid() {
		return fmt.Errorf("%w: rule %q", ErrInvalidTriggerPolicy, r.ID)
	}
	switch r.Action {
	case TriggerStepUp, TriggerTemporaryContainment:
		if r.Duration <= 0 || r.Duration > maxDuration {
			return fmt.Errorf("%w: rule %q duration", ErrInvalidTriggerPolicy, r.ID)
		}
	case TriggerNoAction:
		if r.Duration != 0 {
			return fmt.Errorf("%w: no-action rule %q has a duration", ErrInvalidTriggerPolicy, r.ID)
		}
	}
	return nil
}

// TriggerPolicy is an immutable-by-value, versioned response policy. It does
// not contain detector weights or make a personnel decision; it only maps a
// previously validated RiskAssessment to a bounded response.
type TriggerPolicy struct {
	ID                string
	Version           string
	Rules             []TriggerRule
	Review            ReviewRoute
	MaxActionDuration time.Duration
	Digest            string
}

func (p TriggerPolicy) Validate() error {
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Version) == "" || p.MaxActionDuration <= 0 {
		return ErrInvalidTriggerPolicy
	}
	if len(p.Rules) == 0 {
		return fmt.Errorf("%w: no rules", ErrInvalidTriggerPolicy)
	}
	if err := p.Review.Validate(); err != nil {
		return err
	}
	seenIDs := make(map[string]struct{}, len(p.Rules))
	seenScores := make(map[int]struct{}, len(p.Rules))
	for _, rule := range p.Rules {
		if err := rule.Validate(p.MaxActionDuration); err != nil {
			return err
		}
		if _, exists := seenIDs[rule.ID]; exists {
			return fmt.Errorf("%w: duplicate rule %q", ErrInvalidTriggerPolicy, rule.ID)
		}
		if _, exists := seenScores[rule.MinimumScore]; exists {
			return fmt.Errorf("%w: duplicate score %d", ErrInvalidTriggerPolicy, rule.MinimumScore)
		}
		seenIDs[rule.ID] = struct{}{}
		seenScores[rule.MinimumScore] = struct{}{}
	}
	if p.Digest != "" {
		digest, err := p.canonicalDigest()
		if err != nil || digest != p.Digest {
			return fmt.Errorf("%w: policy digest", ErrInvalidTriggerPolicy)
		}
	}
	return nil
}

func (p TriggerPolicy) canonicalDigest() (string, error) {
	rules := append([]TriggerRule(nil), p.Rules...)
	sort.Slice(rules, func(i, j int) bool { return rules[i].MinimumScore < rules[j].MinimumScore })
	w := canonicalbytes.New("hcmnext.engines.abuse.TriggerPolicy", 1).
		String("id", p.ID).String("version", p.Version).
		String("review_queue", p.Review.Queue).String("review_case_type", p.Review.CaseType).
		Int("review_sla_ns", int64(p.Review.SLA)).Bool("reviewer_sod", p.Review.ReviewerSoD).
		Int("max_action_duration_ns", int64(p.MaxActionDuration)).Count("rules", len(rules))
	for _, rule := range rules {
		w.String("rule_id", rule.ID).Int("minimum_score", int64(rule.MinimumScore)).
			Int("minimum_confidence", int64(rule.MinimumConfidence)).
			String("action", string(rule.Action)).Int("duration_ns", int64(rule.Duration))
	}
	return w.Digest()
}

// NewTriggerPolicy validates and fingerprints a trigger policy. The input
// rule slice is copied so callers cannot mutate a policy after publication.
func NewTriggerPolicy(id, version string, rules []TriggerRule, review ReviewRoute, maxActionDuration time.Duration) (TriggerPolicy, error) {
	p := TriggerPolicy{ID: id, Version: version, Rules: append([]TriggerRule(nil), rules...), Review: review, MaxActionDuration: maxActionDuration}
	if err := p.Validate(); err != nil {
		return TriggerPolicy{}, err
	}
	digest, err := p.canonicalDigest()
	if err != nil {
		return TriggerPolicy{}, err
	}
	p.Digest = digest
	return p, nil
}

// DefaultTriggerPolicy is a conservative example policy for composition
// tests and adapters. Production callers should publish a versioned policy
// appropriate to their authority catalog.
func DefaultTriggerPolicy() TriggerPolicy {
	p, err := NewTriggerPolicy("abuse-response", "2026.09", []TriggerRule{
		{ID: "step-up", MinimumScore: 50, MinimumConfidence: 60, Action: TriggerStepUp, Duration: 10 * time.Minute},
		{ID: "review", MinimumScore: 70, MinimumConfidence: 0, Action: TriggerHumanReview},
		{ID: "contain", MinimumScore: 90, MinimumConfidence: 85, Action: TriggerTemporaryContainment, Duration: 15 * time.Minute},
	}, ReviewRoute{Queue: "security-review", CaseType: "abuse-risk", SLA: time.Hour, ReviewerSoD: true}, 15*time.Minute)
	if err != nil {
		panic("abuse: default trigger policy is invalid: " + err.Error())
	}
	return p
}

// TriggerAuthority names the authorities the caller actually holds. These
// are inputs, not claims inferred from the score or from the risk principal.
type TriggerAuthority struct {
	Holder     string
	MayStepUp  bool
	MayReview  bool
	MayContain bool
}

func (a TriggerAuthority) Validate() error {
	if strings.TrimSpace(a.Holder) == "" || (!a.MayStepUp && !a.MayReview && !a.MayContain) {
		return fmt.Errorf("%w: no authority", ErrInvalidTriggerRequest)
	}
	return nil
}

// ContainmentTarget is a narrow capability scope. It is intentionally not a
// worker-status or payroll-disposition type; containment can pause a scoped
// capability but cannot encode termination, separation, or final-pay action.
type ContainmentTarget struct {
	TenantID   string
	Principal  string
	Capability string
}

func (t ContainmentTarget) Validate(expectedPrincipal string) error {
	if strings.TrimSpace(t.TenantID) == "" || strings.TrimSpace(t.Principal) == "" ||
		strings.TrimSpace(t.Capability) == "" || t.Principal != expectedPrincipal {
		return fmt.Errorf("%w: containment target", ErrInvalidTriggerRequest)
	}
	normalized := strings.ToLower(strings.ReplaceAll(t.Capability, "-", "_"))
	for _, forbidden := range []string{"terminate", "termination", "separate", "separation", "offboard", "dismiss", "fire", "payroll_termination", "employment_termination"} {
		if strings.Contains(normalized, forbidden) {
			return ErrTerminationTarget
		}
	}
	return nil
}

// TriggerRequest supplies the validated assessment and current authority
// context. At is supplied by the caller to keep the kernel deterministic.
type TriggerRequest struct {
	Assessment RiskAssessment
	At         time.Time
	Authority  TriggerAuthority
	Target     ContainmentTarget
}

func (r TriggerRequest) Validate() error {
	if err := r.Assessment.Validate(); err != nil {
		return fmt.Errorf("%w: assessment: %v", ErrInvalidTriggerRequest, err)
	}
	if r.At.IsZero() || !r.Assessment.WindowEnd.Before(r.At) {
		return fmt.Errorf("%w: evaluation time", ErrInvalidTriggerRequest)
	}
	if err := r.Authority.Validate(); err != nil {
		return err
	}
	return nil
}

// StepUpTrigger is a bounded obligation for the existing AUTHN-005 adapter.
// It carries no credential or bearer token and must be evaluated again by the
// step-up owner at execution time.
type StepUpTrigger struct {
	AssessmentDigest  string
	ExpiresAt         time.Time
	RequiredAssurance string
}

// HumanReviewTrigger is a route, not a finding disposition or accusation.
type HumanReviewTrigger struct {
	AssessmentDigest string
	Route            ReviewRoute
}

// TemporaryContainment is a scoped, reversible signal for the existing
// CP-008 adapter. The adapter may translate it to a signed kill switch, but
// this pure package never issues or applies that switch.
type TemporaryContainment struct {
	AssessmentDigest string
	Target           ContainmentTarget
	ExpiresAt        time.Time
	Reversible       bool
}

// TriggerDecision is the complete pure result. A non-empty action always
// carries the human route, and all effectful-looking outputs are bounded by
// ExpiresAt and bound to the assessment and policy digests.
type TriggerDecision struct {
	Action           TriggerAction
	AssessmentDigest string
	PolicyID         string
	PolicyVersion    string
	PolicyDigest     string
	RuleID           string
	At               time.Time
	ExpiresAt        time.Time
	Reason           string
	Review           HumanReviewTrigger
	StepUp           *StepUpTrigger
	Containment      *TemporaryContainment
	Digest           string
}

func (d TriggerDecision) Validate() error {
	if !d.Action.Valid() || strings.TrimSpace(d.AssessmentDigest) == "" ||
		strings.TrimSpace(d.PolicyID) == "" || strings.TrimSpace(d.PolicyVersion) == "" ||
		strings.TrimSpace(d.PolicyDigest) == "" || d.At.IsZero() || strings.TrimSpace(d.Reason) == "" || d.Digest == "" {
		return ErrInvalidTriggerRequest
	}
	if d.Action == TriggerNoAction {
		if !d.ExpiresAt.IsZero() || d.StepUp != nil || d.Containment != nil || d.Review.Route.Queue != "" {
			return ErrInvalidTriggerRequest
		}
		return nil
	}
	if err := d.Review.Route.Validate(); err != nil {
		return err
	}
	if d.Review.AssessmentDigest != d.AssessmentDigest {
		return ErrInvalidTriggerRequest
	}
	switch d.Action {
	case TriggerStepUp:
		if d.StepUp == nil || d.Containment != nil || d.ExpiresAt.IsZero() || !d.At.Before(d.ExpiresAt) ||
			d.StepUp.AssessmentDigest != d.AssessmentDigest || d.StepUp.ExpiresAt != d.ExpiresAt || strings.TrimSpace(d.StepUp.RequiredAssurance) == "" {
			return ErrInvalidTriggerRequest
		}
	case TriggerHumanReview:
		if d.StepUp != nil || d.Containment != nil || !d.ExpiresAt.IsZero() {
			return ErrInvalidTriggerRequest
		}
	case TriggerTemporaryContainment:
		if d.Containment == nil || d.StepUp != nil || d.ExpiresAt.IsZero() || !d.At.Before(d.ExpiresAt) || !d.Containment.Reversible ||
			d.Containment.AssessmentDigest != d.AssessmentDigest || d.Containment.ExpiresAt != d.ExpiresAt {
			return ErrInvalidTriggerRequest
		}
		if err := d.Containment.Target.Validate(d.Containment.Target.Principal); err != nil {
			return err
		}
	}
	digest, err := d.canonicalDigest()
	if err != nil || digest != d.Digest {
		return ErrInvalidTriggerRequest
	}
	return nil
}

func (d TriggerDecision) canonicalDigest() (string, error) {
	w := canonicalbytes.New("hcmnext.engines.abuse.TriggerDecision", 1).
		String("action", string(d.Action)).String("assessment_digest", d.AssessmentDigest).
		String("policy_id", d.PolicyID).String("policy_version", d.PolicyVersion).
		String("policy_digest", d.PolicyDigest).String("rule_id", d.RuleID).
		String("at", d.At.UTC().Format(time.RFC3339Nano)).String("expires_at", d.ExpiresAt.UTC().Format(time.RFC3339Nano)).
		String("reason", d.Reason).String("review_assessment", d.Review.AssessmentDigest).
		String("review_queue", d.Review.Route.Queue).String("review_case_type", d.Review.Route.CaseType).
		Int("review_sla_ns", int64(d.Review.Route.SLA)).Bool("reviewer_sod", d.Review.Route.ReviewerSoD)
	if d.StepUp != nil {
		w.Bool("has_step_up", true).String("step_up_assessment", d.StepUp.AssessmentDigest).
			String("step_up_expires_at", d.StepUp.ExpiresAt.UTC().Format(time.RFC3339Nano)).
			String("required_assurance", d.StepUp.RequiredAssurance)
	} else {
		w.Bool("has_step_up", false)
	}
	if d.Containment != nil {
		w.Bool("has_containment", true).String("containment_assessment", d.Containment.AssessmentDigest).
			String("tenant_id", d.Containment.Target.TenantID).String("principal", d.Containment.Target.Principal).
			String("capability", d.Containment.Target.Capability).
			String("containment_expires_at", d.Containment.ExpiresAt.UTC().Format(time.RFC3339Nano)).
			Bool("reversible", d.Containment.Reversible)
	} else {
		w.Bool("has_containment", false)
	}
	return w.Digest()
}

func selectTriggerRule(p TriggerPolicy, score int) (TriggerRule, bool) {
	rules := append([]TriggerRule(nil), p.Rules...)
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].MinimumScore != rules[j].MinimumScore {
			return rules[i].MinimumScore > rules[j].MinimumScore
		}
		return rules[i].ID < rules[j].ID
	})
	for _, rule := range rules {
		if score >= rule.MinimumScore {
			return rule, true
		}
	}
	return TriggerRule{}, false
}

// EvaluateTrigger applies one explicit response policy to one validated risk
// assessment. It is pure: no review, step-up, kill-switch, database, clock,
// or employment/payroll side effect is performed here.
func EvaluateTrigger(p TriggerPolicy, r TriggerRequest) (TriggerDecision, error) {
	if err := p.Validate(); err != nil {
		return TriggerDecision{}, err
	}
	if p.Digest == "" {
		return TriggerDecision{}, fmt.Errorf("%w: policy digest", ErrInvalidTriggerPolicy)
	}
	if err := r.Validate(); err != nil {
		return TriggerDecision{}, err
	}
	rule, matched := selectTriggerRule(p, r.Assessment.Score)
	decision := TriggerDecision{
		Action: TriggerNoAction, AssessmentDigest: r.Assessment.Digest,
		PolicyID: p.ID, PolicyVersion: p.Version, PolicyDigest: p.Digest,
		At: r.At.UTC(), Reason: "BELOW_POLICY_THRESHOLD",
	}
	if matched {
		decision.RuleID = rule.ID
		decision.Action = rule.Action
		decision.Reason = "RISK_THRESHOLD_MATCHED"
		if r.Assessment.Confidence < rule.MinimumConfidence {
			decision.Action = TriggerHumanReview
			decision.Reason = "CONFIDENCE_REQUIRES_HUMAN_REVIEW"
		}
	}

	if decision.Action == TriggerStepUp && !r.Authority.MayStepUp {
		decision.Action = TriggerHumanReview
		decision.Reason = "STEP_UP_AUTHORITY_MISSING_REVIEW_REQUIRED"
	}
	if decision.Action == TriggerTemporaryContainment && !r.Authority.MayContain {
		decision.Action = TriggerHumanReview
		decision.Reason = "CONTAINMENT_AUTHORITY_MISSING_REVIEW_REQUIRED"
	}
	if decision.Action == TriggerHumanReview && !r.Authority.MayReview {
		return TriggerDecision{}, ErrTriggerAuthority
	}

	if decision.Action != TriggerNoAction {
		decision.Review = HumanReviewTrigger{AssessmentDigest: r.Assessment.Digest, Route: p.Review}
	}
	switch decision.Action {
	case TriggerStepUp:
		decision.ExpiresAt = r.At.Add(rule.Duration)
		decision.StepUp = &StepUpTrigger{AssessmentDigest: r.Assessment.Digest, ExpiresAt: decision.ExpiresAt, RequiredAssurance: "HIGH"}
	case TriggerTemporaryContainment:
		if err := r.Target.Validate(r.Assessment.Principal); err != nil {
			return TriggerDecision{}, err
		}
		decision.ExpiresAt = r.At.Add(rule.Duration)
		decision.Containment = &TemporaryContainment{AssessmentDigest: r.Assessment.Digest, Target: r.Target, ExpiresAt: decision.ExpiresAt, Reversible: true}
	}
	digest, err := decision.canonicalDigest()
	if err != nil {
		return TriggerDecision{}, err
	}
	decision.Digest = digest
	return decision, nil
}

// ExplainTrigger returns audit-safe response metadata without raw activity,
// worker data, or a personnel conclusion.
type TriggerExplanation struct {
	Action           TriggerAction
	Reason           string
	AssessmentDigest string
	PolicyDigest     string
	RuleID           string
	HasHumanRoute    bool
	Reversible       bool
}

func ExplainTrigger(d TriggerDecision) (TriggerExplanation, error) {
	if err := d.Validate(); err != nil {
		return TriggerExplanation{}, err
	}
	exp := TriggerExplanation{Action: d.Action, Reason: d.Reason, AssessmentDigest: d.AssessmentDigest, PolicyDigest: d.PolicyDigest, RuleID: d.RuleID, HasHumanRoute: d.Action != TriggerNoAction}
	exp.Reversible = d.Action == TriggerStepUp || d.Action == TriggerTemporaryContainment
	return exp, nil
}
