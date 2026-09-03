package fixtures

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/people"
)

func TestCalendar(t *testing.T) {
	cal, err := Calendar()
	if err != nil {
		t.Fatal(err)
	}
	if cal.Ref == "" || cal.Version == "" {
		t.Fatal("empty calendar")
	}
}

func TestWorkerRef(t *testing.T) {
	ref, err := WorkerRef("jane-doe")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Id == "" {
		t.Fatal("empty worker id")
	}
	if ref.Tenant != Tenant {
		t.Fatalf("tenant mismatch %v", ref.Tenant)
	}
	_, err = WorkerRef("no-such-worker-xyz")
	if err == nil {
		t.Fatal("expected error for unknown worker")
	}
}

func TestMoney(t *testing.T) {
	m, err := Money("100.00", "USD")
	if err != nil {
		t.Fatal(err)
	}
	if m.Currency() != "USD" {
		t.Fatalf("currency %q", m.Currency())
	}
	if _, err := Money("not-a-number", "USD"); err == nil {
		t.Fatal("expected error for bad money")
	}
}

func TestPercent(t *testing.T) {
	p, err := Percent("0.1000")
	if err != nil {
		t.Fatal(err)
	}
	_ = p
	if _, err := Percent("bad"); err == nil {
		t.Fatal("expected error for bad percent")
	}
}

func TestLegacyScenarios(t *testing.T) {
	s, err := LegacyScenarios()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Scenarios) == 0 {
		t.Fatal("no scenarios")
	}
}

func TestMemoryWorkerFacts(t *testing.T) {
	m, err := NewMemoryWorkerFacts()
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatal("nil")
	}
	if len(m.Missing) != 0 {
		t.Fatal("missing not empty")
	}
}

func TestMemoryBandCatalog(t *testing.T) {
	c, err := NewMemoryBandCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if c.CatalogVersion() == "" {
		t.Fatal("empty catalog version")
	}
	if c.CatalogVersion() == "" {
		t.Fatal("empty")
	}
}

func TestBand(t *testing.T) {
	c, err := NewMemoryBandCatalog()
	if err != nil {
		t.Fatal(err)
	}
	_ = c
	if len(c.bands) == 0 {
		t.Fatal("no bands")
	}
	b, err := Band(c.bands[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.ID == "" {
		t.Fatal("empty band id")
	}
	if _, err := Band("no-such-band-xyz"); err == nil {
		t.Fatal("expected error for unknown band")
	}
}

func TestAllowAllDeny(t *testing.T) {
	decision := AllowAll("v1", "test", []people.FieldID{people.FieldWorkerNumber, people.FieldLegalName})
	if len(decision.Fields) != 2 {
		t.Fatalf("fields %d", len(decision.Fields))
	}
	denied := DenyFields(decision, "need-to-know", people.FieldLegalName)
	if denied.Fields[people.FieldLegalName].Effect != people.EffectDeny {
		t.Fatal("expected deny")
	}
	withheld := WithheldSubject(decision, "reason")
	if withheld.SubjectDisclosable {
		t.Fatal("expected withheld")
	}
}
