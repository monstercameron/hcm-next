package authz_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust/authz"
)

// TestTodo_TRUST_012 proves TRUST-012's RED and GREEN clauses: a sensitive
// repository query without a currently evaluated scope fails closed, a broad
// wildcard cannot be expressed by any caller, and the planner intersects
// tenant, organization, population, field, time and purpose scope down to
// only the authorized projections.
func TestTodo_TRUST_012(t *testing.T) {
	managed := workerSubject(tenantAcme, subjectWorkerID)
	unmanaged := workerSubject(tenantAcme, subjectOtherID)

	gate := authz.NewRepositoryGate(map[values.EntityRef]map[authz.FieldID]string{
		managed: {
			authz.FieldWorkerNumber:      "W-0001",
			authz.FieldJobTitle:          "Technician",
			authz.FieldBaseSalary:        "120000",
			authz.FieldBankAccountNumber: "DE89-3704",
		},
		unmanaged: {
			authz.FieldWorkerNumber: "W-0002",
			authz.FieldJobTitle:     "Analyst",
		},
	})

	t.Run("a direct repository query without an evaluated scope fails closed", func(t *testing.T) {
		zero := authz.RepositoryScope{}
		if !zero.Zero() || zero.Valid() {
			t.Fatal("the zero RepositoryScope must be the invalid, never-evaluated scope")
		}
		if zero.Effect() != authz.EffectDenied {
			t.Error("the zero scope must read as denied")
		}
		if zero.AuthorizesRecord(managed, baseInstant) {
			t.Error("the zero scope must authorize no record")
		}
		if zero.FieldRuling(authz.FieldWorkerNumber).Effect != authz.EffectDenied {
			t.Error("the zero scope must deny every field")
		}
		if err := zero.Validate(); !errors.Is(err, authz.ErrScopeRequired) {
			t.Errorf("zero scope Validate = %v, want ErrScopeRequired", err)
		}
		if _, err := gate.Query(zero, []values.EntityRef{managed}, baseInstant); !errors.Is(err, authz.ErrScopeRequired) {
			t.Errorf("gate query with the zero scope = %v, want ErrScopeRequired", err)
		}
	})

	t.Run("a broad wildcard cannot be caller constructed", func(t *testing.T) {
		if _, err := authz.PlanRepositoryScope(authz.RepositoryQueryRequest{
			Principal:   newPrincipal(t, principalOpts{roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authz.PurposePayrollProcessing}}),
			Tenant:      tenantAcme,
			EffectiveAt: baseInstant,
			Fields:      []authz.FieldID{authz.FieldWorkerNumber},
		}); err == nil {
			t.Fatal("a query with no explicit candidates must be rejected: there is no authorized wildcard shape")
		}
	})

	t.Run("the planner rejects an unbound tenant or evaluation instant", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authz.PurposePayrollProcessing}})
		candidate := authz.ScopeInput{Subject: managed}
		base := authz.RepositoryQueryRequest{
			Principal: principal, EffectiveAt: baseInstant,
			Tenant: tenantAcme, Candidates: []authz.ScopeInput{candidate},
			Fields: []authz.FieldID{authz.FieldWorkerNumber},
		}
		for name, req := range map[string]authz.RepositoryQueryRequest{
			"zero tenant":  func() authz.RepositoryQueryRequest { r := base; r.Tenant = values.TenantId(""); return r }(),
			"zero instant": func() authz.RepositoryQueryRequest { r := base; r.EffectiveAt = values.Instant{}; return r }(),
		} {
			t.Run(name, func(t *testing.T) {
				if _, err := authz.PlanRepositoryScope(req); err == nil {
					t.Fatal("planner accepted an incomplete query boundary")
				}
			})
		}
	})

	t.Run("the planner intersects tenant, population, field, time and purpose and the gate serves only authorized projections", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}, purposes: []string{authz.PurposeCompensationReview}})
		scope, err := authz.PlanRepositoryScope(authz.RepositoryQueryRequest{
			Principal:   principal,
			Purpose:     authz.PurposeCompensationReview,
			EffectiveAt: baseInstant,
			Tenant:      tenantAcme,
			Candidates: []authz.ScopeInput{
				{Subject: managed, Relationships: []authz.RelationshipFact{managerFact(managed)}},
				{Subject: unmanaged},
			},
			Fields: []authz.FieldID{authz.FieldWorkerNumber, authz.FieldJobTitle, authz.FieldBaseSalary, authz.FieldBankAccountNumber},
		})
		if err != nil {
			t.Fatalf("PlanRepositoryScope: %v", err)
		}
		if err := scope.Validate(); err != nil {
			t.Fatalf("scope.Validate: %v", err)
		}
		if scope.Effect() != authz.EffectAllow {
			t.Fatalf("scope Effect = %s, want ALLOW", scope.Effect())
		}
		allowed := scope.AllowedSubjects()
		if len(allowed) != 1 || allowed[0] != managed {
			t.Fatalf("AllowedSubjects = %v, want only the managed worker", allowed)
		}

		projections, err := gate.Query(scope, []values.EntityRef{managed, unmanaged}, baseInstant)
		if err != nil {
			t.Fatalf("gate.Query: %v", err)
		}
		if len(projections) != 1 || projections[0].Subject != managed {
			t.Fatalf("gate returned %d projections, want exactly the one authorized subject", len(projections))
		}
		byField := map[authz.FieldID]authz.FieldValue{}
		for _, fv := range projections[0].Fields {
			byField[fv.FieldID] = fv
		}
		if byField[authz.FieldBaseSalary].Effect != authz.EffectAllow || byField[authz.FieldBaseSalary].Value != "120000" {
			t.Errorf("compensation under compensation_review = %+v, want the raw value", byField[authz.FieldBaseSalary])
		}
		// The manager holds no bank grant under any purpose: the masked field
		// must be omitted entirely, not merely emptied.
		if _, ok := byField[authz.FieldBankAccountNumber]; ok {
			t.Error("bank account number must not appear in the projection at all")
		}
		if byField[authz.FieldWorkerNumber].Value != "W-0001" {
			t.Errorf("core field value = %q", byField[authz.FieldWorkerNumber].Value)
		}
	})

	t.Run("a scope is valid only for the instant it was evaluated at", func(t *testing.T) {
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
		if scope.AuthorizesRecord(managed, shortlyAfter) {
			t.Error("a scope must not authorize a record at any instant other than its evaluated one")
		}
		if _, err := gate.Query(scope, []values.EntityRef{managed}, shortlyAfter); !errors.Is(err, authz.ErrScopeRequired) {
			t.Errorf("gate query at a later instant = %v, want ErrScopeRequired", err)
		}
	})

	t.Run("an organization-out-of-scope query plans to a denied scope and serves zero rows", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authz.PurposePayrollProcessing}})
		scope, err := authz.PlanRepositoryScope(authz.RepositoryQueryRequest{
			Principal:    principal,
			EffectiveAt:  baseInstant,
			Tenant:       tenantAcme,
			PrincipalOrg: authz.OrgUnitRef{Tenant: tenantAcme, ID: "org-a"},
			ResourceOrg:  authz.OrgUnitRef{Tenant: tenantAcme, ID: "org-unrelated"},
			OrgEdges:     []authz.OrgEdge{{Child: authz.OrgUnitRef{Tenant: tenantAcme, ID: "org-a1"}, Parent: authz.OrgUnitRef{Tenant: tenantAcme, ID: "org-a"}, Effective: mustOpenIntervalUnchecked(recentPast)}},
			Candidates:   []authz.ScopeInput{{Subject: managed}},
			Fields:       []authz.FieldID{authz.FieldWorkerNumber},
		})
		if err != nil {
			t.Fatalf("PlanRepositoryScope: %v", err)
		}
		if scope.Effect() != authz.EffectDenied {
			t.Fatalf("scope Effect = %s, want DENIED", scope.Effect())
		}
		if len(scope.AllowedSubjects()) != 0 {
			t.Errorf("a denied scope must authorize no subjects, got %v", scope.AllowedSubjects())
		}
		// A denied scope is still an evaluated scope: the gate serves zero
		// rows, not an error, so the response shape cannot distinguish this
		// from an empty store.
		projections, err := gate.Query(scope, []values.EntityRef{managed}, baseInstant)
		if err != nil {
			t.Fatalf("gate.Query on a denied scope: %v", err)
		}
		if len(projections) != 0 {
			t.Errorf("gate returned %d projections for a denied scope, want 0", len(projections))
		}
	})

	t.Run("the planner records its evidence", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleWorkerSelf)}, purposes: []string{authz.PurposeSelfService}})
		scope, err := authz.PlanRepositoryScope(authz.RepositoryQueryRequest{
			Principal:   principal,
			EffectiveAt: baseInstant,
			Tenant:      tenantAcme,
			Candidates:  []authz.ScopeInput{{Subject: managed}},
			Fields:      []authz.FieldID{authz.FieldWorkerNumber},
		})
		if err != nil {
			t.Fatalf("PlanRepositoryScope: %v", err)
		}
		if scope.Purpose() != authz.PurposeSelfService || scope.PolicyVersion() == "" || scope.InputsDigest() == "" || scope.EvidenceID() == "" {
			t.Fatalf("scope evidence incomplete: purpose=%q policy=%q digest=%q evidence=%q",
				scope.Purpose(), scope.PolicyVersion(), scope.InputsDigest(), scope.EvidenceID())
		}
	})
}
