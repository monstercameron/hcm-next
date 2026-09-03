// Package frontier derives workflow frontier advancement deterministically
// (WF-RUN-024).
//
// Owner: workflow. Phase: P1B (WF-RUN-024).
//
// # What this package is
//
// [Advance] is the whole package: a pure function from a pinned compiled plan,
// a value snapshot of one instance's state, and one typed node outcome, to the
// exact set of successors, node states, join counters, terminal dimensions and
// scheduling intents that follow. It is the arithmetic of "what happens next",
// separated from every mechanism that could make the answer depend on when or
// where it was asked.
//
// It therefore does none of the following, and there is no option that turns
// any of them on:
//
//   - It never calls a step handler. A handler's result arrives as a
//     [NodeOutcome] value; this package never produces one.
//   - It never enqueues anything. It returns [Intent] values naming the work a
//     runtime must persist, and the runtime persists them
//     ([Transition.Intents]).
//   - It never touches storage, the network, the clock or a random source. A
//     transition computed twice from the same three arguments is byte-identical,
//     which is what [Transition.Digest] proves.
//
// # Why the frontier is a value, not a handle
//
// [InstanceState] is a snapshot of values — sorted slices of comparable
// records, no maps, no pointers. Two consequences follow. A caller cannot hand
// this package a live row and have it mutated underneath a transaction, and a
// transition can be recomputed from a persisted snapshot long after the process
// that first computed it is gone, which is what makes WF-RUN-025's
// commit-and-retry-returns-its-receipt claim provable.
//
// # Explicit routing, all the way down
//
// The compiler proves at publication time that every outcome a node can
// produce has an explicit edge (internal/workflow, WF-COMP-002). This package
// makes the same refusal at advancement time rather than trusting that proof:
//
//   - An outcome with no outgoing edge is [CodeMissingRoute], never a guessed
//     first edge.
//   - A DECISION whose evaluator returned a route key it never declared takes
//     the node's explicit default_route if one is declared, and is
//     [CodeNoMatchingRoute] if none is. A default is applied because it was
//     declared, never because a route was missing.
//   - A JOIN activates only when its declared strategy's count is met. A JOIN
//     with no declaration in the state is [CodeJoinNotDeclared]; the strategy is
//     never inferred at advancement time.
//   - A branch the taken route excluded is marked SKIPPED and receives no
//     intent. A skipped node never activates work.
//   - An END whose completion would leave other nodes on the frontier is
//     [CodeTerminalFrontierRemains], naming them. A workflow does not end
//     quietly while work is still outstanding.
//
// # Scheduling intent vocabulary
//
// [Advance] returns intents, not actions. The five kinds are [IntentReady],
// [IntentWorkItemRequired], [IntentSignalSubscriptionRequired],
// [IntentTimerRequired] and [IntentComplete]. Which kind a successor produces
// is a function of its compiled step type alone, so a runtime cannot decide
// that an APPROVAL is really ready-work or that a WAIT needs no timer.
package frontier
