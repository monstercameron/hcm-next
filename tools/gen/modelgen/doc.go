// Package modelgen implements MSRC-007: it reads the compiled
// [github.com/monstercameron/human-capital-management-suite/internal/intent/model] registry — the
// same registry [github.com/monstercameron/human-capital-management-suite/tools/gen/storagemanifest]
// reads for DB-002/003/004 — and deterministically generates typed Go model
// values, per-entity validators and a generated registry into
// gen/go/hcmnext/model.
//
// SchemaFlux was disqualified as this generator's engine
// (definitions/generation/schemaflux-qualification.yaml, TOOL-004): every
// SchemaFlux entry point is a model-provider call with no real offline path,
// so it cannot compile a structured definition into Go without either
// dialing out or returning synthetic filler. This package is the same
// "generation is deterministic tooling, not an AI call" fallback the
// qualification record describes for the fourteen business-intent
// definitions, applied here to the model registry instead: [Build] and
// [Render] are pure functions of the compiled registry, so two runs against
// the same source always produce byte-identical output and never perform
// network I/O.
//
// Generation happens in two pure stages. [Build] walks the registry and
// resolves each property's declared Go type against a fixed, closed type
// table (typeTable in types.go); an unrecognized type fails generation
// rather than falling back to map[string]any, which is the literal MSRC-007
// RED clause ("generated type ... uses map[string]any for modeled fields").
// [Render] turns the resulting [ModelSet] into formatted Go source: one
// struct type per entity with a field per owned property, a Validate method
// enforcing every REQUIRED property's presence and, for properties typed as
// an internal/kernel/values invariant type (EffectiveInterval, Instant,
// KnownAt, RecordedAt) or an internal/intent/lifecycle Dimensions tuple,
// that value's own Validate(); plus a generated Registry publishing typed
// entity/property/relationship metadata, a precomputed immutable-write flag
// per property (MSRC-009 depends on this), and a canonical digest.
//
// The generated package is committed under gen/go/hcmnext/model exactly like
// the protoc-gen-go output beside it, and this package's own tests are what
// prove it is current: [TestTodo_MSRC_007_Golden] regenerates in memory and
// diffs the result against the checked-in file (the TOOL-010 drift-test
// pattern), and pins a golden content digest.
package modelgen
