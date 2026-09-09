// Package leave contains the kernel-pure Leave channel conformance adapter.
// It exercises the same commercial resolver used by Promotion without
// creating a leave intent, work item, message, or other effect.
package leave

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/commercial"
)

// EntitlementRequest is metadata for one Leave initiation or resume route.
// Route and phase are intentionally not interpreted by the commercial gate.
type EntitlementRequest struct {
	TenantID   string
	Capability string
	At         time.Time
	Channel    commercial.Channel
	Phase      string
}

// EntitlementGate is the Leave adapter over the shared commercial authority.
type EntitlementGate struct {
	gate commercial.Gate
}

// NewEntitlementGate composes the Leave channel with one immutable tenant
// entitlement snapshot.
func NewEntitlementGate(snapshot commercial.EntitlementSnapshot) (EntitlementGate, error) {
	gate, err := commercial.NewGate(snapshot)
	if err != nil {
		return EntitlementGate{}, err
	}
	return EntitlementGate{gate: gate}, nil
}

// Decide resolves the channel-neutral commercial decision for discovery.
func (g EntitlementGate) Decide(request EntitlementRequest) commercial.EntitlementDecision {
	return g.gate.Resolve(commercial.NewEntitlementRequest(request.TenantID, request.Capability, request.At, request.Channel))
}

// Admit returns the pinned decision or the shared typed commercial refusal.
func (g EntitlementGate) Admit(request EntitlementRequest) (commercial.EntitlementDecision, error) {
	return g.gate.Admit(commercial.NewEntitlementRequest(request.TenantID, request.Capability, request.At, request.Channel))
}
