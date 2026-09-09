// Package transaction implements the Gate A slice of the transaction
// coordinator: TX-002, resolving and fencing the ConsistencyBoundary that a
// compiled [intent.TransactionPlan]'s participants would commit under.
//
// This package owns plan/participant admission, the consistency boundary and
// its coordinator fence, exactly as ARCH-GO-011 assigns it:
// "transaction owns plan/participants/read-write sets/effects/boundaries/
// prepare/commit/receipt/idempotency/fences/correction". Conflict analysis
// (write footprints, overlap, classification) lives in the sibling package
// github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict, which
// this package may depend on but which must never import back into this
// package or any of its subpackages -- that reverse edge is exactly the
// coordination/conflict cycle ARCH-GO-011 forbids.
//
// Nothing here executes a transaction. P1A resolves the boundary as an
// explicit, immutable value -- a coordinator fence token, the admitted
// stream/aggregate footprint, and a canonical lock order -- and never opens a
// database connection, never commits, and never mutates a reservation. Every
// participant the boundary does not admit becomes a durable effect instead of
// a silent addition to the local ACID set. Prepare and commit against current
// stream heads (TX-003, TX-004) are a later Gate B slice this package does
// not implement.
//
// Semantic owner: transaction (coordination). Phase: P1A.
package transaction
