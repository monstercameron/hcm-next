package productui

import "fmt"

// WidgetDefinition is one registered widget contract: its type, trust
// tier, pinned version, and opaque classification ceiling. Comparison
// of ceilings awaits the classification taxonomy (a later lifecycle
// step); registration pins the ceiling without comparing it, and no
// ordering is invented here.
type WidgetDefinition struct {
	ID                  string
	Tier                string
	Version             int64
	ClassificationLimit string
}

// WidgetRegistry is the registered widget set: small by design,
// extended by the studio platform, never by page authors.
type WidgetRegistry struct {
	Widgets []WidgetDefinition
}

// Trust tiers from the widget trust contract.
const (
	WidgetTierGoverned = "governed"
	WidgetTierContent  = "content"
	WidgetTierExternal = "external"
)

// registeredWidgets is the initial registry: one widget per trust
// tier. Governed widgets own material actions through strict typed
// contracts; content widgets show sanitized summaries; external
// embeds are sandboxed and denied sensitive context by default.
var registeredWidgets = []WidgetDefinition{
	{ID: "proposal-form", Tier: WidgetTierGoverned, Version: 1, ClassificationLimit: "confidential"},
	{ID: "metric-display", Tier: WidgetTierContent, Version: 1, ClassificationLimit: "internal"},
	{ID: "external-frame", Tier: WidgetTierExternal, Version: 1, ClassificationLimit: "public"},
}

// authorityClasses is the provenance vocabulary a binding may carry:
// canonical facts, external observations, manager observations, and
// agent hypotheses.
var authorityClasses = map[string]bool{"canonical": true, "external": true, "manager": true, "agent": true}

// RegisteredWidgets returns the widget registry, freshly copied on
// every call so callers cannot mutate the contract.
func RegisteredWidgets() WidgetRegistry {
	widgets := make([]WidgetDefinition, len(registeredWidgets))
	copy(widgets, registeredWidgets)
	return WidgetRegistry{Widgets: widgets}
}

// WidgetBinding is one typed composition binding: which registered
// widget version it targets plus its provenance-typed value. The
// server composes bindings; presentation validates them. Sensitive
// marks server-judged sensitive context, which external embeds must
// never receive.
type WidgetBinding struct {
	WidgetType     string
	WidgetVersion  int64
	AuthorityClass string
	Classification string
	SourceType     string
	SourceID       string
	SourceVersion  int64
	Value          string
	DisplayValue   string
	Editable       bool
	Masked         bool
	Sensitive      bool
}

// WidgetVerdict is the binding answer: compatible plus the stable
// reasons, in validation order, when not. Reasons stay nil on
// success.
type WidgetVerdict struct {
	Compatible bool
	Reasons    []string
}

// ValidateWidgetBinding checks one binding against the registry: the
// widget must be registered, its version positive and supported, the
// authority class known, classification and source present, masked
// bindings carrying a display form, and external embeds never bound
// to sensitive context. Violations accumulate in fixed order.
func ValidateWidgetBinding(binding WidgetBinding, registry WidgetRegistry) WidgetVerdict {
	known := make(map[string]WidgetDefinition, len(registry.Widgets))
	for _, widget := range registry.Widgets {
		known[widget.ID] = widget
	}
	var reasons []string
	definition, ok := known[binding.WidgetType]
	if !ok {
		reasons = append(reasons, fmt.Sprintf("unknown widget %q", binding.WidgetType))
	} else if binding.WidgetVersion <= 0 || binding.WidgetVersion > definition.Version {
		reasons = append(reasons, fmt.Sprintf("unsupported widget version %d for %q", binding.WidgetVersion, binding.WidgetType))
	}
	if !authorityClasses[binding.AuthorityClass] {
		reasons = append(reasons, fmt.Sprintf("unknown authority class %q", binding.AuthorityClass))
	}
	if binding.Classification == "" {
		reasons = append(reasons, "missing classification")
	}
	if binding.SourceType == "" || binding.SourceID == "" {
		reasons = append(reasons, "missing binding source")
	}
	if binding.Masked && binding.DisplayValue == "" {
		reasons = append(reasons, "masked binding needs a display value")
	}
	if ok && definition.Tier == WidgetTierExternal && binding.Sensitive {
		reasons = append(reasons, fmt.Sprintf("external widget %q refuses sensitive context", binding.WidgetType))
	}
	if len(reasons) > 0 {
		return WidgetVerdict{Compatible: false, Reasons: reasons}
	}
	return WidgetVerdict{Compatible: true}
}

// ValidateDraftWidgets validates one draft through its widget
// bindings. Every binding must validate; reasons accumulate across
// bindings in composition order.
func ValidateDraftWidgets(draft PageDraft, registry WidgetRegistry) WidgetVerdict {
	var reasons []string
	for _, binding := range draft.Composition.Widgets {
		verdict := ValidateWidgetBinding(binding, registry)
		reasons = append(reasons, verdict.Reasons...)
	}
	if len(reasons) > 0 {
		return WidgetVerdict{Compatible: false, Reasons: reasons}
	}
	return WidgetVerdict{Compatible: true}
}
