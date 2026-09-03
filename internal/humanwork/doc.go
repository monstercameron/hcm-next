// Package humanwork owns the approval-requirement half of Human Work: what a
// proposal must have approved, by whom, how many of them, in what order, by
// when, and what happens when nobody answers.
//
// Semantic owner: human work (BI.WORK). Phase: P1A.
//
// The package draws one line and holds it everywhere: a candidate is not an
// approver. Resolution answers "who is authorized to decide this", which is
// also the input to task routing, but routing a task to a person never grants
// that person decision authority and decision authority is re-evaluated at
// decision time by internal/intent/approval, not read back off the task.
//
// The resolution language is deliberately closed. It is a small typed tree of
// named, role, relationship, group, any, all and quorum nodes with a bounded
// depth and node count - not CEL, not a script, not a string that a future
// tenant can turn into a program. Everything it can ask, it asks through the
// Directory port, so resolution reads a supplied projection and never a
// database, a clock or a network.
package humanwork
