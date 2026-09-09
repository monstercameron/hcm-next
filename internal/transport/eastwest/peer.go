package eastwest

import (
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

var (
	ErrPeerNotVerified = errors.New("eastwest: peer has no verified mTLS workload identity")
	ErrPeerExpired     = errors.New("eastwest: peer workload identity is expired")
	ErrPeerCell        = errors.New("eastwest: peer is outside the requested cell")
	ErrPeerWorkload    = errors.New("eastwest: peer workload is not the declared dependency workload")
	ErrPeerMethod      = errors.New("eastwest: method is not authorized for the declared dependency")
	ErrManifestModule  = errors.New("eastwest: service dependency manifest has the wrong module")
	ErrDuplicatePath   = errors.New("eastwest: service dependency manifest contains a duplicate path")
)

const modulePath = "github.com/monstercameron/human-capital-management-suite"

// CompileManifest is the strict publication entry point. It retains the
// existing pure Compile implementation but rejects a manifest that is not
// the repository's signed service-dependency vocabulary.
func CompileManifest(m *Manifest) (*Policy, error) {
	if m == nil || m.Module != modulePath || m.Version < 1 {
		return nil, ErrManifestModule
	}
	seen := make(map[string]struct{}, len(m.Dependencies))
	for _, dependency := range m.Dependencies {
		key := dependency.Consumer + "\x00" + dependency.Dependency
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("%w: %s->%s", ErrDuplicatePath, dependency.Consumer, dependency.Dependency)
		}
		seen[key] = struct{}{}
	}
	return Compile(m)
}

// AllowsPeer is the mTLS-aware and service-AuthZ-aware east-west decision.
// Network reachability, a role name, or a stale dependency record alone never
// authorizes a path.
func (p *Policy) AllowsPeer(peer workload.MTLSIdentity, target, method, cell string, now time.Time) error {
	if !peer.Verified() {
		return ErrPeerNotVerified
	}
	if !peer.ValidAt(now) {
		return ErrPeerExpired
	}
	if cell == "" || peer.Cell() != cell {
		return ErrPeerCell
	}
	entry, ok := p.entries[peer.Service()+"->"+target]
	if !ok {
		return ErrPeerWorkload
	}
	if entry.WorkloadID != peer.Service() {
		return ErrPeerWorkload
	}
	if entry.CellScope != "cell-local" && entry.CellScope != "global" {
		return ErrPeerCell
	}
	if !authorizedMethod(peer.Service(), target, method) {
		return fmt.Errorf("%w: %s target=%s", ErrPeerMethod, method, target)
	}
	verified, err := time.Parse(time.RFC3339, entry.VerifiedAt)
	if err != nil || now.Before(verified) {
		return fmt.Errorf("%w: endpoint not active", ErrPeerExpired)
	}
	expires, err := time.Parse(time.RFC3339, entry.ExpiresAt)
	if err != nil || !now.Before(expires) {
		return fmt.Errorf("%w: endpoint expired", ErrPeerExpired)
	}
	return nil
}

func authorizedMethod(service, target, method string) bool {
	if method != "READ" && method != "WRITE" {
		return false
	}
	switch target {
	case "postgres-store":
		return method == "READ" || service == "worker" || service == "migrate"
	case "connector-observation", "external-provider":
		return service == "worker" || service == "connector-observation"
	default:
		return false
	}
}

// Explain is a bounded, deterministic policy description.
func (p *Policy) Explain() string {
	if p == nil {
		return "east-west policy unavailable"
	}
	return fmt.Sprintf("east-west policy v1 entries=%d digest=%s deny-by-default mTLS+service-authz", len(p.entries), p.digest)
}
