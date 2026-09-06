package app

import (
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/transport"
	"github.com/monstercameron/hcm-next/internal/trust"
)

// deriveOrigin builds the trusted origin record for one invocation.
//
// Every field comes from the verified principal or from the invocation the
// admission layer resolved; nothing comes from the request body. That is the
// whole point of the type: the wire CreateIntentRequest has no origin field at
// all, so a caller cannot select one, and this function is the only place an
// origin is minted on the serving path.
//
// The trigger reference and correlation id are the caller's causal bookkeeping,
// and this cell takes them from the invocation's own request id rather than
// from the body, because a caller-supplied correlation id would let two
// unrelated acts be filed as one.
func deriveOrigin(principal *trust.Principal, inv *transport.Invocation) (intent.Origin, error) {
	kind := originKindOf(principal)
	trusted := intent.TrustedOriginContext{
		Kind:                kind,
		Tenant:              principal.Tenant(),
		OrganizationScopeID: principal.OrganizationScopeID(),
		Initiator: intent.PrincipalReference{
			PrincipalID:          principal.Subject(),
			Kind:                 initiatorKindOf(principal.SubjectKind()),
			IdentityAssuranceRef: principal.EvidenceID(),
		},
		DelegationRefs:       principal.DelegationRefs(),
		SessionRef:           principal.SessionRef(),
		Assurance:            assuranceOf(principal.Assurance()),
		Channel:              channelOf(inv),
		TrustedContextDigest: principal.Fingerprint(),
	}
	if !kind.Interactive() {
		// A non-interactive origin must name an authenticated producer and the
		// evidence for the fact that triggered it. On this serving path both
		// are the credential's own evidence: the workload authenticated, and
		// the invocation is the record of what it asked for.
		trusted.ProducerRef = principal.Subject()
		trusted.SourceEvidenceRef = principal.EvidenceID()
	}

	claim := intent.OriginClaim{
		TriggerRef:    triggerRefOf(inv, principal),
		CorrelationID: correlationRefOf(inv, principal),
	}
	return intent.NewOrigin(claim, trusted)
}

// originKindOf classifies the verified principal. A delegated human is told
// apart from a plain human by the delegation references the authority service
// issued, never by anything the caller said.
func originKindOf(p *trust.Principal) intent.OriginKind {
	switch p.SubjectKind() {
	case trust.SubjectKindHuman:
		if len(p.DelegationRefs()) > 0 {
			return intent.OriginDelegatedHuman
		}
		return intent.OriginHuman
	case trust.SubjectKindAgent:
		return intent.OriginAgent
	case trust.SubjectKindService, trust.SubjectKindIntegration:
		// A connector calling this service synchronously is one of our own
		// workload identities acting under the entitlements we granted it.
		// EXTERNAL_EVENT is minted by the event-ingestion path, which is the
		// only place that has an authenticated foreign producer to record; a
		// request arriving here has none, and claiming one would be the exact
		// provider-authority inheritance INTENT-012 refuses.
		return intent.OriginWorkload
	default:
		return intent.OriginUnspecified
	}
}

// initiatorKindOf maps the authenticated actor kind onto the kernel's
// initiator kind. It mirrors internal/transport's own mapping; the two are
// separate because that one produces a wire enum and this one a kernel value,
// and neither package may import the other's.
func initiatorKindOf(k trust.SubjectKind) intent.Initiator {
	switch k {
	case trust.SubjectKindHuman:
		return intent.InitiatorHuman
	case trust.SubjectKindService:
		return intent.InitiatorService
	case trust.SubjectKindAgent:
		return intent.InitiatorAgent
	case trust.SubjectKindIntegration:
		return intent.InitiatorIntegration
	default:
		return intent.InitiatorUnspecified
	}
}

// assuranceOf maps the session assurance onto the origin's assurance. The two
// enumerations are ordered identically; the mapping is explicit rather than a
// cast so that a change to either is a compile-time conversation.
func assuranceOf(a trust.Assurance) intent.OriginAssurance {
	switch a {
	case trust.AssuranceLow:
		return intent.AssuranceLow
	case trust.AssuranceSubstantial:
		return intent.AssuranceSubstantial
	case trust.AssuranceHigh:
		return intent.AssuranceHigh
	default:
		return intent.AssuranceUnspecified
	}
}

// channelOf normalizes the transport surface onto the origin channel. An
// unknown or absent invocation resolves to API: this service is only reachable
// through its two transports, and both are the same API surface.
func channelOf(inv *transport.Invocation) intent.Channel {
	if inv == nil {
		return intent.ChannelAPI
	}
	channel, err := intent.ParseChannel(string(inv.Kind()))
	if err != nil {
		return intent.ChannelAPI
	}
	return channel
}

// triggerRefOf names what triggered the intent: the transport method when
// there is an invocation, and the credential's evidence otherwise.
func triggerRefOf(inv *transport.Invocation, p *trust.Principal) string {
	if inv != nil && inv.Method() != "" {
		return inv.Method()
	}
	return p.EvidenceID()
}

// correlationRefOf returns the correlation id the origin records. It is the
// invocation's request id, which is the same value the instance envelope
// carries, so an origin and its intent are joinable without a second key.
func correlationRefOf(inv *transport.Invocation, p *trust.Principal) string {
	if inv != nil && inv.RequestID() != "" {
		return inv.RequestID()
	}
	return p.EvidenceID()
}
