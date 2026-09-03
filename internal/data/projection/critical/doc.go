// Package critical owns the critical-projection applier: the part of the
// critical-projection subplane (owner: data plane; phase: P1A; DATA-006,
// part of the NEXT-004 slice) that turns an appended ledger event into the
// exact rows the intent_instance and proposal_revision read models hold
// (migrations/00004_intent_and_proposal.sql).
//
// internal/data/projection owns the generic, schema-agnostic checkpoint - the
// exact source sequence a named projection has applied for a stream - and
// gives no meaning to the bytes it advances past. This package sits one
// layer up: it uses that same checkpoint (under its own [ProjectionName], a
// name distinct from whatever projection name a caller's own outbox.Commit
// call advances) as the ordering and idempotency guard, and only when the
// guard says "this is a genuinely new event" does it decode the event's
// typed payload and mutate intent_instance or proposal_revision.
//
// # Why this is a safe second checkpoint on the same event
//
// internal/data/outbox.Commit already advances one projection checkpoint per
// call, chosen by its caller (e.g. internal/intent/app/pgstore names its own
// checkpoint "intent_instance" and writes the row itself, inline, for the
// single creation event it ever produces). This package's [ProjectionName]
// is a second, independent checkpoint on the same stream: nothing stops two
// checkpoints from tracking the same stream's progress independently, and
// doing so here is what lets this package be a general-purpose applier for
// every lifecycle event an intent's stream carries - not just creation -
// without either checkpoint disagreeing about what "applied" means for the
// other's caller.
//
// # Assumed stream shape
//
// Every event this package is asked to apply for one intent - its creation
// (hcmnext.intents.v1.IntentInstance) and every later proposal revision
// (hcmnext.intents.v1.ProposalRevision) - is assumed to live on that
// intent's own stream, in that order, matching internal/intent/app/pgstore's
// "one stream per intent" convention (pgstore.go, StreamKey). A
// proposal_revision row's foreign key to intent_instance
// (migrations/00004_intent_and_proposal.sql,
// proposal_revision_intent) depends on the creation event having already
// been applied on the same stream, ahead of any revision; the per-stream
// sequence guard in [Apply] is what makes that true rather than merely
// assumed.
//
// # Idempotency and ordering (DATA-006)
//
// [Apply] calls internal/data/projection.Apply before touching either read
// model table. That call is the ordering and idempotency guard: a sequence
// more than one past the checkpoint's watermark is refused
// (projection.ErrSequenceGap) rather than silently skipping the missing
// event, and a sequence at or before the watermark is a no-op - the event
// was already applied, so this call changes nothing and reports
// Applied=false. Only a sequence that extends the watermark by exactly one
// reaches the read-model mutation below.
//
// The read-model mutation adds a second, table-level guard on top: an
// intent_instance upsert only replaces the stored row when the incoming
// InstanceVersion is strictly greater than what is stored
// (upsertIntentInstance's ON CONFLICT ... WHERE clause), and a
// proposal_revision insert either creates a new immutable revision or, on a
// second attempt at the same (tenant, intent, revision), verifies the
// stored bytes still match rather than silently accepting a second write.
// In the ordinary path the checkpoint guard above makes both conditions
// unreachable; they exist so that a caller who reuses this package's SQL
// directly, bypassing [Apply], still cannot make a stale write win.
//
// [Verify] recomputes intent_instance and proposal_revision from the ledger
// by replaying every event on a stream through the same [Mapper] [Apply]
// uses, and diffs the recomputed state against what is actually stored -
// the DATA-006 "Verify" contract, and the same shape DATA-010's projection
// rebuild will eventually reuse at larger scale.
package critical
