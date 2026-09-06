package vulnimpact

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/sbom"
)

func testDocument(rootName, rootVersion string, components ...sbom.Component) sbom.Document {
	return sbom.Document{Metadata: sbom.Metadata{Component: sbom.Component{Name: rootName, Version: rootVersion}}, Components: components}
}

func TestFindingAndReport_ExplainAndDigest(t *testing.T) {
	finding := Finding{VulnerabilityID: "V-1", ArtifactID: "app", ArtifactVersion: "1.0.0", Module: "mod", Version: "2.0.0", Severity: SeverityHigh, SBOMDigest: "sbom", Digest: "finding"}
	if got := finding.CanonicalDigest(); got != "finding" {
		t.Fatalf("CanonicalDigest()=%q, want finding", got)
	}
	if got := finding.Explain(); !strings.Contains(got, "vulnerability V-1 affects mod@2.0.0 in artifact app@1.0.0") || !strings.Contains(got, "finding") {
		t.Fatalf("Explain()=%q, missing finding identity", got)
	}
	report := Report{Vulnerability: Vulnerability{ID: "V-1", Module: "mod", Severity: SeverityHigh}, Findings: []Finding{finding}, AffectedArtifacts: []string{"app"}, AffectedTenants: []string{"tenant"}, Confidence: "COMPLETE", Watermark: "sbom", RemediationOwner: "owner", CustomerNotification: true, Digest: "report"}
	if got := report.Explain(); !strings.Contains(got, "vulnerability impact V-1 for mod (HIGH)") || !strings.Contains(got, "customer notification true") {
		t.Fatalf("Report.Explain()=%q, missing report fields", got)
	}
}

func TestResolveArtifacts_ValidationAndOrdering(t *testing.T) {
	valid := Vulnerability{ID: "V-1", Module: "dep", AffectedVersionRange: ">=1.0.0 <2.0.0", Severity: SeverityLow}
	base := testDocument("root", "1.0.0", sbom.Component{Name: "dep", Version: "1.5.0"})
	cases := []struct {
		name string
		v    Vulnerability
		as   []Artifact
		want string
	}{
		{name: "empty range", v: Vulnerability{Module: "dep", AffectedVersionRange: "", Severity: SeverityLow}, want: "invalid affected version range"},
		{name: "empty alternative", v: Vulnerability{Module: "dep", AffectedVersionRange: "1.0.0 ||", Severity: SeverityLow}, want: "empty alternative"},
		{name: "missing module", v: Vulnerability{AffectedVersionRange: "1.0.0", Severity: SeverityLow}, want: "module is required"},
		{name: "unknown severity", v: Vulnerability{Module: "dep", AffectedVersionRange: "1.0.0", Severity: SeverityUnknown}, want: "invalid severity"},
		{name: "missing artifact identity", v: valid, as: []Artifact{{SBOM: testDocument("", "1.0.0", sbom.Component{Name: "dep", Version: "1.5.0"})}}, want: "root has no name"},
		{name: "missing artifact version", v: valid, as: []Artifact{{ID: "app", SBOM: testDocument("root", "", sbom.Component{Name: "dep", Version: "1.5.0"})}}, want: "no SBOM root version"},
		{name: "duplicate artifact", v: valid, as: []Artifact{{ID: "app", SBOM: base}, {ID: "app", SBOM: base}}, want: "duplicate artifact"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.as == nil {
				tc.as = []Artifact{{ID: "app", SBOM: base}}
			}
			_, err := ResolveArtifacts(tc.as, tc.v)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want substring %q", err, tc.want)
			}
		})
	}

	artifacts := []Artifact{
		{ID: "z-app", SBOM: testDocument("z", "1.0.0", sbom.Component{Name: "dep", Version: "1.9.0"})},
		{ID: "a-app", SBOM: testDocument("a", "1.0.0", sbom.Component{Name: "dep", Version: "1.1.0"}, sbom.Component{Name: "dep", Version: "2.0.0"})},
	}
	findings, err := ResolveArtifacts(artifacts, valid)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 || findings[0].ArtifactID != "a-app" || findings[0].Version != "1.1.0" || findings[1].ArtifactID != "z-app" {
		t.Fatalf("findings=%+v, want sorted matching components", findings)
	}
	if findings[0].CanonicalDigest() == "" || findings[0].Explain() == "" {
		t.Fatalf("finding lacks identity or explanation: %+v", findings[0])
	}
}

