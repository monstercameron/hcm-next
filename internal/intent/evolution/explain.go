package evolution

import (
	"fmt"
	"strings"
)

// Explain renders a compatibility report as a deterministic, human-readable
// verdict table: one line per violation, then one line per compatible
// change, in the same order the report already carries them in. It is the
// one explanation an operator, an approver or an auditor reads — never a
// second, independently drifting narrative.
func (r Report) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s -> %s: %s\n", r.Previous, r.Current, r.Verdict)
	if len(r.Violations) == 0 && len(r.Changes) == 0 {
		b.WriteString("  no differences\n")
		return b.String()
	}
	for _, v := range r.Violations {
		fmt.Fprintf(&b, "  INCOMPATIBLE [%s] %s: %s\n", v.Code, v.Field, v.Detail)
	}
	for _, c := range r.Changes {
		fmt.Fprintf(&b, "  compatible   [%s] %s: %s\n", c.Code, c.Field, c.Detail)
	}
	return b.String()
}

// Explain renders a supersession record as a human-readable governance
// summary: what replaced what and why, who authored and approved it, when
// it takes effect, what happens to instances already live, and the digest
// that proves this text describes an unmutated record.
func (s SupersessionRecord) Explain() string {
	return fmt.Sprintf(
		"%s superseded by %s (%s)\n"+
			"  reason: %s\n"+
			"  authored by %s, approved by %s\n"+
			"  effective: %s\n"+
			"  live instances: %s\n"+
			"  digest: %s\n",
		s.Previous, s.Current, s.CompatibilityVerdict,
		s.Reason,
		s.AuthorPrincipalID, s.ApproverPrincipalID,
		s.EffectiveAt,
		s.LiveInstancePolicy,
		s.Digest,
	)
}
