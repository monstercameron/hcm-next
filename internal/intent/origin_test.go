package intent_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// trustedHumanOrigin is the trusted context a UI request from a verified human
// resolves to. Tests copy it and break exactly one thing.
func trustedHumanOrigin() intent.TrustedOriginContext {
	return intent.TrustedOriginContext{
		Kind:                 intent.OriginHuman,
		Tenant:               values.TenantId("acme-eu"),
		OrganizationScopeID:  "org:acme-eu:engineering",
		Initiator:            principal(),
		SessionRef:           "session:hr-partner-7:19f2",
		Assurance:            intent.AssuranceHigh,
		Channel:              intent.ChannelUI,
		TrustedContextDigest: "sha256:trusted-context-1",
	}
}

// trustedScheduleOrigin is the trusted context a scheduler firing resolves to.
func trustedScheduleOrigin() intent.TrustedOriginContext {
	return intent.TrustedOriginContext{
		Kind:                intent.OriginSchedule,
		Tenant:              values.TenantId("acme-eu"),
		OrganizationScopeID: "org:acme-eu:engineering",
		Initiator: intent.PrincipalReference{
			PrincipalID:          "workload:scheduler",
			Kind:                 intent.InitiatorSchedule,
			IdentityAssuranceRef: "assurance.workload_identity/v1",
		},
		Assurance:            intent.AssuranceSubstantial,
		Channel:              intent.ChannelScheduler,
		TrustedContextDigest: "sha256:trusted-context-2",
		ProducerRef:          "scheduler:eu-1",
		SourceEvidenceRef:    "evidence:schedule_fire:88213",
	}
}

// trustedExternalEventOrigin is the trusted context a connector event resolves
// to: our own integration identity acting, with the foreign provider recorded
// as a producer only.
func trustedExternalEventOrigin() intent.TrustedOriginContext {
	return intent.TrustedOriginContext{
		Kind:                intent.OriginExternalEvent,
		Tenant:              values.TenantId("acme-eu"),
		OrganizationScopeID: "org:acme-eu:engineering",
		Initiator: intent.PrincipalReference{
			PrincipalID:          "workload:connector.payroll_in",
			Kind:                 intent.InitiatorIntegration,
			IdentityAssuranceRef: "assurance.workload_identity/v1",
		},
		Assurance:            intent.AssuranceSubstantial,
		Channel:              intent.ChannelPartner,
		TrustedContextDigest: "sha256:trusted-context-3",
		ProducerRef:          "connector:payroll_in:eu-1",
		SourceEvidenceRef:    "evidence:event_receipt:41192",
		ProviderAuthorityRef: "provider:globalpayco",
	}
}

// originClaim is the caller-supplied part of an origin: causal bookkeeping and
// nothing else.
func originClaim() intent.OriginClaim {
	return intent.OriginClaim{
		TriggerRef:    "trigger:ui.promote_worker.submit",
		CorrelationID: "corr:1",
	}
}

