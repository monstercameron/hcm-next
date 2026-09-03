package authz_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/trust/authz"
)

func allowedRequest(t *testing.T) authz.Request {
	t.Helper()
	subject := workerSubject(tenantAcme, subjectOtherID)
	principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}, purposes: []string{authz.PurposeCompensationReview, authz.PurposePerformanceReview}})
	return authz.Request{
		Principal:     principal,
		Purpose:       authz.PurposeCompensationReview,
		EffectiveAt:   baseInstant,
		Subject:       subject,
		Relationships: []authz.RelationshipFact{managerFact(subject)},
		Fields:        []authz.FieldID{authz.FieldWorkerNumber, authz.FieldBaseSalary, authz.FieldCaseNotes},
	}
}

// TestTodo_TRUST_011 is the TRUST-011 primary test: explainable
// authorization decisions.
func TestTodo_TRUST_011(t *testing.T) {
	t.Run("a complete allowed decision validates and explains without error", func(t *testing.T) {
		decision, err := authz.Enforce(allowedRequest(t))
		if err != nil {
			t.Fatalf("Enforce: %v", err)
		}
		if err := decision.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if decision.SubjectDisclosable != true {
			t.Fatal("SubjectDisclosable = false, want true for an authorized manager")
		}
		if decision.Fields[authz.FieldBaseSalary].Effect != authz.EffectAllow {
			t.Errorf("FieldBaseSalary Effect = %s, want ALLOW", decision.Fields[authz.FieldBaseSalary].Effect)
		}
		if decision.Fields[authz.FieldCaseNotes].Effect != authz.EffectDenied {
			t.Errorf("FieldCaseNotes Effect = %s, want DENIED", decision.Fields[authz.FieldCaseNotes].Effect)
		}
		if len(decision.PolicyVersions) == 0 {
			t.Error("PolicyVersions is empty")
		}
		if len(decision.MatchedRules) == 0 {
			t.Error("MatchedRules is empty")
		}
		if decision.InputsDigest == "" || decision.EvidenceID == "" {
			t.Error("InputsDigest/EvidenceID is empty")
		}
		if decision.Explain() == "" {
			t.Error("Explain returned an empty string")
		}
	})

	t.Run("a denied tenant scope withholds every field uniformly", func(t *testing.T) {
		req := allowedRequest(t)
		req.Subject.Tenant = tenantVendor // cross-tenant, no sharing grant
		decision, err := authz.Enforce(req)
		if err != nil {
			t.Fatalf("Enforce: %v", err)
		}
		if decision.SubjectDisclosable {
			t.Fatal("SubjectDisclosable = true, want false for a denied tenant boundary")
		}
		if err := decision.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		for f, ruling := range decision.Fields {
			if ruling.Effect != authz.EffectWithheld {
				t.Errorf("field %s Effect = %s, want WITHHELD", f, ruling.Effect)
			}
		}
	})

	t.Run("a denied relationship scope also withholds every field uniformly", func(t *testing.T) {
		req := allowedRequest(t)
		req.Relationships = nil // no manager-chain fact for this subject
		decision, err := authz.Enforce(req)
		if err != nil {
			t.Fatalf("Enforce: %v", err)
		}
		if decision.SubjectDisclosable {
			t.Fatal("SubjectDisclosable = true, want false with no relationship established")
		}
		if err := decision.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})

	t.Run("a decision missing required evidence fails Validate", func(t *testing.T) {
		decision, err := authz.Enforce(allowedRequest(t))
		if err != nil {
			t.Fatalf("Enforce: %v", err)
		}

		incomplete := []func(*authz.Decision){
			func(d *authz.Decision) { d.PolicyVersions = nil },
			func(d *authz.Decision) { d.Purpose = "" },
			func(d *authz.Decision) { d.Assurance = 0 },
			func(d *authz.Decision) { d.MatchedRules = nil },
			func(d *authz.Decision) { d.Fields = nil },
			func(d *authz.Decision) { d.Scope.Relationship = authz.RelationshipUnspecified },
		}
		for i, mutate := range incomplete {
			mutated := decision
			mutate(&mutated)
			if err := mutated.Validate(); err == nil {
				t.Errorf("mutation %d: Validate succeeded on an incomplete decision", i)
			}
		}
	})

	t.Run("Simulate and Enforce agree byte-for-byte on the same input", func(t *testing.T) {
		req := allowedRequest(t)
		enforced, err := authz.Enforce(req)
		if err != nil {
			t.Fatalf("Enforce: %v", err)
		}
		simulated, err := authz.Simulate(req)
		if err != nil {
			t.Fatalf("Simulate: %v", err)
		}
		if enforced.InputsDigest != simulated.InputsDigest {
			t.Fatalf("digests differ: enforce=%s simulate=%s", enforced.InputsDigest, simulated.InputsDigest)
		}
		if enforced.Explain() != simulated.Explain() {
			t.Errorf("explanations differ:\nenforce:  %s\nsimulate: %s", enforced.Explain(), simulated.Explain())
		}
	})

	t.Run("the same inputs always produce the same digest", func(t *testing.T) {
		req := allowedRequest(t)
		first, err := authz.Enforce(req)
		if err != nil {
			t.Fatalf("Enforce: %v", err)
		}
		second, err := authz.Enforce(req)
		if err != nil {
			t.Fatalf("Enforce: %v", err)
		}
		if first.InputsDigest != second.InputsDigest {
			t.Error("evaluating identical inputs twice produced different digests")
		}
	})
}

