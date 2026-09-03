// Package bitemporal implements authorized event/fact queries over the
// existing ledger tables (owner: data plane; phase: P1A; DATA-005).
//
// It is a read-only adapter. It appends nothing, migrates nothing, and owns
// no table: every query here reads internal/data/ledger's ledger_event table
// exactly as LEDGER-001/LEDGER-004 defined it. Two independent time axes are
// already present on every row and this package never conflates them:
//
//	effective_at  business time - when the asserted fact applies
//	recorded_at   system time - when the ledger learned of the assertion
//
// AsOf resolves the first axis ("what was true on this business date").
// KnownAt resolves the second ("what had the ledger recorded by this
// instant"). The general Query entry point combines both into the five modes
// the effective-date debugger needs (specs/hris-admin-dataops.md "Effective-
// Date Debugger"): CURRENT, EFFECTIVE_AS_OF, KNOWN_AS_OF, BETWEEN and
// HISTORY.
//
// # Corrections versus supersessions
//
// The ledger records only one thing at the assertion-class level:
// CORRECTION, which the ledger specification defines as "a later governed
// assertion [that] corrects, completes, or supersedes an earlier assertion"
// (transaction-ledger-reconciliation-and-repair.md 8.5). Section 8.7 then
// lists "explicit supersession" and "retroactive correction" as distinct
// repair actions. This package draws that distinction the only way the
// stored data supports it: by comparing a correction's own effective_at to
// the effective_at of the assertion it corrects.
//
//   - Same effective_at -> CORRECTION: the record is fixing what was already
//     asserted to be true at that business instant. History is amended
//     without moving the boundary.
//   - Different (normally later) effective_at -> SUPERSESSION: the record
//     introduces a new business-time boundary, so it also competes as an
//     ordinary timeline entry rather than only overwriting its target in
//     place.
//
// A correction whose target cannot be resolved (the ledger enforces that
// corrects_stream_key/corrects_sequence are both present or both absent, but
// not that the target row exists, since the partitioned table has no
// self-referential foreign key across hash partitions) is conservatively
// classified CORRECTION rather than SUPERSESSION, so an unresolved target
// never silently wins a business-time comparison it was not proven to win.
//
// Both kinds resolve through one deterministic ordering rule for "what wins
// as of (effective, known)": among the facts visible under the query's
// effective and known-at bounds, the winner is the one with the greatest
// effective_at, tie-broken by the greatest recorded_at, tie-broken by the
// greatest sequence. A correction (same effective_at, later recorded_at)
// wins its tie-break; a supersession (later effective_at) wins outright.
// Neither requires walking a correction chain.
//
// # Authorization
//
// internal/trust/authz is being built by another lane and is not imported
// here. This package instead takes authorization as a plain input value,
// Decision, that some other component (eventually TRUST-012's query
// planner; a hand-built fixture in this package's own tests today) is
// responsible for producing. Every boundary a Decision states - tenant,
// subject (stream_key) allow/deny, field (schema_ref) allow/deny, and a
// knowledge-time ceiling - is compiled into the SQL WHERE clause before the
// statement runs. A denied field or a withheld subject is therefore never
// selected out of PostgreSQL; it does not arrive at the Go process to be
// filtered there. See decision.go for why "field" means schema_ref.
//
// # Evidence
//
// Digest and Evidence in digest.go produce a canonical, reproducible record
// of one query: the request, the decision it executed under, and the exact
// ordered result it produced. Two calls with the same request, decision, and
// ledger state (in particular, the same KnownAt horizon) must always compute
// the same digest - this is the replay-stability property TestTodo_DATA_005
// exercises: appending new events after a fixed KnownAt must never change
// the answer or its digest for that KnownAt.
package bitemporal
