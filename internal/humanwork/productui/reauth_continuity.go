// Package productui governs re-authentication recovery
// continuity. When a session interruption signs the user
// out, the warning's re-authentication link carries the
// current address as its resume target so signing in
// again restores the user's place. This policy decides
// which destinations accept a target: relative workspace
// addresses take it — existing queries survive and a
// stale target is replaced — while absolute URLs, foreign
// hosts, non-workspace paths, and unparsable bases pass
// through untouched instead of gaining a destination the
// shell never projected. The adapter supplies the current
// address from its own primitive; this policy never
// records where the user was.
package productui

import (
	"net/url"
	"strings"
)

// ResolveResumeHref carries a resume target on a
// re-authentication destination. Only relative workspace
// addresses accept the target.
func ResolveResumeHref(base, resume string) string {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/workspace/") {
		return base
	}
	query := parsed.Query()
	query.Set("resume", resume)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
