package legal

import "fmt"

// Explain renders a deterministic, human-readable one-line summary of a
// release: identity, jurisdiction, version, review status, digest, and its
// place in the supersession chain. It exists for an audit log or an
// operator-facing receipt; nothing in [Evaluate] reads it, and it is never
// part of the digest.
func (p RulePack) Explain() string {
	supersedes := "none"
	if p.Supersedes != nil {
		supersedes = fmt.Sprintf("%s v%d.%d", p.Supersedes.PackID, p.Supersedes.Version, p.Supersedes.MinorVersion)
	}
	supersededBy := "none"
	if p.SupersededBy != nil {
		supersededBy = fmt.Sprintf("%s v%d.%d", p.SupersededBy.PackID, p.SupersededBy.Version, p.SupersededBy.MinorVersion)
	}
	return fmt.Sprintf(
		"rulepack id=%s v%d.%d jurisdiction=%s window=%s review_status=%s source_type=%s obligations=%d digest=%s supersedes=%s superseded_by=%s signatures=%d",
		p.PackID, p.Version, p.MinorVersion, p.Jurisdiction, p.Window, p.ReviewStatus, p.SourceType,
		len(p.obligations()), p.Digest, supersedes, supersededBy, len(p.Signatures))
}

// RollbackTarget returns the release this one supersedes, if any: the
// version an operator would pin evaluation back to if this release were
// withdrawn. ok is false for a pack's first release, which has nothing to
// roll back to.
func (p RulePack) RollbackTarget() (target RulePackRelease, ok bool) {
	if p.Supersedes == nil {
		return RulePackRelease{}, false
	}
	return *p.Supersedes, true
}
