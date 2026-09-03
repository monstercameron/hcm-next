// Package explorer implements the ledger/provenance explorer half of
// ADMIN-003: read-only, authorization-gated projections over the ledger's
// own read surfaces (planning/specs/transaction-ledger-reconciliation-and-repair.md
// 8.5 "Transaction Ledger"; planning/specs/provenance-graph-and-lineage.md).
//
// Four read shapes are exposed, each a thin, redaction-applying wrapper over
// an existing data-plane read port rather than a new query engine:
//
//   - [StreamListing] lists one stream's events (internal/data/ledger.Reader.ReadStream).
//   - [EventDetail] reads one exact event plus its stream's hash-chain
//     verification (internal/data/ledger.Reader.ReadEvent,
//     internal/data/ledger/hashchain.Digester.Verify).
//   - [Lineage] walks correction/supersession ancestry
//     (internal/data/ledger/lineage.Ancestors, .Descendants).
//   - [BitemporalAsOf] / [BitemporalKnownAt] resolve business-time and
//     knowledge-time reads (internal/data/bitemporal.AsOf, .KnownAt).
//
// This package performs no write of any kind and holds no state beyond a
// caller-supplied *pgx connection/transaction: every function here is a
// pure read followed by a pure redaction/rendering pass.
//
// # Authorization
//
// [StreamListing] and [EventDetail] gate an event's Payload and its
// provenance fields (Authority, SourceRef) with an
// *internal/trust/authz.Decision the caller has already evaluated: a
// non-disclosable subject withholds both, and each field independently
// requires an ALLOW ruling under [FieldPayload] / [FieldProvenance] (see
// [gateOpen] in redact.go) - identity, timing and integrity metadata
// (sequence, digest, chain hash) remain visible regardless, since an
// operator explorer that cannot show which event broke a chain, or when,
// is not useful for the incident it exists to diagnose. [Lineage] carries
// no payload at all ([lineage.Node] is identity-only) but still honors a
// non-disclosable decision by refusing to walk it.
//
// [BitemporalAsOf] and [BitemporalKnownAt] take a
// internal/data/bitemporal.Decision instead: that package already compiles
// every subject/field/time boundary into the SQL predicate itself, so a
// denied field is absent from PostgreSQL's own result set before it ever
// reaches this package - the strongest form of "denied fields never leave
// the adapter" this repository has, and this package adds nothing on top of
// it beyond a content digest.
package explorer
