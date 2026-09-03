package authz_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust/authz"
)

// TestTodo_TRUST_012_Security is the TRUST-012 security test. The gate is
// the data layer's last line of defense, so its failure modes must be
// invisible: a caller who bypasses the planner gets nothing, a denied scope
// is indistinguishable from an empty store, an unauthorized subject is
// dropped without a trace in the response shape, and a redacted value never
// appears in any form.
func TestTodo_TRUST_012_Security(t *testing.T) {
	managed := workerSubject(tenantAcme, subjectWorkerID)
	unmanaged := workerSubject(tenantAcme, subjectOtherID)
	foreign := workerSubject(tenantVendor, subjectOtherID)

	full := authz.NewRepositoryGate(map[values.EntityRef]map[authz.FieldID]string{
		managed:   {authz.FieldWorkerNumber: "W-0001", authz.FieldBaseSalary: "120000"},
		unmanaged: {authz.FieldWorkerNumber: "W-0002"},
		foreign:   {authz.FieldWorkerNumber: "V-0001"},
	})
	empty := authz.NewRepositoryGate(nil)

	t.Run("an unavailable nil repository gate fails closed", func(t *testing.T) {
		var unavailable *authz.RepositoryGate
		projections, err := unavailable.Query(authz.RepositoryScope{}, nil, baseInstant)
		if !errors.Is(err, authz.ErrScopeRequired) {
			t.Fatalf("nil gate Query error = %v, want ErrScopeRequired", err)
		}
		if projections != nil {
			t.Fatalf("nil gate Query projections = %v, want nil", projections)
		}
	})

	t.Run("a denied scope is indistinguishable from an empty store", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}, purposes: []string{authz.PurposeCompensationReview}})
		scope, err := authz.PlanRepositoryScope(authz.RepositoryQueryRequest{
			Principal:   principal,
			EffectiveAt: baseInstant,
			Tenant:      tenantAcme,
			Candidates:  []authz.ScopeInput{{Subject: unmanaged}},
			Fields:      []authz.FieldID{authz.FieldWorkerNumber},
		})
		if err != nil {
			t.Fatalf("PlanRepositoryScope: %v", err)
		}
		// The scope is allow at the tenant stage but the candidate is not
		// managed: zero authorized subjects. The gate's answer must not
		// depend on whether the store holds the record.
		for _, gate := range []*authz.RepositoryGate{full, empty} {
			projections, err := gate.Query(scope, []values.EntityRef{unmanaged}, baseInstant)
			if err != nil {
				t.Fatalf("gate.Query: %v", err)
			}
			if len(projections) != 0 {
				t.Fatalf("gate returned %d projections for an unauthorized subject, want 0", len(projections))
			}
		}
	})

	t.Run("an unauthorized or foreign subject is dropped without changing the response shape", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}, purposes: []string{authz.PurposeCompensationReview}})
		scope, err := authz.PlanRepositoryScope(authz.RepositoryQueryRequest{
			Principal:   principal,
			EffectiveAt: baseInstant,
			Tenant:      tenantAcme,
			Candidates:  []authz.ScopeInput{{Subject: managed, Relationships: []authz.RelationshipFact{managerFact(managed)}}},
			Fields:      []authz.FieldID{authz.FieldWorkerNumber},
		})
		if err != nil {
			t.Fatalf("PlanRepositoryScope: %v", err)
		}
		projections, err := full.Query(scope, []values.EntityRef{managed, unmanaged, foreign}, baseInstant)
		if err != nil {
			t.Fatalf("gate.Query: %v", err)
		}
		if len(projections) != 1 || projections[0].Subject != managed {
			t.Fatalf("gate returned %v, want only the managed subject", projections)
		}
		for _, p := range projections {
			if p.Subject.Tenant != tenantAcme {
				t.Errorf("projection leaks a foreign-tenant subject: %s", p.Subject.String())
			}
		}
	})

	t.Run("a redacted field never carries its raw value", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleAuditor)}, purposes: []string{authz.PurposeAuditReview}})
		scope, err := authz.PlanRepositoryScope(authz.RepositoryQueryRequest{
			Principal:   principal,
			EffectiveAt: baseInstant,
			Tenant:      tenantAcme,
			Candidates:  []authz.ScopeInput{{Subject: managed}},
			Fields:      []authz.FieldID{authz.FieldWorkerNumber, authz.FieldBaseSalary},
		})
		if err != nil {
			t.Fatalf("PlanRepositoryScope: %v", err)
		}
		projections, err := full.Query(scope, []values.EntityRef{managed}, baseInstant)
		if err != nil {
			t.Fatalf("gate.Query: %v", err)
		}
		for _, p := range projections {
			for _, fv := range p.Fields {
				if fv.FieldID == authz.FieldBaseSalary && (fv.Effect != authz.EffectRedacted || fv.Value != authz.RedactedPlaceholder) {
					t.Errorf("redacted field = %+v, want the placeholder only", fv)
				}
			}
		}
	})

	t.Run("a field outside the evaluated mask is denied, not defaulted", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}, purposes: []string{authz.PurposeCompensationReview}})
		scope, err := authz.PlanRepositoryScope(authz.RepositoryQueryRequest{
			Principal:   principal,
			EffectiveAt: baseInstant,
			Tenant:      tenantAcme,
			Candidates:  []authz.ScopeInput{{Subject: managed, Relationships: []authz.RelationshipFact{managerFact(managed)}}},
			Fields:      []authz.FieldID{authz.FieldWorkerNumber},
		})
		if err != nil {
			t.Fatalf("PlanRepositoryScope: %v", err)
		}
		if ruling := scope.FieldRuling(authz.FieldBaseSalary); ruling.Effect != authz.EffectDenied {
			t.Errorf("field outside the mask Effect = %s, want DENIED", ruling.Effect)
		}
		if ruling := scope.FieldRuling("unregistered.field"); ruling.Effect != authz.EffectDenied {
			t.Errorf("unregistered field Effect = %s, want DENIED", ruling.Effect)
		}
	})

	t.Run("an evaluated scope cannot be widened by a caller", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}, purposes: []string{authz.PurposeCompensationReview}})
		scope, err := authz.PlanRepositoryScope(authz.RepositoryQueryRequest{
			Principal:   principal,
			EffectiveAt: baseInstant,
			Tenant:      tenantAcme,
			Candidates:  []authz.ScopeInput{{Subject: managed, Relationships: []authz.RelationshipFact{managerFact(managed)}}},
			Fields:      []authz.FieldID{authz.FieldWorkerNumber},
		})
		if err != nil {
			t.Fatalf("PlanRepositoryScope: %v", err)
		}
		// Widening attempts through the exported surface must all fail
		// closed: the accessors return copies, so mutating them changes
		// nothing, and the gate still serves only the scope's own subjects
		// under its own field mask.
		_ = append(scope.AllowedSubjects(), unmanaged)
		_ = append(scope.Organizations(), authz.OrgUnitRef{Tenant: tenantAcme, ID: "org-anything"})
		_ = append(scope.MandatoryDenies(), "p1a.mandatory.cross_tenant_sensitive_domains")
		scope.Fields()[authz.FieldBaseSalary] = authz.FieldRuling{Effect: authz.EffectAllow, RuleID: "forged"}

		projections, err := full.Query(scope, []values.EntityRef{managed, unmanaged}, baseInstant)
		if err != nil {
			t.Fatalf("gate.Query: %v", err)
		}
		if len(projections) != 1 || projections[0].Subject != managed {
			t.Fatalf("gate returned %v after widening attempts, want only the managed subject", projections)
		}
		for _, fv := range projections[0].Fields {
			if fv.FieldID == authz.FieldBaseSalary {
				t.Error("a forged field ruling must not reach the projection")
			}
		}
	})
}

