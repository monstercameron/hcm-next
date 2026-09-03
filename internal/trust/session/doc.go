// Package session owns server-side session lifecycle: creation, refresh
// token rotation with single-use replay detection, idle and absolute
// timeouts, revocation, and the durable evidence record for each
// transition.
//
// Semantic owner: governance-and-trust. Phase: P1A. Todo: TRUST-003.
// Depends on: TRUST-002 (internal/trust/federation).
//
// # Model
//
// A [Record] is one session: a stable [ID], the tenant and subject it was
// created for, the assurance level its authentication reached, an idle and
// an absolute expiry, and the hash of the one refresh token that currently
// rotates it. Presenting that refresh token to [Manager.Refresh] both
// proves possession and rotates it: the presented token is retired and a
// new one takes its place, so a refresh token is single-use by
// construction, not by a policy someone could forget to enforce.
//
// Every refresh token this package has ever issued for a session, not only
// the immediately previous one, is remembered as retired. Presenting any
// retired token — not just the current one, and not only the one
// immediately before it — is a replay: it revokes the session outright,
// because a retired token can only be in an attacker's hands if the
// legitimate holder's most recent token was already exfiltrated and used,
// or the attacker raced ahead of the legitimate holder. Either way,
// continuing to trust the session is unsafe.
//
// A [Record] is immutable once returned: every accessor and every method
// that changes session state returns a fresh snapshot rather than a pointer
// callers could mutate through. [Manager] holds the only mutable state, and
// it never hands the mutable original out.
//
// # What this package does not do
//
// Sessions here are held in memory, keyed and guarded by one mutex; nothing
// in this package talks to PostgreSQL. That is a deliberate, named scope
// limit for the same reason [KeySource] in package federation is a port
// rather than a live JWKS client: a durable, replicated session store is
// its own qualified adapter, not a detail this todo may invent. The model
// (single-use refresh, family-wide revocation on replay, idle/absolute
// timeouts, an append-only evidence trail) is exactly what a durable store
// needs to persist; only the storage medium is deferred.
package session
