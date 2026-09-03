// Package data is the boundary marker of the HCM Next data plane (owner: data
// plane; phase: P1A). It holds no code of its own; it states the contract that
// its subpackages implement.
//
// # What lives here
//
//   - migrations (repository root, /migrations) declares the physical schema in
//     SQL. Nothing else creates or alters a table.
//   - internal/data/schema owns the schema release and migration journal: what
//     was applied, from which artifact digest, by which tool, when, and whether
//     it succeeded.
//   - internal/data/ledger owns the authoritative append path: assertion class,
//     canonical digest, per-stream sequence and stream-head compare-and-swap.
//   - internal/data/pgtest runs a real PostgreSQL for the tests, because the
//     rules below are enforced by the database and cannot be proven against a
//     fake.
//
// # Authoritative truth lives here
//
// The ledger and the tenant-scoped authoritative tables are the irreplaceable
// truth sources. Projections, search indexes, analytical copies and outbox
// records are derived: they carry provenance, versions and watermarks, and they
// can be rebuilt from the ledger. A projection that disagrees with the ledger is
// wrong by definition, and a projection outage is measurable lag rather than
// invisible inconsistency.
//
// Authority is attached to the assertion, never inferred from the fact that an
// event was recorded. An EXTERNAL_OBSERVATION says that a source reported a
// value; it becomes a DOMAIN_FACT only where an authority assignment governs
// that scope over the effective interval.
//
// # The workflow plane never writes these tables
//
// Workflow steps and agents describe what should happen. They do not open
// transactions against these tables and they hold no credentials that would let
// them. Authoritative writes go through the repositories in this package, which
// enforce tenant scope, assertion class, temporal validity and compare-and-swap
// on every path. This is what keeps the ledger's history explainable: every row
// has one owner who can say why it is there.
//
// # External calls never happen inside a transaction
//
// An append, a projection update and an outbox record commit together in one
// PostgreSQL transaction. Nothing inside that transaction talks to a network:
// no provider call, no queue publish, no webhook. A remote system that is slow,
// unreachable or ambiguous must never be able to hold a database transaction
// open or leave business truth half-written.
//
// External effects run after the commit, from the outbox, as idempotent
// activities with their own retry and reconciliation. The commit establishes
// truth; distribution is at-least-once and consumers are idempotent. Where
// affected streams cannot share one transaction boundary, the caller must not
// pretend to atomicity: it uses an explicit coordinator with durable intent,
// fencing and compensation instead.
//
// # Time
//
// Three times are distinct and all three are recorded: occurred_at is when the
// originating activity happened, effective_at is when the business fact applies,
// and recorded_at is when HCM Next durably wrote it down. Business time is never
// inferred from created_at, and intervals are half-open [from, to).
package data
