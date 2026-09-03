// Package inspector implements ADMIN-002: a read-only, authorization-gated
// projection of one compiled workflow plan's execution state for the
// governed execution inspector (planning/specs/workflow-runtime.md
// "Execution Inspector and Deterministic Replay"; planning/specs/
// hris-admin-dataops.md).
//
// [BuildWorkflowView] traverses a *workflow.CompiledWorkflow's node/edge
// graph - the same immutable IR the runtime pins - and overlays it with
// whatever an [ExecutionTrace] reports: per-node status and evidence, and
// pending human work. WF-RUN-019 (the durable runtime's own execution and
// attempt tables) has not shipped yet, so in P1A the only trace source is a
// [SimulationReceipt] a caller assembles from a SIMULATE-mode run of the
// plan. ExecutionTrace is the seam: it is defined here, minimally, exactly
// so a later durable runtime can satisfy it with real attempt history
// without this package - or anything built against it - changing.
//
// Every BuildWorkflowView call takes the caller's own
// internal/trust/authz.Decision. A view built under a decision whose
// subject is not disclosable, or whose gated fields are denied, never
// populates the corresponding node status, attempt, evidence references,
// failure reason or pending human work: those come back as the typed
// unavailable state [StatusUnknown] and empty slices, never as an omitted
// struct field a caller could mistake for "not yet computed" versus "not
// authorized to see." This package performs no database access, no
// workflow execution and no mutation of any kind; it is a pure function of
// its three inputs (plan, trace, decision).
package inspector
