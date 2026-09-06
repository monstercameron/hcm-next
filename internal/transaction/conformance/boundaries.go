// Package conformance contains crash-boundary proofs for the transaction
// commit contract. It deliberately depends on the public commit seam rather
// than on PostgreSQL implementation details beyond the rows the contract
// promises to leave durable.
package conformance

// Boundary is a named point at which a transaction worker can stop.
type Boundary string

const (
	// BoundaryBeforeAppend is before the first ledger append.
	BoundaryBeforeAppend Boundary = "before-append"
	// BoundaryAfterAppend is after the ledger append and before checkpoints.
	BoundaryAfterAppend Boundary = "after-append"
	// BoundaryAfterProjection is after critical projections are applied.
	BoundaryAfterProjection Boundary = "after-projection"
	// BoundaryAfterOutbox is after external-effect intents are enqueued.
	BoundaryAfterOutbox Boundary = "after-outbox"
	// BoundaryBeforeReceipt is before the durable replay receipt is recorded.
	BoundaryBeforeReceipt Boundary = "before-receipt"
	// BoundaryAfterCommit is after PostgreSQL acknowledged the local commit.
	BoundaryAfterCommit Boundary = "after-commit"
)

var boundaries = []Boundary{
	BoundaryBeforeAppend,
	BoundaryAfterAppend,
	BoundaryAfterProjection,
	BoundaryAfterOutbox,
	BoundaryBeforeReceipt,
	BoundaryAfterCommit,
}

// Boundaries returns every commit boundary in execution order.
func Boundaries() []Boundary { return append([]Boundary(nil), boundaries...) }