// TestIntentOriginRejectsSpoofedActorAndTrigger is the PRIMARY test for
// INTENT-012.
//
// RED: a caller-selected principal, tenant, session, delegation chain,
// assurance, producer or provider authority is a rejection; a schedule, event,
// repair or operator origin with no authenticated producer and source evidence
// is a rejection; an agent presenting itself as a human is a rejection; and an
// external event may not act as its provider.
//
// GREEN: the nine trusted origin kinds record initiator, on-behalf-of chain,
// trigger, correlation, causation, assurance, channel and trusted context, and
// the record grants nothing.
func TestIntentOriginRejectsSpoofedActorAndTrigger(t *testing.T) {
	t.Run("RED: caller-selected trusted context", func(t *testing.T) {
		cases := []struct {
			name   string
			break_ func(*intent.OriginClaim)
		}{
			{"principal", func(c *intent.OriginClaim) { c.PrincipalID = "principal:ceo" }},
			{"tenant", func(c *intent.OriginClaim) { c.TenantID = "globex" }},
			{"session", func(c *intent.OriginClaim) { c.SessionRef = "session:forged" }},
			{"delegation chain", func(c *intent.OriginClaim) {
				c.OnBehalfOf = []intent.DelegationReference{{
					DelegationID:          "delegation:forged",
					DelegatingPrincipalID: "principal:ceo",
					DelegatedPrincipalID:  "principal:hr-partner-7",
					AuthorityDigest:       "sha256:forged",
				}}
			}},
			{"assurance", func(c *intent.OriginClaim) { c.Assurance = intent.AssuranceHigh }},
			{"producer", func(c *intent.OriginClaim) { c.ProducerRef = "scheduler:forged" }},
			{"provider authority", func(c *intent.OriginClaim) { c.ProviderAuthorityRef = "provider:globalpayco" }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				claim := originClaim()
				tc.break_(&claim)
				_, err := intent.NewOrigin(claim, trustedHumanOrigin())
				if !errors.Is(err, intent.ErrCallerSelectedAuthority) {
					t.Fatalf("a caller selected its own %s: %v", tc.name, err)
				}
			})
		}
	})

	t.Run("RED: a caller-declared origin kind must agree with the credential", func(t *testing.T) {
		claim := originClaim()
		claim.Kind = intent.OriginOperator
		if _, err := intent.NewOrigin(claim, trustedHumanOrigin()); !errors.Is(err, intent.ErrCallerSelectedAuthority) {
			t.Fatalf("a caller declared itself an operator: %v", err)
		}
	})

	t.Run("RED: a non-interactive origin needs a producer and source evidence", func(t *testing.T) {
		for _, kind := range []intent.OriginKind{
			intent.OriginSchedule, intent.OriginInternalEvent,
			intent.OriginExternalEvent, intent.OriginRepair, intent.OriginOperator,
			intent.OriginWorkload,
		} {
			if kind.Interactive() {
				t.Fatalf("%s is classified interactive", kind)
			}
		}
		for _, drop := range []func(*intent.TrustedOriginContext){
			func(c *intent.TrustedOriginContext) { c.ProducerRef = "" },
			func(c *intent.TrustedOriginContext) { c.SourceEvidenceRef = "" },
		} {
			trusted := trustedScheduleOrigin()
			drop(&trusted)
			if _, err := intent.NewOrigin(originClaim(), trusted); !errors.Is(err, intent.ErrUntrustedOrigin) {
				t.Fatalf("an unsubstantiated schedule origin was recorded: %v", err)
			}
		}
	})

	t.Run("RED: an agent may not present itself as a human", func(t *testing.T) {
		trusted := trustedHumanOrigin()
		trusted.Initiator.Kind = intent.InitiatorAgent
		_, err := intent.NewOrigin(originClaim(), trusted)
		if !errors.Is(err, intent.ErrUntrustedOrigin) {
			t.Fatalf("an agent was recorded as a human origin: %v", err)
		}
		if !strings.Contains(err.Error(), "AGENT") {
			t.Fatalf("the rejection does not name the verified initiator kind: %v", err)
		}
	})

	t.Run("RED: an external event may not inherit provider authority", func(t *testing.T) {
		trusted := trustedExternalEventOrigin()
		trusted.Initiator.PrincipalID = trusted.ProviderAuthorityRef
		if _, err := intent.NewOrigin(originClaim(), trusted); !errors.Is(err, intent.ErrUntrustedOrigin) {
			t.Fatalf("an external event acted as its provider: %v", err)
		}
	})

	t.Run("RED: an origin with no trigger or correlation is rejected", func(t *testing.T) {
		for _, drop := range []func(*intent.OriginClaim){
			func(c *intent.OriginClaim) { c.TriggerRef = "" },
			func(c *intent.OriginClaim) { c.CorrelationID = "" },
		} {
			claim := originClaim()
			drop(&claim)
			if _, err := intent.NewOrigin(claim, trustedHumanOrigin()); !errors.Is(err, intent.ErrUntrustedOrigin) {
				t.Fatalf("an origin with no causal bookkeeping was recorded: %v", err)
			}
		}
	})

	t.Run("GREEN: every kind records its actor, trigger and context", func(t *testing.T) {
		trustedFor := func(kind intent.OriginKind) intent.TrustedOriginContext {
			trusted := trustedScheduleOrigin()
			trusted.Kind = kind
			switch kind {
			case intent.OriginHuman:
				return trustedHumanOrigin()
			case intent.OriginDelegatedHuman:
				d := trustedHumanOrigin()
				d.Kind = intent.OriginDelegatedHuman
				d.OnBehalfOf = []intent.DelegationReference{{
					DelegationID:          "delegation:vacation-cover",
					DelegatingPrincipalID: "principal:hr-director-1",
					DelegatedPrincipalID:  "principal:hr-partner-7",
					AuthorityDigest:       "sha256:delegation-1",
				}}
				return d
			case intent.OriginAgent:
				a := trustedHumanOrigin()
				a.Kind = intent.OriginAgent
				a.Initiator.Kind = intent.InitiatorAgent
				a.Channel = intent.ChannelAPI
				return a
			case intent.OriginExternalEvent:
				return trustedExternalEventOrigin()
			case intent.OriginInternalEvent:
				trusted.Initiator.Kind = intent.InitiatorSystemEvent
				trusted.Channel = intent.ChannelEventBus
			case intent.OriginWorkload, intent.OriginRepair:
				trusted.Initiator.Kind = intent.InitiatorService
				trusted.Channel = intent.ChannelAPI
			case intent.OriginOperator:
				trusted.Initiator.Kind = intent.InitiatorHuman
				trusted.Channel = intent.ChannelOperatorConsole
			}
			return trusted
		}
		kinds := intent.OriginKinds()
		if len(kinds) != 9 {
			t.Fatalf("origin kinds = %d, want the nine trusted sources", len(kinds))
		}
		for _, kind := range kinds {
			origin, err := intent.NewOrigin(originClaim(), trustedFor(kind))
			if err != nil {
				t.Fatalf("%s: %v", kind, err)
			}
			if origin.Kind != kind {
				t.Fatalf("%s recorded as %s", kind, origin.Kind)
			}
			if origin.TriggerRef != "trigger:ui.promote_worker.submit" || origin.CorrelationID != "corr:1" {
				t.Fatalf("%s lost its causal bookkeeping: %+v", kind, origin)
			}
			if origin.TrustedContextDigest == "" || !origin.Assurance.Valid() || !origin.Channel.Valid() {
				t.Fatalf("%s lost its trusted context: %+v", kind, origin)
			}
			if err := origin.Validate(); err != nil {
				t.Fatalf("%s does not revalidate: %v", kind, err)
			}
			if origin.ConfersAuthority() {
				t.Fatalf("%s claims to confer authority", kind)
			}
		}
	})

	t.Run("GREEN: a delegated human keeps its on-behalf-of chain", func(t *testing.T) {
		trusted := trustedHumanOrigin()
		trusted.Kind = intent.OriginDelegatedHuman
		trusted.OnBehalfOf = []intent.DelegationReference{{
			DelegationID:          "delegation:vacation-cover",
			DelegatingPrincipalID: "principal:hr-director-1",
			DelegatedPrincipalID:  "principal:hr-partner-7",
			AuthorityDigest:       "sha256:delegation-1",
		}}
		origin, err := intent.NewOrigin(originClaim(), trusted)
		if err != nil {
			t.Fatalf("delegated origin: %v", err)
		}
		if len(origin.OnBehalfOf) != 1 || origin.OnBehalfOf[0].DelegatingPrincipalID != "principal:hr-director-1" {
			t.Fatalf("the delegation chain was not recorded: %+v", origin.OnBehalfOf)
		}
		// A plain HUMAN origin under a delegation chain is a mis-classification,
		// not a HUMAN: the two are told apart so that an investigation can ask
		// "who authorized this" and get an answer.
		trusted.Kind = intent.OriginHuman
		if _, err := intent.NewOrigin(originClaim(), trusted); !errors.Is(err, intent.ErrUntrustedOrigin) {
			t.Fatalf("a delegated act was recorded as a plain human origin: %v", err)
		}
	})
}

