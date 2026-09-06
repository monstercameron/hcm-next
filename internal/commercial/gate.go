package commercial

import (
	"errors"
	"time"
)

const (
	// PromotionEntitlementCapability is the commercial capability reference for
	// the Promotion channel.
	PromotionEntitlementCapability = "hcmnext.people.promote_worker/v1"
	// LeaveEntitlementCapability is the commercial capability reference for the
	// Leave channel.
	LeaveEntitlementCapability = "hcmnext.workforce.leave_request/v1"
	// EntitlementEvidenceKind identifies the digest carried on a refusal or
	// accepted execution binding.
	EntitlementEvidenceKind = "commercial.entitlement.snapshot"
)

var ErrEntitlementDenied = errors.New("commercial: entitlement denied")

// Gate is the channel-neutral commercial admission boundary. It validates
// the snapshot once at composition and then resolves every request against
// that exact immutable revision.
type Gate struct {
	resolver EntitlementResolver
}

// NewGate composes an entitlement gate over one validated immutable snapshot.
func NewGate(snapshot EntitlementSnapshot) (Gate, error) {
	if err := snapshot.Validate(); err != nil {
		return Gate{}, err
	}
	return Gate{resolver: snapshot}, nil
}

// NewResolverGate composes an adapter over a resolver supplied by a durable
// tenant store. The resolver remains the sole authority for the answer.
func NewResolverGate(resolver EntitlementResolver) (Gate, error) {
	if resolver == nil {
		return Gate{}, ErrInvalidEntitlementSnapshot
	}
	return Gate{resolver: resolver}, nil
}

// Resolve returns the channel-neutral decision. Channel and phase metadata
// are deliberately not used to change the answer.
func (g Gate) Resolve(request EntitlementRequest) EntitlementDecision {
	if g.resolver == nil {
		return EntitlementDecision{Code: CodeCapabilityOutOfScope}
	}
	return g.resolver.Resolve(request)
}

// Admit returns the decision and a typed, non-disclosing refusal when the
// requested operation is not entitled. The decision is returned on refusal so
// every adapter can retain the exact fingerprint for evidence.
func (g Gate) Admit(request EntitlementRequest) (EntitlementDecision, error) {
	decision := g.Resolve(request)
	if decision.Allowed() {
		return decision, nil
	}
	return decision, &EntitlementRefusal{decision: decision}
}

// EntitlementRefusal carries the typed decision without putting tenant,
// capability or contract details into its error string.
type EntitlementRefusal struct {
	decision EntitlementDecision
}

func (r *EntitlementRefusal) Error() string { return ErrEntitlementDenied.Error() }

func (r *EntitlementRefusal) Is(target error) bool { return target == ErrEntitlementDenied }

// Decision returns the immutable decision that caused the refusal.
func (r *EntitlementRefusal) Decision() EntitlementDecision { return r.decision }

// NewEntitlementRequest is a convenience constructor for channel adapters. Phase is not
// part of the commercial decision, which prevents resume or assisted routes
// from becoming an alternate entitlement policy.
func NewEntitlementRequest(tenantID, capability string, at time.Time, channel Channel) EntitlementRequest {
	return EntitlementRequest{TenantID: tenantID, Capability: capability, At: at, Channel: channel}
}
