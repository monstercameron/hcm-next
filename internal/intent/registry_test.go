package intent_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
)

// mutate returns the catalog with one definition replaced by f's result, so a
// RED case differs from the checked-in table in exactly one respect.
func mutate(ref string, f func(*intent.Definition)) []intent.Definition {
	defs := definitions.All()
	for i := range defs {
		if defs[i].Ref.String() == ref {
			f(&defs[i])
		}
	}
	return defs
}

func compile(defs []intent.Definition) (*intent.Registry, error) {
	return intent.NewRegistry(intent.ProfileBootstrap, defs, definitions.Policies(), definitions.Catalog())
}

// TestTodo_MODEL_010 is the PRIMARY test for the intent-definition registry.
//
// RED: the registry rejects a missing required DRAFT_CONTRACT field, an unknown
// schema, an unknown capability, a family outside the three, an invalid
// family/effect pairing, a population scope on a non-CHANGE_REQUEST, and
// material version reuse.
//
// GREEN: exactly the checked-in definitions resolve by (intent_type_id,
// version) with their family, side-effect profile and release; nothing below
// DRAFT_CONTRACT resolves; the registry is a compiled-in table under the
// BOOTSTRAP profile.
func TestTodo_MODEL_010(t *testing.T) {
	t.Run("RED", func(t *testing.T) {
		cases := []struct {
			name  string
			defs  []intent.Definition
			cause error
		}{
			{
				name:  "missing required DRAFT_CONTRACT field",
				defs:  mutate("hcmnext.people.promote_worker/v1", func(d *intent.Definition) { d.Description = "" }),
				cause: intent.ErrInvalidDefinition,
			},
			{
				name: "unknown input schema",
				defs: mutate("hcmnext.people.promote_worker/v1", func(d *intent.Definition) {
					d.InputSchema = intent.SchemaRef{
						SchemaID:         "hcmnext.people.v1.NotRegistered",
						Version:          1,
						ProtobufFullName: "hcmnext.people.v1.NotRegistered",
					}
				}),
				cause: intent.ErrInvalidDefinition,
			},
			{
				name: "unknown capability",
				defs: mutate("hcmnext.people.promote_worker/v1", func(d *intent.Definition) {
					d.RequiredCapabilities = []string{"people.promote.teleport/v1"}
				}),
				cause: intent.ErrInvalidDefinition,
			},
			{
				name: "family outside the three kernel families",
				defs: mutate("hcmnext.people.promote_worker/v1", func(d *intent.Definition) {
					// 2 is the reserved number of the retired PROCESS_REQUEST.
					d.Family = intent.Family(2)
				}),
				cause: intent.ErrInvalidDefinition,
			},
			{
				name: "analytical request declaring a mutation",
				defs: mutate("hcmnext.people.explain_worker_state/v1", func(d *intent.Definition) {
					d.SideEffect = intent.SideEffectInternalMutation
				}),
				cause: intent.ErrInvalidDefinition,
			},
			{
				name: "calculation request that is not pure",
				defs: mutate("hcmnext.rewards.simulate_compensation/v1", func(d *intent.Definition) {
					d.SideEffect = intent.SideEffectReadOnly
				}),
				cause: intent.ErrInvalidDefinition,
			},
			{
				name: "population scope on a non-CHANGE_REQUEST",
				defs: mutate("hcmnext.operations.detect_drift/v1", func(d *intent.Definition) {
					d.PopulationScope = &intent.PopulationScope{
						ScopeRef:               "population.all_workers/v1",
						ItemSubjectKind:        "WORKER",
						FrozenSnapshotRequired: true,
						PerItemChildIntent:     true,
					}
				}),
				cause: intent.ErrInvalidDefinition,
			},
			{
				name:  "material version reuse",
				defs:  append(definitions.All(), definitions.All()[0]),
				cause: intent.ErrDuplicateDefinition,
			},
			{
				name: "P1A definition with a non-zero effect class",
				defs: mutate("hcmnext.people.promote_worker/v1", func(d *intent.Definition) {
					d.EffectClass = intent.EffectClassInternalMutation
				}),
				cause: intent.ErrInvalidDefinition,
			},
			{
				name: "P1A mutating definition allowing EXECUTE",
				defs: mutate("hcmnext.people.promote_worker/v1", func(d *intent.Definition) {
					d.AllowedModes = append(d.AllowedModes, intent.ModeExecute)
				}),
				cause: intent.ErrModeNotAllowed,
			},
			{
				name: "definition transitions widen the kernel lifecycle",
				defs: mutate("hcmnext.people.promote_worker/v1", func(d *intent.Definition) {
					d.AllowedTransitions = map[lifecycle.Dimension][]lifecycle.TransitionRule{
						lifecycle.DimensionRequest: {{
							From: "DRAFT", To: "APPROVED", RetentionClass: "INTENT_LIFECYCLE",
						}},
					}
				}),
				cause: lifecycle.ErrProfileWiden,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := compile(tc.defs); !errors.Is(err, tc.cause) {
					t.Fatalf("compiled a registry that should have been rejected: err=%v want cause %v",
						err, tc.cause)
				}
			})
		}
	})

	t.Run("GREEN", func(t *testing.T) {
		reg := mustRegistry(t)
		if got, want := reg.Profile(), intent.ProfileBootstrap; got != want {
			t.Fatalf("registry profile = %q, want %q", got, want)
		}
		if got, want := reg.Len(), len(definitions.All()); got != want {
			t.Fatalf("registry publishes %d definitions, source table has %d", got, want)
		}
		// The catalog is the drafted slice; the count is a consequence of the
		// source table, never a separate number to maintain.
		if got := reg.Len(); got != 14 {
			t.Fatalf("the drafted catalog slice is fourteen definitions, registry has %d", got)
		}
		for _, d := range reg.Definitions() {
			got, err := reg.Resolve(d.Ref)
			if err != nil {
				t.Fatalf("resolve %s: %v", d.Ref, err)
			}
			if got.Family != d.Family || got.SideEffect != d.SideEffect || got.Release != d.Release {
				t.Fatalf("%s resolved with a different family, side effect or release", d.Ref)
			}
			if !d.Maturity.InCatalog() {
				t.Fatalf("%s resolves at maturity %s, below DRAFT_CONTRACT", d.Ref, d.Maturity)
			}
		}
		// The release split from the catalog: eight P1A, five P1B, one
		// conformance fixture.
		counts := map[intent.Release]int{}
		for _, d := range reg.Definitions() {
			counts[d.Release]++
		}
		for rel, want := range map[intent.Release]int{
			intent.ReleaseP1A:         8,
			intent.ReleaseP1B:         5,
			intent.ReleaseConformance: 1,
		} {
			if counts[rel] != want {
				t.Fatalf("release %s has %d definitions, want %d", rel, counts[rel], want)
			}
		}
		// Every P1A definition is zero-effect: P1A is observe, preflight and
		// simulate, and nothing else.
		for _, d := range reg.ByRelease(intent.ReleaseP1A) {
			if !d.ZeroEffect() {
				t.Fatalf("P1A definition %s has effect class %s", d.Ref, d.EffectClass)
			}
		}
	})
}

