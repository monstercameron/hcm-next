package productui

import (
	"fmt"
	"sort"
)

// WidgetDefinition is one registered widget contract: its type, trust
// tier, pinned version, and opaque classification ceiling.
// Registration pins the ceiling without comparing it; comparison
// happens in ValidateDraftCeiling against the publication ladder,
// and the Classification service taxonomy stays authoritative.
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

// WidgetPermission is one widget's catalog status under a page
// ceiling: the widget, whether the page may compose it, and the
// stable refusal reason when it may not. Reasons stay empty on
// permission.
type WidgetPermission struct {
	Widget    WidgetDefinition
	Permitted bool
	Reason    string
}

// PermittedWidgetCatalog lists every registered widget with its
// status under one page classification ceiling: a widget clears
// the catalog exactly when its classification limit sits at or
// below the ceiling. This is the capability side of ceiling
// enforcement — binding data still gates per binding — so
// authors learn a widget is unusable before composing it.
// Undeclared ceilings clear nothing and unranked labels refuse,
// both fail-closed; the catalog sorts by widget ID and never
// mutates the registry.
func PermittedWidgetCatalog(ceiling string, registry WidgetRegistry) []WidgetPermission {
	ordered := append([]WidgetDefinition(nil), registry.Widgets...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	catalog := make([]WidgetPermission, 0, len(ordered))
	ceilingRank, ceilingOK := ClassificationRank(ceiling)
	for _, widget := range ordered {
		permission := WidgetPermission{Widget: widget}
		switch {
		case ceiling == "":
			permission.Reason = "missing classification ceiling"
		case !ceilingOK:
			permission.Reason = fmt.Sprintf("unknown classification ceiling %q", ceiling)
		default:
			limitRank, limitOK := ClassificationRank(widget.ClassificationLimit)
			switch {
			case !limitOK:
				permission.Reason = fmt.Sprintf("unranked classification limit %q for widget %q", widget.ClassificationLimit, widget.ID)
			case limitRank > ceilingRank:
				permission.Reason = fmt.Sprintf("widget %q limit %q exceeds page ceiling %q", widget.ID, widget.ClassificationLimit, ceiling)
			default:
				permission.Permitted = true
			}
		}
		catalog = append(catalog, permission)
	}
	return catalog
}