// TestTodo_INTENT_012_Integration drives origin through the composed kernel:
// the checked-in catalog registry, the real canonical digester and the envelope
// the storage layer round-trips, plus the surface normalization every transport
// depends on.
func TestTodo_INTENT_012_Integration(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")

	origin, err := intent.NewOrigin(originClaim(), trustedHumanOrigin())
	if err != nil {
		t.Fatalf("origin: %v", err)
	}

	t.Run("an instance carries its origin and revalidates with it", func(t *testing.T) {
		spec := promoteSpec()
		spec.Origin = origin
		inst, _, err := intent.NewInstance(spec, def, d, countingIDs("00000012"), fixedClock())
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if inst.Origin.Kind != intent.OriginHuman {
			t.Fatalf("origin was not recorded on the envelope: %+v", inst.Origin)
		}
		if err := inst.Validate(def); err != nil {
			t.Fatalf("revalidate: %v", err)
		}
	})

	t.Run("a governed draft carries its origin too", func(t *testing.T) {
		spec := promoteSpec()
		spec.Origin = origin
		inst, _, err := intent.Draft(spec, def, d, countingIDs("00000013"), fixedClock())
		if err != nil {
			t.Fatalf("draft: %v", err)
		}
		if inst.Origin.Kind != intent.OriginHuman {
			t.Fatalf("a drafted change request lost its origin: %+v", inst.Origin)
		}
	})

	t.Run("every surface normalizes into the same channel contract", func(t *testing.T) {
		want := map[string]intent.Channel{
			"UI": intent.ChannelUI, "web": intent.ChannelUI,
			"API": intent.ChannelAPI, "gRPC": intent.ChannelAPI, "grpcbridge": intent.ChannelAPI,
			"cli":    intent.ChannelCLI,
			"mobile": intent.ChannelMobile, "iOS": intent.ChannelMobile, "android": intent.ChannelMobile,
			"kiosk":   intent.ChannelKiosk,
			"partner": intent.ChannelPartner, "connector": intent.ChannelPartner,
			"scheduler": intent.ChannelScheduler,
			"event-bus": intent.ChannelEventBus,
			"operator":  intent.ChannelOperatorConsole,
		}
		for surface, expect := range want {
			got, err := intent.ParseChannel(surface)
			if err != nil {
				t.Fatalf("surface %q: %v", surface, err)
			}
			if got != expect {
				t.Fatalf("surface %q normalized to %s, want %s", surface, got, expect)
			}
		}
		if _, err := intent.ParseChannel("smart_fridge"); !errors.Is(err, intent.ErrUntrustedOrigin) {
			t.Fatalf("an unknown surface invented a channel: %v", err)
		}
	})
}