// TestTodo_MODEL_010_Property asserts registry invariants over the whole table
// rather than over one hand-picked definition.
func TestTodo_MODEL_010_Property(t *testing.T) {
	reg := mustRegistry(t)
	seen := map[intent.Ref]bool{}
	for _, d := range reg.Definitions() {
		if seen[d.Ref] {
			t.Fatalf("%s appears twice", d.Ref)
		}
		seen[d.Ref] = true

		if d.Ref.Domain() == "" {
			t.Fatalf("%s is not domain-qualified", d.Ref)
		}
		// Family and side effect are paired, never inferred from the name.
		switch d.Family {
		case intent.FamilyAnalyticalRequest:
			if d.SideEffect != intent.SideEffectReadOnly {
				t.Fatalf("%s is analytical with side effect %s", d.Ref, d.SideEffect)
			}
		case intent.FamilyCalculationRequest:
			if d.SideEffect != intent.SideEffectPure {
				t.Fatalf("%s is a calculation with side effect %s", d.Ref, d.SideEffect)
			}
		case intent.FamilyChangeRequest:
			if !d.SideEffect.Mutates() {
				t.Fatalf("%s is a change request with side effect %s", d.Ref, d.SideEffect)
			}
		default:
			t.Fatalf("%s has family %s, outside the three", d.Ref, d.Family)
		}
		if d.PopulationScope != nil && d.Family != intent.FamilyChangeRequest {
			t.Fatalf("%s carries a population scope on a %s", d.Ref, d.Family)
		}
		// Renaming a display label must not change identity: the label resolves
		// back to this exact reference.
		refs := reg.LookupDisplayName(strings.ToUpper(d.DisplayName))
		found := false
		for _, r := range refs {
			if r == d.Ref {
				found = true
			}
		}
		if !found {
			t.Fatalf("display name %q does not resolve to %s", d.DisplayName, d.Ref)
		}
	}
	// Domain qualification is what keeps same-named definitions apart. Two
	// definitions may share a verb_noun only if their domains differ.
	byVerb := map[string][]intent.Ref{}
	for _, d := range reg.Definitions() {
		parts := strings.Split(d.Ref.TypeID, ".")
		byVerb[parts[len(parts)-1]] = append(byVerb[parts[len(parts)-1]], d.Ref)
	}
	for verb, refs := range byVerb {
		domains := map[string]bool{}
		for _, r := range refs {
			if domains[r.Domain()] {
				t.Fatalf("verb %q is declared twice inside domain %q", verb, r.Domain())
			}
			domains[r.Domain()] = true
		}
	}
}

// TestTodo_MODEL_010_Golden pins the published catalog. A definition added,
// removed, renamed or re-familied without updating the golden fails here.
func TestTodo_MODEL_010_Golden(t *testing.T) {
	reg := mustRegistry(t)
	type row struct {
		Ref         string
		DisplayName string
		Family      string
		SideEffect  string
		EffectClass string
		Release     string
		Maturity    string
		Policy      string
	}
	rows := make([]row, 0, reg.Len())
	for _, d := range reg.Definitions() {
		rows = append(rows, row{
			Ref:         d.Ref.String(),
			DisplayName: d.DisplayName,
			Family:      d.Family.String(),
			SideEffect:  d.SideEffect.String(),
			EffectClass: d.EffectClass.String(),
			Release:     d.Release.String(),
			Maturity:    d.Maturity.String(),
			Policy:      d.NegativeStatePolicyRef,
		})
	}
	goldenJSON(t, "model_010_catalog.json", rows)
}

