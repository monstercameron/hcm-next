// Package productslice defines ALIGN-001's machine-readable
// ProductSliceDefinition: the one record that admits a default-shipped
// product feature by naming every reference it depends on -- the business
// intents it delivers, the features it covers, the pages and widgets it
// renders, the capabilities it invokes, the Phase 1 packages it needs, and
// the todos that prove it -- so a slice can be validated against the real,
// already-governed registries those references name instead of a second
// hand-maintained catalog that could silently drift from them
// (planning/specs/default-product-slice-alignment.md "ProductSliceDefinition").
//
// The specification sketches a much larger record (human_jobs, floorplans,
// query/command/event contracts, classifications, retention policy, and
// more). ALIGN-001 is scoped to the eleven fields below: identity/version,
// the seven reference lists [ProductSliceDefinition.Validate] resolves
// against a live [Registries] snapshot, plus jurisdictions, personas and
// exit criteria as descriptive fields with no registry of their own today.
// A later ALIGN todo extends this schema; it must add fields, not rename or
// remove these, since [Digest] and the checked-in
// definitions/planning/product-slices.yaml depend on the exact shape.
package productslice

// ProductSliceDefinition is one admitted default-shipped product slice: the
// smallest unit the release gate reasons about when it asks "does every
// route/action have a business owner, does every material state have a
// durable disposition, does every table have a consumer and owner" (the
// contract's "compiler rejects a slice when..." rule). This type is the
// GREEN half of that rule: [ProductSliceDefinition.Validate] is the
// compiler, and an empty or dangling field is exactly what it refuses.
type ProductSliceDefinition struct {
	// SliceID identifies this slice across versions, e.g. "promotion".
	// Version, not SliceID, changes between revisions of the same slice.
	SliceID string `yaml:"slice_id" json:"slice_id"`

	// Version is this definition's revision number. It starts at 1;
	// [ProductSliceDefinition.Digest] changes whenever any semantically
	// meaningful field changes, including Version itself.
	Version int `yaml:"version" json:"version"`

	// BusinessIntents names, by id, the BusinessIntent(s) this slice
	// delivers. Each ref must resolve to a bound intent id in the real
	// feature/intent coverage registry (definitions/governance/
	// feature-intent-coverage.yaml, INTENT-010) -- never an intent this
	// slice invents.
	BusinessIntents []string `yaml:"business_intents" json:"business_intents"`

	// Features names, by coverage-registry id, the intake features this
	// slice covers. Each ref must resolve to a feature_id in the same
	// coverage registry as BusinessIntents.
	Features []string `yaml:"features" json:"features"`

	// Pages names, by page id, the PageDefinitions (tools/uxqual/pagedef,
	// WEB-002) this slice renders.
	Pages []string `yaml:"pages" json:"pages"`

	// Widgets names, by "<id>@<version>" registry ref, the governed widgets
	// (tools/uxqual/widgetreg, WEB-005) this slice's pages place.
	Widgets []string `yaml:"widgets" json:"widgets"`

	// Capabilities names, by id, the published capabilities
	// (internal/capability) this slice invokes. Each ref must resolve to a
	// capability id the BOOTSTRAP registry publishes -- the same registry
	// BIND-001's binding table (internal/capability/binding) binds to wire
	// methods and Go handlers.
	Capabilities []string `yaml:"capabilities" json:"capabilities"`

	// Packages names, by full module import path, the packages this slice
	// needs from the Phase 1 production closure (tools/policy/phaseonegate,
	// ARCH-GO-018). A ref outside that live allowlist is refused: a slice
	// can never smuggle authority to run code the release gate has not
	// admitted.
	Packages []string `yaml:"packages" json:"packages"`

	// Todos names, by id, the planning/todos.md todo(s) whose passing test
	// suite proves this slice (definitions/planning/todo-registry.json).
	Todos []string `yaml:"todos" json:"todos"`

	// Jurisdictions names the legal/regulatory jurisdictions this slice is
	// admitted for (e.g. "US-ALL", or a specific state code). ALIGN-001
	// does not resolve these against a jurisdiction registry -- a later
	// ALIGN todo owns that vocabulary -- it only requires at least one.
	Jurisdictions []string `yaml:"jurisdictions" json:"jurisdictions"`

	// Personas names the acting roles this slice is designed for (e.g.
	// "manager", "compensation.approver"). Not resolved against a registry
	// for the same reason as Jurisdictions.
	Personas []string `yaml:"personas" json:"personas"`

	// ExitCriteria records the human-readable release-conformance
	// statements (specs/default-product-slice-alignment.md "Release and
	// conformance rule") this slice must prove before it ships. These are
	// prose, not refs, and are not resolved against a registry.
	ExitCriteria []string `yaml:"exit_criteria" json:"exit_criteria"`
}

// clone returns a deep copy so a caller can never mutate this definition's
// backing arrays through a returned value.
func (d ProductSliceDefinition) clone() ProductSliceDefinition {
	d.BusinessIntents = append([]string(nil), d.BusinessIntents...)
	d.Features = append([]string(nil), d.Features...)
	d.Pages = append([]string(nil), d.Pages...)
	d.Widgets = append([]string(nil), d.Widgets...)
	d.Capabilities = append([]string(nil), d.Capabilities...)
	d.Packages = append([]string(nil), d.Packages...)
	d.Todos = append([]string(nil), d.Todos...)
	d.Jurisdictions = append([]string(nil), d.Jurisdictions...)
	d.Personas = append([]string(nil), d.Personas...)
	d.ExitCriteria = append([]string(nil), d.ExitCriteria...)
	return d
}
