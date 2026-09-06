package messagetemplate

import "fmt"

// Version reports the message-template engine package's own contract version
// (the ARCH-GO-009 engine contract symbol). It is distinct from a Template's
// business version: that is governed data the engine renders; this is the
// shape of the engine contract itself and only changes when this package's
// exported contract changes incompatibly.
func Version() int { return 1 }

// Explain reports what a rendering is bound to (template key and version,
// purpose, channel, locale, classification) and the digest of its output,
// without repeating the rendered subject or body, so it is safe to place in
// a refusal, an evidence record or a log.
func (r Rendered) Explain() string {
	return fmt.Sprintf("rendered template %s@%d for purpose %s over channel %s (locale %s, classification %s) with digest %s",
		r.Key, r.Version, r.Purpose, r.Channel, r.Locale, r.Classification, r.Digest)
}
