// Package effects supplies the two production ports
// internal/workflow/execute's own bounded driver (Options.DB/Steps/
// WorkItems/Terminal/Guard/Retention) does not itself provide an
// implementation for:
//
//   - [PolicyResolver], a data-driven runtime.WorkflowResolver: which
//     compiled workflow/pin a start request binds is looked up in a small
//     ordered table the composition root supplies, never a graph a business
//     service embeds.
//   - [LedgerTerminalWriter], an execute.TerminalWriter that performs the
//     one governed business write a workflow instance's COMPLETE
//     continuation raises -- the promotion outcome appended to the ledger
//     through internal/data/outbox.Commit (one ledger event, one projection
//     advance, one outbox message) inside the same transaction
//     internal/workflow/execute's own continuation sink already wraps in
//     internal/transaction/idempotency.Guard.
//
// Neither type owns a workflow graph, a clock, a goroutine or a retry loop:
// every instant is the caller's own RecordedAt, and every write happens
// exactly once inside the transaction execute's driver supplies.
package effects
