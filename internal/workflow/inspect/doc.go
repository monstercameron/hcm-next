// Package inspect renders the governed execution inspector's read-only
// projection over persisted workflow runtime state (owner: workflow-runtime;
// phase: P1A; WF-RUN-019).
//
// # What it is
//
// [Build] takes one [runtime.Instance], its recorded [runtime.NodeExecution]
// rows and an already-evaluated [Authorization], and returns a [View]: the
// traversal planning/specs/workflow-runtime.md "Execution Inspector and
// Deterministic Replay" describes, in the order it describes it —
//
//	definition -> instance -> node -> governance -> transaction ->
//	connector -> observation/reconciliation -> trace
//
// — plus the instance's runtime status, its five lifecycle dimensions (never
// collapsed into one) and its current frontier.
//
// # It projects; it does not execute and does not read
//
// This package performs no I/O. It does not open a database, does not walk a
// plan and cannot change business state: an inspector that could act would be
// an intervention API, which is a different todo and a different authority.
// The caller loads state through internal/workflow/runtime and hands it here,
// which is also why [Build] is trivially safe to call concurrently.
//
// It is likewise not a scheduler view. There is no lease holder, no next-retry
// instant and no timer in a [NodeView]: WF-RUN-000 gates those primitives, and
// showing a "next retry 20:14" that nothing computes would be a screen that
// lies. What a node does carry is its declared retry policy reference and its
// attempt number, which are facts the runtime store actually holds.
//
// # Redaction is explicit, and omission is reported
//
// WF-RUN-019's RED clause has two halves, and they pull in opposite
// directions: an inspector must not omit the current node, attempt, retry,
// proposal, baseline, policy, effect or repair references, and must not leak
// protected input or output content. So references are always rendered, and
// the ones that point at protected artifacts are [Ref] values that carry
// either the reference or the reason it was withheld — never a bare empty
// string that reads as "there was nothing there".
//
// The same rule governs whole sections. A denied section is named in
// [Completeness.Redactions] rather than silently dropped, and any datum the
// projection expected but did not receive — a frontier node with no recorded
// execution, most of all — is named in [Completeness.Gaps]. A view is
// [Completeness.Complete] only when nothing was denied and nothing was
// missing, which is what stops a partial view from being read as a full one.
//
// The redaction shape follows internal/domains/intelligence's ExplainTransaction:
// an authorization decision is an input, not something this package computes;
// section and field rulings are separate; and whether the instance may be
// known to exist at all is a third, separate ruling, because the existence of
// a promotion workflow is itself information.
package inspect