func TestResolveAndAnalyze_DeploymentBoundaries(t *testing.T) {
	document := testDocument("root", "1.0.0", sbom.Component{Name: "dep", Version: "1.0.0"}, sbom.Component{Name: "other", Version: "1.0.0"})
	vulnerability := Vulnerability{ID: "V-1", Module: "dep", AffectedVersionRange: "=1.0.0", Severity: SeverityCritical}
	findings, err := Resolve(document, vulnerability)
	if err != nil || len(findings) != 1 || findings[0].ArtifactID != "root" {
		t.Fatalf("Resolve() findings=%+v err=%v", findings, err)
	}
	report, err := Analyze([]Artifact{{ID: "app", SBOM: document}, {ID: "unaffected", SBOM: testDocument("other-root", "1.0.0", sbom.Component{Name: "dep", Version: "2.0.0"})}}, vulnerability, DeploymentMap{
		"app":        {"tenant-b", "", "tenant-a", "tenant-a"},
		"unaffected": {"must-not-appear"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 || len(report.AffectedArtifacts) != 1 || report.AffectedArtifacts[0] != "app" {
		t.Fatalf("report affected wrong artifacts: %+v", report)
	}
	if len(report.TenantImpacts) != 1 || strings.Join(report.TenantImpacts[0].TenantIDs, ",") != "tenant-a,tenant-b" || strings.Join(report.AffectedTenants, ",") != "tenant-a,tenant-b" {
		t.Fatalf("report tenant boundary/order incorrect: %+v", report)
	}
	if !report.CustomerNotification || report.Watermark == "" || report.Digest == "" || report.Explain() == "" {
		t.Fatalf("report lacks derived state: %+v", report)
	}
	empty, err := Analyze([]Artifact{{ID: "app", SBOM: testDocument("root", "1.0.0")}}, Vulnerability{Module: "dep", AffectedVersionRange: "=1.0.0", Severity: SeverityLow}, DeploymentMap{"app": {"ignored"}})
	if err != nil || len(empty.Findings) != 0 || empty.CustomerNotification || len(empty.AffectedTenants) != 0 || empty.Watermark != "" {
		t.Fatalf("nonmatching analysis=%+v err=%v", empty, err)
	}
}

func TestCanonicalSBOMDigest_IsStableAndContentSensitive(t *testing.T) {
	document := testDocument("app", "1.0.0")
	first, err := CanonicalSBOMDigest(document)
	if err != nil || first == "" {
		t.Fatalf("CanonicalSBOMDigest()=%q err=%v", first, err)
	}
	second, err := CanonicalSBOMDigest(document)
	if err != nil || first != second {
		t.Fatalf("same document digest changed: %q/%q err=%v", first, second, err)
	}
	different, err := CanonicalSBOMDigest(testDocument("app", "1.0.1"))
	if err != nil || different == first {
		t.Fatalf("changed document digest=%q, want different from %q", different, first)
	}
	if _, err := json.Marshal(document); err != nil {
		t.Fatal(err)
	}
}

func TestVersionRangeParsing_CoversOperatorsWildcardsAndErrors(t *testing.T) {
	cases := []struct {
		raw      string
		matching string
		not      string
	}{
		{raw: "1.2.3", matching: "1.2.3", not: "1.2.4"},
		{raw: "v1.2.x", matching: "1.2.9", not: "1.3.0"},
		{raw: "1.x", matching: "1.9.9", not: "2.0.0"},
		{raw: ">1.2.3", matching: "1.2.4", not: "1.2.3"},
		{raw: ">=1.2.3", matching: "1.2.3", not: "1.2.2"},
		{raw: "<1.2.3", matching: "1.2.2", not: "1.2.3"},
		{raw: "<=1.2.3", matching: "1.2.3", not: "1.2.4"},
		{raw: "~1.2.3", matching: "1.2.9", not: "1.3.0"},
		{raw: "^1.2.3", matching: "1.9.9", not: "2.0.0"},
		{raw: "^0.2.3", matching: "0.2.9", not: "0.3.0"},
		{raw: "^0.0.3", matching: "0.0.3", not: "0.0.4"},
		{raw: "1.0.0 - 2.0.0", matching: "2.0.0", not: "2.0.1"},
		{raw: "1.0.0 || 3.0.0", matching: "3.0.0", not: "2.0.0"},
		{raw: "*", matching: "99.0.0", not: "bad"},
		{raw: "x", matching: "1.0.0", not: "bad"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			rangeSet, err := parseRange(tc.raw)
			if err != nil || !rangeSet.matches(tc.matching) || rangeSet.matches(tc.not) {
				t.Fatalf("range %q parsed=%v err=%v; matching=%v not=%v", tc.raw, rangeSet, err, rangeSet.matches(tc.matching), rangeSet.matches(tc.not))
			}
		})
	}
	invalid := []string{"", "||", ">1.0", "1..x", "x.1.0", "1.2.3.4", "=", "~1.x", "^1.x", "1.2.x.3"}
	for _, raw := range invalid {
		t.Run("invalid_"+raw, func(t *testing.T) {
			if _, err := parseRange(raw); err == nil {
				t.Fatalf("parseRange(%q) succeeded, want error", raw)
			}
		})
	}
	if got := normalizeVersion("V1.2.3"); got != "v1.2.3" {
		t.Fatalf("normalizeVersion uppercase=%q", got)
	}
	if got := normalizeVersion(""); got != "" {
		t.Fatalf("normalizeVersion empty=%q", got)
	}
	if got := nextMinor("v2.0.0"); got != "v2.1.0" {
		t.Fatalf("nextMinor=%q", got)
	}
	if got := nextMinor("v2"); got != "v2.1.0" {
		t.Fatalf("nextMinor abbreviated=%q", got)
	}
	if got := caretUpper("v1.2.3"); got != "v2.0.0" {
		t.Fatalf("caretUpper major=%q", got)
	}
	if got := caretUpper("v0.2.3"); got != "v0.3.0" {
		t.Fatalf("caretUpper minor=%q", got)
	}
	if got := caretUpper("v0.0.3"); got != "v0.0.4" {
		t.Fatalf("caretUpper patch=%q", got)
	}
	if versionPartCount("1.2.3+build") != 3 {
		t.Fatalf("versionPartCount build metadata incorrect")
	}
}

func TestWatermarkAndUniqueSorted_RemoveDuplicatesAndEmptyValues(t *testing.T) {
	if got := strings.Join(uniqueSorted([]string{"b", "", "a", "b"}), ","); got != "a,b" {
		t.Fatalf("uniqueSorted=%q", got)
	}
	findings := []Finding{{SBOMDigest: "z"}, {SBOMDigest: "a"}, {SBOMDigest: "z"}}
	if got := watermark(findings); got != "a,z" {
		t.Fatalf("watermark=%q", got)
	}
	if got := watermark(nil); got != "" {
		t.Fatalf("empty watermark=%q", got)
	}
}