// TestTodo_INTENT_012_Security is the structural half of "records origin
// without granting authority": an origin that disagrees with its envelope, or
// that names another tenant, never reaches storage, and the Origin type carries
// no field that could be read as a grant.
func TestTodo_INTENT_012_Security(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")

	origin, err := intent.NewOrigin(originClaim(), trustedHumanOrigin())
	if err != nil {
		t.Fatalf("origin: %v", err)
	}

	t.Run("an origin from another tenant is refused", func(t *testing.T) {
		spec := promoteSpec()
		spec.Origin = origin
		spec.Origin.Tenant = values.TenantId("globex")
		if _, _, err := intent.NewInstance(spec, def, d, nil, fixedClock()); !errors.Is(err, intent.ErrUntrustedOrigin) {
			t.Fatalf("an envelope kept an origin from another tenant: %v", err)
		}
	})

	t.Run("an origin naming another actor is refused", func(t *testing.T) {
		spec := promoteSpec()
		spec.Origin = origin
		spec.Origin.Initiator.PrincipalID = "principal:ceo"
		if _, _, err := intent.NewInstance(spec, def, d, nil, fixedClock()); !errors.Is(err, intent.ErrUntrustedOrigin) {
			t.Fatalf("an envelope kept an origin naming another actor: %v", err)
		}
	})

	t.Run("Origin carries no authority-bearing field", func(t *testing.T) {
		// Origin answers "where did this come from". The moment it grows a
		// role, capability, entitlement or scope grant, a caller that can
		// influence any part of the origin can influence what the intent may
		// do, which is the exact failure this todo exists to prevent.
		forbidden := []string{
			"role", "capability", "capabilities", "permission", "permissions",
			"entitlement", "entitlements", "grant", "grants", "scopegrant",
			"authorization", "privilege", "privileges", "allow", "override",
		}
		typ := reflect.TypeOf(intent.Origin{})
		for i := range typ.NumField() {
			name := strings.ToLower(typ.Field(i).Name)
			for _, bad := range forbidden {
				if strings.Contains(name, bad) {
					t.Fatalf("Origin.%s reads as authority; governance decides authority, origin does not",
						typ.Field(i).Name)
				}
			}
		}
		if (intent.Origin{}).ConfersAuthority() {
			t.Fatal("Origin.ConfersAuthority is not a constant false")
		}
	})
}

