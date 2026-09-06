package snapshot

import (
	"fmt"
	"strings"
)

// Version reports the snapshot engine package's own contract version (the
// ARCH-GO-009 engine contract symbol). It is distinct from any reference or
// configuration version an InputEntry carries: those describe governed
// inputs the engine resolved; this describes the shape of the engine's
// exported contract and only changes when that contract changes
// incompatibly.
func Version() int { return 1 }

// Explain reports, in reading order, what a resolved snapshot is made of:
// the tenant and known-at horizon it was resolved under, then one line per
// entry naming the input, its owner, its authority class and the watermark
// it was read at. It carries no input values, so it is safe to place in a
// refusal, an evidence record or a log.
func (s ReadSnapshot) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "snapshot %s for tenant %s at known-at horizon %v with %d input(s)",
		s.Digest, s.Tenant, s.KnownAtHorizon, len(s.Entries))
	for _, e := range s.Entries {
		fmt.Fprintf(&b, "\n- %s (owner %s, authority %s, watermark %s)",
			e.Name, e.Owner, e.Authority, e.Watermark)
	}
	return b.String()
}
