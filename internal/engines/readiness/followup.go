package readiness

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var ErrInvalidFollowUp = errors.New("readiness: invalid follow-up compilation")

type FollowUpKind string
type FollowUpGate string

const (
	FollowUpIntent         FollowUpKind = "DRAFT_INTENT"
	FollowUpTask           FollowUpKind = "DRAFT_TASK"
	FollowUpObligation     FollowUpKind = "DRAFT_OBLIGATION"
	GateHumanReview        FollowUpGate = "REQUIRES_HUMAN_REVIEW"
	GateGovernanceApproval FollowUpGate = "REQUIRES_GOVERNANCE_APPROVAL"
)

func (g FollowUpGate) Valid() bool { return g == GateHumanReview || g == GateGovernanceApproval }

func (k FollowUpKind) Valid() bool {
	return k == FollowUpIntent || k == FollowUpTask || k == FollowUpObligation
}

// FollowUpPolicy is explicit caller configuration. It supplies no executable
// behavior and cannot authorize execution; it only bounds the drafts emitted
// for a non-ready evaluation.
type FollowUpPolicy struct {
	Domain        Domain
	Version       uint64
	Owner         string
	Scope         string
	Risk          string
	Kinds         []FollowUpKind
	RequiredGates []FollowUpGate
	Digest        string
}

func (p FollowUpPolicy) Validate() error {
	if !p.Domain.Valid() || p.Version == 0 || strings.TrimSpace(p.Owner) == "" || strings.TrimSpace(p.Scope) == "" || strings.TrimSpace(p.Risk) == "" || strings.TrimSpace(p.Digest) == "" {
		return fmt.Errorf("%w: owner, scope and risk are required", ErrInvalidFollowUp)
	}
	if p.Risk != "LOW" && p.Risk != "MEDIUM" && p.Risk != "HIGH" {
		return fmt.Errorf("%w: risk must be LOW, MEDIUM or HIGH", ErrInvalidFollowUp)
	}
	if len(p.Kinds) == 0 || len(p.Kinds) > 3 {
		return fmt.Errorf("%w: one to three draft kinds are required", ErrInvalidFollowUp)
	}
	seen := map[FollowUpKind]struct{}{}
	for _, k := range p.Kinds {
		if !k.Valid() {
			return fmt.Errorf("%w: invalid draft kind %q", ErrInvalidFollowUp, k)
		}
		if _, ok := seen[k]; ok {
			return fmt.Errorf("%w: duplicate draft kind %q", ErrInvalidFollowUp, k)
		}
		seen[k] = struct{}{}
	}
	if len(p.RequiredGates) != 2 {
		return fmt.Errorf("%w: human and governance gates are required", ErrInvalidFollowUp)
	}
	gateSeen := make(map[FollowUpGate]struct{}, len(p.RequiredGates))
	for _, gate := range p.RequiredGates {
		if !gate.Valid() {
			return fmt.Errorf("%w: invalid required gate", ErrInvalidFollowUp)
		}
		if _, ok := gateSeen[gate]; ok {
			return fmt.Errorf("%w: duplicate required gate", ErrInvalidFollowUp)
		}
		gateSeen[gate] = struct{}{}
	}
	if _, ok := gateSeen[GateHumanReview]; !ok {
		return fmt.Errorf("%w: human review gate is required", ErrInvalidFollowUp)
	}
	if _, ok := gateSeen[GateGovernanceApproval]; !ok {
		return fmt.Errorf("%w: governance approval gate is required", ErrInvalidFollowUp)
	}
	want, err := followUpPolicyDigest(p)
	if err != nil || want != p.Digest {
		return fmt.Errorf("%w: policy digest mismatch", ErrInvalidFollowUp)
	}
	return nil
}

func NewFollowUpPolicy(p FollowUpPolicy) (FollowUpPolicy, error) {
	p.Kinds = append([]FollowUpKind(nil), p.Kinds...)
	sort.Slice(p.Kinds, func(i, j int) bool { return p.Kinds[i] < p.Kinds[j] })
	p.RequiredGates = append([]FollowUpGate(nil), p.RequiredGates...)
	sort.Slice(p.RequiredGates, func(i, j int) bool { return p.RequiredGates[i] < p.RequiredGates[j] })
	p.Digest, _ = followUpPolicyDigest(p)
	if err := p.Validate(); err != nil {
		return FollowUpPolicy{}, err
	}
	return p, nil
}

