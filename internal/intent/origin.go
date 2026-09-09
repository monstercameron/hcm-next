package intent

import (
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// OriginKind is the trusted source an intent came from. It is derived from the
// verified credential and the trusted boundary, never from the request body.
//
// Nine kinds are distinguished because each one answers a different question
// during an investigation: who typed this, who delegated it, which agent acted,
// which of our workloads acted, which schedule fired, which of our own events
// fired, which foreign system's event arrived, which repair ran, and which
// operator reached in. Collapsing any pair of them loses an answer.
type OriginKind uint8

// OriginKind values.
const (
	OriginUnspecified OriginKind = iota
	// OriginHuman is a person acting for themselves.
	OriginHuman
	// OriginDelegatedHuman is a person acting for another person under a
	// recorded delegation chain.
	OriginDelegatedHuman
	// OriginAgent is a model-driven agent acting on a person's behalf. An
	// agent never carries more authority than the principal it acts for.
	OriginAgent
	// OriginWorkload is one of this platform's own service identities.
	OriginWorkload
	// OriginSchedule is a scheduler firing a due obligation.
	OriginSchedule
	// OriginInternalEvent is one of this platform's own domain events.
	OriginInternalEvent
	// OriginExternalEvent is a foreign system's event arriving over a
	// connector. Its provider is a producer, never an authority.
	OriginExternalEvent
	// OriginRepair is a governed repair or reconciliation run.
	OriginRepair
	// OriginOperator is a named operator acting through a support path.
	OriginOperator
)

var originKindNames = map[OriginKind]string{
	OriginUnspecified:    "UNSPECIFIED",
	OriginHuman:          "HUMAN",
	OriginDelegatedHuman: "DELEGATED_HUMAN",
	OriginAgent:          "AGENT",
	OriginWorkload:       "WORKLOAD",
	OriginSchedule:       "SCHEDULE",
	OriginInternalEvent:  "INTERNAL_EVENT",
	OriginExternalEvent:  "EXTERNAL_EVENT",
	OriginRepair:         "REPAIR",
	OriginOperator:       "OPERATOR",
}

func (k OriginKind) String() string { return enumName(originKindNames, k, "OriginKind") }

// Valid reports whether k is a declared origin kind other than UNSPECIFIED.
func (k OriginKind) Valid() bool {
	_, ok := originKindNames[k]
	return ok && k != OriginUnspecified
}

// OriginKinds returns the nine trusted origin kinds in declaration order.
func OriginKinds() []OriginKind {
	return []OriginKind{
		OriginHuman, OriginDelegatedHuman, OriginAgent, OriginWorkload,
		OriginSchedule, OriginInternalEvent, OriginExternalEvent,
		OriginRepair, OriginOperator,
	}
}

// Interactive reports whether the kind is a live caller at a surface, as
// opposed to a non-interactive producer (a schedule, an event, a repair run or
// an operator path) that must additionally name its producer and its evidence.
func (k OriginKind) Interactive() bool {
	switch k {
	case OriginHuman, OriginDelegatedHuman, OriginAgent:
		return true
	default:
		return false
	}
}

// allowedInitiators is the closed set of verified initiator kinds each origin
// kind may be built from. It is the rule that stops an agent presenting itself
// as a human: the initiator kind comes from the verified credential, and an
// origin kind that disagrees with it is refused at the trusted boundary rather
// than recorded and believed.
var allowedInitiators = map[OriginKind][]Initiator{
	OriginHuman:          {InitiatorHuman},
	OriginDelegatedHuman: {InitiatorHuman},
	OriginAgent:          {InitiatorAgent},
	OriginWorkload:       {InitiatorService, InitiatorIntegration},
	OriginSchedule:       {InitiatorSchedule},
	OriginInternalEvent:  {InitiatorSystemEvent, InitiatorRule},
	OriginExternalEvent:  {InitiatorIntegration},
	OriginRepair:         {InitiatorService, InitiatorHuman},
	OriginOperator:       {InitiatorHuman, InitiatorService},
}

// AllowsInitiator reports whether an origin kind may be built from a verified
// initiator kind.
func (k OriginKind) AllowsInitiator(i Initiator) bool {
	return slices.Contains(allowedInitiators[k], i)
}

// Channel is the surface an intent was raised at. UI, API, CLI, mobile, kiosk
// and partner surfaces normalize into this one enumeration so that every one of
// them produces the same origin contract and none of them can invent a field.
type Channel uint8

// Channel values.
const (
	ChannelUnspecified Channel = iota
	ChannelUI
	ChannelAPI
	ChannelCLI
	ChannelMobile
	ChannelKiosk
	ChannelPartner
	ChannelScheduler
	ChannelEventBus
	ChannelOperatorConsole
)

var channelNames = map[Channel]string{
	ChannelUnspecified:     "UNSPECIFIED",
	ChannelUI:              "UI",
	ChannelAPI:             "API",
	ChannelCLI:             "CLI",
	ChannelMobile:          "MOBILE",
	ChannelKiosk:           "KIOSK",
	ChannelPartner:         "PARTNER",
	ChannelScheduler:       "SCHEDULER",
	ChannelEventBus:        "EVENT_BUS",
	ChannelOperatorConsole: "OPERATOR_CONSOLE",
}

func (c Channel) String() string { return enumName(channelNames, c, "Channel") }

// Valid reports whether c is a declared channel other than UNSPECIFIED.
func (c Channel) Valid() bool { _, ok := channelNames[c]; return ok && c != ChannelUnspecified }

// channelAliases are the surface names the transports actually use. They are
// normalization only: a surface that is not in this table gets a typed
// rejection rather than a new channel, so a new front end cannot quietly widen
// the origin contract.
var channelAliases = map[string]Channel{
	"ui": ChannelUI, "web": ChannelUI, "console": ChannelUI,
	"api": ChannelAPI, "grpc": ChannelAPI, "rest": ChannelAPI, "grpcbridge": ChannelAPI,
	"http_edge": ChannelAPI,
	"cli":       ChannelCLI,
	"mobile":    ChannelMobile, "ios": ChannelMobile, "android": ChannelMobile,
	"kiosk":   ChannelKiosk,
	"partner": ChannelPartner, "connector": ChannelPartner,
	"scheduler": ChannelScheduler, "schedule": ChannelScheduler,
	"event_bus": ChannelEventBus, "events": ChannelEventBus,
	"operator_console": ChannelOperatorConsole, "operator": ChannelOperatorConsole,
}

// ParseChannel normalizes a surface name onto a channel. Case and surrounding
// whitespace do not matter; an unknown surface does.
func ParseChannel(surface string) (Channel, error) {
	key := strings.ToLower(strings.TrimSpace(surface))
	if c, ok := channelAliases[key]; ok {
		return c, nil
	}
	if c, ok := channelAliases[strings.ReplaceAll(key, "-", "_")]; ok {
		return c, nil
	}
	return ChannelUnspecified, newError("ParseChannel", "origin.channel", ErrUntrustedOrigin,
		"%q is not a normalized surface", surface)
}

// OriginAssurance is the identity assurance the credential behind the origin
// reached. It is evidence about the authentication, not a permission.
type OriginAssurance uint8

// OriginAssurance values.
const (
	AssuranceUnspecified OriginAssurance = iota
	AssuranceLow
	AssuranceSubstantial
	AssuranceHigh
)

var assuranceNames = map[OriginAssurance]string{
	AssuranceUnspecified: "UNSPECIFIED",
	AssuranceLow:         "LOW",
	AssuranceSubstantial: "SUBSTANTIAL",
	AssuranceHigh:        "HIGH",
}

func (a OriginAssurance) String() string { return enumName(assuranceNames, a, "OriginAssurance") }

// Valid reports whether a is a declared assurance other than UNSPECIFIED.
func (a OriginAssurance) Valid() bool {
	_, ok := assuranceNames[a]
	return ok && a != AssuranceUnspecified
}

// TrustedOriginContext is everything the trusted boundary derived for itself:
// the verified principal, the scope that credential resolved to, the delegation
// chain the authority service actually issued, the surface the request arrived
// at, and, for a non-interactive source, the authenticated producer and the
// evidence record for the triggering fact.
//
// Nothing in it comes from the request body. It is a separate type from
// [OriginClaim] precisely so that the two can never be confused at a call site:
// the only way to build an [Origin] is to hand both of them to [NewOrigin],
// which then refuses any overlap.
type TrustedOriginContext struct {
	// Kind is the origin kind the trusted boundary derived.
	Kind OriginKind

	Tenant              values.TenantId
	OrganizationScopeID string

	// Initiator is the verified principal. Its kind must be one the origin
	// kind admits.
	Initiator PrincipalReference

	// OnBehalfOf is the delegation chain the authority service issued, when
	// the full chain has been materialized.
	OnBehalfOf []DelegationReference

	// DelegationRefs are the delegation grants the credential carries, by id.
	// A credential names the grants it was issued under before anything
	// resolves them into hops, and a delegated act must be recorded as
	// delegated at that point rather than after: either form substantiates a
	// DELEGATED_HUMAN origin, and neither is caller-supplied.
	DelegationRefs []string

	// SessionRef points at the authenticated session record.
	SessionRef string

	Assurance OriginAssurance
	Channel   Channel

	// TrustedContextDigest pins the exact trusted context this origin was
	// derived from, so an origin can be re-checked against the credential
	// evidence it claims to come from.
	TrustedContextDigest string

	// ProducerRef names the authenticated producer of a non-interactive
	// source: the scheduler instance, the event publisher, the repair runner
	// or the operator path.
	ProducerRef string

	// SourceEvidenceRef points at the evidence record for the triggering
	// fact: the schedule firing, the event receipt, the repair authorization
	// or the support-access grant.
	SourceEvidenceRef string

	// ProviderAuthorityRef records which external provider produced an
	// external event. It is recorded so an investigation can find the
	// provider; it is never authority, and an external event whose initiator
	// is the provider itself is refused.
	ProviderAuthorityRef string
}

// OriginClaim is what the caller said. Every field on it that the caller does
// not get to choose is a rejection when populated, rather than a value that is
// silently overwritten: a client that believes it is acting as someone else
// must be told it is wrong.
type OriginClaim struct {
	// Kind is the origin the caller believes it has. UNSPECIFIED means "you
	// tell me", which is the normal case; anything else must agree exactly
	// with what the trusted boundary derived.
	Kind OriginKind

	// TriggerRef, CorrelationID and CausationID are the caller's own causal
	// bookkeeping and are safe to accept: none of them grants anything.
	TriggerRef    string
	CorrelationID string
	CausationID   string

	// The remaining fields are server-derived. A populated one is a rejection.
	PrincipalID          string
	TenantID             string
	SessionRef           string
	OnBehalfOf           []DelegationReference
	DelegationRefs       []string
	Assurance            OriginAssurance
	ProducerRef          string
	ProviderAuthorityRef string
}

// spoofedFields returns the trusted fields this claim tried to select.
func (c OriginClaim) spoofedFields() []string {
	var got []string
	for _, f := range []struct {
		field string
		set   bool
	}{
		{"origin.principal_id", c.PrincipalID != ""},
		{"origin.tenant_id", c.TenantID != ""},
		{"origin.session_ref", c.SessionRef != ""},
		{"origin.on_behalf_of", len(c.OnBehalfOf) > 0},
		{"origin.delegation_refs", len(c.DelegationRefs) > 0},
		{"origin.assurance", c.Assurance != AssuranceUnspecified},
		{"origin.producer_ref", c.ProducerRef != ""},
		{"origin.provider_authority_ref", c.ProviderAuthorityRef != ""},
	} {
		if f.set {
			got = append(got, f.field)
		}
	}
	return got
}

// Origin is the immutable trusted origin record.
//
// It deliberately carries no role, capability, entitlement, scope grant or
// permission field. Origin says where an intent came from; governance decides,
// independently and from the capability registry and the policy bundle, what
// that origin may do. [Origin.ConfersAuthority] is a constant false, and the
// structural test in this package fails if a field that would confer authority
// is ever added.
type Origin struct {
	Kind OriginKind

	Tenant              values.TenantId
	OrganizationScopeID string

	Initiator      PrincipalReference
	OnBehalfOf     []DelegationReference
	DelegationRefs []string
	SessionRef     string

	TriggerRef    string
	CorrelationID string
	CausationID   string

	Assurance OriginAssurance
	Channel   Channel

	TrustedContextDigest string
	ProducerRef          string
	SourceEvidenceRef    string
	ProviderAuthorityRef string
}

// ConfersAuthority reports whether the origin grants any authority. It is
// always false. Authority is a governance decision taken from the capability
// registry and the policy bundle against the verified principal; an origin is
// an input to that decision and never a substitute for it.
func (Origin) ConfersAuthority() bool { return false }

// IsSet reports whether an origin was recorded at all.
func (o Origin) IsSet() bool { return o.Kind != OriginUnspecified }

// NewOrigin records a trusted origin, refusing any caller attempt to select one.
//
// The order matters. The claim is checked first, so a caller that tried to
// name a principal gets ErrCallerSelectedAuthority whether or not its trusted
// context would have been valid; then the trusted context is checked, so a
// boundary that failed to substantiate a schedule or an event gets
// ErrUntrustedOrigin rather than a half-recorded origin.
func NewOrigin(claim OriginClaim, trusted TrustedOriginContext) (Origin, error) {
	if spoofed := claim.spoofedFields(); len(spoofed) > 0 {
		return Origin{}, newError("NewOrigin", spoofed[0], ErrCallerSelectedAuthority,
			"the request selected %s; trusted origin is derived server-side",
			strings.Join(spoofed, ", "))
	}
	if claim.Kind != OriginUnspecified && claim.Kind != trusted.Kind {
		return Origin{}, newError("NewOrigin", "origin.kind", ErrCallerSelectedAuthority,
			"the request declared origin %s but the verified credential is %s",
			claim.Kind, trusted.Kind)
	}
	if err := trusted.validate(); err != nil {
		return Origin{}, err
	}
	for _, req := range []struct{ field, value string }{
		{"origin.trigger_ref", claim.TriggerRef},
		{"origin.correlation_id", claim.CorrelationID},
	} {
		if err := requireCanonicalText(req.field, req.value); err != nil {
			return Origin{}, newError("NewOrigin", req.field, ErrUntrustedOrigin, "%v", err)
		}
	}
	if claim.CausationID != "" {
		if err := requireCanonicalText("origin.causation_id", claim.CausationID); err != nil {
			return Origin{}, newError("NewOrigin", "origin.causation_id", ErrUntrustedOrigin, "%v", err)
		}
	}

	origin := Origin{
		Kind:                 trusted.Kind,
		Tenant:               trusted.Tenant,
		OrganizationScopeID:  trusted.OrganizationScopeID,
		Initiator:            trusted.Initiator,
		OnBehalfOf:           slices.Clone(trusted.OnBehalfOf),
		DelegationRefs:       slices.Clone(trusted.DelegationRefs),
		SessionRef:           trusted.SessionRef,
		TriggerRef:           claim.TriggerRef,
		CorrelationID:        claim.CorrelationID,
		CausationID:          claim.CausationID,
		Assurance:            trusted.Assurance,
		Channel:              trusted.Channel,
		TrustedContextDigest: trusted.TrustedContextDigest,
		ProducerRef:          trusted.ProducerRef,
		SourceEvidenceRef:    trusted.SourceEvidenceRef,
		ProviderAuthorityRef: trusted.ProviderAuthorityRef,
	}
	return origin, nil
}

// validate checks everything the trusted boundary itself must have got right.
func (t TrustedOriginContext) validate() error {
	if !t.Kind.Valid() {
		return newError("NewOrigin", "origin.kind", ErrUntrustedOrigin,
			"the trusted boundary derived no origin kind")
	}
	if err := t.Tenant.Validate(); err != nil {
		return newError("NewOrigin", "origin.tenant_id", ErrUntrustedOrigin, "%v", err)
	}
	if err := requireCanonicalText("origin.organization_scope_id", t.OrganizationScopeID); err != nil {
		return newError("NewOrigin", "origin.organization_scope_id", ErrUntrustedOrigin, "%v", err)
	}
	if err := t.Initiator.Validate(); err != nil {
		return newError("NewOrigin", "origin.initiator", ErrUntrustedOrigin, "%v", err)
	}
	if !t.Kind.AllowsInitiator(t.Initiator.Kind) {
		return newError("NewOrigin", "origin.initiator.kind", ErrUntrustedOrigin,
			"origin %s cannot be built from a verified %s initiator", t.Kind, t.Initiator.Kind)
	}
	for _, hop := range t.OnBehalfOf {
		if err := hop.Validate(); err != nil {
			return newError("NewOrigin", "origin.on_behalf_of", ErrUntrustedOrigin, "%v", err)
		}
	}
	switch t.Kind {
	case OriginDelegatedHuman:
		if len(t.OnBehalfOf) == 0 && len(t.DelegationRefs) == 0 {
			return newError("NewOrigin", "origin.on_behalf_of", ErrUntrustedOrigin,
				"a delegated human origin records no delegation chain")
		}
	case OriginHuman:
		if len(t.OnBehalfOf) > 0 || len(t.DelegationRefs) > 0 {
			return newError("NewOrigin", "origin.on_behalf_of", ErrUntrustedOrigin,
				"a human acting under a delegation chain is DELEGATED_HUMAN, not HUMAN")
		}
	}
	for _, ref := range t.DelegationRefs {
		if err := requireCanonicalText("origin.delegation_refs", ref); err != nil {
			return newError("NewOrigin", "origin.delegation_refs", ErrUntrustedOrigin, "%v", err)
		}
	}
	if !t.Assurance.Valid() {
		return newError("NewOrigin", "origin.assurance", ErrUntrustedOrigin,
			"origin %s records no identity assurance", t.Kind)
	}
	if !t.Channel.Valid() {
		return newError("NewOrigin", "origin.channel", ErrUntrustedOrigin,
			"origin %s records no normalized channel", t.Kind)
	}
	if err := requireCanonicalText("origin.trusted_context_digest", t.TrustedContextDigest); err != nil {
		return newError("NewOrigin", "origin.trusted_context_digest", ErrUntrustedOrigin, "%v", err)
	}
	if t.Kind.Interactive() {
		if err := requireCanonicalText("origin.session_ref", t.SessionRef); err != nil {
			return newError("NewOrigin", "origin.session_ref", ErrUntrustedOrigin, "%v", err)
		}
	} else {
		for _, req := range []struct{ field, value string }{
			{"origin.producer_ref", t.ProducerRef},
			{"origin.source_evidence_ref", t.SourceEvidenceRef},
		} {
			if req.value == "" {
				return newError("NewOrigin", req.field, ErrUntrustedOrigin,
					"origin %s names no authenticated producer and source evidence", t.Kind)
			}
			if err := requireCanonicalText(req.field, req.value); err != nil {
				return newError("NewOrigin", req.field, ErrUntrustedOrigin, "%v", err)
			}
		}
	}
	if t.Kind == OriginExternalEvent {
		if t.ProviderAuthorityRef == "" {
			return newError("NewOrigin", "origin.provider_authority_ref", ErrUntrustedOrigin,
				"an external event names no producing provider")
		}
		// The provider is a producer. An external event whose acting principal
		// is the provider itself is the provider's authority arriving through
		// the front door, which is exactly what this rule exists to stop: a
		// connector runs as one of our own integration service identities and
		// carries the entitlements we granted it, not the ones the provider
		// has in its own system.
		if t.Initiator.PrincipalID == t.ProviderAuthorityRef {
			return newError("NewOrigin", "origin.initiator.principal_id", ErrUntrustedOrigin,
				"an external event may not act as its provider %q", t.ProviderAuthorityRef)
		}
	} else if t.ProviderAuthorityRef != "" {
		return newError("NewOrigin", "origin.provider_authority_ref", ErrUntrustedOrigin,
			"origin %s is not an external event and names a provider authority", t.Kind)
	}
	return nil
}

// Validate re-checks a materialized origin, so a record written by an older
// binary cannot reintroduce a shape the kernel now refuses. It is the same rule
// set [NewOrigin] applied, run against the recorded values.
func (o Origin) Validate() error {
	trusted := TrustedOriginContext{
		Kind:                 o.Kind,
		Tenant:               o.Tenant,
		OrganizationScopeID:  o.OrganizationScopeID,
		Initiator:            o.Initiator,
		OnBehalfOf:           o.OnBehalfOf,
		DelegationRefs:       o.DelegationRefs,
		SessionRef:           o.SessionRef,
		Assurance:            o.Assurance,
		Channel:              o.Channel,
		TrustedContextDigest: o.TrustedContextDigest,
		ProducerRef:          o.ProducerRef,
		SourceEvidenceRef:    o.SourceEvidenceRef,
		ProviderAuthorityRef: o.ProviderAuthorityRef,
	}
	if err := trusted.validate(); err != nil {
		return err
	}
	for _, req := range []struct{ field, value string }{
		{"origin.trigger_ref", o.TriggerRef},
		{"origin.correlation_id", o.CorrelationID},
	} {
		if err := requireCanonicalText(req.field, req.value); err != nil {
			return newError("Validate", req.field, ErrUntrustedOrigin, "%v", err)
		}
	}
	return nil
}

// AgreesWith reports whether the origin describes the same actor and scope as
// the envelope it is recorded on. An envelope whose initiator or tenant differs
// from its own origin is two different stories about one request.
func (o Origin) AgreesWith(tenant values.TenantId, initiator PrincipalReference) error {
	if o.Tenant != tenant {
		return newError("Validate", "origin.tenant_id", ErrUntrustedOrigin,
			"origin records tenant %s but the instance is tenant %s", o.Tenant, tenant)
	}
	if o.Initiator != initiator {
		return newError("Validate", "origin.initiator", ErrUntrustedOrigin,
			"origin records principal %q but the instance initiator is %q",
			o.Initiator.PrincipalID, initiator.PrincipalID)
	}
	return nil
}
