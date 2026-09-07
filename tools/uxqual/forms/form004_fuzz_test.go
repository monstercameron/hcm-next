package forms

import "testing"

// FuzzTodo_FORM_004 is the FUZZ matrix test for FORM-004. It fuzzes the five
// human-entered typed fields and proves, for every generated input: (1)
// neither route ever panics, (2) FromCapabilityCall is deterministic (the
// same inputs always produce the same digest), and (3) the form-submission
// route and the capability-call route agree on the digest for identical
// inputs whenever both accept them at all -- FORM-004's core "equivalent
// route" property, stated as an invariant rather than only against the one
// fixed fixture TestTodo_FORM_004_Integration exercises.
func FuzzTodo_FORM_004(f *testing.F) {
	f.Add("worker-1048", "Registered Nurse III", "RN3", "$98,000.00", "2026-10-01", "Scope of practice increase.")
	f.Add("", "", "", "", "", "")
	f.Add("worker-1", "Title\nwith\nnewlines", "P4", "$0.00", "not-a-date", "")
	f.Add("worker-2", "Título con acentos áéíóú", "N4", "€1.234,56", "2026-13-40", "原因说明")

	f.Fuzz(func(t *testing.T, workerID, jobTitle, grade, compensation, effectiveDate, reason string) {
		inputs := PromotionRequestInputs{
			WorkerID:             workerID,
			ProposedJobTitle:     jobTitle,
			ProposedGrade:        grade,
			ProposedCompensation: compensation,
			EffectiveDate:        effectiveDate,
			BusinessReason:       reason,
		}

		capInstance, capErr := FromCapabilityCall(inputs)

		answers := map[string]string{
			fieldProposedJobTitle:     jobTitle,
			fieldProposedGrade:        grade,
			fieldProposedCompensation: compensation,
			fieldEffectiveDate:        effectiveDate,
			fieldBusinessReason:       reason,
		}
		formInstance, formErr := FromFormSubmission(workerID, answers)

		// Both routes validate the identical typed shape, so they must agree
		// on whether the input is even acceptable.
		if (capErr == nil) != (formErr == nil) {
			t.Fatalf("routes disagree on validity: capability err=%v, form err=%v (inputs=%+v)", capErr, formErr, inputs)
		}
		if capErr != nil {
			return // both rejected; nothing further to compare
		}

		if capInstance.Digest != formInstance.Digest {
			t.Fatalf("digest mismatch for identical inputs %+v: capability=%s form=%s", inputs, capInstance.Digest, formInstance.Digest)
		}
		if capInstance.Inputs != inputs {
			t.Fatalf("FromCapabilityCall did not preserve its inputs verbatim: got %+v, want %+v", capInstance.Inputs, inputs)
		}

		// Determinism: calling again with the same inputs must reproduce the
		// same digest.
		again, err := FromCapabilityCall(inputs)
		if err != nil {
			t.Fatalf("FromCapabilityCall(same inputs, second call) unexpectedly failed: %v", err)
		}
		if again.Digest != capInstance.Digest {
			t.Fatalf("FromCapabilityCall is not deterministic for inputs %+v: %s vs %s", inputs, again.Digest, capInstance.Digest)
		}
	})
}
