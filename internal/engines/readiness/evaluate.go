package readiness

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var ErrInvalidEvaluation = errors.New("readiness: invalid evaluation")

// Evaluation is the aggregate, pure readiness decision for one requirement.
// It contains only typed resolution outcomes and safe reason codes; evidence
// references are intentionally not copied into this result.
type Evaluation struct {
	RequirementID     string
	RequirementRev    uint64
	RequirementDigest string
	ResolutionDigest  string
	PolicyVersion     int
	Status            ReadinessStatus
	Blockers          []string
	Conditions        []string
	Validity          values.EffectiveInterval
	AsOf              values.Instant
	KnownAt           values.KnownAt
	ExplanationDigest string
	Digest            string
}

func (e Evaluation) Validate() error {
	if strings.TrimSpace(e.RequirementID) == "" || e.RequirementRev == 0 || strings.TrimSpace(e.RequirementDigest) == "" || strings.TrimSpace(e.ResolutionDigest) == "" || e.PolicyVersion != contractVersion || !e.Status.Valid() || e.Validity.Validate() != nil || e.Validity.Kind() != values.IntervalKindInstant || e.AsOf.Validate() != nil || e.KnownAt.Canonical() == nil || strings.TrimSpace(e.ExplanationDigest) == "" || strings.TrimSpace(e.Digest) == "" {
		return fmt.Errorf("%w: malformed result", ErrInvalidEvaluation)
	}
	if e.Status == StatusReady && (len(e.Blockers) != 0 || len(e.Conditions) != 0) {
		return fmt.Errorf("%w: READY result has blockers or conditions", ErrInvalidEvaluation)
	}
	if e.Status == StatusConditional && len(e.Conditions) == 0 {
		return fmt.Errorf("%w: CONDITIONAL result has no conditions", ErrInvalidEvaluation)
	}
	if e.Status == StatusNotReady && len(e.Blockers) == 0 {
		return fmt.Errorf("%w: NOT_READY result has no blockers", ErrInvalidEvaluation)
	}
	if e.Status == StatusUnknown && len(e.Blockers) == 0 && len(e.Conditions) == 0 {
		return fmt.Errorf("%w: UNKNOWN result has no safe reason", ErrInvalidEvaluation)
	}
	wantExplanation, err := explanationDigest(e)
	if err != nil || e.ExplanationDigest != wantExplanation {
		return fmt.Errorf("%w: explanation digest mismatch", ErrInvalidEvaluation)
	}
	wantDigest, err := evaluationDigest(e)
	if err != nil || e.Digest != wantDigest {
		return fmt.Errorf("%w: evaluation digest mismatch", ErrInvalidEvaluation)
	}
	return nil
}

// Evaluate folds a previously resolved requirement into the common readiness
// status vocabulary. It performs no I/O or business effects. Unknown is never
// promoted to READY, and conditional evidence is never silently accepted.
func Evaluate(requirement ReadinessRequirement, resolution Resolution) (Evaluation, error) {
	if err := requirement.Validate(); err != nil {
		return Evaluation{}, err
	}
	if err := resolution.Validate(); err != nil {
		return Evaluation{}, err
	}
	if resolution.RequirementID != requirement.RequirementID || resolution.Revision != requirement.Revision {
		return Evaluation{}, fmt.Errorf("%w: resolution does not match requirement", ErrInvalidEvaluation)
	}
	inside, err := requirement.Effective.ContainsInstant(resolution.AsOf)
	if err != nil || !inside {
		return Evaluation{}, fmt.Errorf("%w: resolution as-of is outside requirement validity", ErrInvalidEvaluation)
	}
	e := Evaluation{
		RequirementID: requirement.RequirementID, RequirementRev: requirement.Revision,
		RequirementDigest: requirement.CanonicalDigest, ResolutionDigest: resolution.Digest,
		PolicyVersion: contractVersion, Status: StatusUnknown, Validity: requirement.Effective,
		AsOf: resolution.AsOf, KnownAt: resolution.KnownAt,
	}
	if requirement.CanonicalDigest == "" {
		return Evaluation{}, fmt.Errorf("%w: requirement version is not pinned", ErrInvalidEvaluation)
	}
	switch resolution.Status {
	case ResolutionSatisfied:
		e.Status = StatusReady
	case ResolutionConditional:
		e.Status = StatusConditional
		e.Conditions = append(e.Conditions, "evidence_conditional")
	case ResolutionUnsatisfied:
		if requirement.Policy == PolicyConditional {
			e.Status = StatusConditional
			e.Conditions = append(e.Conditions, "requirement_not_satisfied")
		} else {
			e.Status = StatusNotReady
			e.Blockers = append(e.Blockers, "requirement_not_satisfied")
		}
	case ResolutionUnknown:
		e.Status = StatusUnknown
	}
	for _, reason := range resolution.Reasons {
		if strings.TrimSpace(reason) == "" {
			continue
		}
		if e.Status == StatusConditional {
			e.Conditions = append(e.Conditions, reason)
		} else if e.Status == StatusNotReady || e.Status == StatusUnknown {
			e.Blockers = append(e.Blockers, reason)
		}
	}
	e.Blockers, err = safeReasons(e.Blockers)
	if err != nil {
		return Evaluation{}, err
	}
	e.Conditions, err = safeReasons(e.Conditions)
	if err != nil {
		return Evaluation{}, err
	}
	e.ExplanationDigest, err = explanationDigest(e)
	if err != nil {
		return Evaluation{}, err
	}
	e.Digest, err = evaluationDigest(e)
	if err != nil {
		return Evaluation{}, err
	}
	if err := e.Validate(); err != nil {
		return Evaluation{}, err
	}
	return e, nil
}

func safeReason(reason string) bool {
	switch reason {
	case "evidence_conditional", "evidence_not_authorized", "evidence_not_pinned", "evidence_not_satisfied", "evidence_not_trusted", "evidence_stale", "requirement_not_satisfied":
		return true
	default:
		return false
	}
}

func safeReasons(reasons []string) ([]string, error) {
	set := make(map[string]struct{}, len(reasons))
	for _, reason := range reasons {
		if !safeReason(reason) {
			return nil, fmt.Errorf("%w: unsafe explanation reason", ErrInvalidEvaluation)
		}
		set[reason] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for reason := range set {
		result = append(result, reason)
	}
	sort.Strings(result)
	return result, nil
}

func explanationDigest(e Evaluation) (string, error) {
	w := canonicalbytes.New("hcmnext.engines.readiness.Explanation", contractVersion).
		String("requirement_id", e.RequirementID).String("status", e.Status.String()).
		Count("blockers", len(e.Blockers)).Count("conditions", len(e.Conditions))
	for _, v := range e.Blockers {
		w.String("blocker", v)
	}
	for _, v := range e.Conditions {
		w.String("condition", v)
	}
	return w.Digest()
}

func evaluationDigest(e Evaluation) (string, error) {
	w := canonicalbytes.New("hcmnext.engines.readiness.Evaluation", contractVersion).
		String("requirement_digest", e.RequirementDigest).String("resolution_digest", e.ResolutionDigest).Int("requirement_revision", int64(e.RequirementRev)).
		Int("policy_version", int64(e.PolicyVersion)).String("status", e.Status.String()).
		Value("validity", e.Validity).Value("as_of", e.AsOf).Value("known_at", e.KnownAt).
		String("explanation_digest", e.ExplanationDigest)
	for _, v := range e.Blockers {
		w.String("blocker", v)
	}
	for _, v := range e.Conditions {
		w.String("condition", v)
	}
	return w.Digest()
}
