package productui

import "fmt"

// Inspected binding kinds, in composition order: widgets, then
// actions.
const (
	InspectedBindingWidget = "widget"
	InspectedBindingAction = "action"
)

// BindingFinding is one inspected binding: its kind, a stable
// reference (widget type or bare capability — never token
// material), its index within its kind, its validity with the
// validator's verbatim reasons, and authorization-relevant notes
// in fixed attribute order. Notes describe declarations only:
// token presence is noted, token values never appear.
type BindingFinding struct {
	Kind    string
	Ref     string
	Index   int
	Valid   bool
	Reasons []string
	Notes   []string
}

// InspectBindings inspects one composition's bindings in
// composition order: every widget binding through the widget
// validator against the registry, every action binding through
// the action validator. Findings report validity; they never
// decide authorization — availability stays proved by gateway
// tokens server-side. The composition is read, never mutated.
func InspectBindings(composition PageComposition, registry WidgetRegistry) []BindingFinding {
	findings := make([]BindingFinding, 0, len(composition.Widgets)+len(composition.Actions))
	for index, binding := range composition.Widgets {
		verdict := ValidateWidgetBinding(binding, registry)
		finding := BindingFinding{Kind: InspectedBindingWidget, Ref: binding.WidgetType, Index: index,
			Valid: verdict.Compatible, Reasons: verdict.Reasons}
		if binding.Classification != "" {
			finding.Notes = append(finding.Notes, fmt.Sprintf("classification %q", binding.Classification))
		}
		if binding.AuthorityClass != "" {
			finding.Notes = append(finding.Notes, fmt.Sprintf("authority %q", binding.AuthorityClass))
		}
		if binding.SourceType != "" {
			finding.Notes = append(finding.Notes, fmt.Sprintf("source-type %q", binding.SourceType))
		}
		if binding.SourceID != "" {
			finding.Notes = append(finding.Notes, fmt.Sprintf("source-id %q", binding.SourceID))
		}
		if binding.Editable {
			finding.Notes = append(finding.Notes, "editable")
		}
		if binding.Masked {
			finding.Notes = append(finding.Notes, "masked")
		}
		if binding.Sensitive {
			finding.Notes = append(finding.Notes, "sensitive")
		}
		findings = append(findings, finding)
	}
	for index, binding := range composition.Actions {
		verdict := ValidateActionBinding(binding)
		finding := BindingFinding{Kind: InspectedBindingAction, Ref: binding.Capability, Index: index,
			Valid: verdict.Compatible, Reasons: verdict.Reasons}
		if binding.Token != "" {
			finding.Notes = append(finding.Notes, "token present")
		} else {
			finding.Notes = append(finding.Notes, "token missing")
		}
		if binding.InputType != "" {
			finding.Notes = append(finding.Notes, fmt.Sprintf("input %q", binding.InputType))
		}
		finding.Notes = append(finding.Notes, fmt.Sprintf("version %d", binding.ExpectedVersion))
		findings = append(findings, finding)
	}
	return findings
}
