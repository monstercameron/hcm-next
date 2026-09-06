// Package search implements RETRIEVAL-001: one governed, PostgreSQL-only
// lexical search projection, built and queried under the same authorization
// discipline every other governed read in this cell obeys (owner: data
// plane; phase: P2).
//
// # Why Postgres and nothing else
//
// RETRIEVAL-001's GREEN clause requires that "specialized search infra-
// structure" -- Elasticsearch, OpenSearch, a vector store -- clear a measured
// gate before it is introduced. This package is the alternative that has to
// be tried first: [migrations/00037_lexical_search.sql]'s search_projection
// table carries one GIN-indexed tsvector column per tenant-scoped subject,
// and [Query] matches against it with PostgreSQL's own full-text search. No
// other retrieval system exists behind this package; when one is warranted,
// it is added beside this one under its own measured evidence, not in place
// of it.
//
// # A projection, not a second source of truth
//
// [Project] never invents anything: it is fed the exact facts an
// authoritative reader (internal/domains/people's [people.WorkerFacts], for
// subject_kind "worker") already returned, and it writes only the subset of
// those fields this package has declared classification-cleared for lexical
// search (see fields.go). A field the allowlist does not name -- compen-
// sation, employment status, FTE, a manager relationship reference, every
// field people.AllFields() defines that is not in [ClearedFields] -- is
// dropped before it ever reaches the database, even when the caller's input
// carries it. That is what makes the projection rebuildable with a pure-
// function proof: replaying the same source revision through [Project]
// always produces the same search_text, because search_text is a
// deterministic function of (cleared fields, their values) and nothing else
// -- no wall clock, no caller identity, no map-iteration order.
//
// # Three refusals a lexical index cannot skip
//
// A search index answers a question no single-subject governed read does --
// "which subjects, out of everyone this tenant has" -- so it has three ways
// to leak that a per-subject explanation does not:
//
//   - A caller with no search scope at all must be refused before a single
//     statement runs. [Query] checks [Authorization] first and returns
//     [ErrScopeDenied] without touching the database.
//   - A tenant boundary is enforced by migration 00037's row level security,
//     the same fail-closed app.tenant_id session predicate every other
//     tenant-scoped table in this schema uses (internal/data/tenancy).
//   - A subject the caller may search for existing rows about, but may not
//     be told exists -- the same WITHHELD case
//     people.ExplainWorkerState protects -- is removed from the result set by
//     the caller-supplied [Discloser] before [Query] returns anything. A
//     subject [Discloser] refuses is not returned with a redacted value: it
//     is not returned at all, and [Query] never learns why.
//
// [Query] returns subject references only -- never a field value, never a
// snippet of search_text. A caller that wants to disclose a matched
// subject's fields makes that a second, separately authorized governed read
// (people.ExplainWorkerState for a worker), exactly the two-step shape
// RETRIEVAL-001's Refs (specs/platform-responsibility-boundaries.md) require:
// search finds candidates, disclosure is a different question answered by a
// different, already-battle-tested authority.
package search
