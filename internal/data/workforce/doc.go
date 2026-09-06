// Package workforce owns the durable workforce the Promotion journey creates
// employees into: migrations/00023_journey_workforce.sql's journey_worker
// table, and the governed worker read that projects those rows onto
// internal/domains/people's own fact model (owner: data plane; phase: P1B).
//
// # Why the package exists
//
// Before it, the only workers a composed cell could see were the four in
// internal/domains/fixtures/testdata/workers.json, answered in memory by
// fixtures.MemoryWorkerFacts. That corpus is deliberately fixed: it is the
// shared regression population, and a run that could add to it would stop
// being a regression. So "create an employee and promote them" had nowhere to
// put the employee. journey_worker is that place, and [Store] is the only Go
// path to it.
//
// # One read path, not two
//
// [NewLayeredWorkerFacts] is the whole point of the read half. It composes the
// corpus reader and [Facts] into a single people.WorkerFacts that a cell wires
// once: the corpus answers first, and only when the corpus reports
// Exists = false does the created population get asked. Everything downstream
// -- the capability gateway's explain_worker_state handler, the workspace's
// read surface, the journey engine's own current-placement read -- therefore
// reaches created workers through the one governed read it already used, under
// the same authorization decision and with the same evidence recorded. There
// is deliberately no second reader for created workers, because a second
// reader is a second place a field can be disclosed from.
//
// The projection [Facts] builds is the same shape fixtures.MemoryWorkerFacts
// builds, field for field: the requested projection and nothing wider, one
// open-ended effective interval from the row's effective_from, the row's own
// known-at, its revision-stream position as the fact revision and the set
// watermark, and a complete source authority and provenance. That is what lets
// people.ExplainWorkerState treat a created worker exactly like a corpus
// worker rather than having to know which population it came from.
//
// # What is deliberately absent
//
// There is no update and no delete. journey_worker is append-only in the
// schema (the forbid_mutation trigger, a SELECT/INSERT-only grant), so an
// employee is a fact rather than an editable row, and correcting one means a
// new revision row on the same revision stream -- a bitemporal read this
// package does not yet implement and its migration's header names as out of
// scope.
//
// Every statement runs inside a tenant-scoped transaction
// (internal/data/tenancy.WithTenant): journey_worker is row-level-security
// protected, so the tenant is a property of the connection rather than a WHERE
// clause a caller could forget. [Store]'s methods take the caller's own
// executor so they can join a transaction the caller already holds; [Facts]
// opens and rolls back its own read transaction, because a governed read has
// no caller-visible transaction to join.
package workforce
