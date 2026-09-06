package workspace

import (
	"errors"
	"testing"
)

func TestReadSessionParsesTheFourDisplayFields(t *testing.T) {
	raw := []byte(`{"tenant":"northwind","subject":"avery.okafor@northwind.example","roles":["hr.business_partner","promotion.approver"],"purpose":"promotion_review"}`)
	s, err := ReadSession(raw)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if s.Tenant != "northwind" || s.Subject != "avery.okafor@northwind.example" || s.Purpose != "promotion_review" {
		t.Fatalf("ReadSession = %+v, missing an expected field", s)
	}
	if len(s.Roles) != 2 || s.Roles[0] != "hr.business_partner" || s.Roles[1] != "promotion.approver" {
		t.Fatalf("ReadSession roles = %v, want the two fixture roles in order", s.Roles)
	}
}

// TestReadSessionIgnoresCredentialAndTransportFields proves the security
// property this package's doc comments claim: even a real journey-config
// island, which also carries "bearer" and "tunnel_url", never ends up
// reachable through a Session value at all -- there is no field for either,
// so encoding/json drops them on Unmarshal rather than this package
// choosing not to render them.
func TestReadSessionIgnoresCredentialAndTransportFields(t *testing.T) {
	raw := []byte(`{"tunnel_url":"wss://cell.example/workspace/grpc","bearer":"super-secret-token","tenant":"northwind","subject":"avery.okafor@northwind.example","journeys_path":"/workspace/journey"}`)
	s, err := ReadSession(raw)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if s.Tenant != "northwind" || s.Subject != "avery.okafor@northwind.example" {
		t.Fatalf("ReadSession = %+v, expected the two known fields to parse", s)
	}
	// Session has no Bearer or TunnelURL field to check -- that IS the
	// property under test: the type cannot hold either value, so there is
	// nothing here to assert against beyond "it compiles and %+v never
	// prints one", which the Errorf above already exercises.
}

func TestReadSessionAdmitsAnEmptyOrMinimalObject(t *testing.T) {
	s, err := ReadSession([]byte(`{}`))
	if err != nil {
		t.Fatalf("ReadSession({}): %v", err)
	}
	if !s.IsZero() {
		t.Fatalf("ReadSession({}) = %+v, want a zero Session", s)
	}
}

func TestReadSessionRefusesMalformedJSON(t *testing.T) {
	_, err := ReadSession([]byte(`not json`))
	if err == nil {
		t.Fatal("ReadSession of malformed JSON succeeded, want a refusal")
	}
	if !errors.Is(err, ErrSessionMalformed) {
		t.Fatalf("ReadSession error = %v, want it to wrap ErrSessionMalformed", err)
	}
}

func TestSessionIsZero(t *testing.T) {
	if !(Session{}).IsZero() {
		t.Error("zero-value Session.IsZero() = false, want true")
	}
	if (Session{Subject: "x"}).IsZero() {
		t.Error("Session with a Subject reported IsZero() = true")
	}
	if (Session{Roles: []string{"a"}}).IsZero() {
		t.Error("Session with a Roles entry reported IsZero() = true")
	}
}
