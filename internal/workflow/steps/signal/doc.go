// Package signal implements WF-STEP-006: the SIGNAL step's pure correlation
// and acceptance semantics.
//
// The durable-runtime gate ruling (definitions/runtime/durable-runtime-decision.yaml,
// "Prototype ruling") forbids a background poller or worker in this phase.
// SIGNAL conformance here is a durable, typed [SignalSubscription] plus a
// pure [Accept] function a caller-driven runtime invokes with a
// caller-supplied inbound [Signal] and time. Accept never reads a network
// socket, a webhook queue or the wall clock; every input it needs — the
// subscription, the signal, the signals already recorded against it, a
// signature [Verifier], and "now" — is passed in explicitly.
//
// Accept enforces, in order, the boundary checks planning/workflows/_engine/
// workflow-context-contract.md §1/§3 require before an external event may
// advance material work: tenant match, event-type/correlation-key match
// (unmatched), correlation-value match, schema match, source allowlist,
// signature, exactly-once idempotency (including the "same scoped id,
// different bytes is an incident" rule), declared ordering, and the
// subscription's close time (late). Every attempt — accepted, duplicate or
// refused — is recorded as an immutable [LogEntry] so it stays inspectable;
// only a first ACCEPTED schedules a continuation.
package signal
