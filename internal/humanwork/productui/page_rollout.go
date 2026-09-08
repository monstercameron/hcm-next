package productui

import "fmt"

// RolloutScope is one rollout target: the organization-applicability
// scope receiving the revision and the epoch second it goes live.
// Zero EffectiveFrom means live at publication. Scope strings are
// declared, never validated, here: organization existence stays
// with the organization domain, and presentation must not grow a
// second scope authority.
type RolloutScope struct {
	Scope         string
	EffectiveFrom int64
}

// PageRollout binds one immutable published revision to its staged
// publication: which scopes receive it and when. The version plus
// digest pin the revision; rollout never carries content of its
// own. A newer revision's rollout supersedes per scope once live —
// rollout advances forward only, and moving a scope backward is
// rollback (a later lifecycle step), never a rollout edit.
type PageRollout struct {
	Page    PageID
	Version int64
	Digest  string
	Scopes  []RolloutScope
}

// RolloutVerdict is the rollout answer: compatible plus the stable
// reasons, in validation order, when not. Reasons stay nil on
// success.
type RolloutVerdict struct {
	Compatible bool
	Reasons    []string
}

// ValidatePageRollout checks one rollout structurally: a named
// page, a positive revision version, a pinning digest, at least one
// scope, no blank or duplicate scopes, and no negative effective
// dates. Violations accumulate in fixed order: page, version,
// digest, scope presence, then per-scope reasons in rollout order.
func ValidatePageRollout(rollout PageRollout) RolloutVerdict {
	var reasons []string
	if rollout.Page == "" {
		reasons = append(reasons, "missing rollout page")
	}
	if rollout.Version <= 0 {
		reasons = append(reasons, fmt.Sprintf("rollout revision version must be positive, got %d", rollout.Version))
	}
	if rollout.Digest == "" {
		reasons = append(reasons, "missing rollout digest")
	}
	if len(rollout.Scopes) == 0 {
		reasons = append(reasons, "rollout names no scopes")
	}
	seen := map[string]bool{}
	for _, scope := range rollout.Scopes {
		switch {
		case scope.Scope == "":
			reasons = append(reasons, "blank rollout scope")
		case seen[scope.Scope]:
			reasons = append(reasons, fmt.Sprintf("duplicate rollout scope %q", scope.Scope))
		default:
			seen[scope.Scope] = true
		}
		if scope.EffectiveFrom < 0 {
			reasons = append(reasons, fmt.Sprintf("negative effective date for scope %q", scope.Scope))
		}
	}
	if len(reasons) > 0 {
		return RolloutVerdict{Compatible: false, Reasons: reasons}
	}
	return RolloutVerdict{Compatible: true}
}

// VerifyRolloutTarget binds one rollout to an actually-published
// revision: the page and version must exist in the revision log
// and the digest must match. Unknown or tampered targets refuse.
// Durable rollout state stays with the studio platform; this is
// the presentation-side target check before a rollout publishes.
func VerifyRolloutTarget(log *PageRevisionLog, rollout PageRollout) error {
	recorded, ok := log.Revision(rollout.Page, rollout.Version)
	if !ok {
		return fmt.Errorf("productui: rollout targets unpublished revision for page %q version %d", rollout.Page, rollout.Version)
	}
	if rollout.Digest == "" || rollout.Digest != recorded.Digest {
		return fmt.Errorf("productui: rollout digest mismatch for page %q version %d", rollout.Page, rollout.Version)
	}
	return nil
}

// RolloutLiveAt reports whether one rollout serves its revision to
// a scope at one epoch second: the scope must be listed and its
// effective date reached. The query is mechanical — publish only
// validated, target-verified rollouts.
func RolloutLiveAt(rollout PageRollout, scope string, now int64) bool {
	for _, target := range rollout.Scopes {
		if target.Scope == scope {
			return now >= target.EffectiveFrom
		}
	}
	return false
}
