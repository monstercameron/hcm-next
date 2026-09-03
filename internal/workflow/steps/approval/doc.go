// Package approval implements the pure resolution boundary between governed
// approval WorkItems and the workflow frontier.
//
// The package owns no queue, decision store, clock, transaction, or scheduler.
// A caller loads immutable work-item and approval-decision evidence, supplies
// the current instant and any explicit invalidation or cancellation event, and
// Resolve returns either an awaiting marker or one of APPROVED, REJECTED,
// INVALIDATED, EXPIRED, and CANCELLED. Persistence remains owned by Human Work
// and Intent approval; advancing the workflow remains owned by the runtime.
package approval
