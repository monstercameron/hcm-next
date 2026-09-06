// Package partition proves that hash- and range-partitioning an authoritative
// table changes only its physical layout, never its observable semantics
// (owner: data plane; phase: P1A; DATA-017).
//
// # Two pieces
//
// [PartitionPlan] is operational metadata: which table, which strategy, which
// key column(s), and either a partition count (hash) or an ordered list of
// range bounds. It carries no domain schema (DATA-017 REFACTOR, "partition
// policy is operational metadata, not domain schema") -- nothing about
// assertion classes, streams or tenants lives here, only the physical routing
// fact, plus [PartitionPlan.Validate] and a canonical [PartitionPlan.Digest]
// for detecting drift between what code declares and what a live database
// enforces.
//
// [Kit] is the conformance proof itself. Given an already-partitioned table,
// it builds a non-partitioned shadow copy with the identical columns,
// constraints, row level security policies and triggers (LIKE does not carry
// RLS or triggers, so those are read back out of the catalog and reproduced
// by hand), loads it with the exact rows the partitioned table already holds,
// and then lets a caller run the same query or the same refused statement
// against both and assert they agree -- byte for byte on read, and citing the
// same refusal wording on write. [Kit.CheckPartitionPolicies] and
// [Kit.CheckPartitionTriggers] additionally confirm every individual
// partition -- not just the parent relation a query is normally written
// against -- carries the parent's own policies and triggers, since PostgreSQL
// does not extend either automatically to a directly-addressed partition
// (migrations/00008_tenant_isolation.sql explains why at length).
//
// # Applied to ledger_event
//
// [LedgerEventPlan] and the tests in ledger_event_test.go apply both pieces to
// migrations/00005_ledger.sql's ledger_event: PARTITION BY HASH (tenant_id)
// into four partitions, each already carrying the append-only trigger
// (cloned automatically by PostgreSQL) and, since migrations/00008, its own
// copy of the tenant_isolation row level security policy (PostgreSQL does not
// clone RLS policies the way it clones triggers). No production migration
// gap was found; if this proof ever does find one, the GREEN condition here
// is to report it, not to edit migrations/ from this package's lane.
package partition
