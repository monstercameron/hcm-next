// Package manifest implements ENDPOINT-001, ENDPOINT-009, PROTO-009,
// PROTO-010 and the pure rendering half of API-001.
//
// It compiles one canonical, deterministic EndpointManifest from three
// reviewed sources — the generated Protobuf service descriptors
// (gen/go/hcmnext/{intents,registry}/v1, discovered through
// google.golang.org/protobuf/reflect/protoregistry rather than a
// hand-maintained route table), the BOOTSTRAP capability registry
// (internal/capability), and the fourteen BusinessIntent definitions
// (internal/intent/definitions) — so that a handwritten, unowned or
// unbound route can never publish a production method
// (planning/specs/http-grpc-endpoint-contract.md "Endpoint manifest").
//
// # Relationship to internal/transport/validate.go
//
// internal/transport/validate.go carries a hand-written requiredFields
// table as an explicit placeholder: "This table is the placeholder for the
// generated endpoint manifest. When ENDPOINT-001 lands, [DefaultValidator]
// reads the manifest instead and this map goes away." That file is frozen
// for this change (owned by a different lane); this package generates the
// equivalent presence-rule table independently, in [RequiredFieldPaths], so
// the two can be diffed without editing either. See presence.go for the
// side-by-side citation of every entry.
//
// # Determinism
//
// [Build] never reads the clock, the environment, or map-iteration order
// into its output: every slice it returns is explicitly sorted, so calling
// it twice — in the same process or a different one — produces
// byte-identical [EndpointManifest.CanonicalJSON] output and the same
// [EndpointManifest.Digest]. [TestTodo_ENDPOINT_001_Property] proves this.
//
// # Four surfaces, one source
//
//   - [Build] / [EndpointDefinition]: ENDPOINT-001 and PROTO-010 — one row
//     per RPC method of IntentService and RegistryService, with owner,
//     capability binding, gRPC/HTTP transport exposure, P1A [Disposition],
//     request presence rules, idempotency class and deadline budget.
//   - [BuildDefaultDispositionReport] / [IntentDisposition] /
//     [CapabilityDisposition]: ENDPOINT-009 — every one of the fourteen
//     intent definitions and every BOOTSTRAP capability maps to exactly one
//     [EndpointDispositionCategory]; a missing mapping is a build-time
//     (RED) failure, never a silent gap.
//   - [BuildMessagePolicies]: PROTO-009 — one unknown-field and
//     dynamic-type policy per Protobuf message reachable from the two
//     services, including the explicit dynamic-type allowlist that bounds
//     hcmnext.intents.v1.TypedPayload's opaque wire bytes.
//   - [RenderDiscoveryDocument]: the API-001 pure function that projects an
//     [EndpointManifest] plus its disposition and capability inputs into a
//     served-shape [DiscoveryDocument]. It has no side effects and no
//     transport binding of its own; wiring it onto a live gRPC reflection
//     service or HTTP handler is a different lane's work.
package manifest
