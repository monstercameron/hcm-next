// Package migrate executes one already-approved workflow migration for a
// single live instance at a safe point (WF-RUN-018).
//
// It sits strictly downstream of three other packages:
//
//   - internal/workflow/migrationpreview (WF-RUN-017) classifies a live
//     instance's compatibility against a target compiled plan without
//     touching either the plan or the instance. This package never reruns
//     that classification; it only trusts a [PreviewRecord] a caller sealed
//     from one earlier [migrationpreview.Preview] call.
//   - internal/workflow/runtime (WF-RUN-001/008/023/025) owns every durable
//     row this package writes to. [Migrate] calls only that package's own
//     exported store methods plus one additive export this ticket adds,
//     [runtime.Store.RecordVersionMigration] -- there is no direct SQL here.
//   - internal/data/runtimestate's workflow_checkpoint store (WF-RUN-008)
//     is where "the instance is paused at a checkpoint" is proven and where
//     the migration itself leaves its own digested checkpoint behind.
//
// # What "safe point" means here
//
// [Migrate] requires the instance to be PAUSED, requires the frontier's node
// execution to still be an eligible safe point under
// [runtime.SafePointEligibility], and requires the latest durable checkpoint
// to describe exactly the instance's current version and frontier. Any of
// those failing is [CodeUnsafePoint].
//
// # What "approved" means here
//
// An [Approval] must name the exact [PreviewRecord.Digest] a caller reviewed,
// and its approver must be a principal distinct from whoever executes the
// migration ([CodeSeparationOfDuties]). A preview digest that does not match
// is [CodeUnapprovedDigest].
//
// # What "stale" means here
//
// A [PreviewRecord] pins the exact compiled-plan digests of the source and
// target plans it classified. If the plans a caller presents to [Migrate]
// digest to anything else, the preview no longer describes what is about to
// happen, and the call is refused as [CodeStalePreview] rather than trusted.
//
// # What this package refuses to migrate
//
// Only a CONTINUE (SAFE) or BRIDGE (TRANSFORMABLE) classification for the
// exact instance may proceed. A STRANDED (IMPOSSIBLE) classification is
// [CodeStranded]; anything the preview marked REQUIRES_REPAIR is
// [CodeRequiresRepair]. Neither is silently approximated into a migration.
package migrate
