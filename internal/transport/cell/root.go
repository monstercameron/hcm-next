package cell

import "net/http"

// RootPattern is the one non-RPC route the edge answers at the origin root.
// It is method- and path-exact (Go's "{$}" wildcard matches "/" and nothing
// under it), so it never shadows a Connect procedure, every one of which is a
// POST to its own path.
const RootPattern = "GET /{$}"

// rootRedirect sends a browser that opened the bare origin to target. It is a
// 303 rather than a permanent redirect on purpose: the destination is a
// composition decision (which workspace this process serves), not a property
// of the origin a browser should cache across deployments.
func rootRedirect(target string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusSeeOther)
	})
}
