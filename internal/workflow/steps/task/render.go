package task

import "fmt"

// RenderForm produces the server-rendered HTML surface WF-STEP-004's
// accessibility contract is checked against: one labeled, required control
// for the node's declared typed output, and a separate, equally required
// accommodation-acknowledgement control citing the compiled
// AccommodationPolicy.
//
// It is not a form engine -- FORM-001..003's sections, repeat groups and
// conditional visibility rules are DESIGN/CONFORMANCE ONLY in P1B -- it is
// the fixed accessibility contract every TASK node's completion evidence
// must satisfy regardless of what a later form engine renders inside it: a
// native `required` attribute paired with `aria-required="true"` (never one
// alone, per the same rule tools/uxqual/forms.CheckRequiredFieldSemantics
// checks for FORM-004), a label bound to its control by `for`/`id`, and an
// explicit accommodation control rather than an assumption that nobody needs
// one.
func RenderForm(n CompiledTaskNode) (string, error) {
	if err := validateNode(n); err != nil {
		return "", err
	}
	return fmt.Sprintf(`<form aria-label=%q data-form-ref=%q data-form-version="%d">
  <label for="task-output">Submission for %s</label>
  <input id="task-output" name="task-output" type="text" required aria-required="true" aria-describedby="task-output-help">
  <p id="task-output-help">Typed output validated against %s@v%d.</p>

  <label for="accommodation-ack">Accommodation acknowledgement</label>
  <input id="accommodation-ack" name="accommodation-ack" type="checkbox" required aria-required="true" aria-describedby="accommodation-ack-help" data-accommodation-policy=%q data-accommodation-policy-version="%d">
  <p id="accommodation-ack-help">Confirms accessibility policy %s@v%d was reviewed and any accommodation need was recorded, never assumed absent.</p>
</form>
`, n.WorkType, n.FormDefinition.Ref, n.FormDefinition.Version, n.NodeID,
		n.OutputSchema.SchemaID, n.OutputSchema.Version,
		n.AccommodationPolicy.Ref, n.AccommodationPolicy.Version,
		n.AccessibilityPolicy.Ref, n.AccessibilityPolicy.Version), nil
}