// TestTodo_TRUST_012_Mutation proves the planner's intersection stages are
// each load-bearing: mutating one input stage at a time changes the planned
// scope accordingly, so no stage is dead code masked by another.
func TestTodo_TRUST_012_Mutation(t *testing.T) {
	managed := workerSubject(tenantAcme, subjectWorkerID)
	principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}, purposes: []string{authz.PurposeCompensationReview, authz.PurposePerformanceReview}})

	plan := func(facts []authz.RelationshipFact, purpose string, fields []authz.FieldID) (authz.RepositoryScope, error) {
		return authz.PlanRepositoryScope(authz.RepositoryQueryRequest{
			Principal:   principal,
			Purpose:     purpose,
			EffectiveAt: baseInstant,
			Tenant:      tenantAcme,
			Candidates:  []authz.ScopeInput{{Subject: managed, Relationships: facts}},
			Fields:      fields,
		})
	}

	t.Run("relationship mutation", func(t *testing.T) {
		scope, err := plan([]authz.RelationshipFact{managerFact(managed)}, authz.PurposeCompensationReview, []authz.FieldID{authz.FieldWorkerNumber})
		if err != nil {
			t.Fatalf("PlanRepositoryScope: %v", err)
		}
		if len(scope.AllowedSubjects()) != 1 {
			t.Fatalf("baseline subjects = %v, want the managed worker", scope.AllowedSubjects())
		}

		// Expire the manager-chain fact: the same principal, candidate and
		// purpose must now plan to zero authorized subjects.
		expired := managerFact(managed)
		expired.Effective = mustInterval(t, farPast, recentPast)
		scope, err = plan([]authz.RelationshipFact{expired}, authz.PurposeCompensationReview, []authz.FieldID{authz.FieldWorkerNumber})
		if err != nil {
			t.Fatalf("PlanRepositoryScope(expired fact): %v", err)
		}
		if len(scope.AllowedSubjects()) != 0 {
			t.Errorf("subjects after expiring the fact = %v, want none", scope.AllowedSubjects())
		}
	})

	t.Run("purpose mutation", func(t *testing.T) {
		fields := []authz.FieldID{authz.FieldBaseSalary}
		withGrant, err := plan([]authz.RelationshipFact{managerFact(managed)}, authz.PurposeCompensationReview, fields)
		if err != nil {
			t.Fatalf("PlanRepositoryScope: %v", err)
		}
		withoutGrant, err := plan([]authz.RelationshipFact{managerFact(managed)}, authz.PurposePerformanceReview, fields)
		if err != nil {
			t.Fatalf("PlanRepositoryScope: %v", err)
		}
		if withGrant.FieldRuling(authz.FieldBaseSalary).Effect == withoutGrant.FieldRuling(authz.FieldBaseSalary).Effect {
			t.Error("changing the declared purpose left the field mask unchanged; purpose binding is not load-bearing")
		}
	})

	t.Run("field registry mutation", func(t *testing.T) {
		scope, err := plan([]authz.RelationshipFact{managerFact(managed)}, authz.PurposePerformanceReview, []authz.FieldID{authz.FieldBankAccountNumber})
		if err != nil {
			t.Fatalf("PlanRepositoryScope: %v", err)
		}
		if scope.FieldRuling(authz.FieldBankAccountNumber).Effect != authz.EffectDenied {
			t.Fatal("baseline bank ruling should be DENIED for a manager")
		}
		original := authz.FieldRegistry[authz.FieldBankAccountNumber]
		t.Cleanup(func() { authz.FieldRegistry[authz.FieldBankAccountNumber] = original })
		authz.FieldRegistry[authz.FieldBankAccountNumber] = authz.FieldDefinition{ID: authz.FieldBankAccountNumber, Domain: authz.DomainCore}

		scope, err = plan([]authz.RelationshipFact{managerFact(managed)}, authz.PurposePerformanceReview, []authz.FieldID{authz.FieldBankAccountNumber})
		if err != nil {
			t.Fatalf("PlanRepositoryScope(mutated registry): %v", err)
		}
		if scope.FieldRuling(authz.FieldBankAccountNumber).Effect != authz.EffectAllow {
			t.Error("after reclassifying the field into a granted domain, Effect = DENIED; the field mask is not registry-driven")
		}
	})

	t.Run("organization scope mutation", func(t *testing.T) {
		// An organization-scoped comp admin is authorized inside their org
		// closure and denied outside it: moving the resource org flips the
		// whole scope, so the tenant/org stage is load-bearing even for an
		// administrative role.
		scopedPrincipal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authz.PurposePayrollProcessing}})
		planAt := func(resourceOrg string) authz.RepositoryScope {
			t.Helper()
			scope, err := authz.PlanRepositoryScope(authz.RepositoryQueryRequest{
				Principal:    scopedPrincipal,
				EffectiveAt:  baseInstant,
				Tenant:       tenantAcme,
				PrincipalOrg: authz.OrgUnitRef{Tenant: tenantAcme, ID: "org-a"},
				ResourceOrg:  authz.OrgUnitRef{Tenant: tenantAcme, ID: resourceOrg},
				OrgEdges:     []authz.OrgEdge{{Child: authz.OrgUnitRef{Tenant: tenantAcme, ID: "org-a1"}, Parent: authz.OrgUnitRef{Tenant: tenantAcme, ID: "org-a"}, Effective: mustOpenIntervalUnchecked(recentPast)}},
				Candidates:   []authz.ScopeInput{{Subject: managed}},
				Fields:       []authz.FieldID{authz.FieldWorkerNumber},
			})
			if err != nil {
				t.Fatalf("PlanRepositoryScope: %v", err)
			}
			return scope
		}
		if effect := planAt("org-a1").Effect(); effect != authz.EffectAllow {
			t.Fatalf("in-closure resource Effect = %s, want ALLOW", effect)
		}
		if effect := planAt("org-unrelated").Effect(); effect != authz.EffectDenied {
			t.Errorf("out-of-closure resource Effect = %s, want DENIED", effect)
		}
	})
}
