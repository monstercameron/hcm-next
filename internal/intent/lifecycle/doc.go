// Package lifecycle owns the five intent lifecycle dimensions and the fixed
// legality rules that govern them.
//
// Semantic owner: intent kernel. Phase: P1A.
//
// An intent carries exactly five state dimensions and no universal collapsed
// status:
//
//	RequestState       where is the request itself?
//	ExecutionState     what has the runtime done?
//	BusinessState      did the business outcome happen?
//	ConsistencyState   does observed external state agree with intent?
//	ObligationState    are attached obligations discharged?
//
// Proposal revisions, approval bindings, closure records, incidents and
// outcome tracking are linked records, not further dimensions. Adding a sixth
// dimension or a collapsed status requires a recorded scope exchange, so the
// package deliberately exposes no way to do either.
//
// Independence does not mean every tuple is legal. Legality is a short fixed
// rule set — exactly the six rules [Rules] returns — evaluated by one function,
// [Check], which command transitions, projection rebuild, replay and repair all
// share. A projection may never repair an illegal tuple by silently selecting a
// preferred status; [Replay] fails instead.
//
// [Profile] and [Machine] carry the lifecycle-definition layer: declared states
// and transitions per dimension, unreachable-state and undeclared-transition
// rejection, and the append-only [History] of [TransitionRecord]s that every
// authoritative transition writes.
package lifecycle
