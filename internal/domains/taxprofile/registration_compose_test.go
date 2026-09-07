package taxprofile

import (
	"errors"
	"testing"
)

type rawRegistrationPort struct{ values []TaxRegistrationRevision }

func (p rawRegistrationPort) Registrations() []TaxRegistrationRevision { return p.values }

func TestComposeRegistrations_OrdersValidatesAndRefusesDuplicates(t *testing.T) {
	ny := validRegistration(t, "US-NY")
	ca := validRegistration(t, "US-CA")
	port, err := NewMemoryRegistrationPort([]TaxRegistrationRevision{ny, ca})
	if err != nil {
		t.Fatalf("port: %v", err)
	}
	got, err := ComposeRegistrations(port)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if len(got) != 2 || got[0].Jurisdiction != "US-CA" || got[1].Jurisdiction != "US-NY" {
		t.Fatalf("registrations must be ordered by jurisdiction, got %+v", got)
	}
	if _, err := ComposeRegistrations(rawRegistrationPort{values: []TaxRegistrationRevision{ny, ny}}); !errors.Is(err, ErrDuplicateRegistration) {
		t.Fatalf("duplicate registration must be refused, got %v", err)
	}
	if _, err := ComposeRegistrations(rawRegistrationPort{values: []TaxRegistrationRevision{{Jurisdiction: "US-TX"}}}); err == nil {
		t.Fatal("invalid registration from a port must be refused")
	}
	if _, err := ComposeRegistrations(nil); err == nil {
		t.Fatal("nil port must be refused")
	}
	if got, err := ComposeRegistrations(rawRegistrationPort{}); err != nil || len(got) != 0 {
		t.Fatalf("empty port must compose to no registrations, got %v %v", got, err)
	}
}
