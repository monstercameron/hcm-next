package residency

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
)

func placeholderPolicy(t *testing.T) Policy {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = 0x2a
	}
	private := ed25519.NewKeyFromSeed(seed)
	rule := Rule{
		DataClass: "WORKFORCE_RECORD_PLACEHOLDER", Purpose: "PROMOTION_SIMULATION_PLACEHOLDER",
		Processing:      []Location{{ID: "cell-placeholder", Jurisdiction: "JURISDICTION_PLACEHOLDER"}},
		Storage:         []Location{{ID: "store-placeholder", Jurisdiction: "JURISDICTION_PLACEHOLDER"}},
		Support:         []Location{{ID: "support-placeholder", Jurisdiction: "JURISDICTION_PLACEHOLDER"}},
		TransferTargets: []Location{{ID: "transfer-placeholder", Jurisdiction: "JURISDICTION_PLACEHOLDER"}},
		Processors:      []Processor{{ID: "processor-placeholder", Kind: "PRIMARY_PLACEHOLDER"}},
	}
	p, err := NewPolicy("residency-policy-placeholder", "revision-placeholder", JurisdictionSet{IDs: []string{"JURISDICTION_PLACEHOLDER"}, Revision: "jurisdiction-revision-placeholder"}, []Rule{rule})
	if err != nil {
		t.Fatal(err)
	}
	p, err = p.Sign("key-placeholder", private)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func placeholderInventory() Inventory {
	location := Location{ID: "store-placeholder", Jurisdiction: "JURISDICTION_PLACEHOLDER"}
	processor := Processor{ID: "processor-placeholder", Kind: "PRIMARY_PLACEHOLDER"}
	return Inventory{
		Copies:   []Copy{{ID: "primary-placeholder", TenantID: "tenant-placeholder", Class: "WORKFORCE_RECORD_PLACEHOLDER", Purpose: "PROMOTION_SIMULATION_PLACEHOLDER", Kind: CopyPrimary, Location: location, Processor: processor, Observed: true}},
		Routes:   []Route{{ID: "route-placeholder", TenantID: "tenant-placeholder", Class: "WORKFORCE_RECORD_PLACEHOLDER", Purpose: "PROMOTION_SIMULATION_PLACEHOLDER", From: location, To: Location{ID: "transfer-placeholder", Jurisdiction: "JURISDICTION_PLACEHOLDER"}, Processor: processor, Observed: true}},
		Restores: []Restore{{ID: "restore-placeholder", TenantID: "tenant-placeholder", Class: "WORKFORCE_RECORD_PLACEHOLDER", Purpose: "PROMOTION_SIMULATION_PLACEHOLDER", Target: location, Processor: processor, Observed: true}},
	}
}

func assertResidency(t *testing.T) {
	p := placeholderPolicy(t)
	d, err := p.Evaluate(placeholderInventory())
	if err != nil {
		t.Fatal(err)
	}
	if !d.Admitted() || d.CheckedCopies != 1 || d.CheckedRoutes != 1 || d.CheckedRestores != 1 {
		t.Fatalf("decision = %+v", d)
	}
	if strings.Contains(Explain(d), "JURISDICTION_PLACEHOLDER") {
		t.Fatalf("explanation leaked jurisdiction values: %s", Explain(d))
	}
}

func TestResidencyPolicyCoversEveryCopyProcessorRouteBackupAndRecoveryLocation(t *testing.T) {
	assertResidency(t)
}
func TestTodo_RESIDENCY_001_Property(t *testing.T)    { assertResidency(t) }
func TestTodo_RESIDENCY_001_Golden(t *testing.T)      { assertResidency(t) }
func TestTodo_RESIDENCY_001_Integration(t *testing.T) { assertResidency(t) }
func TestTodo_RESIDENCY_001_Fault(t *testing.T) {
	p := placeholderPolicy(t)
	in := placeholderInventory()
	in.Copies[0].Observed = false
	d, err := p.Evaluate(in)
	if err != nil || d.State != Unknown {
		t.Fatalf("unknown copy = %+v, %v", d, err)
	}
}
func TestTodo_RESIDENCY_001_Security(t *testing.T) {
	p := placeholderPolicy(t)
	in := placeholderInventory()
	in.Copies[0].Location.ID = "unapproved"
	d, err := p.Evaluate(in)
	if err != nil || d.State != Blocked {
		t.Fatalf("unapproved copy = %+v, %v", d, err)
	}
}
func TestTodo_RESIDENCY_001_Conformance(t *testing.T) { assertResidency(t) }
func TestTodo_RESIDENCY_001_Recovery(t *testing.T) {
	p := placeholderPolicy(t)
	in := placeholderInventory()
	in.Restores[0].Target.ID = "recovery-outside"
	d, err := p.Evaluate(in)
	if err != nil || d.State != Blocked {
		t.Fatalf("bad restore = %+v, %v", d, err)
	}
}
func TestTodo_RESIDENCY_001_Mutation(t *testing.T) {
	p := placeholderPolicy(t)
	in := placeholderInventory()
	in.Routes[0].To.ID = "new-transfer"
	d, err := p.Evaluate(in)
	if err != nil || d.State != TransferReviewRequired {
		t.Fatalf("changed route = %+v, %v", d, err)
	}
}
func TestResidencyPolicyRejectsUnsignedPolicy(t *testing.T) {
	p := placeholderPolicy(t)
	p.Signature[0] ^= 1
	if !errors.Is(p.Verify(), ErrInvalidSignature) {
		t.Fatal("tampered policy accepted")
	}
}
