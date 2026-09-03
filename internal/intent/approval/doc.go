// Package approval binds approval decisions to exactly one immutable proposal
// revision, and decides when a later revision invalidates them.
//
// Semantic owner: intent (BI.WORK). Phase: P1A.
//
// Two rules carry the package. The first is that a decision binds the proposal
// it was made against, not the proposal that happens to be current: a vote
// naming any other revision, digest, control context or task version is
// refused, and the binding a decision records is always the server-held one,
// never a value the voter supplied. The second is that only a material change
// invalidates: a control-snapshot republish - a policy bundle, a taxonomy, a
// reference dataset - is revalidated context, so a proposal whose material
// result is unchanged keeps its approvals and records the revalidation instead.
//
// The package causes no effects. Recording a decision produces an artifact and
// an assessment produces a zero-effect receipt; neither writes domain state,
// consumes an approval or makes a plan executable. That is APPROVAL-003's job
// and it is deliberately not reachable from here.
package approval
