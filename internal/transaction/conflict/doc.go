// Package conflict implements CONFLICT-001 (normalized proposal write
// footprints) and CONFLICT-002 (classification of concurrent proposal
// conflicts) for Gate A.
//
// It is pure: every function here operates on snapshots of already-known
// state -- write footprints, candidate proposals, versioned merge rules --
// and performs no database access, no reservation mutation and no
// transaction coordination of its own. A preflight result computed here is
// advisory until the coordinator validates and fences it at commit time
// (TX-003/CONFLICT-003, a later Gate B slice this package does not
// implement).
//
// Per ARCH-GO-011, this package owns "footprints/write intents/overlap/
// classification/reservation checks and returns typed analysis consumed
// before prepare"; it may never import
// github.com/monstercameron/hcm-next/internal/transaction (the coordinator)
// or any of that package's own subpackages. The coordinator depends on
// conflict analysis, never the reverse -- importing back up would recreate
// the exact coordination/conflict cycle that rule forbids.
//
// Semantic owner: transaction (conflict analysis). Phase: P1A.
package conflict
