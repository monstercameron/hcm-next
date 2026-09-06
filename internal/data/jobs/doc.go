// Package jobs is JOB-001's durable store for governed job definitions,
// runs, partitions and checkpoints (migration 00034_job_definitions.sql).
//
// # Why this package exists
//
// planning/todos.md's JOB-001 asks for exactly what migration 00026 already
// built for workflow scheduling state (DB-012): CAS-fenced live rows for
// state that opens and advances, append-only evidence for facts that must
// never be rewritten, and no sweeper. This package is that same house
// pattern applied to batch jobs instead of workflow runtime. job_definition
// publishes an immutable revision the same way migration 00003's
// definition_version does; job_run and job_partition are compare-and-swap
// fenced live rows the same way migration 00016's workflow_instance and
// 00026's workflow_ready_work are; job_checkpoint is append-only evidence the
// same way 00026's workflow_checkpoint is.
//
// # What this deliberately is not
//
// Nothing in this package claims a partition, fires a job on a trigger, or
// dispatches work on a clock. The WF-RUN-000 gate that blocks scheduler code
// in front of the workflow runtime's durable state applies here too, by the
// same reasoning: Publish, StartRun, ClaimPartition and Checkpoint are all
// caller-driven -- a caller observes a trigger firing (SCHED-001) or a
// governance decision and calls into this store directly. A future
// dispatcher (JOB-002's admission, JOB-003's fenced leases) is what will call
// these methods on a schedule; building that dispatcher is out of scope
// here.
//
// job_definition's trigger_digest and target_definition_ref/version are
// value references, not foreign keys: internal/engines/schedule's published
// triggers are a content-addressed, in-memory publication port with no
// durable table yet, and the intent type catalog is declared configuration
// this schema does not own -- the same reasoning migration 00004's
// intent_instance.definition_ref already rests on.
package jobs
