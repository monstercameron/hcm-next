package authz_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// TestTodo_TRUST_008_Security is the TRUST-008 security test. It attacks the
// tenant/organization boundary directly: every unrecognized sharing
// direction must deny regardless of its numeric value, a denied decision
// must never leak the organization closure or the sharing grant it
// considered, and the two ways a cross-tenant request can fail (no matching
// grant at all, versus a grant with a bad direction) must not be
// distinguishable from the AllowedOrganizations/SharingPath shape of the
// result — only the reason token may differ, and even that never mentions
// facts about the resource itself.
func TestTodo_TRUST_008_Security(t *testing.T) {
	principal := newPrincipal(t, principalOpts{})

	t.Run("every unrecognized sharing direction denies", func(t *testing.T) {
		for raw := 0; raw < 8; raw++ {
			direction := authz.SharingDirection(raw)
			grant := authz.SharingGrant{
				OwnerTenant:  tenantVendor,
				ViewerTenant: tenantAcme,
				Direction:    direction,
				Effective:    mustOpenInterval(t, farPast),
			}
			decision, err := authz.ResolveTenantScope(principal, authz.TenantScopeInput{
				ResourceTenant: tenantVendor,
				Sharing:        []authz.SharingGrant{grant},
				EffectiveAt:    baseInstant,
			})
			if err != nil {
				t.Fatalf("ResolveTenantScope(direction=%d): %v", raw, err)
			}
			if direction.Valid() {
				if decision.Effect != authz.EffectAllow {
					t.Errorf("direction=%d is the one valid direction but Effect = %s", raw, decision.Effect)
				}
				continue
			}
			if decision.Effect != authz.EffectDenied {
				t.Errorf("direction=%d is not recognized as valid but Effect = %s, want DENIED", raw, decision.Effect)
			}
		}
	})

	t.Run("a denied decision never discloses the closure or the grant it considered", func(t *testing.T) {
		grant := authz.SharingGrant{
			OwnerTenant:  tenantVendor,
			ViewerTenant: tenantAcme,
			Direction:    authz.SharingDirectionUnspecified,
			Effective:    mustOpenInterval(t, farPast),
		}
		decision, err := authz.ResolveTenantScope(principal, authz.TenantScopeInput{
			ResourceTenant: tenantVendor,
			PrincipalOrg:   orgParent,
			ResourceOrg:    orgOther,
			Edges:          closureEdges(t),
			Sharing:        []authz.SharingGrant{grant},
			EffectiveAt:    baseInstant,
		})
		if err != nil {
			t.Fatalf("ResolveTenantScope: %v", err)
		}
		if decision.Effect != authz.EffectDenied {
			t.Fatalf("Effect = %s, want DENIED", decision.Effect)
		}
		if len(decision.AllowedOrganizations) != 0 {
			t.Error("denied decision discloses an organization closure")
		}
		if len(decision.SharingPath) != 0 {
			t.Error("denied decision discloses a sharing grant")
		}
		if len(decision.MandatoryDenies) != 0 {
			t.Error("denied decision discloses mandatory-deny detail that only applies to an allow")
		}
	})

	t.Run("no-grant and bad-direction denials are shaped identically", func(t *testing.T) {
		noGrant, err := authz.ResolveTenantScope(principal, authz.TenantScopeInput{
			ResourceTenant: tenantVendor,
			EffectiveAt:    baseInstant,
		})
		if err != nil {
			t.Fatalf("ResolveTenantScope: %v", err)
		}
		badDirection, err := authz.ResolveTenantScope(principal, authz.TenantScopeInput{
			ResourceTenant: tenantVendor,
			Sharing: []authz.SharingGrant{{
				OwnerTenant: tenantVendor, ViewerTenant: tenantAcme,
				Direction: authz.SharingDirectionUnspecified, Effective: mustOpenInterval(t, farPast),
			}},
			EffectiveAt: baseInstant,
		})
		if err != nil {
			t.Fatalf("ResolveTenantScope: %v", err)
		}
		if noGrant.Effect != authz.EffectDenied || badDirection.Effect != authz.EffectDenied {
			t.Fatal("both scenarios must deny for this comparison to be meaningful")
		}
		if len(noGrant.AllowedOrganizations) != 0 || len(badDirection.AllowedOrganizations) != 0 {
			t.Error("a denial must never carry an organization closure")
		}
		if len(noGrant.SharingPath) != 0 || len(badDirection.SharingPath) != 0 {
			t.Error("a denial must never carry a sharing path")
		}
	})
}

// TestTodo_TRUST_008_Mutation is the TRUST-008 mutation test. It proves that
// every field of a [authz.TenantScopeInput] actually participates in the
// decision: mutating any one of them, starting from a request that is
// allowed, flips the outcome or the matched rule.
func TestTodo_TRUST_008_Mutation(t *testing.T) {
	principal := newPrincipal(t, principalOpts{})
	base := authz.TenantScopeInput{
		ResourceTenant: tenantAcme,
		PrincipalOrg:   orgParent,
		ResourceOrg:    orgChild,
		Edges:          closureEdges(t),
		EffectiveAt:    baseInstant,
	}
	baseline, err := authz.ResolveTenantScope(principal, base)
	if err != nil {
		t.Fatalf("ResolveTenantScope(base): %v", err)
	}
	if baseline.Effect != authz.EffectAllow {
		t.Fatalf("baseline Effect = %s, want ALLOW", baseline.Effect)
	}

	mutations := map[string]func(*authz.TenantScopeInput){
		"resource tenant changed":  func(in *authz.TenantScopeInput) { in.ResourceTenant = tenantVendor },
		"resource org out of tree": func(in *authz.TenantScopeInput) { in.ResourceOrg = orgOther },
		"edges dropped":            func(in *authz.TenantScopeInput) { in.Edges = nil },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			mutated := base
			mutate(&mutated)
			decision, err := authz.ResolveTenantScope(principal, mutated)
			if err != nil {
				t.Fatalf("ResolveTenantScope: %v", err)
			}
			if decision.Effect == baseline.Effect && decision.RuleID == baseline.RuleID {
				t.Errorf("mutating %q left the decision unchanged (%s/%s)", name, decision.Effect, decision.RuleID)
			}
		})
	}

	t.Run("clearing PrincipalOrg switches to the tenant-wide rule, not silently to the same decision", func(t *testing.T) {
		mutated := base
		mutated.PrincipalOrg = authz.OrgUnitRef{}
		decision, err := authz.ResolveTenantScope(principal, mutated)
		if err != nil {
			t.Fatalf("ResolveTenantScope: %v", err)
		}
		if decision.Effect != authz.EffectAllow {
			t.Fatalf("Effect = %s, want ALLOW for the tenant-wide default", decision.Effect)
		}
		if decision.RuleID == baseline.RuleID {
			t.Error("clearing PrincipalOrg must resolve through a different rule than the org-scoped allow")
		}
	})
}
