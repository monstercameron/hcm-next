package location

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func locationInterval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	a, _ := values.NewLocalDate(2026, time.January, 1)
	b, _ := values.NewLocalDate(2027, time.January, 1)
	iv, err := values.NewLocalDateInterval(a, b, values.CalendarRef{Ref: "us-federal", Version: "2026"})
	if err != nil {
		t.Fatal(err)
	}
	return iv
}

func knownAt(t *testing.T) values.KnownAt {
	t.Helper()
	known, err := values.NewKnownAt(values.NewInstant(time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	return known
}

func validAddress(t *testing.T) Address {
	t.Helper()
	a, err := NewAddress(Address{Lines: []string{"  1 Main   Street ", " Suite 4 "}, Locality: " New   York ", AdministrativeArea: " New York ", PostalCode: "10001", CountryCode: "us", SubdivisionCode: "us-ny"})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func validWorksite(t *testing.T) WorksiteRevision {
	t.Helper()
	w, err := NewWorksiteRevision(WorksiteRevision{WorksiteID: "worksite:001", Revision: 1, WorkLocationRef: "location:001", Name: " New   York   Office ", Address: validAddress(t), LocalityCandidates: []string{"New York"}, TimezoneCandidates: []string{"America/New_York"}, SourceAuthority: "worksite.registry/v1", Confidence: ConfidenceHigh, Effective: locationInterval(t), KnownAt: knownAt(t)})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func validTable(t *testing.T) JurisdictionTable {
	t.Helper()
	table, err := NewJurisdictionTable(JurisdictionTable{TableID: "tax-labor-table", Version: "2026.1", Rules: []JurisdictionRule{{CountryCode: "US", SubdivisionCode: "US-NY", Locality: "New York", FederalTax: "US-FED-TAX", StateTax: "US-NY-TAX", LocalTax: "US-NYC-TAX", FederalLabor: "US-FED-LABOR", StateLabor: "US-NY-LABOR", LocalLabor: "US-NYC-LABOR"}, {CountryCode: "US", FederalTax: "US-FED-TAX", StateTax: "US-DEFAULT-TAX", LocalTax: "US-DEFAULT-LOCAL-TAX", FederalLabor: "US-FED-LABOR", StateLabor: "US-DEFAULT-LABOR", LocalLabor: "US-DEFAULT-LOCAL-LABOR"}}})
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func TestLocationModelSeparatesHomeAddressWorksiteTaxResidenceAndMailing(t *testing.T) {
	a := validAddress(t)
	if a.CountryCode != "US" || a.SubdivisionCode != "US-NY" || a.Line1 != "1 Main Street" || a.CanonicalDigest == "" {
		t.Fatalf("normalized address = %+v", a)
	}
	w := validWorksite(t)
	if w.Address.CanonicalDigest != a.CanonicalDigest || w.CanonicalDigest == "" {
		t.Fatal("worksite did not retain address identity")
	}
	r, err := NewJurisdictionResolver(validTable(t))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := r.Resolve(w)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.StateTax != "US-NY-TAX" || resolved.LocalLabor != "US-NYC-LABOR" || resolved.Tax.State != resolved.StateTax {
		t.Fatalf("resolution = %+v", resolved)
	}
}

func TestTodo_LOCATION_001_Property(t *testing.T) {
	a := validAddress(t)
	b := validAddress(t)
	if a.CanonicalDigest != b.CanonicalDigest {
		t.Fatal("equal addresses have different digests")
	}
	a.Lines[0] = "tampered"
	if b.Lines[0] == "tampered" {
		t.Fatal("address construction aliased lines")
	}
	w := validWorksite(t)
	if got, err := w.Digest(); err != nil || got != w.CanonicalDigest {
		t.Fatalf("worksite digest = %q, %v", got, err)
	}
}

func TestTodo_LOCATION_001_Golden(t *testing.T) {
	w := validWorksite(t)
	if len(w.Canonical()) == 0 {
		t.Fatal("worksite has no canonical bytes")
	}
	x, err := w.Explain()
	if err != nil || !x.HasAddress || x.Digest != w.CanonicalDigest {
		t.Fatalf("explanation = %+v, %v", x, err)
	}
	table := validTable(t)
	if got, err := table.Digest(); err != nil || got != table.CanonicalDigest {
		t.Fatalf("table digest = %q, %v", got, err)
	}
}

func TestTodo_LOCATION_001_Race(t *testing.T) {
	w := validWorksite(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = w.Explain(); _ = w.Validate() }()
	}
	wg.Wait()
}

func TestTodo_LOCATION_001_Fault(t *testing.T) {
	a := validAddress(t)
	a.CountryCode = "USA"
	if err := a.Validate(); err == nil {
		t.Fatal("invalid country code accepted")
	}
	w := validWorksite(t)
	w.KnownAt = values.KnownAt{}
	if err := w.Validate(); err == nil {
		t.Fatal("missing known-at accepted")
	}
	if _, err := ResolveWorksite(validTable(t), w); err == nil {
		t.Fatal("invalid worksite resolved")
	}
}

func TestTodo_LOCATION_001_Security(t *testing.T) {
	w := validWorksite(t)
	x, err := w.Explain()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"1 Main Street", "10001", "worksite:001", "worksite.registry/v1"} {
		if strings.Contains(fmtLocationExplanation(x), secret) {
			t.Fatalf("explanation leaked %q", secret)
		}
	}
}

func fmtLocationExplanation(x Explanation) string { return fmt.Sprintf("%+v", x) }

func TestTodo_LOCATION_001_Conformance(t *testing.T) {
	if Confidence("PROVIDER_ACCEPTED").Valid() {
		t.Fatal("provider state became confidence")
	}
	table := validTable(t)
	w := validWorksite(t)
	w.Address.CountryCode = "CA"
	w.Address.CanonicalDigest = ""
	w.CanonicalDigest = ""
	if _, err := ResolveWorksite(table, w); !errors.Is(err, ErrJurisdictionUnknown) {
		t.Fatalf("unknown jurisdiction = %v", err)
	}
}

func TestTodo_LOCATION_001_Mutation(t *testing.T) {
	w := validWorksite(t)
	parent := w
	next, err := w.Successor(WorksiteRevision{Name: "New York Office Corrected", WorkLocationRef: "location:001", Address: validAddress(t), LocalityCandidates: []string{"New York"}, SourceAuthority: "correction/v1", Confidence: ConfidenceAuthoritative, Effective: locationInterval(t), KnownAt: knownAt(t)})
	if err != nil {
		t.Fatal(err)
	}
	if next.Revision != 2 || next.ParentRevision != 1 || next.ParentDigest != parent.CanonicalDigest {
		t.Fatalf("lineage = %+v", next)
	}
	forged := next
	forged.CanonicalDigest = "sha256:forged"
	if err := forged.Validate(); err == nil {
		t.Fatal("forged worksite digest accepted")
	}
}
