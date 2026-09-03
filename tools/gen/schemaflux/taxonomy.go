package schemaflux

import "regexp"

// declaredKernelFamilies are the three live kernel families
// (schema/proto/hcmnext/intents/v1/business_intent.proto KernelFamily).
// PROCESS_REQUEST, FILING_REQUEST, CASE and BATCH_OPERATION are retired
// (reserved in the proto) and deliberately excluded: a definition that still
// declares one of them is not resolved, it is stale, matching the note in
// internal/intent/definitions.go about CreateRepairPlan's source YAML.
var declaredKernelFamilies = map[string]bool{
	"CHANGE_REQUEST":      true,
	"CALCULATION_REQUEST": true,
	"ANALYTICAL_REQUEST":  true,
}

// declaredOwnerPlanes and declaredOwnerDomains are the taxonomy the fourteen
// source definitions declare today (schema/schemaflux/business_intents/v1).
// Extending either is a source change, matching the closed-catalog posture
// internal/intent/definitions.go documents for the compiled table itself.
var declaredOwnerPlanes = map[string]bool{
	"DOMAIN":               true,
	"WORKFLOW":             true,
	"INTELLIGENCE":         true,
	"OPERATIONS_ASSURANCE": true,
}

var declaredOwnerDomains = map[string]bool{
	"PEOPLE":           true,
	"REWARDS":          true,
	"WORKFORCE_BUDGET": true,
	"HUMAN_WORK":       true,
	"PROVENANCE":       true,
	"RECONCILIATION":   true,
	"REPAIR":           true,
}

// refShape matches the canonical "<dotted.name>/v<version>" spelling every
// schema and capability reference in the source YAML uses (see
// internal/intent/definitions.go's schema() helper, which panics on anything
// that does not split on "/v"). Version 0 and leading-zero versions are
// rejected, matching internal/intent.Ref.Validate's "non-canonical version"
// rule.
var refShape = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z0-9_]+)+/v[1-9][0-9]*$`)

// looksLikeRef reports whether s has the canonical "<dotted.name>/v<version>"
// shape. It is a structural check only: no live schema or capability
// registry ships in this repository yet (that is separate, later work), so
// today's qualified generator can validate reference *shape*, and rejects
// anything that does not parse rather than silently accepting free text.
func looksLikeRef(s string) bool {
	return refShape.MatchString(s)
}
