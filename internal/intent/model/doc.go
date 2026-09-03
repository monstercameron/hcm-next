// Package model implements the MODEL-011..MODEL-030 registries that turn the
// exploratory business-entity catalog under planning/data/models into the
// normative closure model those documents describe: EntityDefinition and
// PropertyDefinition publication (MODEL-011), aggregate ownership
// registration (MODEL-012), relationship definitions with bitemporal
// constraints (MODEL-013), the schema release lifecycle (MODEL-017),
// provenance graph edges (MODEL-020), source-authority assignments
// (MODEL-021), data-classification propagation (MODEL-023), records
// declarations and retention assignment (MODEL-026), and the model coverage
// report (MODEL-030).
//
// The package is scoped to the fourteen aggregate roots the drafted intent
// catalog and the Promotion path actually require (see catalog.go), not the
// full exploratory vocabulary in planning/data/models. Registries here are
// immutable once compiled, exactly like [github.com/monstercameron/hcm-next/internal/intent.Registry]:
// there is no Add, no Remove and no setter, so publishing a new entity,
// property, relationship or policy is a source change, never a runtime call.
package model
