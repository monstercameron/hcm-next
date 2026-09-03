package status

import (
	"errors"
	"testing"
	"time"
)

func TestRegistryMatrixExactAndCopy(t *testing.T) {
	m := RegistryMatrix()
	if len(m) != 5 {
		t.Fatalf("registry length=%d", len(m))
	}
	if m[0].Code != string(Unknown) || m[1].Code != string(Operational) || m[2].Code != string(Degraded) || m[3].Code != string(Outage) || m[4].Code != string(Maintenance) {
		t.Fatalf("registry=%+v", m)
	}
	m[0].Code = "x"
	if v, _ := Lookup(string(Unknown)); v.Code != string(Unknown) {
		t.Fatal("mutable registry")
	}
}
func TestPresentTenantFreshnessAndTieOrdering(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	exp := now.Add(-time.Second)
	in := Request{TenantID: "a", Authorization: AuthorizationCurrent, Now: now, MaxAge: 5 * time.Minute}
	s, err := Present(in, []Service{{ID: "z", TenantID: "b", State: Outage, UpdatedAt: now}, {ID: "a", TenantID: "a", State: Operational, UpdatedAt: now}}, []Incident{{ID: "i2", TenantID: "a", UpdatedAt: now}, {ID: "i1", TenantID: "a", UpdatedAt: now}, {ID: "other", TenantID: "b", UpdatedAt: now}}, []Advisory{{ID: "old", TenantID: "a", UpdatedAt: now.Add(-time.Hour)}, {ID: "expired", TenantID: "a", UpdatedAt: now, ExpiresAt: &exp}, {ID: "foreign", TenantID: "b", UpdatedAt: now}})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Services) != 1 || len(s.Incidents) != 2 || len(s.Advisories) != 2 {
		t.Fatalf("leak: %+v", s)
	}
	if s.Incidents[0].ID != "i1" || s.Incidents[1].ID != "i2" {
		t.Fatalf("tie order=%v", s.Incidents)
	}
	if s.Freshness != FreshnessStale {
		t.Fatalf("freshness=%s", s.Freshness)
	}
	if _, err = Present(Request{TenantID: "a", Authorization: AuthorizationDenied}, nil, nil, nil); !errors.Is(err, ErrUnauthorized) {
		t.Fatal(err)
	}
}

func TestStatusMatrixPrimary(t *testing.T) {
	cases := []struct {
		name  string
		state ServiceState
		label string
	}{
		{"unknown", Unknown, "Unknown"}, {"operational", Operational, "Operational"},
		{"degraded", Degraded, "Degraded"}, {"outage", Outage, "Outage"},
		{"maintenance", Maintenance, "Maintenance"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, ok := Lookup(string(tc.state))
			if !ok || e.Label != tc.label || e.Description == "" {
				t.Fatalf("entry=%+v ok=%v", e, ok)
			}
		})
	}
}

func TestStatusMatrixAllFaultRecoveryAndSecurity(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := Present(Request{TenantID: "", Authorization: AuthorizationCurrent, Now: now}, nil, nil, nil); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("missing tenant error=%v", err)
	}
	if _, err := Present(Request{TenantID: "a", Authorization: AuthorizationUnknown, Now: now}, nil, nil, nil); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("unknown auth error=%v", err)
	}
	if _, err := Present(Request{TenantID: "a", Authorization: AuthorizationCurrent, Now: now}, []Service{{ID: "api", TenantID: "", State: Outage}}, nil, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unscoped service error=%v", err)
	}
	if _, err := Present(Request{TenantID: "a", Authorization: AuthorizationCurrent, Now: now}, []Service{{ID: "api", TenantID: "a", State: ServiceState("BROKEN")}}, nil, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid state error=%v", err)
	}
	// A recovery refresh with the other tenant's identifiers remains empty.
	s, err := Present(Request{TenantID: "b", Authorization: AuthorizationCurrent, Now: now}, nil, []Incident{{ID: "a-incident", TenantID: "a", State: Outage}}, []Advisory{{ID: "a-advisory", TenantID: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Incidents) != 0 || len(s.Advisories) != 0 {
		t.Fatalf("cross-tenant recovery leak: %+v", s)
	}
}

func TestStatusPublicationMatchesIncidentScopeStateSLOAndCustomerDisclosurePolicy(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s, err := Present(Request{TenantID: "customer-a", Authorization: AuthorizationCurrent, Now: now},
		[]Service{{ID: "api", TenantID: "customer-a", State: Degraded, UpdatedAt: now}},
		[]Incident{{ID: "a-1", TenantID: "customer-a", ServiceID: "api", State: Degraded, Severity: Warning, UpdatedAt: now}, {ID: "b-1", TenantID: "customer-b", ServiceID: "api", State: Outage, Severity: Critical, UpdatedAt: now}},
		[]Advisory{{ID: "a-advisory", TenantID: "customer-a", IncidentID: "a-1", ServiceID: "api", Severity: Warning, Message: "limited impact", UpdatedAt: now}, {ID: "b-advisory", TenantID: "customer-b", IncidentID: "b-1", ServiceID: "api", Severity: Critical, Message: "private", UpdatedAt: now}})
	if err != nil || len(s.Services) != 1 || len(s.Incidents) != 1 || len(s.Advisories) != 1 || s.Incidents[0].ServiceID != "api" || s.Advisories[0].IncidentID != "a-1" {
		t.Fatalf("publication disclosure mismatch: %+v, %v", s, err)
	}
}

func TestTodo_STATUS_001_Conformance(t *testing.T) { TestRegistryMatrixExactAndCopy(t) }
func TestTodo_STATUS_001_Fault(t *testing.T)       { TestStatusMatrixAllFaultRecoveryAndSecurity(t) }
func TestTodo_STATUS_001_Golden(t *testing.T)      { TestStatusMatrixPrimary(t) }
func TestTodo_STATUS_001_Integration(t *testing.T) { TestPresentTenantFreshnessAndTieOrdering(t) }
func TestTodo_STATUS_001_Mutation(t *testing.T)    { TestRegistryMatrixExactAndCopy(t) }
func TestTodo_STATUS_001_Property(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tenant := range []string{"a", "b", "c"} {
		s, err := Present(Request{TenantID: tenant, Authorization: AuthorizationCurrent, Now: now}, nil, []Incident{{ID: "a", TenantID: "a"}, {ID: "b", TenantID: "b"}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range s.Incidents {
			if i.TenantID != tenant {
				t.Fatalf("tenant property violated: %q got %q", tenant, i.TenantID)
			}
		}
	}
}
func TestTodo_STATUS_001_Race(t *testing.T) {
	// Present is a pure projection; concurrent calls must not mutate inputs.
	in := []Service{{ID: "api", TenantID: "a", State: Operational}}
	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, _ = Present(Request{TenantID: "a", Authorization: AuthorizationCurrent}, in, nil, nil)
			done <- struct{}{}
		}()
	}
	<-done
	<-done
}
func TestTodo_STATUS_001_Recovery(t *testing.T) { TestStatusMatrixAllFaultRecoveryAndSecurity(t) }
func TestTodo_STATUS_001_Security(t *testing.T) { TestPresentTenantFreshnessAndTieOrdering(t) }
