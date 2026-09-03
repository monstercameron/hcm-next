// Package wait implements WF-STEP-005: the WAIT step's pure timer semantics.
//
// The durable-runtime gate ruling (definitions/runtime/durable-runtime-decision.yaml,
// "Prototype ruling") forbids a timer wheel, sleeper, poller, background
// worker, lease or retry loop in this phase. WAIT conformance is instead a
// durable, typed continuation record — [TimerRequirement] — plus a pure
// resolution function, [Resolve], that a caller-driven runtime invokes with
// caller-supplied time and events. Nothing in this package sleeps, reads the
// wall clock or starts a goroutine; TestTodo_WF_STEP_005_Conformance asserts
// that by scanning the package's own source.
//
// [ComputeTimerRequirement] takes a [CompiledWaitNode] — the WAIT-specific
// wake condition and dataset-versioning policy a compiled WAIT node carries —
// plus the dataset versions in force, and produces a [TimerRequirement]: a
// fire-at instant, the calendar/timezone/tzdb identity that produced it, the
// calculation evidence, and a content digest. A local wall-clock wake
// condition that lands on a DST gap or fold under a REJECT_GAP policy is
// refused into the requirement itself (ReviewRequired), never guessed.
//
// internal/workflow's compiled Node/CompiledNode types do not yet carry
// WAIT-specific fields (compile.go and definition.go are out of this lane's
// scope, and no accessor into them was needed: CompiledNode has nothing
// wait-specific to expose). [CompiledWaitNode] is this package's own typed
// view of what a compiled WAIT node declares; a future compiler pass is
// expected to populate one from the real compiled plan.
package wait
