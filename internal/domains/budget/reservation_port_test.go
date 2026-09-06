package budget

import "testing"

func TestReservationStoreSatisfiesReservationPort(t *testing.T) {
	var port ReservationPort = NewReservationStore()
	if port == nil {
		t.Fatal("new reservation store returned a nil reservation port")
	}
}
