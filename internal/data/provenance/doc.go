// Package provenance owns provenance publishing (owner: data plane; phase:
// P1A; DATA-014, part of the NEXT-004 slice): recording, for every
// appended ledger event and every persisted external observation, where the
// fact came from - source authority, evidence, digests, the principal that
// caused it, and the connector/observation it was reported through - and
// answering "what led to this intent" honestly (specs/provenance-graph-and-lineage.md).
//
// The published README describes a full typed graph of node and edge kinds
// (SourceAssertion, Transformation, Decision, ... USED, DERIVED_FROM,
// CAUSED, ...). This package implements the P1A slice of that contract: one
// edge kind - "this tenant's intent has provenance for this source" - keyed
// by (tenant, source kind, source reference), plus the completeness
// judgment ([Lineage]) a caller needs to know whether that edge set is the
// whole story yet. It is deliberately not the general graph; a later
// package can add node/edge kinds without this one's row shape changing,
// the same way internal/data/ledger/hashchain added a side table beside the
// ledger it never modified.
//
// # DDL note
//
// provenance_record (schema.go's [SchemaDDL]) is a new side table, not a
// migration: migrations/ is owned by other in-flight work, and this
// package's instructions are to report the DDL it needs rather than add a
// migration file. Like internal/data/ledger/hashchain's own side table, it
// defines no new domain or function - only a table built from
// tenant_ref, semantic_key and the forbid_mutation() trigger already
// declared in migrations/00001 and migrations/00002 - so it is safe to fold
// verbatim into a future numbered migration. Callers apply it once per
// schema, typically through pgtest.DB.Exec in tests (fixtures_test.go); a
// production deployment applies it as part of a real migration once a
// migration number is free.
//
// # Publishing (DATA-014 GREEN)
//
// [Publish] validates the request - most importantly, that it carries at
// least one evidence ID (DATA-014's named RED clause: "provenance is never
// published without its evidence id") - inserts one immutable
// provenance_record row and enqueues one internal/data/outbox message
// classified "provenance.published", inside the caller's transaction.
// Publish is idempotent by (tenant, source kind, source reference): the
// record ID is derived deterministically from that triple, so a second
// Publish call for the same source is a no-op that returns the original
// record with Published=false rather than a second logical edge - the
// README's "idempotent by publisher/source/edge identity."
//
// # Lineage (DATA-014 GREEN)
//
// [Lineage] returns every provenance_record published for one intent's
// ledger stream, plus an honest completeness verdict: COMPLETE when every
// event on the stream has a matching record, PARTIAL when some do not, and
// UNKNOWN when the stream has no events at all to judge. It never reports
// COMPLETE by assuming a missing publisher caught up.
package provenance
