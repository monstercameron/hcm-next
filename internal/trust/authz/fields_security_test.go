package authz_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/trust/authz"
)

// TestTodo_TRUST_010_Security is the TRUST-010 security test. A restricted
// field must never leak its sensitivity through the shape of the response:
// resolving a mixed batch of allowed and denied fields must not error out
// early, must not omit the denied fields, and a denial reason must never
// distinguish "you may never see this" from "you may not see this under
// this purpose" in a way that would let a caller enumerate the policy by
// trial and error faster than by reading it.
func TestTodo_TRUST_010_Security(t *testing.T) {
	t.Run("a mixed batch resolves every field with no error and no omission", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleWorkerSelf)}, purposes: []string{authz.PurposeSelfService}})
		fields := []authz.FieldID{
			authz.FieldWorkerNumber, authz.FieldBaseSalary, authz.FieldCaseNotes, authz.FieldBankAccountNumber,
		}
		decision, err := authz.ResolveFields(principal, authz.PurposeSelfService, fields, nil)
		if err != nil {
			t.Fatalf("ResolveFields: %v", err)
		}
		if err := decision.Covers(fields); err != nil {
			t.Fatalf("Covers: %v", err)
		}
		if decision.Rulings[authz.FieldCaseNotes].Effect != authz.EffectDenied {
			t.Error("worker self has no employee-relations grant; case notes must deny")
		}
		if decision.Rulings[authz.FieldBaseSalary].Effect != authz.EffectAllow {
			t.Error("worker self is granted compensation under self_service_view")
		}
	})

	t.Run("mandatory cross-tenant deny overrides every role grant uniformly, not just some domains", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authz.PurposePayrollProcessing}})
		sensitive := []authz.FieldID{authz.FieldBaseSalary, authz.FieldBankAccountNumber, authz.FieldTaxID}
		decision, err := authz.ResolveFields(principal, authz.PurposePayrollProcessing, sensitive, []string{"p1a.mandatory.cross_tenant_sensitive_domains"})
		if err != nil {
			t.Fatalf("ResolveFields: %v", err)
		}
		for _, f := range sensitive {
			if decision.Rulings[f].Effect != authz.EffectDenied {
				t.Errorf("field %s Effect = %s, want DENIED under the mandatory cross-tenant restriction", f, decision.Rulings[f].Effect)
			}
		}
		// Core/contact must still be reachable: the mandatory deny targets
		// sensitive domains specifically, it does not become a blanket
		// cross-tenant deny of everything.
		core, err := authz.ResolveFields(principal, authz.PurposePayrollProcessing, []authz.FieldID{authz.FieldWorkerNumber}, []string{"p1a.mandatory.cross_tenant_sensitive_domains"})
		if err != nil {
			t.Fatalf("ResolveFields: %v", err)
		}
		if core.Rulings[authz.FieldWorkerNumber].Effect != authz.EffectAllow {
			t.Errorf("core field Effect = %s, want ALLOW even under the cross-tenant mandatory deny", core.Rulings[authz.FieldWorkerNumber].Effect)
		}
	})

	t.Run("a principal with no recognized role denies every governed field, not just some", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{"unrecognized_role"}, purposes: []string{authz.PurposeSelfService, authz.PurposeAuditReview}})
		fields := []authz.FieldID{authz.FieldWorkerNumber, authz.FieldBaseSalary}
		decision, err := authz.ResolveFields(principal, authz.PurposeSelfService, fields, nil)
		if err != nil {
			t.Fatalf("ResolveFields: %v", err)
		}
		for _, f := range fields {
			if decision.Rulings[f].Effect != authz.EffectDenied {
				t.Errorf("field %s Effect = %s, want DENIED for an unrecognized role", f, decision.Rulings[f].Effect)
			}
		}
	})
}

// TestTodo_TRUST_010_Mutation proves that field classification actually
// drives the ruling: it is not a fixed answer per field name, and mutating
// which domain a field belongs to in the one field registry changes the
// ruling accordingly, which is what "one field registry drives every
// consumer" has to mean operationally.
func TestTodo_TRUST_010_Mutation(t *testing.T) {
	principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}, purposes: []string{authz.PurposePerformanceReview}})

	baseline, err := authz.ResolveFields(principal, authz.PurposePerformanceReview, []authz.FieldID{authz.FieldBaseSalary}, nil)
	if err != nil {
		t.Fatalf("ResolveFields(baseline): %v", err)
	}
	if baseline.Rulings[authz.FieldBaseSalary].Effect != authz.EffectDenied {
		t.Fatalf("baseline Effect = %s, want DENIED (manager has no compensation grant for performance_review)", baseline.Rulings[authz.FieldBaseSalary].Effect)
	}

	original := authz.FieldRegistry[authz.FieldBaseSalary]
	t.Cleanup(func() { authz.FieldRegistry[authz.FieldBaseSalary] = original })

	// Reclassify base salary into the performance domain, which a manager
	// does hold a performance_review grant for. If ResolveFields is truly
	// registry-driven, the ruling must flip with it.
	authz.FieldRegistry[authz.FieldBaseSalary] = authz.FieldDefinition{ID: authz.FieldBaseSalary, Domain: authz.DomainPerformance}

	mutated, err := authz.ResolveFields(principal, authz.PurposePerformanceReview, []authz.FieldID{authz.FieldBaseSalary}, nil)
	if err != nil {
		t.Fatalf("ResolveFields(mutated registry): %v", err)
	}
	if mutated.Rulings[authz.FieldBaseSalary].Effect != authz.EffectAllow {
		t.Errorf("after reclassifying the field into a granted domain, Effect = %s, want ALLOW", mutated.Rulings[authz.FieldBaseSalary].Effect)
	}

	t.Run("mandatory deny mutation", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleCompAdmin)}, purposes: []string{authz.PurposePayrollProcessing}})
		without, err := authz.ResolveFields(principal, authz.PurposePayrollProcessing, []authz.FieldID{authz.FieldBankAccountNumber}, nil)
		if err != nil {
			t.Fatalf("ResolveFields: %v", err)
		}
		if without.Rulings[authz.FieldBankAccountNumber].Effect != authz.EffectAllow {
			t.Fatalf("baseline Effect = %s, want ALLOW", without.Rulings[authz.FieldBankAccountNumber].Effect)
		}
		with, err := authz.ResolveFields(principal, authz.PurposePayrollProcessing, []authz.FieldID{authz.FieldBankAccountNumber}, []string{"p1a.mandatory.cross_tenant_sensitive_domains"})
		if err != nil {
			t.Fatalf("ResolveFields: %v", err)
		}
		if with.Rulings[authz.FieldBankAccountNumber].Effect == without.Rulings[authz.FieldBankAccountNumber].Effect {
			t.Error("adding the mandatory cross-tenant deny left the ruling unchanged")
		}
	})
}
