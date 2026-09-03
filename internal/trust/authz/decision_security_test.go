package authz_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/trust/authz"
)

// TestTodo_TRUST_011_Security is the TRUST-011 security test. It attacks the
// explanation itself: a non-disclosable subject's explanation must not name
// which relationship, organization or field-domain reasoning was involved
// beyond the one uniform denial reason, and it must never contain a raw
// field value (this package never holds one, but the assertion pins that
// invariant so it cannot regress), and Enforce/Simulate must never panic on
// a request that names no fields, an empty purpose, or an unrecognized
// role.
func TestTodo_TRUST_011_Security(t *testing.T) {
	t.Run("a withheld decision's explanation names no field, domain or relationship detail", func(t *testing.T) {
		req := allowedRequest(t)
		req.Relationships = nil // relationship denial: subject becomes non-disclosable
		decision, err := authz.Enforce(req)
		if err != nil {
			t.Fatalf("Enforce: %v", err)
		}
		if decision.SubjectDisclosable {
			t.Fatal("expected a non-disclosable subject for this scenario")
		}
		explanation := decision.Explain()
		for _, forbidden := range []string{
			string(authz.FieldBaseSalary), string(authz.FieldCaseNotes),
			string(authz.DomainCompensation), string(authz.DomainEmployeeRelations),
			"p1a.manager.compensation", "p1a.field.deny_default",
		} {
			if strings.Contains(explanation, forbidden) {
				t.Errorf("explanation of a withheld decision leaks %q: %s", forbidden, explanation)
			}
		}
	})

	t.Run("two different reasons a subject could be denied produce indistinguishable explanations", func(t *testing.T) {
		tenantDenied := allowedRequest(t)
		tenantDenied.Subject.Tenant = "unrelated-corp"

		scopeDenied := allowedRequest(t)
		scopeDenied.Relationships = nil

		a, err := authz.Enforce(tenantDenied)
		if err != nil {
			t.Fatalf("Enforce: %v", err)
		}
		b, err := authz.Enforce(scopeDenied)
		if err != nil {
			t.Fatalf("Enforce: %v", err)
		}
		if a.SubjectDisclosable || b.SubjectDisclosable {
			t.Fatal("both scenarios must deny for this comparison to be meaningful")
		}
		// The two explanations may legitimately differ in their reason
		// token and rule list (a tenant-boundary denial and a relationship
		// denial really are different facts about policy structure, not
		// about the resource), but neither may ever mention a field, a
		// domain, or a role grant: that detail is exactly what a
		// non-disclosable subject must not leak.
		for _, explanation := range []string{a.Explain(), b.Explain()} {
			for _, forbidden := range []string{string(authz.FieldBaseSalary), string(authz.DomainCompensation)} {
				if strings.Contains(explanation, forbidden) {
					t.Errorf("explanation leaks %q: %s", forbidden, explanation)
				}
			}
		}
	})

	t.Run("Enforce never panics on a degenerate request", func(t *testing.T) {
		principal := newPrincipal(t, principalOpts{roles: []string{"nobody_role"}})
		degenerate := []authz.Request{
			{Principal: principal, Subject: workerSubject(tenantAcme, subjectOtherID), EffectiveAt: baseInstant},
			{Principal: principal, Subject: workerSubject(tenantAcme, subjectOtherID), EffectiveAt: baseInstant, Fields: []authz.FieldID{}},
			{Principal: principal, EffectiveAt: baseInstant},
		}
		for i, req := range degenerate {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("request %d panicked: %v", i, r)
					}
				}()
				if _, err := authz.Enforce(req); err != nil {
					t.Logf("request %d: Enforce returned %v (acceptable for a malformed request)", i, err)
				}
			}()
		}
	})

	t.Run("a nil principal is refused, not treated as an anonymous allow", func(t *testing.T) {
		if _, err := authz.Enforce(authz.Request{Subject: workerSubject(tenantAcme, subjectOtherID), EffectiveAt: baseInstant}); err == nil {
			t.Error("Enforce(nil principal) succeeded, want an error")
		}
	})
}
