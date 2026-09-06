package application

// Role selection.
//
// The role set is a function returning a slice, not a package-level map a
// package init appends to. That is the whole point of ARCH-GO-020's RED
// clause about global mutable registries: with a registry, "which roles does
// this binary have" depends on which packages happened to be linked in, and
// nobody can answer it by reading one file. With a function, the answer is
// this file.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
)

// Role names one composable process role.
type Role string

// RoleServe is the P1A API cell: the canonical gRPC surface, the HTTP edge
// and, unless configured off, the human-facing workspace.
const RoleServe Role = "serve"

// Roles is the closed set of roles this application root can compose.
func Roles() []Role { return []Role{RoleServe} }

// String renders the role name.
func (r Role) String() string { return string(r) }

// Validate reports a role this root cannot compose.
func (r Role) Validate() error {
	for _, known := range Roles() {
		if r == known {
			return nil
		}
	}
	return fmt.Errorf("application: unknown role %q; known roles are %s", string(r), RoleNames())
}

// RoleNames renders the known role set for an error message or a usage line.
func RoleNames() string {
	names := make([]string, 0, len(Roles()))
	for _, role := range Roles() {
		names = append(names, string(role))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// SpecFor selects a role and returns the bootstrap Spec that runs it. A
// command's whole composition responsibility is this call plus handing the
// result to bootstrap.Run.
func SpecFor(role Role, args []string, opts ...Option) (bootstrap.Spec, error) {
	if err := role.Validate(); err != nil {
		return bootstrap.Spec{}, err
	}
	switch role {
	case RoleServe:
		return ServeSpec(args, opts...), nil
	default:
		// Unreachable while Validate and this switch agree; it is here so
		// that adding a Role without adding its arm is a startup failure
		// rather than a silently empty process.
		return bootstrap.Spec{}, fmt.Errorf("application: role %q has no composition", string(role))
	}
}
