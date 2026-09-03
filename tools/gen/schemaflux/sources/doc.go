// Package sources loads, resolves and cross-checks the HCM SchemaFlux
// metamodel and entity/relationship/registry sources (MSRC-002 through
// MSRC-005):
//
//	schema/schemaflux/metamodel/v1/metamodel.yaml    (MSRC-002)
//	schema/schemaflux/registries/v1/registries.yaml  (authorities, retention classes)
//	schema/schemaflux/entities/v1/*.yaml             (per-family entity/relationship sources)
//
// It is a sibling of tools/gen/schemaflux, not a replacement for it: that
// package's Definition/Catalog/Compile pipeline governs the fourteen
// business-intent definitions and P1A capability manifest (MSRC-001) and
// remains untouched. This package governs a different kind of source — the
// metamodel and entity registry that MSRC-002 through MSRC-005 encode — and
// reuses tools/gen/schemaflux's exported [schemaflux.Digest] and
// [schemaflux.CombinedDigest] helpers rather than duplicating them.
//
// # Cross-checking
//
// An entity, relationship, authority or retention class source entry may
// declare covered: true, asserting that it matches a live entry in
// internal/intent/model.Catalog() (the compiled MODEL-011..MODEL-030
// registry). [CrossCheckModel] verifies that claim field-for-field and
// reports every mismatch; it never silently accepts drift between the YAML
// source and the Go registry, and it never writes to internal/intent/model.
//
// # Resolution
//
// [Compile] parses no files itself — callers assemble a [Bundle] via the
// Load* functions — but it resolves every cross-reference a bundle's sources
// declare (relationship endpoints, property authority/retention references,
// aggregate child references) against the bundle's own combined vocabulary
// and the metamodel's declared enums, and returns a typed
// [UnresolvedReferenceError] for each one that does not resolve. A source
// with zero unresolved references compiles to a [Manifest] that
// [Manifest.YAML] renders deterministically for
// definitions/generation/model-sources.yaml.
package sources
