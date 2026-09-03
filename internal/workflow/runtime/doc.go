// Package runtime persists WorkflowInstance and NodeExecution state for the
// P1A workflow kernel (owner: workflow-runtime; phase: P1A; WF-RUN-001).
//
// # What this is
//
// planning/specs/workflow-runtime.md separates four logical stores. This
// package owns exactly one of them:
//
//	RUNTIME   instances / node executions   <- this package
//	DEFINITIONS / BUSINESS EVIDENCE / OPERATIONAL INDEXES   elsewhere
//
// Runtime state answers "where is execution now". The ledger answers "how the
// business reached this state", and nothing here duplicates it: an instance
// row carries references and digests, never business facts and never raw
// payloads. WF-RUN-001's REFACTOR clause is that division, and it is why
// [Instance.InputRef] is a digest rather than an input document.
//
// # What this deliberately is not
//
// definitions/runtime/durable-runtime-decision.yaml (WF-RUN-000) records the
// BUILD decision and, in the same breath, a blocking re-evaluation gate in
// front of every P1B scheduler, lease, fencing and timer primitive. So there
// is no scheduler here, no lease table, no fence token, no timer, no retry
// backoff and no background worker. There is also no Start and no Advance:
// this package records state a driver decided on, it does not decide.
//
// Concurrency is carried by one mechanism only — optimistic versioning on the
// instance row. [Store.RecordInstanceState] and [Store.RecordNodeExecution]
// take the instance version the caller believes it holds and refuse with
// [CodeStaleInstance] when the stored version has moved. That is fencing-free
// by construction: nobody is trusted because they hold a lease, only because
// their view of the instance is still current.
//
// # Legal transitions
//
// The instance and node state machines in the spec are encoded in
// [LegalInstanceTransition] and [LegalNodeTransition], and every recorded
// transition is checked against them before it reaches the database. An
// illegal transition is [CodeIllegalTransition]; the check is in Go rather
// than a trigger because the transition table is the same artifact the
// compiler and the inspector read, and two copies of it would drift.
//
// # Identity
//
// A node execution's identity is derived, not allocated:
// [NodeExecutionID] hashes (tenant, instance, node, attempt) into a stable
// UUID. Recording the same attempt twice therefore collides on the primary key
// instead of forking a second identity for one execution, and a caller that
// restarted mid-write can recompute the id it was going to use.
//
// # Transactions
//
// [Store.RecordNodeExecution] and [Store.RecordNodeTransition] issue two
// statements — the instance-version compare-and-set, then the node write — and
// this package never opens a transaction of its own. Pass a [dbport.Tx] the
// caller began and will finish; on a bare connection the two statements
// autocommit independently and a failed node write would leave the instance
// version bumped with nothing to show for it. Reads are single statements and
// need no transaction.
//
// # Tenant scoping
//
// Every row carries tenant_id explicitly and migrations/00016_workflow_runtime.sql
// puts a fail-closed row level security policy on both tables. A caller that
// wants that policy enforced sets app.tenant_id the way
// internal/data/tenancy.WithTenant does; this package does not re-export that
// helper, and it never infers a tenant from a session.
package runtime
