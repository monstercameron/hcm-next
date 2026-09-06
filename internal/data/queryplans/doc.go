// Package queryplans is DB-020's declared catalogue of critical queries and
// the pgtest-backed prover that holds them to it (owner: data plane; phase:
// GATE_B).
//
// # Why a catalogue, not a comment on each index
//
// Every tenant-scoped table's index is chosen for a reason: a specific query
// some store issues, at a scale where the difference between an index scan
// and a sequential scan is the difference between a bounded read and an
// outage. That reason lives in the head of whoever added the index, and it
// drifts the moment the store's own query text changes underneath it. This
// package makes the reason a checked fact instead: [Catalog] names each
// critical query, the package that issues it and the table it must not scan
// sequentially at declared scale; [Prove] seeds that table to the declared
// threshold, drives the real store call through a recording [Spy] so the
// exact SQL text a production caller would run is what gets examined, and
// asks PostgreSQL itself -- via EXPLAIN (FORMAT JSON), never a guess about
// what an index "should" do -- what access path it chose.
//
// # Why the plan is captured by spying, not by copying SQL text
//
// A catalogue entry does not carry a hand-copied SQL string: strings drift
// from the store method they were copied out of the moment either one is
// edited alone. Instead an entry's Prepare function returns a run closure
// that calls the real, exported store method (internal/data/workforce,
// internal/data/ledger, internal/data/jobs, internal/data/intentcontrol) with
// a [dbport.Conn] the prover controls. [Spy] wraps that connection, records
// every statement and argument list the store actually sends, and the
// prover runs EXPLAIN over the recorded statement rather than a
// reconstruction of it. Two callers of the same store method are therefore
// always proven against the one query PostgreSQL will really receive.
//
// # Why RLS stays on
//
// Every plan is captured on a connection that has run "SET ROLE hcmnext_app"
// and set app.tenant_id (internal/data/tenancy's own session setting), the
// same way a production request-scoped connection reaches these tables
// (migrations/00008_tenant_isolation.sql). A plan captured as the migration-
// owning superuser would never show the row level security predicate at
// all, which would prove nothing about the path a real request takes.
//
// # What "critical" means here
//
// The catalogue is not exhaustive; it is the read paths named in
// planning/todos.md's DB-020 entry: ledger event reads by stream and by
// bitemporal coordinate, the workforce journey's worker list and lookup, a
// job's partition roster, and an intent's governance context. A query this
// package could not attach to an existing exported store call -- because no
// store implements it yet -- is not fabricated into the catalogue; see
// catalog.go's own doc comment for the one such gap this lane found and left
// named rather than invented.
package queryplans
