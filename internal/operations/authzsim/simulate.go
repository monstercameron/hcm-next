package authzsim

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/internal/viewdigest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// Result is one simulation's outcome: the decision authz.Simulate produced,
// pinned against the policy version the caller expected to be simulating
// under, and a redaction-safe explanation.
type Result struct {
	Decision authz.Decision
	// PolicyVersion is the version the caller stated it was simulating
	// against - an operator-supplied label for comparison and audit, not an
	// input to evaluation: authz.Simulate always evaluates under whichever
	// policy is compiled in, exactly like authz.Enforce does, and this
	// package cannot select a different one.
	PolicyVersion string
	// PolicyVersionMatch is true when PolicyVersion appears in
	// Decision.PolicyVersions. A caller simulating against a policy version
	// the running evaluator does not report is looking at stale evidence -
	// this makes that mismatch explicit rather than silent.
	PolicyVersionMatch bool
	Explanation        string
	Digest             string
}

// Simulate wraps internal/trust/authz.Simulate. policyVersion states which
// policy version the caller believes it is simulating against; it never
// feeds evaluation (see [Result.PolicyVersion]), so passing a different
// value for the same req always produces the identical Decision and
// Explanation - only PolicyVersion and PolicyVersionMatch (and therefore
// Digest) change.
func Simulate(req authz.Request, policyVersion string) (Result, error) {
	dec, err := authz.Simulate(req)
	if err != nil {
		return Result{}, fmt.Errorf("authzsim: simulate: %w", err)
	}

	match := false
	for _, v := range dec.PolicyVersions {
		if v == policyVersion {
			match = true
			break
		}
	}

	result := Result{
		Decision:           dec,
		PolicyVersion:      policyVersion,
		PolicyVersionMatch: match,
		Explanation:        dec.Explain(),
	}
	result.Digest = digestResult(result)
	return result, nil
}

func digestResult(r Result) string {
	return viewdigest.New().
		String("decision.inputs_digest", r.Decision.InputsDigest).
		String("decision.evidence_id", r.Decision.EvidenceID).
		String("policy_version", r.PolicyVersion).
		Bool("policy_version_match", r.PolicyVersionMatch).
		String("explanation", r.Explanation).
		Digest()
}
