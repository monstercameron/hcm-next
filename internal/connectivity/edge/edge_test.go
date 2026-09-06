package edge

import (
	"strings"
	"testing"
)

// placeholderManifest uses no real vendor, certificate, or customer values.
// The implementation selection is intentionally labelled for human supply.
func placeholderManifest() Manifest {
	return Manifest{
		SchemaVersion: 1, QualificationID: "qualification:edge:placeholder", ImplementationRef: "placeholder:edge-provider", ImplementationVersion: "placeholder:pinned-version",
		Routes:   []Route{{ID: "promotion-api", Host: "placeholder:api-host", PathPrefix: "/v1/promotion", Methods: []string{"POST", "GET"}, Backend: "promotion-api", TLSVersion: "TLS1.3", CertificateRef: "placeholder:certificate", CertificateDigest: "sha256:" + strings.Repeat("a", 64), DNSName: "placeholder:api-host", DNSIPs: []string{"192.0.2.10"}, WAFRef: "placeholder:waf-policy", WAFVersion: "2026.09.01", OwnerRef: "placeholder:edge-owner", MaxRPS: 100, BodyLimitBytes: 1048576, FailureAction: "DENY"}},
		EastWest: []EastWestRule{{FromService: "promotion-worker", ToService: "ledger", Protocol: "grpc", Port: 8443, OwnerRef: "placeholder:platform-owner", FailClosed: true}},
		Egress:   []EgressRule{{ID: "provider-read", Destination: "placeholder:provider-host", Port: 443, Purpose: "promotion-observation", OwnerRef: "placeholder:egress-owner", Logged: true, RedactionRef: "placeholder:redaction-policy", FailureAction: "DENY"}},
		Outage:   OutagePlan{DetectionSignal: "edge.error_budget", DegradedAction: "pause-new-effects", ReplacementRef: "placeholder:replacement", MigrationPlan: "requalify-and-cutover"},
	}
}

func TestEdgeImplementationManifestRejectsAbstractWildcardOrUnverifiedRouteControl(t *testing.T) {
	evidence, err := Compile(placeholderManifest())
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Status != "HUMAN_SELECTION_REQUIRED" || evidence.Ready {
		t.Fatalf("placeholder implementation was marked ready: %+v", evidence)
	}
	bad := placeholderManifest()
	bad.Routes[0].Host = "*"
	if err := Check(bad); err == nil {
		t.Fatal("wildcard route was accepted")
	}
}

func TestTodo_EDGE_010_Property(t *testing.T) {
	one, err := Digest(placeholderManifest())
	if err != nil {
		t.Fatal(err)
	}
	m := placeholderManifest()
	m.Routes[0].Methods = []string{"GET", "POST"}
	two, err := Digest(m)
	if err != nil {
		t.Fatal(err)
	}
	if one != two {
		t.Fatalf("method order changed digest: %s != %s", one, two)
	}
}

func TestTodo_EDGE_010_Golden(t *testing.T) {
	one, err := Compile(placeholderManifest())
	if err != nil {
		t.Fatal(err)
	}
	two, err := Compile(placeholderManifest())
	if err != nil {
		t.Fatal(err)
	}
	if one.ManifestDigest != two.ManifestDigest || strings.Join(one.Controls, "|") != strings.Join(two.Controls, "|") {
		t.Fatalf("qualification is not deterministic: %+v %+v", one, two)
	}
}

func FuzzTodo_EDGE_010(f *testing.F) {
	f.Add("placeholder:host", "/v1", "promotion-api")
	f.Fuzz(func(t *testing.T, host, path, backend string) {
		m := placeholderManifest()
		m.Routes[0].Host, m.Routes[0].PathPrefix, m.Routes[0].Backend = host, path, backend
		if strings.Contains(host, "*") && Check(m) == nil {
			t.Fatal("wildcard host admitted")
		}
	})
}

func TestTodo_EDGE_010_Integration(t *testing.T) {
	evidence, err := Compile(placeholderManifest())
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Controls) != 7 || evidence.ManifestDigest == "" {
		t.Fatalf("incomplete edge evidence: %+v", evidence)
	}
}

func TestTodo_EDGE_010_Fault(t *testing.T) {
	m := placeholderManifest()
	m.Egress[0].FailureAction = "ALLOW"
	if err := Check(m); err == nil {
		t.Fatal("fail-open egress was accepted")
	}
}

func TestTodo_EDGE_010_Security(t *testing.T) {
	m := placeholderManifest()
	m.Routes[0].CertificateDigest = "latest"
	if err := Check(m); err == nil {
		t.Fatal("mutable certificate was accepted")
	}
}

func TestTodo_EDGE_010_Conformance(t *testing.T) {
	if Version() != 1 || !strings.Contains(Explain(), "fail-closed") {
		t.Fatalf("contract metadata missing: version=%d explain=%q", Version(), Explain())
	}
	if err := Check(placeholderManifest()); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_EDGE_010_Recovery(t *testing.T) {
	m := placeholderManifest()
	m.Outage.ReplacementRef = ""
	if err := Check(m); err == nil {
		t.Fatal("missing replacement path was accepted")
	}
}

func BenchmarkTodo_EDGE_010(b *testing.B) {
	m := placeholderManifest()
	for i := 0; i < b.N; i++ {
		if _, err := Compile(m); err != nil {
			b.Fatal(err)
		}
	}
}

func TestTodo_EDGE_010_Mutation(t *testing.T) {
	m := placeholderManifest()
	original := m.Routes[0].Methods[0]
	if _, err := Compile(m); err != nil {
		t.Fatal(err)
	}
	if m.Routes[0].Methods[0] != original {
		t.Fatal("compile mutated route methods")
	}
}