// TestTodo_MODEL_010_Race exercises concurrent resolution against the immutable
// registry. The registry exposes no mutator, and every accessor returns a copy,
// so concurrent readers cannot observe or cause a change.
func TestTodo_MODEL_010_Race(t *testing.T) {
	reg := mustRegistry(t)
	refs := reg.Refs()
	baseline := reg.Definitions()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ref := refs[i%len(refs)]
			d, err := reg.Resolve(ref)
			if err != nil {
				t.Errorf("concurrent resolve %s: %v", ref, err)
				return
			}
			// Mutating the returned copies must not reach the registry.
			d.DisplayName = "clobbered"
			d.SubjectKinds[0] = "CLOBBERED"
			_ = reg.LookupDisplayName(d.DisplayName)
			list := reg.Definitions()
			list[0].DisplayName = "clobbered"
		}(i)
	}
	wg.Wait()

	after := reg.Definitions()
	for i := range baseline {
		if baseline[i].DisplayName != after[i].DisplayName {
			t.Fatalf("%s display name changed under concurrent readers", baseline[i].Ref)
		}
	}
	if _, err := reg.ResolveText("hcmnext.people.promote_worker/v1"); err != nil {
		t.Fatalf("registry no longer resolves after concurrent access: %v", err)
	}
}

// FuzzTodo_MODEL_010 fuzzes definition-reference parsing. No input may produce
// a reference that is invalid, and every parsed reference must round-trip
// through its canonical text.
func FuzzTodo_MODEL_010(f *testing.F) {
	for _, seed := range []string{
		"hcmnext.people.promote_worker/v1",
		"hcmnext.access.grant_entitlement/v1",
		"promote_worker",
		"hcmnext.people.promote_worker/v0",
		"hcmnext.people.promote_worker/v01",
		"hcmnext..promote_worker/v1",
		"other.people.promote_worker/v1",
		"hcmnext.people.Promote_Worker/v1",
		"",
	} {
		f.Add(seed)
	}
	reg, err := definitions.NewRegistry()
	if err != nil {
		f.Fatalf("compile catalog: %v", err)
	}
	f.Fuzz(func(t *testing.T, s string) {
		ref, err := intent.ParseRef(s)
		if err != nil {
			// A rejected reference must never resolve.
			if _, resolveErr := reg.ResolveText(s); resolveErr == nil {
				t.Fatalf("%q failed to parse yet resolved", s)
			}
			return
		}
		if err := ref.Validate(); err != nil {
			t.Fatalf("ParseRef accepted %q but Validate rejects it: %v", s, err)
		}
		if got := ref.String(); got != s {
			t.Fatalf("round trip: parsed %q, rendered %q", s, got)
		}
		if ref.Domain() == "" {
			t.Fatalf("parsed %q has no domain", s)
		}
	})
}

