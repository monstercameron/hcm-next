package bootstrap

import "fmt"

// Role names one process role from the SVC-001 process-role vocabulary
// (definitions/architecture/process-roles.yaml). It never encodes business
// meaning; it only lets bootstrap tag every lifecycle event, health check
// and log line with which process is running.
//
// The constants below mirror the manifest's `processes[].command` values
// exactly. They are declared here (rather than parsed from the YAML at
// runtime) so a compiled binary never depends on a repository-relative file
// path to know its own role; TestTodo_SVC_002_Integration cross-checks this
// list against the manifest so the two cannot silently drift.
type Role string

const (
	// RoleHCMNext is the primary API/service process (status: initial).
	RoleHCMNext Role = "hcmnext"
	// RoleWorker is the durable/background execution process (status: initial).
	RoleWorker Role = "worker"
	// RoleProjector is the rebuildable-projection process (status: initial).
	RoleProjector Role = "projector"
	// RoleMigrate is the isolated migration command (status: initial).
	RoleMigrate Role = "migrate"
	// RoleFrontendDev is the development-only product-UI server
	// (status: initial). It is in the vocabulary because the manifest
	// declares it and this list mirrors the manifest exactly; that it never
	// fronts a tenant in production is the manifest row's business, not
	// bootstrap's.
	RoleFrontendDev Role = "frontenddev"
	// RoleScheduler is reserved for P1B durable timers (status: later).
	RoleScheduler Role = "scheduler"
	RoleAdmin     Role = "hcmctl"
)

// roleVocabulary is every Role bootstrap recognizes, in manifest order.
var roleVocabulary = []Role{RoleHCMNext, RoleWorker, RoleProjector, RoleMigrate, RoleFrontendDev, RoleScheduler, RoleAdmin}

// ErrUnknownRole is returned by Role.Validate when a Spec names a role
// outside the process-roles.yaml vocabulary.
type ErrUnknownRole struct {
	Role Role
}

func (e *ErrUnknownRole) Error() string {
	return fmt.Sprintf("bootstrap: role %q is not in the process-roles.yaml vocabulary %v", string(e.Role), roleVocabulary)
}

// Validate reports whether r is a recognized process role.
func (r Role) Validate() error {
	for _, known := range roleVocabulary {
		if r == known {
			return nil
		}
	}
	return &ErrUnknownRole{Role: r}
}

// String satisfies fmt.Stringer.
func (r Role) String() string { return string(r) }
