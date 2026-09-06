package docredact

import "fmt"

// Version reports this engine's incompatible contract version.
func Version() int { return 1 }

// Explain returns a stable, value-free description of the engine contract.
func Explain() string {
	return fmt.Sprintf("docredact v%d: audience-bound MASK, REMOVE and keyed TOKENIZE derivatives with source-span lineage and canonical digests", Version())
}

// Explain describes one generated derivative without repeating source text.
func (r Result) Explain() string {
	return fmt.Sprintf("redacted derivative artifact=%s policy=%s version=%s audience=%s redactions=%d digest=%s", r.ArtifactDigest, r.PolicyID, r.PolicyVersion, r.Audience, len(r.Redactions), r.Digest)
}
