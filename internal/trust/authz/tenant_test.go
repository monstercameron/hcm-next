package authz_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/trust/authz"
)

var (
	orgParent = authz.OrgUnitRef{Tenant: tenantAcme, ID: "org-parent"}
	orgChild  = authz.OrgUnitRef{Tenant: tenantAcme, ID: "org-child"}
	orgOther  = authz.OrgUnitRef{Tenant: tenantAcme, ID: "org-other"}
)

func closureEdges(t *testing.T) []authz.OrgEdge {
	t.Helper()
	return []authz.OrgEdge{
		{Child: orgChild, Parent: orgParent, Effective: mustOpenInterval(t, farPast)},
	}
}

// TestTodo_TRUST_008 is the TRUST-008 primary test: resolving tenant and
// organization scope from the principal alone, with cross-tenant access
// impossible by construction.
func TestTodo_TRUST_008(t *testing.T) {
	t.Run("same tenant, no organization scoping resolves tenant-wide", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{})
		decision, err := authz.ResolveTenantScope(principal, authz.TenantScopeInput{
			ResourceTenant: tenantAcme,
			EffectiveAt:    baseInstant,
		})
		if err != nil {
			t.Fatalf("ResolveTenantScope: %v", err)
		}
		if decision.Effect != authz.EffectAllow {
			t.Fatalf("Effect = %s, want ALLOW", decision.Effect)
		}
		if decision.AllowedOrganizations != nil {
			t.Errorf("AllowedOrganizations = %v, want nil for a tenant-wide default", decision.AllowedOrganizations)
		}
		if decision.PolicyVersion != authz.PolicyVersion {
			t.Errorf("PolicyVersion = %q, want %q", decision.PolicyVersion, authz.PolicyVersion)
		}
		if decision.RuleID == "" {
			t.Error("RuleID is empty")
		}
	})

	t.Run("same tenant, resource organization within the principal's closure allows", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{})
		decision, err := authz.ResolveTenantScope(principal, authz.TenantScopeInput{
			ResourceTenant: tenantAcme,
			PrincipalOrg:   orgParent,
			ResourceOrg:    orgChild,
			Edges:          closureEdges(t),
			EffectiveAt:    baseInstant,
		})
		if err != nil {
			t.Fatalf("ResolveTenantScope: %v", err)
		}
		if decision.Effect != authz.EffectAllow {
			t.Fatalf("Effect = %s, want ALLOW", decision.Effect)
		}
		found := false
		for _, o := range decision.AllowedOrganizations {
			if o == orgChild {
				found = true
			}
		}
		if !found {
			t.Errorf("AllowedOrganizations = %v, want it to contain the descendant %v", decision.AllowedOrganizations, orgChild)
		}
	})

	t.Run("same tenant, unauthorized subsidiary denies without existence disclosure", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{})
		decision, err := authz.ResolveTenantScope(principal, authz.TenantScopeInput{
			ResourceTenant: tenantAcme,
			PrincipalOrg:   orgParent,
			ResourceOrg:    orgOther,
			Edges:          closureEdges(t),
			EffectiveAt:    baseInstant,
		})
		if err != nil {
			t.Fatalf("ResolveTenantScope: %v", err)
		}
		if decision.Effect != authz.EffectDenied {
			t.Fatalf("Effect = %s, want DENIED", decision.Effect)
		}
		if decision.Reason == "" {
			t.Error("a denial must carry a reason token")
		}
		if len(decision.AllowedOrganizations) != 0 {
			t.Errorf("a denied decision discloses an organization closure: %v", decision.AllowedOrganizations)
		}
	})

	t.Run("cross-tenant resource denies without a sharing grant", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{})
		decision, err := authz.ResolveTenantScope(principal, authz.TenantScopeInput{
			ResourceTenant: tenantVendor,
			EffectiveAt:    baseInstant,
		})
		if err != nil {
			t.Fatalf("ResolveTenantScope: %v", err)
		}
		if decision.Effect != authz.EffectDenied {
			t.Fatalf("Effect = %s, want DENIED", decision.Effect)
		}
		if len(decision.SharingPath) != 0 {
			t.Errorf("SharingPath = %v, want empty for a denied cross-tenant decision", decision.SharingPath)
		}
	})

	t.Run("invalid shared-resource direction denies", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{})
		grant := authz.SharingGrant{
			OwnerTenant:  tenantVendor,
			ViewerTenant: tenantAcme,
			Direction:    authz.SharingDirectionUnspecified,
			Effective:    mustOpenInterval(t, farPast),
		}
		decision, err := authz.ResolveTenantScope(principal, authz.TenantScopeInput{
			ResourceTenant: tenantVendor,
			Sharing:        []authz.SharingGrant{grant},
			EffectiveAt:    baseInstant,
		})
		if err != nil {
			t.Fatalf("ResolveTenantScope: %v", err)
		}
		if decision.Effect != authz.EffectDenied {
			t.Fatalf("Effect = %s, want DENIED for an invalid sharing direction", decision.Effect)
		}
		if decision.Reason != "invalid_sharing_direction" {
			t.Errorf("Reason = %q, want %q", decision.Reason, "invalid_sharing_direction")
		}
	})

	t.Run("a valid active cross-tenant grant allows under a mandatory sensitive-domain deny", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{})
		grant := authz.SharingGrant{
			OwnerTenant:  tenantVendor,
			ViewerTenant: tenantAcme,
			Direction:    authz.SharingDirectionRecordVisible,
			Effective:    mustOpenInterval(t, farPast),
		}
		decision, err := authz.ResolveTenantScope(principal, authz.TenantScopeInput{
			ResourceTenant: tenantVendor,
			Sharing:        []authz.SharingGrant{grant},
			EffectiveAt:    baseInstant,
		})
		if err != nil {
			t.Fatalf("ResolveTenantScope: %v", err)
		}
		if decision.Effect != authz.EffectAllow {
			t.Fatalf("Effect = %s, want ALLOW", decision.Effect)
		}
		if len(decision.SharingPath) != 1 || decision.SharingPath[0] != grant {
			t.Errorf("SharingPath = %v, want [%v]", decision.SharingPath, grant)
		}
		if len(decision.MandatoryDenies) == 0 {
			t.Fatal("a cross-tenant allow must carry the mandatory sensitive-domain deny")
		}
	})

	t.Run("an inactive sharing grant denies as if it did not exist", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{})
		grant := authz.SharingGrant{
			OwnerTenant:  tenantVendor,
			ViewerTenant: tenantAcme,
			Direction:    authz.SharingDirectionRecordVisible,
			Effective:    mustInterval(t, farPast, recentPast),
		}
		decision, err := authz.ResolveTenantScope(principal, authz.TenantScopeInput{
			ResourceTenant: tenantVendor,
			Sharing:        []authz.SharingGrant{grant},
			EffectiveAt:    baseInstant,
		})
		if err != nil {
			t.Fatalf("ResolveTenantScope: %v", err)
		}
		if decision.Effect != authz.EffectDenied {
			t.Fatalf("Effect = %s, want DENIED for a grant that expired before EffectiveAt", decision.Effect)
		}
	})

	t.Run("inherited mandatory deny cannot be bypassed by a role that would otherwise be granted", func(t *testing.T) {
		// A comp admin who reaches a cross-tenant worker through a valid
		// sharing grant must still be denied compensation data: the
		// mandatory cross-tenant restriction is non-delegable, and no
		// per-field role grant can widen past it.
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authz.PurposeCompensationReview}})
		grant := authz.SharingGrant{
			OwnerTenant:  tenantVendor,
			ViewerTenant: tenantAcme,
			Direction:    authz.SharingDirectionRecordVisible,
			Effective:    mustOpenInterval(t, farPast),
		}
		tenantDecision, err := authz.ResolveTenantScope(principal, authz.TenantScopeInput{
			ResourceTenant: tenantVendor,
			Sharing:        []authz.SharingGrant{grant},
			EffectiveAt:    baseInstant,
		})
		if err != nil {
			t.Fatalf("ResolveTenantScope: %v", err)
		}
		if tenantDecision.Effect != authz.EffectAllow {
			t.Fatalf("tenant scope Effect = %s, want ALLOW so the bypass attempt is meaningful", tenantDecision.Effect)
		}

		fields, err := authz.ResolveFields(principal, authz.PurposeCompensationReview, []authz.FieldID{authz.FieldBaseSalary}, tenantDecision.MandatoryDenies)
		if err != nil {
			t.Fatalf("ResolveFields: %v", err)
		}
		ruling := fields.Rulings[authz.FieldBaseSalary]
		if ruling.Effect != authz.EffectDenied {
			t.Fatalf("comp admin bypassed the mandatory cross-tenant deny: field effect = %s, want DENIED", ruling.Effect)
		}
	})
}
