// Package schemaflux is the SchemaFlux qualification fixture and, because
// that qualification fails, the protoc-fallback qualified generator it
// selects instead (TOOL-004, TOOL-005, MSRC-001).
//
// # Qualification result
//
// planning/specs/go-only-technology-constitution.md "SchemaFlux (preferred
// definition generation)" names one fixture: compile a bounded catalog (the
// fourteen intent definitions under schema/schemaflux/business_intents/v1
// plus the P1A capability manifests) to byte-identical output across two runs
// and two machines, offline, with no model call. github.com/monstercameron/
// schemaflux v1.2.0 is disqualified: its entire public surface (see its
// schemaflux.go — Extracting, Generating, Transforming, Choosing,
// Summarizing, and roughly ninety more builders) is a typed LLM operation
// that dials a configured model provider through internal/llm, which builds
// a plain `&http.Client{}` with no offline code path once a real provider is
// selected. Its own deliberate offline stand-in
// (SCHEMAFLUX_PROVIDER=local / Client.WithMockProvider) answers every
// operation with shape-correct but, by its own mockshape.go's words,
// "obviously synthetic" filler unrelated to the real input — client.go
// separately documents falling back to it silently as producing output
// "indistinguishable from a working deployment until someone reads the
// output," which is why WithMockProvider must be asked for explicitly. Either
// way it rules SchemaFlux out for producing a real registry, catalog or
// fixture. schemafluxtest's cassette
// Player replays a previously recorded model answer; it cannot originate one,
// so it is a unit-test double for code that calls SchemaFlux, not a
// definition compiler. [TestSchemaFluxOfflineFixture] exercises both claims
// empirically under a network-blocking transport before falling back.
//
// The recorded decision lives in definitions/generation/schemaflux-
// qualification.yaml: SchemaFlux disqualified, protoc-style deterministic Go
// generation selected. Protobuf remains the wire contract authority either
// way ([schema/proto/hcmnext/intents/v1/business_intent.proto]); this
// package never becomes a second business schema. It only turns the
// structured YAML sources into a Go registry, a markdown catalog and JSON
// fixtures, and it never imports net/http for anything other than proving
// SchemaFlux would have needed it.
//
// # What this package is not
//
// It does not replace internal/intent/definitions, the hand-authored
// compiled-in P1A registry (frozen, out of this package's lane). TestTodo_
// TOOL_004_Conformance cross-checks this package's independently generated
// catalog against that table's names, families and versions and reports any
// mismatch; it never writes to internal/.
package schemaflux
