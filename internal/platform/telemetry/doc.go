// Package telemetry implements the owned telemetry contracts named in
// planning/todos.md OBS-001, OBS-004, OBS-005 and OBS-006:
//
//   - OBS-001: a signal-neutral Resource, context-propagation keys
//     (correlation/request/evidence/principal reference), an attribute
//     allow-list with per-key classification, and the Envelope that a
//     handler or exporter must pass through before anything reaches it.
//   - OBS-004: a policy Evaluator that classifies, redacts, caps
//     cardinality, applies deterministic sampling and decides which sink
//     classes may receive which attribute classes.
//   - OBS-005: a versioned, immutable metric catalog plus generated
//     dashboard/alert definitions for the P1A cell.
//   - OBS-006: a completeness checker that certifies telemetry health from
//     what a sink actually emitted, and never reports healthy on a missing
//     or failed signal.
//
// Telemetry diagnoses software. It is not a business ledger, audit log,
// billing source, authorization fact or proof that an external effect
// completed (structured-logging-and-opentelemetry.md, "Scope"). This
// package never imports go.opentelemetry.io or any backend SDK: OBS-002
// (a later, out-of-lane todo) owns the OpenTelemetry adapter that will sit
// behind these contracts. It also never imports internal/platform/logging,
// which owns the structured-log envelope (OBS-009/010/011) as a separate,
// concurrently developed lane; the context-propagation keys defined here
// are this package's own copy for metric/span-shaped signals; the log
// envelope's copy is internal/platform/logging's alone to evolve. See
// "anything needed elsewhere" in the delivery report for the follow-up this
// implies.
//
// definitions/telemetry/*.yaml is the machine-readable mirror of this
// package's schema: the attribute allow-list, export policy and metric
// catalog are exercised as the single source of truth, and this package's
// Go registrations are tested against the YAML file directly so the two
// can never silently drift apart.
package telemetry
