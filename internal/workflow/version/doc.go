// Package version publishes immutable compiled workflow versions
// (owner: workflow-runtime; phase: P1A/P1B boundary; WF-COMP-006).
//
// # Boundary
//
// [internal/workflow.Compile] turns a draft [workflow.Definition] into an
// immutable [workflow.CompiledWorkflow]. That in-memory value is not yet a
// publication: nothing records who compiled it, against which compiler, with
// what evidence, or whether anyone with authority ever agreed to run it. This
// package is the publication boundary planning/specs/workflow-runtime.md's
// "Compiler and Publication Pipeline" describes: a [CompiledVersion] is the
// durable, governed record that a compiled plan actually existed, was
// reviewed, and was authorized to run — the artifact WF-RUN-023 pins an
// instance to.
//
// # What is here and what is not
//
// [Publish] mints a [CompiledVersion] in [StatusDraft] and refuses anything
// that would make the record lie about its own content: a plan whose digest
// does not match recompiling its definition, or a caller-supplied digest,
// status or publication clock. [Activate] is the governance gate: it moves a
// DRAFT version to ACTIVE only when the caller presents authorization,
// undisturbed review evidence, a passing test record and every declared
// dependency already active — exactly the RED clause WF-COMP-006 states.
// [Quarantine] and [Retire] are the other lifecycle exits; none of them edit
// a [CompiledVersion] in place; each produces a new copy through [Store.Put].
//
// There is no database code here. [Store] is the port a durable adapter
// implements later; [Registry] is the in-memory adapter this phase ships.
// This package never invokes a workflow, a capability or a clock — every
// timestamp in a [CompiledVersion] is supplied by the caller.
package version
