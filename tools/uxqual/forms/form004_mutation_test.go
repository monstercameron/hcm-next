package forms

import "testing"

// TestTodo_FORM_004_Mutation is the MUTATION matrix test for FORM-004. It
// proves the two new criteria and the equivalent-route digest actually
// depend on the specific thing they claim to, field by field and
// attribute by attribute, so a mutant that drops one check, hard-codes
// "pass", or ignores one typed input is caught rather than slipping past
// TestTodo_FORM_004's fixed fixtures.
func TestTodo_FORM_004_Mutation(t *testing.T) {
	t.Run("CheckRequiredFieldSemantics fails on each missing piece independently", func(t *testing.T) {
		cases := []struct {
			name string
			doc  string
			want bool
		}{
			{"both present", `<input id="f" required aria-required="true">`, true},
			{"missing native required", `<input id="f" aria-required="true">`, false},
			{"missing aria-required", `<input id="f" required>`, false},
			{"aria-required wrong value", `<input id="f" required aria-required="false">`, false},
			{"field entirely absent", `<div>no field here</div>`, false},
		}
		for _, tc := range cases {
			got := CheckRequiredFieldSemantics(wrapBody(tc.doc), []string{"f"})
			if got.Pass != tc.want {
				t.Errorf("%s: CheckRequiredFieldSemantics pass=%v, want %v (%s)", tc.name, got.Pass, tc.want, got.Detail)
			}
		}
	})

	t.Run("CheckErrorAssociation fails on each missing piece independently", func(t *testing.T) {
		cases := []struct {
			name string
			doc  string
			want bool
		}{
			{"both present, resolvable", `<input id="f" aria-invalid="true" aria-describedby="f-error"><p id="f-error">bad</p>`, true},
			{"missing aria-invalid", `<input id="f" aria-describedby="f-error"><p id="f-error">bad</p>`, false},
			{"missing aria-describedby", `<input id="f" aria-invalid="true">`, false},
			{"aria-describedby dangling reference", `<input id="f" aria-invalid="true" aria-describedby="ghost">`, false},
			{"field entirely absent", `<div>no field here</div>`, false},
		}
		for _, tc := range cases {
			got := CheckErrorAssociation(wrapBody(tc.doc), []string{"f"})
			if got.Pass != tc.want {
				t.Errorf("%s: CheckErrorAssociation pass=%v, want %v (%s)", tc.name, got.Pass, tc.want, got.Detail)
			}
		}
	})

	t.Run("IntentInstance digest depends on every PromotionRequestInputs field independently", func(t *testing.T) {
		base := PromotionRequestInputs{
			WorkerID:             "worker-1",
			ProposedJobTitle:     "Senior Engineer",
			ProposedGrade:        "P4",
			ProposedCompensation: "$120,000.00",
			EffectiveDate:        "2026-10-01",
			BusinessReason:       "Scope increase.",
		}
		baseInstance, err := FromCapabilityCall(base)
		if err != nil {
			t.Fatalf("FromCapabilityCall(base): %v", err)
		}

		mutate := func(f func(*PromotionRequestInputs)) string {
			m := base
			f(&m)
			inst, err := FromCapabilityCall(m)
			if err != nil {
				t.Fatalf("FromCapabilityCall(mutant): %v", err)
			}
			return inst.Digest
		}

		mutants := map[string]string{
			"worker_id": mutate(func(m *PromotionRequestInputs) { m.WorkerID += "-x" }),
			"job_title": mutate(func(m *PromotionRequestInputs) { m.ProposedJobTitle += "-x" }),
			"grade":     mutate(func(m *PromotionRequestInputs) { m.ProposedGrade += "-x" }),
			"comp":      mutate(func(m *PromotionRequestInputs) { m.ProposedCompensation = "$999,999.00" }),
			"date":      mutate(func(m *PromotionRequestInputs) { m.EffectiveDate = "2099-01-01" }),
			"reason":    mutate(func(m *PromotionRequestInputs) { m.BusinessReason += " Additional text." }),
		}
		for field, digest := range mutants {
			if digest == baseInstance.Digest {
				t.Errorf("mutating %s did not change IntentInstance.Digest", field)
			}
		}

		// Two independently-built, logically identical inputs must match.
		again, err := FromCapabilityCall(base)
		if err != nil {
			t.Fatalf("FromCapabilityCall(again): %v", err)
		}
		if again.Digest != baseInstance.Digest {
			t.Errorf("two identical PromotionRequestInputs produced different digests: %s vs %s", again.Digest, baseInstance.Digest)
		}
	})

	t.Run("FromFormSubmission rejects an incomplete answer set the same way FromCapabilityCall rejects incomplete inputs", func(t *testing.T) {
		incompleteAnswers := map[string]string{
			fieldProposedJobTitle: "Senior Engineer",
			// proposedGrade, proposedCompensation, effectiveDate, and businessReason all missing.
		}
		if _, err := FromFormSubmission("worker-1", incompleteAnswers); err == nil {
			t.Errorf("FromFormSubmission accepted an incomplete answer set")
		}

		incompleteInputs := PromotionRequestInputs{WorkerID: "worker-1", ProposedJobTitle: "Senior Engineer"}
		if _, err := FromCapabilityCall(incompleteInputs); err == nil {
			t.Errorf("FromCapabilityCall accepted incomplete inputs")
		}
	})
}

func wrapBody(fragment string) string {
	return "<!doctype html><html><body>" + fragment + "</body></html>"
}
