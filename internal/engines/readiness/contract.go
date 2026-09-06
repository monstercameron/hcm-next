// Package readiness owns the small, shared readiness kernel proven by the
// conformance profiles in this package. It describes requirements and
// resolves typed, authorized evidence; it never owns domain eligibility,
// legality, qualification, workflow or business effects.
package readiness

import "fmt"

const contractVersion = 1

// Version reports the readiness engine contract version.
func Version() int { return contractVersion }

// Explain returns a redaction-safe summary of a readiness resolution. Raw
// evidence content and evidence references are deliberately absent.
func Explain(r Resolution) (Explanation, error) { return r.Explain() }

// Explanation is the safe presentation of one requirement resolution.
type Explanation struct {
	RequirementID string
	Status        ResolutionStatus
	Reasons       []string
	EvidenceCount int
	Digest        string
}

// Explain returns a deterministic, redaction-safe summary. It names only the
// requirement and safe reason codes, never evidence payloads or references.
func (r Resolution) Explain() (Explanation, error) {
	if err := r.Validate(); err != nil {
		return Explanation{}, err
	}
	return Explanation{
		RequirementID: r.RequirementID,
		Status:        r.Status,
		Reasons:       append([]string(nil), r.Reasons...),
		EvidenceCount: len(r.Evidence),
		Digest:        r.Digest,
	}, nil
}

// String is useful in logs while retaining the same safe boundary as Explain.
func (e Explanation) String() string {
	return fmt.Sprintf("readiness %s: %s (%d authorized evidence reference(s))", e.RequirementID, e.Status, e.EvidenceCount)
}
