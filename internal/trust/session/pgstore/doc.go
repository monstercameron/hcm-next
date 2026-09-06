// Package pgstore implements [session.Store] over migration 00041's
// trust_session, trust_session_pointer, trust_session_refresh_generation
// and trust_session_evidence tables (owner: governance-and-trust; phase:
// P1A; SECARCH-002).
//
// # Why two of the four tables carry no row level security
//
// [session.Manager] and [session.PersistentManager] look a session up by
// its own opaque, server-generated id (Get/Touch/Revoke/Evidence) or by a
// presented, opaque refresh-token hash (Refresh) -- never by a
// caller-supplied tenant plus id. Migration 00008's row level security
// policies fail closed on a missing app.tenant_id session setting (no
// tenant selected sees zero rows from every tenant-scoped table), which
// means a lookup that must resolve tenant scope from an opaque identifier
// alone cannot itself be guarded by the very policy it exists to
// establish. trust_session_pointer (session_id -> tenant_id) and
// trust_session_refresh_generation (token_hash -> tenant_id, session_id,
// generation) are the two tables that answer exactly that question, and
// both carry zero session content -- no subject, no principal fingerprint,
// no assurance level, no status -- so the only thing reachable through them
// is a pointer, gated by possession of an unguessable 128-bit session id or
// an unguessable 256-bit bearer token's hash. See migration
// 00041_session_store.sql's own header comment for the full reasoning, and
// internal/authn/issuerregistry/pgstore.go's withTenant for the same
// chicken-and-egg shape resolved a different way (there, by trusting a
// caller-supplied tenant identity to already be the storage uuid).
//
// trust_session (session content) and trust_session_evidence (the
// lifecycle audit trail) both carry migration 00008's ordinary
// tenant_isolation policy with FORCE ROW LEVEL SECURITY, unmodified: every
// read or write of either happens only after the tenant has already been
// resolved from one of the two pointer tables, inside the very same
// transaction.
//
// # Concurrency
//
// Every exported method opens and finishes its own transaction: there is no
// shared, in-process lock analogous to [session.Manager]'s mutex, because
// the whole point of this package is that two or more processes (replicas)
// share one PostgreSQL server instead. trust_session.version is a
// compare-and-swap fence every mutating UPDATE's WHERE clause checks and
// increments by exactly one; a stale writer's UPDATE affects zero rows
// rather than silently overwriting a change it never observed.
// [PGStore.Refresh] additionally relies on
// trust_session_refresh_generation's own primary key
// (tenant_id, session_id, generation) and its UNIQUE(token_hash)
// constraint: two callers racing to advance the same session past the same
// generation collide on that constraint as a genuine unique-violation, not
// a read-then-write window a slower caller could lose without the database
// ever refusing anything.
package pgstore
