package budget

import "testing"

func TestReservationStoreSatisfiesReservationPort(t *testing.T) {
	// NewReservationStore returns a struct value, so a runtime nil
	// comparison could never fail; the assignment itself is the assertion.
	var _ ReservationPort = NewReservationStore()
}