// TestTodo_MODEL_015 is the PRIMARY test for negative-state policy
// registration.
//
// RED: compilation rejects a definition that declares an applicable
// UNKNOWN/PARTIAL/DEGRADED/AMBIGUOUS/REDACTED/UNAVAILABLE/STALE state with no
// policy deciding it.
//
// GREEN: a policy yields exactly one of the eight declared actions, with
// evidence, authority, expiry and revalidation.
func TestTodo_MODEL_015(t *testing.T) {
	t.Run("RED", func(t *testing.T) {
		for _, state := range intent.MandatoryNegativeStates() {
			t.Run(state.String(), func(t *testing.T) {
				policies := definitions.Policies()
				for i := range policies {
					delete(policies[i].Rules, state)
				}
				_, err := intent.NewRegistry(intent.ProfileBootstrap, definitions.All(),
					policies, definitions.Catalog())
				if !errors.Is(err, intent.ErrMissingNegativeStatePolicy) {
					t.Fatalf("registry accepted an applicable %s with no policy: %v", state, err)
				}
			})
		}

		t.Run("unresolved policy reference", func(t *testing.T) {
			defs := mutate("hcmnext.operations.detect_drift/v1", func(d *intent.Definition) {
				d.NegativeStatePolicyRef = "hcmnext.negative_state.nonexistent/v9"
			})
			if _, err := compile(defs); !errors.Is(err, intent.ErrMissingNegativeStatePolicy) {
				t.Fatalf("registry accepted an unresolved policy reference: %v", err)
			}
		})

		t.Run("action outside the eight", func(t *testing.T) {
			policies := definitions.Policies()
			rule := policies[0].Rules[intent.NegativeUnknown]
			rule.Action = intent.NegativeAction(99)
			policies[0].Rules[intent.NegativeUnknown] = rule
			_, err := intent.NewRegistry(intent.ProfileBootstrap, definitions.All(),
				policies, definitions.Catalog())
			if !errors.Is(err, intent.ErrInvalidNegativeStatePolicy) {
				t.Fatalf("registry accepted an action outside the eight: %v", err)
			}
		})

		t.Run("decision without evidence, authority or revalidation", func(t *testing.T) {
			for _, drop := range []string{"evidence", "authority", "revalidation"} {
				policies := definitions.Policies()
				rule := policies[0].Rules[intent.NegativeStale]
				switch drop {
				case "evidence":
					rule.EvidenceRef = ""
				case "authority":
					rule.AuthorityRef = ""
				case "revalidation":
					rule.RevalidationRef = ""
				}
				policies[0].Rules[intent.NegativeStale] = rule
				_, err := intent.NewRegistry(intent.ProfileBootstrap, definitions.All(),
					policies, definitions.Catalog())
				if !errors.Is(err, intent.ErrInvalidNegativeStatePolicy) {
					t.Fatalf("registry accepted a policy with no %s: %v", drop, err)
				}
			}
		})

		t.Run("USE_STALE with no expiry", func(t *testing.T) {
			policies := definitions.Policies()
			for i := range policies {
				rule := policies[i].Rules[intent.NegativeStale]
				if rule.Action != intent.ActionUseStale {
					continue
				}
				rule.ExpirySeconds = 0
				policies[i].Rules[intent.NegativeStale] = rule
			}
			_, err := intent.NewRegistry(intent.ProfileBootstrap, definitions.All(),
				policies, definitions.Catalog())
			if !errors.Is(err, intent.ErrInvalidNegativeStatePolicy) {
				t.Fatalf("registry accepted USE_STALE with no expiry: %v", err)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		reg := mustRegistry(t)
		valid := map[intent.NegativeAction]bool{}
		for _, a := range intent.NegativeActions() {
			valid[a] = true
		}
		if len(valid) != 8 {
			t.Fatalf("policy actions are the eight declared actions, got %d", len(valid))
		}
		for _, d := range reg.Definitions() {
			policy, err := reg.PolicyFor(d.Ref)
			if err != nil {
				t.Fatalf("%s: %v", d.Ref, err)
			}
			for _, state := range d.ApplicableNegativeStates {
				rule, err := policy.Decide(state)
				if err != nil {
					t.Fatalf("%s: %s is applicable but undecided: %v", d.Ref, state, err)
				}
				if !valid[rule.Action] {
					t.Fatalf("%s decides %s as %s, outside the eight actions",
						d.Ref, state, rule.Action)
				}
				if rule.EvidenceRef == "" || rule.AuthorityRef == "" {
					t.Fatalf("%s decides %s with no evidence or authority", d.Ref, state)
				}
				if rule.Action != intent.ActionBlock && rule.RevalidationRef == "" {
					t.Fatalf("%s decides %s as %s with no revalidation", d.Ref, state, rule.Action)
				}
			}
		}
	})

	t.Run("policies are referenced, not copied", func(t *testing.T) {
		reg := mustRegistry(t)
		refs := map[string]int{}
		for _, d := range reg.Definitions() {
			refs[d.NegativeStatePolicyRef]++
		}
		if len(refs) >= reg.Len() {
			t.Fatalf("every definition carries its own policy; shared policies should be referenced")
		}
	})
}

// TestTodo_MODEL_015_Property checks that no definition can encounter a
// mandatory negative state its policy leaves undecided.
func TestTodo_MODEL_015_Property(t *testing.T) {
	reg := mustRegistry(t)
	for _, d := range reg.Definitions() {
		policy, err := reg.PolicyFor(d.Ref)
		if err != nil {
			t.Fatalf("%s: %v", d.Ref, err)
		}
		for _, state := range intent.MandatoryNegativeStates() {
			if _, err := policy.Decide(state); err != nil {
				t.Fatalf("%s references %s, which leaves %s undecided",
					d.Ref, policy.Ref(), state)
			}
		}
	}
}

// TestTodo_MODEL_015_Golden pins the decision tables.
func TestTodo_MODEL_015_Golden(t *testing.T) {
	type row struct {
		Policy       string
		State        string
		Action       string
		Evidence     string
		Authority    string
		Expiry       uint32
		Revalidation string
	}
	var rows []row
	for _, p := range definitions.Policies() {
		for _, state := range p.States() {
			rule := p.Rules[state]
			rows = append(rows, row{
				Policy:       p.Ref(),
				State:        state.String(),
				Action:       rule.Action.String(),
				Evidence:     rule.EvidenceRef,
				Authority:    rule.AuthorityRef,
				Expiry:       rule.ExpirySeconds,
				Revalidation: rule.RevalidationRef,
			})
		}
	}
	goldenJSON(t, "model_015_policies.json", rows)
}

// TestTodo_MODEL_015_Fault proves the policy fails closed: an undecided state
// produces a typed error rather than a permissive default.
func TestTodo_MODEL_015_Fault(t *testing.T) {
	reg := mustRegistry(t)
	policy, err := reg.PolicyFor(intent.Ref{TypeID: "hcmnext.people.promote_worker", Version: 1})
	if err != nil {
		t.Fatalf("resolve policy: %v", err)
	}
	// QUARANTINED is a declared negative state that no P1A policy decides.
	rule, err := policy.Decide(intent.NegativeQuarantined)
	if err == nil {
		t.Fatalf("an undecided state produced action %s instead of an error", rule.Action)
	}
	if !errors.Is(err, intent.ErrMissingNegativeStatePolicy) {
		t.Fatalf("undecided state error = %v, want ErrMissingNegativeStatePolicy", err)
	}
	if rule.Action != intent.ActionUnspecified {
		t.Fatalf("a failed decision returned action %s; it must return nothing", rule.Action)
	}
}

// FuzzTodo_MODEL_015 fuzzes policy validation. No policy may validate while
// yielding an action outside the eight, and no validated policy may decide a
// state as anything but a declared action.
func FuzzTodo_MODEL_015(f *testing.F) {
	f.Add(uint8(1), uint8(1), uint32(60), true, true, true)
	f.Add(uint8(7), uint8(4), uint32(0), true, true, false)
	f.Add(uint8(9), uint8(9), uint32(0), false, false, false)
	f.Fuzz(func(t *testing.T, state, action uint8, expiry uint32, evidence, authority, revalidation bool) {
		rule := intent.NegativeStateRule{
			Action:        intent.NegativeAction(action),
			ExpirySeconds: expiry,
		}
		if evidence {
			rule.EvidenceRef = "evidence/v1"
		}
		if authority {
			rule.AuthorityRef = "authority/v1"
		}
		if revalidation {
			rule.RevalidationRef = "revalidate/v1"
		}
		p := intent.NegativeStatePolicy{
			ID:      "hcmnext.negative_state.fuzz",
			Version: 1,
			Rules:   map[intent.NegativeState]intent.NegativeStateRule{intent.NegativeState(state): rule},
		}
		err := p.Validate()
		if err == nil {
			if !intent.NegativeState(state).Valid() {
				t.Fatalf("validated a policy deciding an unspecified state")
			}
			if !rule.Action.Valid() {
				t.Fatalf("validated a policy yielding action %d, outside the eight", action)
			}
			if rule.Action == intent.ActionUseStale && rule.ExpirySeconds == 0 {
				t.Fatalf("validated USE_STALE with no expiry")
			}
			return
		}
		if !errors.Is(err, intent.ErrInvalidNegativeStatePolicy) {
			t.Fatalf("policy validation error %v is not typed", err)
		}
	})
}

// TestTodo_INTENT_001 is the PRIMARY test for IntentDefinition resolution.
//
// RED: a free-form name, an unqualified id, a wrong version, a definition
// below DRAFT_CONTRACT and a retired definition cannot instantiate.
//
// GREEN: an exact published definition_ref resolves schemas, family, phase,
// risk and capabilities.
func TestTodo_INTENT_001(t *testing.T) {
	reg := mustRegistry(t)

	t.Run("RED", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			text  string
			cause error
		}{
			{"free-form display name", "PromoteWorker", intent.ErrInvalidReference},
			{"unqualified id", "promote_worker/v1", intent.ErrInvalidReference},
			{"unrooted id", "acme.people.promote_worker/v1", intent.ErrInvalidReference},
			{"wrong version", "hcmnext.people.promote_worker/v2", intent.ErrUnknownDefinition},
			{"zero version", "hcmnext.people.promote_worker/v0", intent.ErrInvalidReference},
			{"no version at all", "hcmnext.people.promote_worker", intent.ErrInvalidReference},
			{"unknown definition", "hcmnext.people.teleport_worker/v1", intent.ErrUnknownDefinition},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := reg.ResolveText(tc.text); !errors.Is(err, tc.cause) {
					t.Fatalf("%q resolved or failed with %v, want %v", tc.text, err, tc.cause)
				}
			})
		}

		t.Run("below DRAFT_CONTRACT does not resolve", func(t *testing.T) {
			defs := mutate("hcmnext.operations.detect_drift/v1", func(d *intent.Definition) {
				d.Maturity = intent.MaturityUnspecified
			})
			if _, err := compile(defs); !errors.Is(err, intent.ErrInvalidDefinition) {
				t.Fatalf("a definition below DRAFT_CONTRACT compiled into the registry: %v", err)
			}
		})

		t.Run("retired definition cannot instantiate", func(t *testing.T) {
			defs := mutate("hcmnext.operations.detect_drift/v1", func(d *intent.Definition) {
				d.Maturity = intent.MaturityRetired
			})
			retiredReg, err := compile(defs)
			if err != nil {
				t.Fatalf("compile with a retired definition: %v", err)
			}
			ref := intent.Ref{TypeID: "hcmnext.operations.detect_drift", Version: 1}
			// Historical resolution is preserved.
			if _, err := retiredReg.Resolve(ref); err != nil {
				t.Fatalf("a retired definition must still resolve historically: %v", err)
			}
			// Invocation is prohibited.
			if _, err := retiredReg.ResolveForInstantiation(ref); !errors.Is(err, intent.ErrNotInvocable) {
				t.Fatalf("a retired definition was instantiable: %v", err)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		def, err := reg.ResolveText("hcmnext.people.promote_worker/v1")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if def.InputSchema.SchemaID != "hcmnext.people.v1.PromoteWorkerRequest" {
			t.Fatalf("input schema = %s", def.InputSchema)
		}
		if def.ResultSchema.SchemaID != "hcmnext.people.v1.PromoteWorkerResult" {
			t.Fatalf("result schema = %s", def.ResultSchema)
		}
		if def.Family != intent.FamilyChangeRequest {
			t.Fatalf("family = %s", def.Family)
		}
		if def.PhaseDepth == "" || def.RiskClass == "" {
			t.Fatalf("phase depth and risk class must resolve, got %q / %q",
				def.PhaseDepth, def.RiskClass)
		}
		if len(def.RequiredCapabilities) == 0 {
			t.Fatalf("capabilities must resolve")
		}
		if _, err := reg.ResolveForInstantiation(def.Ref); err != nil {
			t.Fatalf("a DRAFT_CONTRACT definition must be instantiable: %v", err)
		}
	})

	t.Run("display labels are presentation only", func(t *testing.T) {
		defs := mutate("hcmnext.people.promote_worker/v1", func(d *intent.Definition) {
			d.DisplayName = "Advance Worker"
		})
		renamed, err := compile(defs)
		if err != nil {
			t.Fatalf("compile after rename: %v", err)
		}
		def, err := renamed.ResolveText("hcmnext.people.promote_worker/v1")
		if err != nil {
			t.Fatalf("identity changed with the display label: %v", err)
		}
		if def.DisplayName != "Advance Worker" {
			t.Fatalf("display name = %q", def.DisplayName)
		}
	})
}

// TestTodo_INTENT_001_Golden pins what a resolution returns, so a silent change
// to a definition's resolved contract shows up as a diff.
func TestTodo_INTENT_001_Golden(t *testing.T) {
	reg := mustRegistry(t)
	type resolved struct {
		Ref                  string
		Family               string
		PhaseDepth           string
		RiskClass            string
		InputSchema          string
		ResultSchema         string
		RequiredCapabilities []string
		AllowedInitiators    []string
		AllowedModes         []string
	}
	var rows []resolved
	for _, d := range reg.Definitions() {
		row := resolved{
			Ref:                  d.Ref.String(),
			Family:               d.Family.String(),
			PhaseDepth:           d.PhaseDepth,
			RiskClass:            d.RiskClass,
			InputSchema:          d.InputSchema.String(),
			ResultSchema:         d.ResultSchema.String(),
			RequiredCapabilities: d.RequiredCapabilities,
		}
		for _, i := range d.AllowedInitiators {
			row.AllowedInitiators = append(row.AllowedInitiators, i.String())
		}
		for _, m := range d.AllowedModes {
			row.AllowedModes = append(row.AllowedModes, m.String())
		}
		rows = append(rows, row)
	}
	goldenJSON(t, "intent_001_resolution.json", rows)
}

// TestTodo_MODEL_016 is the PRIMARY test for binding each definition to exact
// model behaviour.
//
// RED: the coverage checker reports a definition missing its subject/root,
// property read or write, decision, evidence, effect, a lifecycle transition on
// any of the five dimensions, its negative policy, or its scenario.
//
// GREEN: every definition binds to the covered entity set and the report is
// "N/N BOUND", where N is the checked-in definition count and nothing else.
func TestTodo_MODEL_016(t *testing.T) {
	reg := mustRegistry(t)

	t.Run("RED", func(t *testing.T) {
		target := intent.Ref{TypeID: "hcmnext.people.promote_worker", Version: 1}
		cases := []struct {
			name    string
			break_  func(*intent.Binding)
			element string
		}{
			{"missing aggregate root", func(b *intent.Binding) { b.AggregateRoots = nil }, "aggregate_roots"},
			{"root outside the covered set", func(b *intent.Binding) {
				b.AggregateRoots = append(b.AggregateRoots, "LearningEnrollment")
			}, "aggregate_roots"},
			{"missing property read", func(b *intent.Binding) { b.ReadProperties = nil }, "read_properties"},
			{"missing property write", func(b *intent.Binding) {
				b.WriteProperties = nil
				b.ChildDefinitions = nil
			}, "write_properties"},
			{"missing decision", func(b *intent.Binding) { b.DecisionRefs = nil }, "decisions"},
			{"missing evidence", func(b *intent.Binding) { b.EvidenceRefs = nil }, "evidence"},
			{"effect on a zero-effect definition", func(b *intent.Binding) {
				b.EffectRefs = []string{"effect.write_assignment/v1"}
			}, "effects"},
			{"missing negative-state policy", func(b *intent.Binding) { b.NegativeStatePolicyRef = "" }, "negative_state_policy"},
			{"missing scenario", func(b *intent.Binding) { b.ScenarioRefs = nil }, "scenarios"},
		}
		for _, dim := range lifecycle.AllDimensions() {
			d := dim
			cases = append(cases, struct {
				name    string
				break_  func(*intent.Binding)
				element string
			}{
				name:    fmt.Sprintf("missing transition on %s", d),
				break_:  func(b *intent.Binding) { delete(b.Transitions, d) },
				element: "transitions",
			})
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				bindings := definitions.Bindings()
				for i := range bindings {
					if bindings[i].Definition == target {
						// Copy the transition map so sibling cases are unaffected.
						cloned := map[lifecycle.Dimension][]lifecycle.TransitionRule{}
						for k, v := range bindings[i].Transitions {
							cloned[k] = v
						}
						bindings[i].Transitions = cloned
						tc.break_(&bindings[i])
					}
				}
				report := intent.CheckCoverage(reg, bindings)
				if report.FullyBound() {
					t.Fatalf("coverage reported fully bound despite a broken %s binding", tc.element)
				}
				found := false
				for _, g := range report.Gaps {
					if g.Definition == target && g.Element == tc.element {
						found = true
					}
				}
				if !found {
					t.Fatalf("coverage did not report a %s gap for %s; gaps: %v",
						tc.element, target, report.Gaps)
				}
			})
		}

		t.Run("definition with no binding at all", func(t *testing.T) {
			report := intent.CheckCoverage(reg, nil)
			if report.Bound != 0 || len(report.Gaps) != reg.Len() {
				t.Fatalf("with no bindings the report should be all gaps, got %s", report.Summary())
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		report := definitions.Coverage(reg)
		if !report.FullyBound() {
			t.Fatalf("coverage is not complete:\n%s", report.Summary())
		}
		if report.Total != reg.Len() {
			t.Fatalf("report denominator is %d, registry publishes %d", report.Total, reg.Len())
		}
		want := fmt.Sprintf("%d/%d BOUND", reg.Len(), reg.Len())
		if got := report.Summary(); got != want {
			t.Fatalf("summary = %q, want %q", got, want)
		}
	})
}

// TestTodo_MODEL_016_Property asserts that every binding names only covered
// entities and reuses the definition's own negative-state policy reference.
func TestTodo_MODEL_016_Property(t *testing.T) {
	reg := mustRegistry(t)
	covered := map[string]bool{}
	for _, e := range intent.CoveredEntities() {
		covered[e] = true
	}
	for _, b := range definitions.Bindings() {
		def, err := reg.Resolve(b.Definition)
		if err != nil {
			t.Fatalf("binding names unknown definition %s: %v", b.Definition, err)
		}
		for _, root := range b.AggregateRoots {
			if !covered[root] {
				t.Fatalf("%s binds %q, outside the covered entity set", b.Definition, root)
			}
		}
		if b.NegativeStatePolicyRef != def.NegativeStatePolicyRef {
			t.Fatalf("%s binds policy %q but the definition references %q",
				b.Definition, b.NegativeStatePolicyRef, def.NegativeStatePolicyRef)
		}
		for _, dim := range lifecycle.AllDimensions() {
			if len(b.Transitions[dim]) == 0 {
				t.Fatalf("%s declares no transition on %s", b.Definition, dim)
			}
		}
	}
}

// TestTodo_MODEL_016_Golden pins the coverage summary and every binding's
// aggregate roots.
func TestTodo_MODEL_016_Golden(t *testing.T) {
	reg := mustRegistry(t)
	type row struct {
		Ref            string
		AggregateRoots []string
		Reads          []string
		Writes         []string
		Effects        []string
		Scenarios      []string
	}
	var rows []row
	for _, b := range definitions.Bindings() {
		rows = append(rows, row{
			Ref:            b.Definition.String(),
			AggregateRoots: b.AggregateRoots,
			Reads:          b.ReadProperties,
			Writes:         b.WriteProperties,
			Effects:        b.EffectRefs,
			Scenarios:      b.ScenarioRefs,
		})
	}
	goldenJSON(t, "model_016_bindings.json", struct {
		Summary  string
		Bindings []row
	}{
		Summary:  definitions.Coverage(reg).Summary(),
		Bindings: rows,
	})
}

// TestTodo_MODEL_016_Race runs the coverage checker concurrently and asserts a
// single stable answer: the checker reads immutable inputs and holds no state.
func TestTodo_MODEL_016_Race(t *testing.T) {
	reg := mustRegistry(t)
	want := definitions.Coverage(reg).Summary()

	var wg sync.WaitGroup
	results := make([]string, 16)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = intent.CheckCoverage(reg, definitions.Bindings()).Summary()
		}(i)
	}
	wg.Wait()
	for i, got := range results {
		if got != want {
			t.Fatalf("concurrent coverage run %d = %q, want %q", i, got, want)
		}
	}
}

// TestTodo_MODEL_016_Mutation is the mutation-style check: it perturbs the
// checked-in table one definition at a time and requires the coverage checker
// to notice every perturbation. A checker that passes everything is worthless,
// so this test fails if any mutation survives.
func TestTodo_MODEL_016_Mutation(t *testing.T) {
	reg := mustRegistry(t)
	mutations := []struct {
		name   string
		mutate func(*intent.Binding)
	}{
		{"drop the aggregate roots", func(b *intent.Binding) { b.AggregateRoots = nil }},
		{"drop the reads", func(b *intent.Binding) { b.ReadProperties = nil }},
		{"drop the decisions", func(b *intent.Binding) { b.DecisionRefs = nil }},
		{"drop the evidence", func(b *intent.Binding) { b.EvidenceRefs = nil }},
		{"drop the scenarios", func(b *intent.Binding) { b.ScenarioRefs = nil }},
	}
	for _, m := range mutations {
		for _, target := range reg.Refs() {
			bindings := definitions.Bindings()
			for i := range bindings {
				if bindings[i].Definition == target {
					m.mutate(&bindings[i])
				}
			}
			if intent.CheckCoverage(reg, bindings).FullyBound() {
				t.Fatalf("mutation %q on %s survived the coverage checker", m.name, target)
			}
		}
	}
}

// FuzzTodo_MODEL_016 fuzzes the coverage checker by dropping an arbitrary
// subset of one binding's elements. Dropping nothing must report fully bound;
// dropping anything must not.
func FuzzTodo_MODEL_016(f *testing.F) {
	f.Add(0, uint8(0))
	f.Add(2, uint8(1))
	f.Add(7, uint8(0xff))
	f.Add(13, uint8(0x2a))
	reg, err := definitions.NewRegistry()
	if err != nil {
		f.Fatalf("compile catalog: %v", err)
	}
	f.Fuzz(func(t *testing.T, index int, drop uint8) {
		bindings := definitions.Bindings()
		if index < 0 {
			index = -index
		}
		index %= len(bindings)
		b := &bindings[index]

		dropped := false
		if drop&1 != 0 {
			b.AggregateRoots, dropped = nil, true
		}
		if drop&2 != 0 {
			b.ReadProperties, dropped = nil, true
		}
		if drop&4 != 0 {
			b.DecisionRefs, dropped = nil, true
		}
		if drop&8 != 0 {
			b.EvidenceRefs, dropped = nil, true
		}
		if drop&16 != 0 {
			b.ScenarioRefs, dropped = nil, true
		}
		if drop&32 != 0 {
			b.NegativeStatePolicyRef, dropped = "", true
		}
		if drop&64 != 0 {
			b.Transitions, dropped = nil, true
		}
		if drop&128 != 0 && (len(b.WriteProperties) > 0 || len(b.ChildDefinitions) > 0) {
			b.WriteProperties, b.ChildDefinitions, dropped = nil, nil, true
		}

		report := intent.CheckCoverage(reg, bindings)
		if report.Total != reg.Len() {
			t.Fatalf("denominator drifted to %d", report.Total)
		}
		if dropped && report.FullyBound() {
			t.Fatalf("dropping elements of %s still reported %s",
				b.Definition, report.Summary())
		}
		if !dropped && !report.FullyBound() {
			t.Fatalf("an unmodified catalog reported gaps:\n%s", report.Summary())
		}
		if !report.FullyBound() && report.Bound >= report.Total {
			t.Fatalf("a gapped report claims %d/%d bound", report.Bound, report.Total)
		}
	})
}

// TestRegistryHandsOutDeepCopies pins the value-semantics contract
// TestTodo_MODEL_010_Race assumes but, before Definition.clone existed, only
// checked for the scalar DisplayName: every accessor that returns a Definition
// must return one that shares no mutable memory with the registry.
//
// This is the non-race half of the proof, so it fails on any platform rather
// than only where the race detector runs. The race half stays in
// TestTodo_MODEL_010_Race, which writes to the same fields from 16 goroutines.
func TestRegistryHandsOutDeepCopies(t *testing.T) {
	reg := mustRegistry(t)
	refs := reg.Refs()

	var ref intent.Ref
	for _, candidate := range refs {
		d, err := reg.Resolve(candidate)
		if err != nil {
			continue
		}
		if len(d.SubjectKinds) > 0 && len(d.RequiredCapabilities) > 0 {
			ref = candidate
			break
		}
	}
	if (ref == intent.Ref{}) {
		t.Skip("no published definition carries both subject kinds and required capabilities")
	}

	first, err := reg.Resolve(ref)
	if err != nil {
		t.Fatalf("Resolve(%s): %v", ref, err)
	}
	wantSubject := first.SubjectKinds[0]
	wantCapability := first.RequiredCapabilities[0]

	first.SubjectKinds[0] = "CLOBBERED"
	first.RequiredCapabilities[0] = "CLOBBERED"
	if first.PopulationScope != nil {
		first.PopulationScope.ScopeRef = "CLOBBERED"
	}
	for dimension, rules := range first.AllowedTransitions {
		if len(rules) > 0 {
			rules[0] = lifecycle.TransitionRule{}
			first.AllowedTransitions[dimension] = rules
		}
		break
	}

	second, err := reg.Resolve(ref)
	if err != nil {
		t.Fatalf("Resolve(%s) after mutation: %v", ref, err)
	}
	if got := second.SubjectKinds[0]; got != wantSubject {
		t.Errorf("SubjectKinds[0] = %q after a caller wrote to an earlier copy, want %q", got, wantSubject)
	}
	if got := second.RequiredCapabilities[0]; got != wantCapability {
		t.Errorf("RequiredCapabilities[0] = %q after a caller wrote to an earlier copy, want %q", got, wantCapability)
	}

	all := reg.Definitions()
	if len(all) == 0 {
		t.Fatal("Definitions() is empty")
	}
	for i := range all {
		if len(all[i].SubjectKinds) > 0 {
			kind := all[i].SubjectKinds[0]
			all[i].SubjectKinds[0] = "CLOBBERED"
			if again := reg.Definitions(); again[i].SubjectKinds[0] != kind {
				t.Errorf("Definitions()[%d].SubjectKinds[0] = %q after a caller wrote to an earlier copy, want %q",
					i, again[i].SubjectKinds[0], kind)
			}
			break
		}
	}
}
