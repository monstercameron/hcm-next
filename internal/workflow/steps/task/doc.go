// Package task implements the pure resume boundary for governed TASK nodes.
//
// It does not implement forms, queues, claims, storage, or authorization. A
// caller supplies a durable WorkItem and, for a completed item, an immutable
// typed Submission plus a Validator. Resolve verifies their exact binding and
// returns a workflow-frontier result without performing any write or reading
// ambient time.
package task