// DraftFollowUp is a bounded, non-executable recommendation. Evidence is
// represented only by the pinned resolution digest, never by restricted refs.
type DraftFollowUp struct {
	SemanticID              string
	Kind                    FollowUpKind
	RequirementID           string
	RequirementDigest       string
	EvaluationDigest        string
	EvidenceDigest          string
	EvaluationPolicyVersion int
	FollowUpPolicyVersion   uint64
	FollowUpPolicyDigest    string
	Owner                   string
	Scope                   string
	Risk                    string
	Reason                  string
	RequiredGates           []FollowUpGate
}

func (d DraftFollowUp) Validate() error {
	if strings.TrimSpace(d.SemanticID) == "" || !d.Kind.Valid() || strings.TrimSpace(d.RequirementID) == "" || strings.TrimSpace(d.RequirementDigest) == "" || strings.TrimSpace(d.EvaluationDigest) == "" || strings.TrimSpace(d.EvidenceDigest) == "" || d.EvaluationPolicyVersion != contractVersion || d.FollowUpPolicyVersion == 0 || strings.TrimSpace(d.FollowUpPolicyDigest) == "" || strings.TrimSpace(d.Owner) == "" || strings.TrimSpace(d.Scope) == "" || strings.TrimSpace(d.Risk) == "" || !safeReason(d.Reason) {
		return fmt.Errorf("%w: malformed draft", ErrInvalidFollowUp)
	}
	if d.Risk != "LOW" && d.Risk != "MEDIUM" && d.Risk != "HIGH" {
		return fmt.Errorf("%w: invalid draft risk", ErrInvalidFollowUp)
	}
	if len(d.RequiredGates) != 2 || d.RequiredGates[0] != GateGovernanceApproval || d.RequiredGates[1] != GateHumanReview {
		return fmt.Errorf("%w: required gates are missing or noncanonical", ErrInvalidFollowUp)
	}
	want, err := draftSemanticID(d)
	if err != nil || want != d.SemanticID {
		return fmt.Errorf("%w: semantic id mismatch", ErrInvalidFollowUp)
	}
	return nil
}

type FollowUpCompilation struct {
	RequirementID     string
	RequirementDigest string
	EvaluationDigest  string
	PolicyDigest      string
	Status            ReadinessStatus
	Drafts            []DraftFollowUp
	Digest            string
}

func (c FollowUpCompilation) Validate() error {
	if strings.TrimSpace(c.RequirementID) == "" || strings.TrimSpace(c.RequirementDigest) == "" || strings.TrimSpace(c.EvaluationDigest) == "" || strings.TrimSpace(c.PolicyDigest) == "" || !c.Status.Valid() || strings.TrimSpace(c.Digest) == "" {
		return fmt.Errorf("%w: malformed compilation", ErrInvalidFollowUp)
	}
	if c.Status == StatusReady && len(c.Drafts) != 0 {
		return fmt.Errorf("%w: READY compilation has drafts", ErrInvalidFollowUp)
	}
	if c.Status != StatusReady && len(c.Drafts) == 0 {
		return fmt.Errorf("%w: non-ready compilation has no drafts", ErrInvalidFollowUp)
	}
	seen := make(map[string]struct{}, len(c.Drafts))
	for _, d := range c.Drafts {
		if err := d.Validate(); err != nil {
			return err
		}
		if d.RequirementID != c.RequirementID || d.RequirementDigest != c.RequirementDigest || d.EvaluationDigest != c.EvaluationDigest || d.FollowUpPolicyDigest != c.PolicyDigest {
			return fmt.Errorf("%w: draft pins differ", ErrInvalidFollowUp)
		}
		if _, ok := seen[d.SemanticID]; ok {
			return fmt.Errorf("%w: duplicate semantic id", ErrInvalidFollowUp)
		}
		seen[d.SemanticID] = struct{}{}
	}
	want, err := compilationDigest(c)
	if err != nil || want != c.Digest {
		return fmt.Errorf("%w: compilation digest mismatch", ErrInvalidFollowUp)
	}
	return nil
}

