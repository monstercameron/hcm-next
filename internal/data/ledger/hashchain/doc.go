// Package hashchain implements DATA-004: a per-stream cryptographic hash
// chain over the ledger events internal/data/ledger already appends and
// digests.
//
// # What it adds over the existing per-event digest
//
// internal/data/ledger (LEDGER-002/DATA-003) already computes a canonical
// digest for every event's payload and records it immutably: the
// ledger_event table's UPDATE/DELETE triggers make an in-place edit to that
// digest impossible through the normal write path. What that digest does
// not prove on its own is that the *sequence* of events a reader observes is
// the sequence that was actually appended: a row deleted by a superuser
// bypassing the application, a stream read out of order, or a corrupted
// export could still present a set of individually well-formed events.
//
// A hash chain closes that gap. Each event's chain link folds the previous
// link's chain hash into a new one together with that event's own digest:
//
//	chain_hash[1] = digest(GENESIS        || event_digest[1])
//	chain_hash[n] = digest(chain_hash[n-1] || event_digest[n])   for n > 1
//
// so the chain hash at the current head is a single value that commits to
// every event digest in the stream, in the order they were appended. A
// verifier that recomputes the chain from the stored event digests and
// compares the result, sequence by sequence, against previously recorded
// chain links can name the exact first sequence where the two disagree,
// whether that disagreement is a gap, a reordering, a duplicate, an altered
// event digest, or a corrupted link record - see [Verify].
//
// # No new column on ledger_event
//
// internal/data/ledger/append.go and the ledger_event/stream_head tables
// (migrations/00005_ledger.sql) are frozen for this work. Rather than adding
// prev_hash/chain_hash columns to ledger_event, this package owns a
// dedicated append-only side table, ledger_hash_chain_link (see
// [SchemaDDL]), keyed the same way ledger_event is
// (tenant_id, stream_key, sequence). It reuses the domains and the
// forbid_mutation() trigger function migrations/00001_platform_control.sql
// and migrations/00002_tenant_primitives.sql already publish; it defines no
// new ones.
//
// SchemaDDL is applied by test setup today (see the package's own tests,
// which call it through pgtest.DB.Exec) rather than through a numbered
// migration file, because migrations/ is owned by other in-flight work.
// Whoever next owns a free migration number should fold SchemaDDL into a
// real migration unchanged; the DDL is written to apply cleanly as-is.
//
// # Computing the digest through internal/engines/wire/digest
//
// [Digester] mints the chain digest through a registered, versioned
// canonicalization profile in internal/engines/wire/digest rather than an ad hoc
// hash, following the same pattern internal/ledger.KernelDigester already
// established for the event digest itself: the two length-framed inputs
// (the previous chain hash and the event's own digest) are carried as the
// wire bytes of an hcmnext.intents.v1.TypedPayload under a dedicated,
// package-private profile ID and registry, so a chain-link digest can never
// be confused with, or substituted for, an event digest or any other
// profile's digest even though the underlying message type is shared.
package hashchain
