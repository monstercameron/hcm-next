package commercial_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/commercial"
)

var pilotAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func pilotContract() commercial.Contract {
	return commercial.Contract{TenantID: "tenant-a", ContractID: "pilot-a", Version: 1,
		EffectiveFrom: pilotAt, EffectiveTo: pilotAt.Add(90 * 24 * time.Hour), Status: commercial.StatusActive,
		Capabilities: []string{"promotion.simulate", "worker.explain"}, PriceCents: 2500000, Currency: "USD"}
}

func TestTodo_COMM_001(t *testing.T) {
	cases := []struct {
		name    string
		request commercial.Request
		want    commercial.DecisionCode
	}{
		{"allow", commercial.Request{TenantID: "tenant-a", Capability: "promotion.simulate", At: pilotAt}, commercial.CodeAllow},
		{"not_yet_effective", commercial.Request{TenantID: "tenant-a", Capability: "promotion.simulate", At: pilotAt.Add(-time.Nanosecond)}, commercial.CodeContractNotYetEffective},
		{"expired", commercial.Request{TenantID: "tenant-a", Capability: "promotion.simulate", At: pilotAt.Add(90 * 24 * time.Hour)}, commercial.CodeContractExpired},
		{"out_of_scope", commercial.Request{TenantID: "tenant-a", Capability: "promotion.execute", At: pilotAt}, commercial.CodeCapabilityOutOfScope},
		{"tenant_mismatch", commercial.Request{TenantID: "tenant-b", Capability: "promotion.simulate", At: pilotAt}, commercial.CodeTenantMismatch},
	}
	s, err := commercial.NewSnapshot(pilotContract())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.Resolve(tc.request).Code; got != tc.want {
				t.Fatalf("code = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTodo_COMM_001_Golden(t *testing.T) {
	s, err := commercial.NewSnapshot(pilotContract())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Fingerprint) != 64 {
		t.Fatalf("fingerprint length = %d, want 64", len(s.Fingerprint))
	}
	other, err := commercial.NewSnapshot(pilotContract())
	if err != nil || other.Fingerprint != s.Fingerprint {
		t.Fatalf("fingerprint is not deterministic: %q != %q", other.Fingerprint, s.Fingerprint)
	}
	if got := s.Resolve(commercial.Request{TenantID: "tenant-a", Capability: "promotion.simulate", At: pilotAt}).Fingerprint; got != s.Fingerprint {
		t.Fatalf("decision fingerprint = %q, want %q", got, s.Fingerprint)
	}
}

func TestTodo_COMM_001_Integration(t *testing.T) {
	s, err := commercial.NewSnapshot(pilotContract())
	if err != nil {
		t.Fatal(err)
	}
	for _, channel := range []commercial.Channel{commercial.ChannelUI, commercial.ChannelHTTP, commercial.ChannelGRPC, commercial.ChannelWorkflow, commercial.ChannelConnector} {
		d := s.Resolve(commercial.Request{TenantID: "tenant-a", Capability: "promotion.execute", At: pilotAt, Channel: channel})
		if d.Code != commercial.CodeCapabilityOutOfScope || d.Fingerprint != s.Fingerprint {
			t.Fatalf("channel %s decision = %+v, want common denial/fingerprint", channel, d)
		}
	}
}

func TestTodo_COMM_001_Security(t *testing.T) {
	s, err := commercial.NewSnapshot(pilotContract())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	s.Contract.Capabilities[0] = "promotion.execute"
	if err := s.Validate(); !errors.Is(err, commercial.ErrInvalidContract) {
		t.Fatalf("tampered snapshot error = %v, want ErrInvalidContract", err)
	}
}

func TestTodo_COMM_001_Mutation(t *testing.T) {
	c := pilotContract()
	s, err := commercial.NewSnapshot(c)
	if err != nil {
		t.Fatal(err)
	}
	c.Version = 2
	c.Capabilities[0] = "promotion.execute/v1"
	if got := s.Resolve(commercial.Request{TenantID: "tenant-a", Capability: "promotion.simulate", At: pilotAt}); !got.Allowed() {
		t.Fatalf("amended source contract changed old snapshot: %+v", got)
	}
	if got := s.Contract.Version; got != 1 {
		t.Fatalf("snapshot version = %d, want 1", got)
	}
	newSnapshot, err := commercial.NewSnapshot(c)
	if err != nil {
		t.Fatal(err)
	}
	if newSnapshot.Fingerprint == s.Fingerprint {
		t.Fatal("amendment did not create a new fingerprint")
	}
}

func TestContractRejectsInvalidTerms(t *testing.T) {
	c := pilotContract()
	c.EffectiveTo = c.EffectiveFrom
	if _, err := commercial.NewSnapshot(c); !errors.Is(err, commercial.ErrInvalidContract) {
		t.Fatalf("invalid interval error = %v", err)
	}
}
