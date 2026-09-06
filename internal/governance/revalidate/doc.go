// Package revalidate implements GOVERN-003: revalidating a governance
// decision immediately before its effect executes.
//
// A [decision.Decision] composed at approval time (GOVERN-001/GOVERN-002) is
// evidence of what was true then. By the time a prepared [TransactionPlan]
// (internal/intent) is about to dispatch or commit, seven categories of fact
// it depended on may have moved: the current AuthZ verdict, the acting
// session's state, the per-write source-authority decision, the field
// classification version, the legal and policy rule-pack versions, the
// budget and position facts a reservation depends on, and the conflict
// classification against everything else pending, approved or executed.
// Executing on the historical approval alone, without checking any of these
// again, is exactly the gap this package closes.
//
// Revalidate is a pure, deterministic function: it takes the historical
// [decision.Inputs] and [decision.Decision] together with the exact
// [decision.Inputs] that would compose today (holding every input the
// historical decision recorded except those seven live categories, which
// [Facts] supplies fresh), and calls [decision.Compose] again. Reusing the
// GOVERN-002 composition function -- rather than re-implementing its
// precedence, deny-dominance and fail-closed rules here -- is what makes
// "confirms the exact proposal" mean something precise: the current
// recomposition reproduces the historical decision's own digest, byte for
// byte, or it does not.
//
// When it does not, Revalidate never guesses at a soft warning: it names
// exactly which of the seven inputs changed and returns one of three typed
// requirements -- BLOCK when the recomposed decision itself no longer
// allows, REPLAN_REQUIRED when a budget, position or conflict fact moved out
// from under the prepared plan's reservations or write-ordering assumptions,
// or REAPPROVAL_REQUIRED when governance still allows but the material basis
// for the prior approval has moved. A [Result] is bound to the prepared
// transaction plan's own digest ([Result.VerifyBoundPlan]): a plan
// recompiled after revalidation ran is refused on that binding alone, even
// if nothing else changed.
//
// This package is kernel-pure: it performs no I/O, calls no other
// subsystem, and never reads the wall clock. The current AuthZ verdict,
// session state, source authority, field classification version, legal and
// policy pack versions, budget and position facts and conflict
// classification all arrive as typed [Facts] a caller's own ports evaluated;
// the current instant arrives through the [Clock] parameter. Semantic owner:
// governance (revalidation). Phase: P1B / Gate B.
package revalidate
