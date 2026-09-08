// Package productui resolves the proposal confirmation
// review. The review composes the chain's governed pieces
// — the values comparison, the collection guide, and the
// missing-fact summary — into the single confirmation a
// submitter reads. Ready means every fact present and the
// change shown; it never authorizes submission, which
// stays server authority. Inputs are never mutated.
package productui

// ProposalConfirmation is the resolved confirmation
// review for one proposal's supplied facts: the values
// change, the carried facts, the collection guide, the
// missing-fact summary, and whether the proposal is ready
// to confirm.
type ProposalConfirmation struct {
	Change        FieldComparison
	WorkerRef     string
	EffectiveDate string
	Guide         ProposalGuide
	Missing       ValidationSummaryModel
	// Ready reports the proposal reviewable. It never
	// authorizes submission.
	Ready bool
}

// ResolveProposalConfirmation resolves the confirmation
// review for one proposal's supplied facts.
func ResolveProposalConfirmation(locale LocaleContext, input ProposalCollection) ProposalConfirmation {
	guide := ResolveProposalGuide(locale, input)
	var issues []ValidationIssue
	for _, step := range guide.Steps {
		if !step.Complete {
			issues = append(issues, ValidationIssue{Message: step.Title + ": " + step.Detail})
		}
	}
	return ProposalConfirmation{
		Change:        CompareFieldValue(locale, locale.Text("work.proposal_step_values"), input.Current, input.Proposed),
		WorkerRef:     input.WorkerRef,
		EffectiveDate: input.EffectiveDate,
		Guide:         guide,
		Missing:       ResolveValidationSummary(locale, ValidationState{Issues: issues}, nil),
		Ready:         guide.Complete,
	}
}