// CompileFollowUps turns a non-ready evaluation into bounded draft work. It
// never executes, persists, publishes or creates an implicit human gate.
// Repeating the same inputs yields identical semantic IDs and bytes.
func CompileFollowUps(requirement ReadinessRequirement, evaluation Evaluation, policy FollowUpPolicy) (FollowUpCompilation, error) {
	if err := requirement.Validate(); err != nil {
		return FollowUpCompilation{}, err
	}
	if err := evaluation.Validate(); err != nil {
		return FollowUpCompilation{}, err
	}
	if err := policy.Validate(); err != nil {
		return FollowUpCompilation{}, err
	}
	if evaluation.RequirementID != requirement.RequirementID || evaluation.RequirementDigest != requirement.CanonicalDigest {
		return FollowUpCompilation{}, fmt.Errorf("%w: evaluation does not match requirement", ErrInvalidFollowUp)
	}
	if policy.Domain != requirement.Origin.Domain {
		return FollowUpCompilation{}, fmt.Errorf("%w: policy domain exceeds requirement authority", ErrInvalidFollowUp)
	}
	result := FollowUpCompilation{RequirementID: requirement.RequirementID, RequirementDigest: requirement.CanonicalDigest, EvaluationDigest: evaluation.Digest, PolicyDigest: policy.Digest, Status: evaluation.Status}
	if evaluation.Status != StatusReady {
		reasons := append([]string(nil), evaluation.Blockers...)
		reasons = append(reasons, evaluation.Conditions...)
		if evaluation.Status == StatusUnknown && len(reasons) == 0 {
			reasons = []string{"evidence_unknown"}
		}
		sort.Strings(reasons)
		for _, reason := range reasons {
			for _, kind := range policy.Kinds {
				d := DraftFollowUp{Kind: kind, RequirementID: requirement.RequirementID, RequirementDigest: requirement.CanonicalDigest, EvaluationDigest: evaluation.Digest, EvidenceDigest: evaluation.ResolutionDigest, EvaluationPolicyVersion: evaluation.PolicyVersion, FollowUpPolicyVersion: policy.Version, FollowUpPolicyDigest: policy.Digest, Owner: policy.Owner, Scope: policy.Scope, Risk: policy.Risk, Reason: reason, RequiredGates: append([]FollowUpGate(nil), policy.RequiredGates...)}
				id, err := draftSemanticID(d)
				if err != nil {
					return FollowUpCompilation{}, err
				}
				d.SemanticID = id
				result.Drafts = append(result.Drafts, d)
			}
		}
	}
	sort.Slice(result.Drafts, func(i, j int) bool { return result.Drafts[i].SemanticID < result.Drafts[j].SemanticID })
	var err error
	result.Digest, err = compilationDigest(result)
	if err != nil {
		return FollowUpCompilation{}, err
	}
	if err := result.Validate(); err != nil {
		return FollowUpCompilation{}, err
	}
	return result, nil
}

func followUpPolicyDigest(p FollowUpPolicy) (string, error) {
	w := canonicalbytes.New("hcmnext.engines.readiness.FollowUpPolicy", contractVersion).String("domain", p.Domain.String()).Int("version", int64(p.Version)).String("owner", p.Owner).String("scope", p.Scope).String("risk", p.Risk).Count("kinds", len(p.Kinds))
	for _, kind := range p.Kinds {
		w.String("kind", string(kind))
	}
	w.Count("required_gates", len(p.RequiredGates))
	for _, gate := range p.RequiredGates {
		w.String("required_gate", string(gate))
	}
	return w.Digest()
}

func draftSemanticID(d DraftFollowUp) (string, error) {
	w := canonicalbytes.New("hcmnext.engines.readiness.DraftFollowUp", contractVersion).
		String("requirement_id", d.RequirementID).String("requirement_digest", d.RequirementDigest).String("evaluation_digest", d.EvaluationDigest).
		Int("evaluation_policy_version", int64(d.EvaluationPolicyVersion)).Int("follow_up_policy_version", int64(d.FollowUpPolicyVersion)).String("follow_up_policy_digest", d.FollowUpPolicyDigest).String("kind", string(d.Kind)).String("owner", d.Owner).
		String("scope", d.Scope).String("risk", d.Risk).String("reason", d.Reason).String("evidence_digest", d.EvidenceDigest).Count("required_gates", len(d.RequiredGates))
	for _, gate := range d.RequiredGates {
		w.String("required_gate", string(gate))
	}
	return w.Digest()
}

func compilationDigest(c FollowUpCompilation) (string, error) {
	w := canonicalbytes.New("hcmnext.engines.readiness.FollowUpCompilation", contractVersion).
		String("requirement_id", c.RequirementID).String("requirement_digest", c.RequirementDigest).String("evaluation_digest", c.EvaluationDigest).String("policy_digest", c.PolicyDigest).String("status", c.Status.String()).Count("drafts", len(c.Drafts))
	for _, d := range c.Drafts {
		w.String("draft", d.SemanticID)
	}
	return w.Digest()
}
