package app

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/commercial"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

const (
	// ReasonPromotionEntitlementRequired is the stable refusal reference shared
	// by every Promotion initiation and resume adapter.
	ReasonPromotionEntitlementRequired = "promotion.commercial_entitlement_required"
	// PromotionEntitlementRuleRef identifies the one app-layer admission rule.
	PromotionEntitlementRuleRef = "commercial.entitlement.snapshot/v1"
)

// PromotionEntitlementRequest carries only request metadata. Phase and
// channel are recorded by the caller for evidence, but never select a second
// commercial policy.
type PromotionEntitlementRequest struct {
	TenantID   string
	Capability string
	At         time.Time
	Channel    commercial.Channel
	Phase      string
}

// PromotionEntitlementBinding is the immutable decision pinned to an accepted
// Promotion execution. Its fingerprint must travel with the execution rather
// than being re-resolved after an amendment.
type PromotionEntitlementBinding struct {
	Decision commercial.EntitlementDecision
}

func (b PromotionEntitlementBinding) Allowed() bool { return b.Decision.Allowed() }

func (b PromotionEntitlementBinding) Fingerprint() string { return b.Decision.Fingerprint }

// PromotionEntitlementGate is the Promotion adapter over the shared
// channel-neutral commercial resolver.
type PromotionEntitlementGate struct {
	gate commercial.Gate
}

// NewPromotionEntitlementGate validates and composes a tenant snapshot for
// Promotion admission.
func NewPromotionEntitlementGate(snapshot commercial.EntitlementSnapshot) (PromotionEntitlementGate, error) {
	gate, err := commercial.NewGate(snapshot)
	if err != nil {
		return PromotionEntitlementGate{}, err
	}
	return PromotionEntitlementGate{gate: gate}, nil
}

// NewPromotionEntitlementGateFromResolver composes Promotion over the
// durable-store resolver without adding a second entitlement policy.
func NewPromotionEntitlementGateFromResolver(resolver commercial.EntitlementResolver) (PromotionEntitlementGate, error) {
	gate, err := commercial.NewResolverGate(resolver)
	if err != nil {
		return PromotionEntitlementGate{}, err
	}
	return PromotionEntitlementGate{gate: gate}, nil
}

// Decide resolves the request without projecting a transport error. This is
// useful for discovery, where the caller needs a safe typed disposition before
// submit or resume.
func (g PromotionEntitlementGate) Decide(request PromotionEntitlementRequest) commercial.EntitlementDecision {
	return g.gate.Resolve(commercial.NewEntitlementRequest(request.TenantID, request.Capability, request.At, request.Channel))
}

// Admit returns a pinned binding or the same typed refusal every Promotion
// transport uses. The refusal carries the snapshot fingerprint as evidence,
// while its message never discloses contract details.
func (g PromotionEntitlementGate) Admit(request PromotionEntitlementRequest) (PromotionEntitlementBinding, error) {
	decision, err := g.gate.Admit(commercial.NewEntitlementRequest(request.TenantID, request.Capability, request.At, request.Channel))
	binding := PromotionEntitlementBinding{Decision: decision}
	if err != nil {
		return binding, PromotionEntitlementError(decision)
	}
	return binding, nil
}

// PromotionEntitlementError projects a channel-neutral denial into the
// existing safe transport envelope. The fingerprint is evidence metadata, not
// a contract payload.
func PromotionEntitlementError(decision commercial.EntitlementDecision) *envelope.Error {
	return envelope.New(envelope.CodeFailedPrecondition, ReasonPromotionEntitlementRequired,
		"the operation is not available under the tenant's commercial entitlement").
		WithViolation("capability", "the requested operation is not entitled for this tenant", PromotionEntitlementRuleRef).
		WithEvidence(envelope.Evidence{Kind: commercial.EntitlementEvidenceKind, Digest: decision.Fingerprint})
}
