package authzsim_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/operations/authzsim"
	"github.com/monstercameron/hcm-next/internal/trust/authz"
)

// TestTodo_ADMIN_003 is the ADMIN-003 primary test for the AuthZ simulator
// half of the todo: Simulate wraps authz.Simulate byte-for-byte, pins the
// result against a stated policy version, renders a non-empty explanation,
// and DiffDecisions reports exactly which fields a role change would alter.
func TestTodo_ADMIN_003(t *testing.T) {
	req := managerRequest(t)

	t.Run("Simulate matches authz.Simulate exactly and reports a policy version match", func(t *testing.T) {
		want, err := authz.Simulate(req)
		if err != nil {
			t.Fatalf("authz.Simulate: %v", err)
		}
		result, err := authzsim.Simulate(req, authz.PolicyVersion)
		if err != nil {
			t.Fatalf("authzsim.Simulate: %v", err)
		}
		if result.Decision.InputsDigest != want.InputsDigest {
			t.Fatalf("Decision.InputsDigest = %s, want %s (authz.Simulate's own digest)", result.Decision.InputsDigest, want.InputsDigest)
		}
		if result.Explanation != want.Explain() {
			t.Fatalf("Explanation = %q, want %q", result.Explanation, want.Explain())
		}
		if !result.PolicyVersionMatch {
			t.Error("PolicyVersionMatch = false, want true for authz.PolicyVersion")
		}
		if result.Digest == "" {
			t.Error("Digest is empty")
		}
	})

	t.Run("a stale policy version is reported as a mismatch, not silently accepted", func(t *testing.T) {
		result, err := authzsim.Simulate(req, "authz.p1a.bootstrap.v0-stale")
		if err != nil {
			t.Fatalf("authzsim.Simulate: %v", err)
		}
		if result.PolicyVersionMatch {
			t.Error("PolicyVersionMatch = true for a version the evaluator never reported")
		}
	})

	t.Run("DiffDecisions reports exactly the field a role change would grant", func(t *testing.T) {
		// Same subject, same declared purpose (compensation_review), same
		// fields throughout: only the principal's role set changes, from an
		// auditor (whose compensation grant is scoped to audit_review, not
		// this purpose) to an auditor who has also gained the manager role
		// (whose compensation grant *does* apply to compensation_review).
		// FieldWorkerNumber (AnyPurpose core grant) and FieldCaseNotes (no
		// role here ever grants employee_relations under this purpose) are
		// unaffected; only FieldBaseSalary should flip.
		subject := workerSubject(tenantAcme, subjectOtherID)
		fields := []authz.FieldID{authz.FieldWorkerNumber, authz.FieldBaseSalary, authz.FieldCaseNotes}

		beforeReq := authz.Request{
			Principal:     newPrincipal(t, principalOpts{roles: []string{string(authz.RoleAuditor)}, purposes: []string{authz.PurposeCompensationReview}}),
			Purpose:       authz.PurposeCompensationReview,
			EffectiveAt:   baseInstant,
			Subject:       subject,
			Relationships: []authz.RelationshipFact{managerFact(subject)},
			Fields:        fields,
		}
		before, err := authz.Simulate(beforeReq)
		if err != nil {
			t.Fatalf("authz.Simulate(before): %v", err)
		}

		afterReq := beforeReq
		afterReq.Principal = newPrincipal(t, principalOpts{
			roles:    []string{string(authz.RoleAuditor), string(authz.RoleManager)},
			purposes: []string{authz.PurposeCompensationReview},
		})
		after, err := authz.Simulate(afterReq)
		if err != nil {
			t.Fatalf("authz.Simulate(after): %v", err)
		}

		diff := authzsim.DiffDecisions(before, after)
		if diff.SubjectDisclosableChanged {
			t.Fatal("SubjectDisclosableChanged = true, want false: both sides authorize the subject")
		}

		var baseSalaryDelta *authzsim.FieldDelta
		changedCount := 0
		for i := range diff.FieldDeltas {
			d := diff.FieldDeltas[i]
			if d.Changed {
				changedCount++
			}
			if d.Field == authz.FieldBaseSalary {
				baseSalaryDelta = &diff.FieldDeltas[i]
			}
		}
		if changedCount != 1 {
			t.Fatalf("%d fields changed, want exactly 1 (FieldBaseSalary): %+v", changedCount, diff.FieldDeltas)
		}
		if baseSalaryDelta == nil {
			t.Fatal("no delta recorded for FieldBaseSalary")
		}
		if !baseSalaryDelta.Changed || baseSalaryDelta.Before != authz.EffectDenied || baseSalaryDelta.After != authz.EffectAllow {
			t.Fatalf("FieldBaseSalary delta = %+v, want DENIED -> ALLOW", baseSalaryDelta)
		}
		if diff.Digest == "" {
			t.Error("Digest is empty")
		}
	})
}
