package approval

import (
	"fmt"
	"strings"
)

// Reuse verdicts: the closed retention vocabulary.
const (
	ReuseRetainedByReference = "RETAINED_BY_REFERENCE"
	ReuseReapprovalRequired  = "REAPPROVAL_REQUIRED"
	ReuseUnknown             = "UNKNOWN"
)

// Drift components: the material kinds replanning can change.
const (
	DriftAmount       = "amount"
	DriftInterval     = "interval"
	DriftObligation   = "obligation"
	DriftView         = "view"
	DriftPresentation = "presentation"
)

// SuccessorLink names what changed between the prior proposal and its
// successor. Component names come from the replanning layer; anything
// outside the vocabulary is unknown drift, never silent retention.
type SuccessorLink struct {
	PriorProposalDigest string
	NextProposalDigest  string
	Changed             []string
}

// ReusePolicy is the versioned retention policy. Presentation-only drift
// retains decisions only when the policy says so: without a policy,
// presentation-only change never invalidates every decision by default,
// and never retains by default either — it reports UNKNOWN.
type ReusePolicy struct {
	Version                string
	RetainPresentationOnly bool
}

// ReuseDecision carries one prior decision forward without editing it:
// the original immutable receipt survives inside Decision, and
// ApplicabilityLink binds it explicitly to the successor proposal.
type ReuseDecision struct {
	Decision          ApprovalDecision
	Verdict           string
	ApplicabilityLink string
	PolicyVersion     string
}

func materialDrift(component string) bool {
	switch component {
	case DriftAmount, DriftInterval, DriftObligation, DriftView:
		return true
	default:
		return false
	}
}

func knownDrift(component string) bool {
	return materialDrift(component) || component == DriftPresentation
}

// EvaluateReuse returns each prior decision under the policy. No decision
// is edited or rebound in place: retention keeps the original receipt by
// reference with an explicit applicability link to the successor.
func EvaluateReuse(policy ReusePolicy, prior []ApprovalDecision, link SuccessorLink) ([]ReuseDecision, error) {
	if strings.TrimSpace(policy.Version) == "" {
		return nil, fmt.Errorf("approval: reuse policy version is required")
	}
	if strings.TrimSpace(link.PriorProposalDigest) == "" || strings.TrimSpace(link.NextProposalDigest) == "" {
		return nil, fmt.Errorf("approval: successor link must bind both proposal digests")
	}
	if link.PriorProposalDigest == link.NextProposalDigest {
		return nil, fmt.Errorf("approval: successor link must advance the proposal")
	}
	out := make([]ReuseDecision, 0, len(prior))
	for _, decision := range prior {
		// No drift retains vacuously: the decision still binds exactly
		// what was decided. Presentation-only drift follows the policy:
		// it never mass-invalidates without one, and never retains
		// against one.
		verdict := ReuseRetainedByReference
		if len(link.Changed) > 0 && !policy.RetainPresentationOnly {
			verdict = ReuseReapprovalRequired
		}
		for _, component := range link.Changed {
			if !knownDrift(component) {
				verdict = ReuseUnknown
				break
			}
			if materialDrift(component) {
				verdict = ReuseReapprovalRequired
				break
			}
		}
		if len(link.Changed) == 0 {
			verdict = ReuseRetainedByReference
		}
		out = append(out, ReuseDecision{
			Decision:          decision,
			Verdict:           verdict,
			ApplicabilityLink: link.NextProposalDigest,
			PolicyVersion:     policy.Version,
		})
	}
	return out, nil
}
