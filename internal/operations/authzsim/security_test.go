package authzsim_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/authzsim"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// TestTodo_ADMIN_003_Security proves the RED clause for the simulator half
// of ADMIN-003: Simulate's explanation carries none of authz.Decision.Explain's
// forbidden detail for a withheld subject, and DiffDecisions collapses to
// the one coarse fact whenever either side is non-disclosable, never naming
// a field, domain, rule or relationship that a caller could use to infer
// what a denied decision would otherwise have granted.
func TestTodo_ADMIN_003_Security(t *testing.T) {
	t.Run("Simulate's explanation leaks nothing for a withheld subject", func(t *testing.T) {
		req := managerRequest(t)
		req.Relationships = nil // no manager-chain fact: subject becomes non-disclosable

		result, err := authzsim.Simulate(req, authz.PolicyVersion)
		if err != nil {
			t.Fatalf("Simulate: %v", err)
		}
		if result.Decision.SubjectDisclosable {
			t.Fatal("expected a non-disclosable subject for this scenario")
		}
		for _, forbidden := range []string{
			string(authz.FieldBaseSalary), string(authz.FieldCaseNotes),
			string(authz.DomainCompensation), string(authz.DomainEmployeeRelations),
			"p1a.manager.compensation",
		} {
			if strings.Contains(result.Explanation, forbidden) {
				t.Errorf("Explanation leaks %q: %s", forbidden, result.Explanation)
			}
		}
	})

	t.Run("DiffDecisions collapses to one coarse fact when either side is withheld", func(t *testing.T) {
		disclosable, err := authz.Simulate(managerRequest(t))
		if err != nil {
			t.Fatalf("authz.Simulate: %v", err)
		}
		withheldReq := managerRequest(t)
		withheldReq.Relationships = nil
		withheld, err := authz.Simulate(withheldReq)
		if err != nil {
			t.Fatalf("authz.Simulate: %v", err)
		}
		if withheld.SubjectDisclosable {
			t.Fatal("expected a non-disclosable subject for this scenario")
		}

		diff := authzsim.DiffDecisions(disclosable, withheld)
		if diff.FieldDeltas != nil {
			t.Fatalf("FieldDeltas = %v, want nil when either side is non-disclosable", diff.FieldDeltas)
		}
		if diff.ScopeRelationshipBefore != "" || diff.ScopeRelationshipAfter != "" {
			t.Errorf("ScopeRelationship before/after = %q/%q, want both empty", diff.ScopeRelationshipBefore, diff.ScopeRelationshipAfter)
		}
		if !diff.SubjectDisclosableChanged {
			t.Error("SubjectDisclosableChanged = false, want true")
		}
		for _, forbidden := range []string{
			string(authz.FieldBaseSalary), string(authz.DomainCompensation), "p1a.manager.compensation", "MANAGER_CHAIN",
		} {
			if strings.Contains(diff.Explanation, forbidden) {
				t.Errorf("Diff explanation leaks %q: %s", forbidden, diff.Explanation)
			}
		}

		// The reverse comparison must be equally silent: order must not
		// matter for what a withheld comparison is willing to name.
		reverse := authzsim.DiffDecisions(withheld, disclosable)
		if reverse.FieldDeltas != nil {
			t.Error("reverse diff named field deltas despite a withheld side")
		}
	})

	t.Run("Simulate never panics on a degenerate request", func(t *testing.T) {
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
						t.Errorf("degenerate request %d panicked: %v", i, r)
					}
				}()
				if _, err := authzsim.Simulate(req, "any-version"); err != nil {
					t.Logf("degenerate request %d: Simulate returned %v (acceptable for a malformed request)", i, err)
				}
			}()
		}
	})

	t.Run("Simulate returns an error rather than panicking on a nil principal", func(t *testing.T) {
		if _, err := authzsim.Simulate(authz.Request{EffectiveAt: baseInstant}, "any-version"); err == nil {
			t.Fatal("Simulate(nil principal) succeeded, want an error")
		}
	})
}