// TestTodo_INTENT_012_Mutation perturbs one field of a valid trusted context at
// a time. Every field the origin contract calls required must produce a typed
// rejection: a mutation that survives is a field the contract does not actually
// enforce.
func TestTodo_INTENT_012_Mutation(t *testing.T) {
	cases := []struct {
		name   string
		base   func() intent.TrustedOriginContext
		break_ func(*intent.TrustedOriginContext)
		cause  error
	}{
		{"no kind", trustedHumanOrigin, func(c *intent.TrustedOriginContext) { c.Kind = intent.OriginUnspecified }, intent.ErrUntrustedOrigin},
		{"no tenant", trustedHumanOrigin, func(c *intent.TrustedOriginContext) { c.Tenant = "" }, intent.ErrUntrustedOrigin},
		{"no organization scope", trustedHumanOrigin, func(c *intent.TrustedOriginContext) { c.OrganizationScopeID = "" }, intent.ErrUntrustedOrigin},
		{"no principal", trustedHumanOrigin, func(c *intent.TrustedOriginContext) { c.Initiator.PrincipalID = "" }, intent.ErrUntrustedOrigin},
		{"no identity assurance reference", trustedHumanOrigin, func(c *intent.TrustedOriginContext) { c.Initiator.IdentityAssuranceRef = "" }, intent.ErrUntrustedOrigin},
		{"no assurance", trustedHumanOrigin, func(c *intent.TrustedOriginContext) { c.Assurance = intent.AssuranceUnspecified }, intent.ErrUntrustedOrigin},
		{"no channel", trustedHumanOrigin, func(c *intent.TrustedOriginContext) { c.Channel = intent.ChannelUnspecified }, intent.ErrUntrustedOrigin},
		{"no trusted context digest", trustedHumanOrigin, func(c *intent.TrustedOriginContext) { c.TrustedContextDigest = "" }, intent.ErrUntrustedOrigin},
		{"no session on an interactive origin", trustedHumanOrigin, func(c *intent.TrustedOriginContext) { c.SessionRef = "" }, intent.ErrUntrustedOrigin},
		{"malformed delegation hop", trustedHumanOrigin, func(c *intent.TrustedOriginContext) {
			c.Kind = intent.OriginDelegatedHuman
			c.OnBehalfOf = []intent.DelegationReference{{DelegationID: "delegation:1"}}
		}, intent.ErrUntrustedOrigin},
		{"provider authority on a non-external origin", trustedHumanOrigin, func(c *intent.TrustedOriginContext) {
			c.ProviderAuthorityRef = "provider:globalpayco"
		}, intent.ErrUntrustedOrigin},
		{"external event with no provider", trustedExternalEventOrigin, func(c *intent.TrustedOriginContext) {
			c.ProviderAuthorityRef = ""
		}, intent.ErrUntrustedOrigin},
		{"schedule origin from a human credential", trustedScheduleOrigin, func(c *intent.TrustedOriginContext) {
			c.Initiator.Kind = intent.InitiatorHuman
		}, intent.ErrUntrustedOrigin},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trusted := tc.base()
			tc.break_(&trusted)
			if _, err := intent.NewOrigin(originClaim(), trusted); !errors.Is(err, tc.cause) {
				t.Fatalf("mutation %q survived: %v", tc.name, err)
			}
		})
	}

	t.Run("the unmutated baselines are all accepted", func(t *testing.T) {
		for _, base := range []func() intent.TrustedOriginContext{
			trustedHumanOrigin, trustedScheduleOrigin, trustedExternalEventOrigin,
		} {
			if _, err := intent.NewOrigin(originClaim(), base()); err != nil {
				t.Fatalf("a valid trusted context was refused: %v", err)
			}
		}
	})

	t.Run("the recorded origin does not alias the caller's delegation chain", func(t *testing.T) {
		trusted := trustedHumanOrigin()
		trusted.Kind = intent.OriginDelegatedHuman
		trusted.OnBehalfOf = []intent.DelegationReference{{
			DelegationID:          "delegation:1",
			DelegatingPrincipalID: "principal:hr-director-1",
			DelegatedPrincipalID:  "principal:hr-partner-7",
			AuthorityDigest:       "sha256:delegation-1",
		}}
		origin, err := intent.NewOrigin(originClaim(), trusted)
		if err != nil {
			t.Fatalf("origin: %v", err)
		}
		trusted.OnBehalfOf[0].DelegatingPrincipalID = "principal:ceo"
		if origin.OnBehalfOf[0].DelegatingPrincipalID != "principal:hr-director-1" {
			t.Fatal("a recorded origin shares its delegation chain with its caller")
		}
	})
}
