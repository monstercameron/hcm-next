package budget

import "time"

// ReservationPort is the persistence-neutral contract for the compensation
// reservation state machine. ReservationStore is the pure in-memory
// reference implementation; durable adapters implement the same operations.
// The port intentionally retains the domain's existing signatures so callers
// can swap the reference store for a repository without changing decisions.
type ReservationPort interface {
	Reserve(CompensationReservationRequest, CompensationBudgetAuthority, time.Time) (CompensationReservation, error)
	Commit(string, uint64, time.Time) (CompensationReservation, error)
	Release(string, uint64, time.Time) (CompensationReservation, error)
	MarkAmbiguous(string, uint64, time.Time) error
	Reconcile(string, uint64, ReservationState, time.Time) (CompensationReservation, error)
	Expire(time.Time) []CompensationReservation
	Get(string) (CompensationReservation, bool)
	Events(string) []ReservationEvent
	Evidence(string) (ReservationEvidence, bool)
}

var _ ReservationPort = (*ReservationStore)(nil)