// TestTodo_TRUST_011_Golden pins the exact evidence shape of a handful of
// canonical decisions, so an accidental change to rule IDs, reasons or
// digests is caught even though it would not fail any effect-only
// assertion.
func TestTodo_TRUST_011_Golden(t *testing.T) {
	cases := []struct {
		name           string
		build          func(t *testing.T) authz.Request
		wantSubjectOK  bool
		wantTenantRule string
		wantScopeRule  string
		wantFieldRule  map[authz.FieldID]string
	}{
		{
			name: "manager reading a direct report's core and compensation fields",
			build: func(t *testing.T) authz.Request {
				return allowedRequest(t)
			},
			wantSubjectOK:  true,
			wantTenantRule: "p1a.tenant.same_tenant_default",
			wantScopeRule:  "p1a.scope.manager_chain",
			wantFieldRule: map[authz.FieldID]string{
				authz.FieldWorkerNumber: "p1a.manager.core",
				authz.FieldBaseSalary:   "p1a.manager.compensation",
				authz.FieldCaseNotes:    "p1a.field.deny_default",
			},
		},
		{
			name: "worker self reading their own record",
			build: func(t *testing.T) authz.Request {
				self := workerSubject(tenantAcme, subjectWorkerID)
				principal := newPrincipal(t, principalOpts{subject: subjectWorkerID, roles: []string{string(authz.RoleWorkerSelf)}, purposes: []string{authz.PurposeSelfService}})
				return authz.Request{
					Principal:   principal,
					Purpose:     authz.PurposeSelfService,
					EffectiveAt: baseInstant,
					Subject:     self,
					Fields:      []authz.FieldID{authz.FieldBaseSalary},
				}
			},
			wantSubjectOK:  true,
			wantTenantRule: "p1a.tenant.same_tenant_default",
			wantScopeRule:  "p1a.scope.self",
			wantFieldRule: map[authz.FieldID]string{
				authz.FieldBaseSalary: "p1a.worker_self.compensation",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := authz.Enforce(tc.build(t))
			if err != nil {
				t.Fatalf("Enforce: %v", err)
			}
			if decision.SubjectDisclosable != tc.wantSubjectOK {
				t.Fatalf("SubjectDisclosable = %v, want %v", decision.SubjectDisclosable, tc.wantSubjectOK)
			}
			if decision.Tenant.RuleID != tc.wantTenantRule {
				t.Errorf("Tenant.RuleID = %q, want %q", decision.Tenant.RuleID, tc.wantTenantRule)
			}
			if decision.Scope.RuleID != tc.wantScopeRule {
				t.Errorf("Scope.RuleID = %q, want %q", decision.Scope.RuleID, tc.wantScopeRule)
			}
			for field, wantRule := range tc.wantFieldRule {
				if got := decision.Fields[field].RuleID; got != wantRule {
					t.Errorf("field %s RuleID = %q, want %q", field, got, wantRule)
				}
			}
			if err := decision.Validate(); err != nil {
				t.Errorf("Validate: %v", err)
			}
		})
	}
}
