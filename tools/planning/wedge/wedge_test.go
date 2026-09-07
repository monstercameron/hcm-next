package wedge

import (
	"strings"
	"testing"
)

func hasViolation(violations []Violation, field, text string) bool {
	for _, v := range violations {
		if v.Field == field && strings.Contains(v.Issue, text) {
			return true
		}
	}
	return false
}

// TestPartnerManifestRejectsUnboundedWedge is the WEDGE-001 primary test.
func TestPartnerManifestRejectsUnboundedWedge(t *testing.T) {
	m := PlaceholderPartnerManifest()
	m.Incumbent.Edition = ""
	m.Fields = nil
	m.Downstream.Independent = false
	m.Price.ReadOnly = false
	m.StopCriteria = nil
	violations := ValidatePartnerManifest(m)
	for _, want := range []struct{ field, issue string }{{"incumbent.edition", "exact incumbent"}, {"fields", "exact pilot"}, {"downstream.independent", "independently"}, {"price.read_only", "read-only"}, {"stop_criteria", "stop"}} {
		if !hasViolation(violations, want.field, want.issue) {
			t.Errorf("missing %s violation in %v", want.field, violations)
		}
	}
	good := PlaceholderPartnerManifest()
	evaluation := EvaluatePartnerManifest(good)
	if evaluation.Status != Qualified || len(evaluation.Violations) != 0 {
		t.Fatalf("placeholder manifest evaluation = %+v, want QUALIFIED with no structural violations", evaluation)
	}
	if len(evaluation.HumanInputsRequired) == 0 {
		t.Fatal("placeholder values must remain visible as human inputs")
	}
	if err := good.VerifyDigest(); err != nil {
		t.Fatalf("placeholder digest: %v", err)
	}
}

func TestTodo_WEDGE_001_Golden(t *testing.T) {
	m := PlaceholderPartnerManifest()
	if got := DigestPartnerManifest(m); got != m.Digest {
		t.Fatalf("digest = %q, manifest digest = %q", got, m.Digest)
	}
	if got := (Violation{Record: "PartnerManifest", Field: "fields", Issue: "exact pilot field set is required"}).Error(); got != "PartnerManifest: fields: exact pilot field set is required" {
		t.Fatalf("violation rendering changed: %q", got)
	}
}

// TestBaselineRejectsMissingDenominator is the WEDGE-002 primary test.
func TestBaselineRejectsMissingDenominator(t *testing.T) {
	b := PlaceholderBaseline()
	b.Metrics[0].Denominator = ""
	b.Metrics[0].ExclusionsDeclared = false
	violations := ValidateBaseline(b)
	if !hasViolation(violations, "metrics[0].denominator", "required") {
		t.Fatalf("missing denominator violation: %v", violations)
	}
	if !hasViolation(violations, "metrics[0].exclusions_declared", "explicitly") {
		t.Fatalf("missing exclusions declaration violation: %v", violations)
	}

	b = PlaceholderBaseline()
	compiled, violations := CompileBaseline(b, []MetricObservation{{MetricName: "change_completion_time", Numerator: 3, Denominator: 4, Population: "PLACEHOLDER_POPULATION", WindowStart: "2026-08-01", WindowEnd: "2026-08-31", SourceRef: "fixture:placeholder-source"}})
	if len(violations) != 0 {
		t.Fatalf("CompileBaseline violations: %v", violations)
	}
	if len(compiled.Metrics) != 1 || compiled.Metrics[0].RateBP != 7500 {
		t.Fatalf("compiled baseline = %+v, want 7500 basis points", compiled)
	}
	compiledAgain, _ := CompileBaseline(b, []MetricObservation{{MetricName: "change_completion_time", Numerator: 3, Denominator: 4, Population: "PLACEHOLDER_POPULATION", WindowStart: "2026-08-01", WindowEnd: "2026-08-31", SourceRef: "fixture:placeholder-source"}})
	if compiledAgain.Metrics[0] != compiled.Metrics[0] {
		t.Fatalf("baseline compilation is not reproducible: %+v vs %+v", compiled, compiledAgain)
	}
}

