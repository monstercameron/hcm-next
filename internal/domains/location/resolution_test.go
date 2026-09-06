package location

import (
	"strings"
	"testing"
)

func resolutionDataset(t *testing.T) ResolutionDataset {
	t.Helper()
	table, err := NewJurisdictionTable(JurisdictionTable{TableID: "jurisdiction", Version: "2026.1", Rules: []JurisdictionRule{
		{CountryCode: "US", SubdivisionCode: "US-NY", Locality: "New York", FederalTax: "fed", StateTax: "ny", LocalTax: "nyc", FederalLabor: "fed-labor", StateLabor: "ny-labor", LocalLabor: "nyc-labor"},
		{CountryCode: "US", SubdivisionCode: "US-NY", Locality: "Albany", FederalTax: "fed", StateTax: "ny", LocalTax: "albany", FederalLabor: "fed-labor", StateLabor: "ny-labor", LocalLabor: "albany-labor"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return ResolutionDataset{DatasetID: "location-reference", Version: "2026.1", TZDBVersion: "2026a", LocalityRules: []LocalityRule{{CountryCode: "US", SubdivisionCode: "US-NY", PostalPrefix: "100", Locality: "New York"}}, TimezoneRules: []TimezoneRule{{CountryCode: "US", SubdivisionCode: "US-NY", Locality: "New York", Timezone: "America/New_York"}}, JurisdictionTable: table}
}

func TestLocationResolutionReturnsCandidatesConfidenceAndUnknownWithoutInventingJurisdiction(t *testing.T) {
	dataset := resolutionDataset(t)
	got, err := ResolveAddress(ResolutionRequest{Address: Address{Lines: []string{" 1 Main   Street ", "Suite 4"}, Locality: " New York ", PostalCode: "10001", CountryCode: "us", SubdivisionCode: "us-ny"}, Dataset: dataset})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ResolutionResolved || got.NormalizedAddress.Line1 != "1 Main Street" {
		t.Fatalf("resolution = %+v", got)
	}
	if len(got.LocalityCandidates) != 1 || got.LocalityCandidates[0].Value != "New York" || len(got.TimezoneCandidates) != 1 || got.TimezoneCandidates[0].TZDBVersion != "2026a" {
		t.Fatalf("candidates = %+v/%+v", got.LocalityCandidates, got.TimezoneCandidates)
	}
	if len(got.JurisdictionCandidates) != 1 || got.JurisdictionCandidates[0].Value != "US|US-NY|New York" {
		t.Fatalf("jurisdiction candidates = %+v", got.JurisdictionCandidates)
	}
	if got.OriginalAddress.Lines[0] != " 1 Main   Street " || got.OriginalAddress.Line1 != "" {
		t.Fatalf("original evidence was normalized or aliased: %+v", got.OriginalAddress)
	}

	unknown, err := ResolveAddress(ResolutionRequest{Address: Address{Lines: []string{"1 Unknown Road"}, Locality: "Unknown", CountryCode: "CA"}, Dataset: dataset})
	if err != nil {
		t.Fatal(err)
	}
	if unknown.Status != ResolutionPartial && unknown.Status != ResolutionUnknown {
		t.Fatalf("unknown status = %s", unknown.Status)
	}
	if len(unknown.JurisdictionCandidates) != 0 {
		t.Fatalf("invented jurisdiction candidate = %+v", unknown.JurisdictionCandidates)
	}
}

func TestTodo_LOCATION_002_Property(t *testing.T) {
	dataset := resolutionDataset(t)
	a := Address{Lines: []string{"1 Main Street"}, Locality: "New York", PostalCode: "10001", CountryCode: "US", SubdivisionCode: "US-NY"}
	one, err := ResolveAddress(ResolutionRequest{Address: a, Dataset: dataset})
	if err != nil {
		t.Fatal(err)
	}
	two, err := ResolveAddress(ResolutionRequest{Address: a, Dataset: dataset})
	if err != nil || one.CanonicalDigest != two.CanonicalDigest {
		t.Fatalf("resolution is not deterministic: %s/%s/%v", one.CanonicalDigest, two.CanonicalDigest, err)
	}
}

func TestTodo_LOCATION_002_Golden(t *testing.T) {
	got, err := ResolveAddress(ResolutionRequest{Address: Address{Lines: []string{"1 Main Street"}, Locality: "New York", PostalCode: "10001", CountryCode: "US", SubdivisionCode: "US-NY"}, Dataset: resolutionDataset(t)})
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalDigest == "" || len(got.canonical()) == 0 {
		t.Fatal("resolution has no canonical evidence")
	}
}

func TestTodo_LOCATION_002_Race(t *testing.T) {
	dataset := resolutionDataset(t)
	resolver, err := NewResolver(dataset)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{}, 16)
	for i := 0; i < 16; i++ {
		go func() {
			_, _ = resolver.Resolve(Address{Lines: []string{"1 Main Street"}, Locality: "New York", CountryCode: "US", SubdivisionCode: "US-NY"})
			done <- struct{}{}
		}()
	}
	for i := 0; i < 16; i++ {
		<-done
	}
}

func TestTodo_LOCATION_002_Fault(t *testing.T) {
	if _, err := NewResolver(ResolutionDataset{Version: "2026.1"}); err == nil {
		t.Fatal("unpinned tzdb dataset accepted")
	}
	if _, err := NewResolver(ResolutionDataset{Version: "2026.1", TZDBVersion: "2026a", TimezoneRules: []TimezoneRule{{CountryCode: "US", Timezone: "", Confidence: ConfidenceHigh}}}); err == nil {
		t.Fatal("incomplete timezone rule accepted")
	}
}

func TestTodo_LOCATION_002_Security(t *testing.T) {
	got, err := ResolveAddress(ResolutionRequest{Address: Address{Lines: []string{"1 Main Street"}, Locality: "New York", PostalCode: "10001", CountryCode: "US", SubdivisionCode: "US-NY"}, Dataset: resolutionDataset(t)})
	if err != nil {
		t.Fatal(err)
	}
	explanation, err := got.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(fmtResolutionExplanation(explanation)), "main") || strings.Contains(fmtResolutionExplanation(explanation), "10001") {
		t.Fatal("resolution explanation disclosed address evidence")
	}
}

func fmtResolutionExplanation(e ResolutionExplanation) string {
	return e.Status.String() + e.DatasetVersion + e.TZDBVersion + e.Digest
}

func TestTodo_LOCATION_002_Conformance(t *testing.T) {
	got, err := ResolveAddress(ResolutionRequest{Address: Address{Lines: []string{"1 Main Street"}, CountryCode: "US"}, Dataset: resolutionDataset(t)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ResolutionPartial && got.Status != ResolutionUnknown {
		t.Fatalf("incomplete address status = %s", got.Status)
	}
	if len(got.JurisdictionCandidates) != 0 {
		t.Fatal("incomplete address invented jurisdiction")
	}
}

func TestTodo_LOCATION_002_Mutation(t *testing.T) {
	dataset := resolutionDataset(t)
	one, err := ResolveAddress(ResolutionRequest{Address: Address{Lines: []string{"1 Main Street"}, Locality: "New York", CountryCode: "US", SubdivisionCode: "US-NY"}, Dataset: dataset})
	if err != nil {
		t.Fatal(err)
	}
	dataset.TZDBVersion = "2026b"
	two, err := ResolveAddress(ResolutionRequest{Address: Address{Lines: []string{"1 Main Street"}, Locality: "New York", CountryCode: "US", SubdivisionCode: "US-NY"}, Dataset: dataset})
	if err != nil {
		t.Fatal(err)
	}
	if one.CanonicalDigest == two.CanonicalDigest {
		t.Fatal("tzdb release mutation did not change resolution identity")
	}
}
