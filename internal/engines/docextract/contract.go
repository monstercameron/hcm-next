package docextract

import "fmt"

// Version reports the docextract engine package's own contract version (the
// ARCH-GO-009 engine contract symbol). It only changes when this package's
// exported contract changes incompatibly.
func Version() int { return 1 }

// Explain reports what an extraction did in operator language without
// repeating any extracted text: the artifact it read, the state it ended
// in, how many pages and bytes were examined against the declared limits,
// how many spans carry lineage, and the digest of the result. It is safe to
// place in a refusal, an evidence record or a log.
func (r Result) Explain() string {
	s := fmt.Sprintf("extraction of artifact %s ended %s after %d page(s) and %d byte(s), producing %d lineage-bearing span(s) with digest %s",
		r.ArtifactDigest, r.State, r.PagesExamined, r.BytesExamined, len(r.Spans), r.Digest)
	if r.Error != "" {
		s += " (refusal: " + r.Error + ")"
	}
	return s
}