func TestTodo_WEDGE_002_Golden(t *testing.T) {
	b := PlaceholderBaseline()
	if got := DigestBaseline(b); got != b.Digest {
		t.Fatalf("digest = %q, baseline digest = %q", got, b.Digest)
	}
	if got := b.Metrics[0].Unit; got != "minutes" {
		t.Fatalf("metric unit = %q, want minutes", got)
	}
}

// TestNativeCapabilityAssessmentRejectsStaleOrUnlicensedClaims is the
// WEDGE-003 primary test and the immutable evidence-schema oracle.
func TestNativeCapabilityAssessmentRejectsStaleOrUnlicensedClaims(t *testing.T) {
	a := PlaceholderNativeCapabilityAssessment()
	a.Claims[0].EvidenceDate = "2026-01-01"
	a.Claims[0].LicenseState = "NOT_LICENSED"
	a.Claims[0].Configured = false
	violations := ValidateNativeCapabilityAssessment(a)
	if !hasViolation(violations, "claims[0].evidence_date", "stale") {
		t.Fatalf("missing stale violation: %v", violations)
	}
	if !hasViolation(violations, "claims[0].license_state", "licensed") {
		t.Fatalf("missing license violation: %v", violations)
	}
	if !hasViolation(violations, "claims[0].configured", "configured") {
		t.Fatalf("missing configuration violation: %v", violations)
	}

	good := PlaceholderNativeCapabilityAssessment()
	if got := ValidateNativeCapabilityAssessment(good); len(got) != 0 {
		t.Fatalf("valid assessment rejected: %v", got)
	}
	if len(good.Gaps) != 1 || good.Gaps[0] == "" {
		t.Fatalf("assessment gaps = %#v, want an explicit gap record", good.Gaps)
	}
	if err := good.VerifyDigest(); err != nil {
		t.Fatalf("assessment digest: %v", err)
	}

	malformed := PlaceholderNativeCapabilityAssessment()
	malformed.Claims[0].EvidenceDate = "undated"
	if !hasViolation(ValidateNativeCapabilityAssessment(malformed), "claims[0].evidence_date", "YYYY-MM-DD") {
		t.Fatal("malformed evidence date was accepted")
	}
}

// TestPilotFieldManifestRejectsImplicitField is the WEDGE-004 primary test.
func TestPilotFieldManifestRejectsImplicitField(t *testing.T) {
	m := PlaceholderPilotFieldManifest()
	m.Fields[0].Authority = ""
	m.Fields[1].Path = "job.*"
	m.Fields[2].GateAOperation = OperationWrite
	violations := ValidatePilotFieldManifest(m)
	for _, want := range []struct{ field, issue string }{{"fields[0].authority", "metadata"}, {"fields[1].path", "literal"}, {"fields[2].gate_a_operation", "mutation"}} {
		if !hasViolation(violations, want.field, want.issue) {
			t.Errorf("missing %s violation in %v", want.field, violations)
		}
	}
	good := PlaceholderPilotFieldManifest()
	if got := ValidatePilotFieldManifest(good); len(got) != 0 {
		t.Fatalf("valid field manifest rejected: %v", got)
	}
}

func TestTodo_WEDGE_004_Golden(t *testing.T) {
	m := PlaceholderPilotFieldManifest()
	if got := DigestPilotFieldManifest(m); got != m.Digest {
		t.Fatalf("digest = %q, field manifest digest = %q", got, m.Digest)
	}
	domains := map[string]bool{}
	for _, f := range m.Fields {
		domains[f.Domain] = true
	}
	for _, domain := range []string{"job", "manager", "organization", "position", "compensation"} {
		if !domains[domain] {
			t.Errorf("fixture lacks %s field", domain)
		}
	}
}

func TestTodo_WEDGE_004_Security(t *testing.T) {
	m := PlaceholderPilotFieldManifest()
	m.Fields[0].Path = "../../compensation.base"
	violations := ValidatePilotFieldManifest(m)
	if !hasViolation(violations, "fields[0].path", "literal") {
		t.Fatalf("path traversal was not rejected: %v", violations)
	}
	m = PlaceholderPilotFieldManifest()
	m.Fields[0].GateAOperation = OperationWrite
	if !hasViolation(ValidatePilotFieldManifest(m), "fields[0].gate_a_operation", "mutation") {
		t.Fatal("Gate A write authority was not rejected")
	}
}
