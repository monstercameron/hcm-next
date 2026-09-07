package forms

import (
	"testing"

	"github.com/monstercameron/hcm-next/tools/uxqual/render/gwc"
	"github.com/monstercameron/hcm-next/tools/uxqual/render/ssr"
	"github.com/monstercameron/hcm-next/tools/uxqual/testdata"
)

// TestTodo_FORM_004_Integration is the INTEGRATION matrix test for
// FORM-004. It proves the equivalent-route claim end to end: starting from
// a real rendered document (SSR, then GWC), extracting what a human's
// submission would actually carry, and independently building the same
// typed inputs the direct capability-call route would receive, both routes
// must produce an [IntentInstance] with the identical canonical digest --
// and changing any one typed input must change both, so the equality is
// not vacuous.
func TestTodo_FORM_004_Integration(t *testing.T) {
	fixture := testdata.PromotionFixture()

	capabilityInputs := PromotionRequestInputs{
		WorkerID:             fixture.Request.WorkerID,
		ProposedJobTitle:     fieldValue(fixture, fieldProposedJobTitle),
		ProposedGrade:        fieldValue(fixture, fieldProposedGrade),
		ProposedCompensation: fieldValue(fixture, fieldProposedCompensation),
		EffectiveDate:        fieldValue(fixture, fieldEffectiveDate),
		BusinessReason:       fieldValue(fixture, fieldBusinessReason),
	}
	if err := capabilityInputs.Validate(); err != nil {
		t.Fatalf("fixture-derived capability inputs should be valid: %v", err)
	}

	capabilityInstance, err := FromCapabilityCall(capabilityInputs)
	if err != nil {
		t.Fatalf("FromCapabilityCall: %v", err)
	}

	for _, tc := range []struct {
		name string
		doc  func() (string, error)
	}{
		{"ssr", func() (string, error) { return ssr.Render(fixture) }},
		{"gwc", func() (string, error) { return gwc.Document(fixture) }},
	} {
		t.Run(tc.name+" form route matches the capability-call route", func(t *testing.T) {
			doc, err := tc.doc()
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			answers, err := ExtractFormAnswers(doc)
			if err != nil {
				t.Fatalf("ExtractFormAnswers: %v", err)
			}

			formInstance, err := FromFormSubmission(fixture.Request.WorkerID, answers)
			if err != nil {
				t.Fatalf("FromFormSubmission: %v", err)
			}

			if formInstance.IntentType != capabilityInstance.IntentType {
				t.Errorf("IntentType mismatch: form=%q capability=%q", formInstance.IntentType, capabilityInstance.IntentType)
			}
			if formInstance.Inputs != capabilityInstance.Inputs {
				t.Errorf("typed inputs mismatch:\n  form:       %+v\n  capability: %+v", formInstance.Inputs, capabilityInstance.Inputs)
			}
			if formInstance.Digest != capabilityInstance.Digest {
				t.Fatalf("canonical digest mismatch for identical inputs:\n  form (%s route):       %s\n  capability call:        %s",
					tc.name, formInstance.Digest, capabilityInstance.Digest)
			}
		})
	}

	t.Run("RED: the equivalence is not vacuous -- a different typed input changes both routes' digest", func(t *testing.T) {
		mutated := capabilityInputs
		mutated.BusinessReason = "A completely different business reason."
		mutatedInstance, err := FromCapabilityCall(mutated)
		if err != nil {
			t.Fatalf("FromCapabilityCall(mutated): %v", err)
		}
		if mutatedInstance.Digest == capabilityInstance.Digest {
			t.Fatalf("changing BusinessReason did not change the capability-call route's digest")
		}

		mutatedAnswers := map[string]string{
			fieldProposedJobTitle:     capabilityInputs.ProposedJobTitle,
			fieldProposedGrade:        capabilityInputs.ProposedGrade,
			fieldProposedCompensation: capabilityInputs.ProposedCompensation,
			fieldEffectiveDate:        capabilityInputs.EffectiveDate,
			fieldBusinessReason:       mutated.BusinessReason,
		}
		mutatedFormInstance, err := FromFormSubmission(fixture.Request.WorkerID, mutatedAnswers)
		if err != nil {
			t.Fatalf("FromFormSubmission(mutated): %v", err)
		}
		if mutatedFormInstance.Digest != mutatedInstance.Digest {
			t.Fatalf("mutated inputs still disagree between routes:\n  form:       %s\n  capability: %s", mutatedFormInstance.Digest, mutatedInstance.Digest)
		}
		if mutatedFormInstance.Digest == capabilityInstance.Digest {
			t.Fatalf("mutated form-route digest equals the original capability-route digest; mutation had no effect")
		}
	})
}
