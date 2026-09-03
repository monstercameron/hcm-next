// Package definitions is the compiled-in intent catalog.
//
// Semantic owner: intent-and-capability. Phase: P1A.
//
// The catalog is the set of IntentDefinitions that exist in repository source
// with a resolvable owner. Today that is the fourteen drafted definitions in
// this package. The roughly 530 candidate names supplied at project intake are
// non-normative naming vocabulary: they are not the catalog, they have no
// maturity state, and nothing here counts, partitions or attests them.
//
// Under the registry BOOTSTRAP profile the catalog is a Go table and the build
// is the publication. Adding a definition is a source change: there is no
// runtime publish call and no count to keep in sync, which is why
// [Registry]'s only denominator is the length of this table.
//
// The declarations mirror the reviewed SchemaFlux sources under
// schema/schemaflux/business_intents/v1 and the release slice in
// planning/specs/business-intent-catalog.md. Where the two disagree the
// specification wins; see the note on CreateRepairPlan in definitions.go.
package definitions
