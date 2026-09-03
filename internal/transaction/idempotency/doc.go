// Package idempotency implements TX-006: durable semantic idempotency over
// migrations/00019_idempotency_record.sql.
//
// ARCH-GO-011 assigns "prepare/commit/receipt/idempotency/fences/correction"
// to internal/transaction, so this package lives as its subpackage the same
// way internal/transaction/conflict does for write-footprint analysis -- a
// sibling internal/transaction may depend on, never one that reverses the
// edge back into it.
//
// The uniqueness scope planning/specs/platform-foundation-gap-closure.md
// section 6 declares is (tenant, capability, semantic effect scope,
// idempotency key). A [Scope] names exactly that tuple, deliberately with no
// principal field: "a change of executor, workflow worker, connector worker
// or governed repair principal does not create a new namespace for the same
// semantic effect" is that section's own rule, and principal belongs in the
// scope only when a caller intentionally widens it (e.g. by folding a
// principal identifier into EffectScope for a capability where separate
// principals produce genuinely distinct business decisions, such as
// independent approval votes).
//
// [Guard] is the package's whole idempotency contract in one call: reserve
// the scope under a caller-declared [RetentionPolicy], run the caller's
// effect inside the caller's own transaction exactly once, and durably
// record the [ResultIdentity] it produced -- atomically with that same
// commit. A replay under the same scope and the same canonical request
// digest returns the stored identity without running the effect again; the
// same scope under a different digest is refused with a typed
// IDEMPOTENCY_CONFLICT and mutates nothing; two concurrent callers racing the
// same scope commit the effect exactly once, because the loser's own
// reservation attempt is the exact same INSERT ... ON CONFLICT DO NOTHING
// PostgreSQL itself serializes against the table's primary key -- there is no
// process-local lock anywhere in this package.
//
// [Store] is the narrow, driver-free port [PostgresStore] implements over
// internal/data/dbport: Reserve, Complete, Lookup and Expire. Nothing in this
// package imports pgx; the Postgres-specific behavior Reserve and Complete
// need (deterministically losing a race, refusing to complete a
// reservation that no longer exists) is expressed entirely through ordinary
// SQL (INSERT ... ON CONFLICT DO NOTHING RETURNING, UPDATE ... RETURNING)
// rather than by inspecting a driver error code.
//
// Nothing in this package reads a wall clock. Every method that needs "now"
// takes it as an explicit parameter, so a retention deadline, a completion
// timestamp or an expiry sweep boundary is always the caller's stated
// instant, never an ambient one this package could disagree with itself
// about between two calls.
//
// Semantic owner: transaction (coordination) / idempotency. Phase: P1B.
package idempotency
