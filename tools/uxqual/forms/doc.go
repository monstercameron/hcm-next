// Package forms implements the FORM-004 evidence for the Promotion request
// workspace UX-001 (tools/uxqual/contract) already defines and both
// renderers (tools/uxqual/render/ssr, tools/uxqual/render/gwc) already
// render: it proves the rendered form is accessible, and it proves an
// equivalent way to complete the same request without the form exists and
// produces the identical governed intent.
//
// This package imports tools/uxqual/contract, tools/uxqual/tokens,
// tools/uxqual/qual, tools/uxqual/render/ssr, tools/uxqual/render/gwc and
// tools/uxqual/testdata as finished, frozen libraries; it does not modify
// any of them.
//
// # Two criteria tools/uxqual/qual does not define
//
// UX-QUAL-001's fixture (tools/uxqual/qual) scores keyboard order,
// screen-reader landmarks/labels, WCAG contrast, 320px reflow, and masking.
// FORM-004 additionally names "required-field semantics" and "error
// association via aria-describedby" -- properties specific to a form with
// required fields and validation errors, which UX-QUAL-001's clean fixture
// never exercises. [CheckRequiredFieldSemantics] and [CheckErrorAssociation]
// add exactly those two, following qual.CriterionResult's own shape so they
// slot into the same evidence format.
//
// Scoring both renderers against an error-bearing fixture
// (definitions/ux/forms/promotion-form-accessibility-decision.yaml records
// the result) shows the Go SSR fallback passing every criterion and GWC
// missing the native `required` attribute and the aria-invalid/
// aria-describedby pair on an errored field -- a real, verified gap in
// tools/uxqual/render/gwc (frozen; this lane cannot fix it), not a
// hypothetical one. This is the same shape of finding
// definitions/ux/workspace-renderer-decision.yaml already records for
// UX-QUAL-001: the accessible route ships today as SSR, and the decision
// record is the evidence, not a silent assumption.
//
// # The equivalent human route
//
// [PromotionRequestInputs] is the one typed payload both routes consume.
// [FromFormSubmission] builds an [IntentInstance] from what a human's
// answers to the rendered form actually submit (see
// [ExtractFormAnswers]); [FromCapabilityCall] builds the identical shape
// directly, the way a caller who cannot use the form -- an API integration,
// a bulk loader, a governed CLI -- would. [IntentInstance.Digest] is equal
// for equal typed inputs regardless of which route produced it; that
// equality, proven end-to-end from a real rendered document through to a
// direct capability call, is FORM-004's "equivalent governed route" GREEN
// clause.
package forms
