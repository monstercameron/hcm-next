package assurance

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func fixture(now time.Time) Assessment {
	return Assessment{ID: "a-1", Kind: KindPenetration, Assessor: "independent-labs", AssessorIndependent: true,
		Scope: []string{"api", "worker-web"}, ExcludedSurfaces: []string{"customer-managed IdP"}, ReleaseVersion: "release-7", TopologyVersion: "topology-3", ThreatVersion: "threat-11", ControlVersion: "controls-9", Environment: "production-like", Method: "authenticated and unauthenticated testing", Date: now.Add(-24 * time.Hour), Expires: now.Add(30 * 24 * time.Hour), ReportRef: "artifact://restricted/a-1",
		Findings: []Finding{{ID: "f-1", Severity: SeverityHigh, Title: "header", Owner: "security", Due: now.Add(-time.Hour), Status: FindingRetested, Retest: &Retest{Assessor: "independent-retest", Independent: true, Date: now.Add(-time.Hour), Result: "pass", EvidenceRef: "artifact://restricted/a-1/retest"}}}}
}

func TestIndependentAssuranceRegisterRejectsUnscopedExpiredUnremediatedOrMisrepresentedEvidence(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		mutate func(*Assessment)
	}{
		{"unscoped", func(a *Assessment) { a.Scope = nil }},
		{"expired", func(a *Assessment) { a.Expires = now.Add(-time.Minute) }},
		{"unremediated critical", func(a *Assessment) { a.Findings[0].Status = FindingOpen; a.Findings[0].Retest = nil }},
		{"misrepresented assessor", func(a *Assessment) { a.AssessorIndependent = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := fixture(now)
			tc.mutate(&a)
			if err := NewRegister().Add(a, now); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestTodo_ASSURANCE_001_Property(t *testing.T) {
	now := time.Now().UTC()
	r := NewRegister()
	a := fixture(now)
	if err := r.Add(a, now); err != nil {
		t.Fatal(err)
	}
	if !r.Gate(now).Allowed {
		t.Fatal("valid evidence must pass")
	}
}

func TestTodo_ASSURANCE_001_Golden(t *testing.T) {
	now := time.Now().UTC()
	r := NewRegister()
	a := fixture(now)
	if err := r.Add(a, now); err != nil {
		t.Fatal(err)
	}
	c, err := r.Claim(a.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.Statement, "not a certification") || !strings.Contains(c.Statement, "customer-managed IdP") {
		t.Fatal("claim must state boundary and exclusions")
	}
}

func TestTodo_ASSURANCE_001_Security(t *testing.T) {
	now := time.Now().UTC()
	a := fixture(now)
	a.Findings[0].Retest.Independent = false
	if err := NewRegister().Add(a, now); err == nil {
		t.Fatal("non-independent retest must fail")
	}
}

func TestTodo_ASSURANCE_001_Conformance(t *testing.T) {
	now := time.Now().UTC()
	a := fixture(now)
	r := NewRegister()
	if err := r.Add(a, now); err != nil {
		t.Fatal(err)
	}
	c, err := r.Claim(a.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Scope) != len(a.Scope) || len(c.ExcludedSurfaces) != len(a.ExcludedSurfaces) {
		t.Fatal("claim scope boundary mismatch")
	}
}

func TestTodo_ASSURANCE_001_Mutation(t *testing.T) {
	now := time.Now().UTC()
	r := NewRegister()
	a := fixture(now)
	if err := r.Add(a, now); err != nil {
		t.Fatal(err)
	}
	a.Scope[0] = "mutated"
	got, err := r.Claim("a-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope[0] == "mutated" {
		t.Fatal("register must retain immutable snapshot")
	}
}

func TestAssessmentValidateRejectsMissingMetadataAndFindingIntegrity(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		mutate func(*Assessment)
	}{
		{"missing id", func(a *Assessment) { a.ID = "" }},
		{"missing assessor", func(a *Assessment) { a.Assessor = " " }},
		{"missing release", func(a *Assessment) { a.ReleaseVersion = "" }},
		{"missing topology", func(a *Assessment) { a.TopologyVersion = "" }},
		{"missing threat", func(a *Assessment) { a.ThreatVersion = "" }},
		{"missing controls", func(a *Assessment) { a.ControlVersion = "" }},
		{"missing environment", func(a *Assessment) { a.Environment = "" }},
		{"missing method", func(a *Assessment) { a.Method = "" }},
		{"missing independence", func(a *Assessment) { a.AssessorIndependent = false }},
		{"bad dates", func(a *Assessment) { a.Expires = a.Date }},
		{"missing date", func(a *Assessment) { a.Date = time.Time{} }},
		{"finding identity", func(a *Assessment) { a.Findings[0].Owner = "" }},
		{"duplicate finding", func(a *Assessment) { a.Findings = append(a.Findings, a.Findings[0]) }},
		{"failed retest", func(a *Assessment) { a.Findings[0].Retest.Result = "fail" }},
		{"missing retest date", func(a *Assessment) { a.Findings[0].Retest.Date = time.Time{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := fixture(now)
			tc.mutate(&a)
			if !errors.Is(a.Validate(now), ErrInvalidAssessment) {
				t.Fatalf("Validate(%s)=%v", tc.name, a.Validate(now))
			}
		})
	}
}

func TestRegisterGateClaimAndAddErrorPaths(t *testing.T) {
	if got := (*Register)(nil).Gate(time.Now()); got.Allowed || len(got.Reasons) != 1 {
		t.Fatalf("nil gate=%+v", got)
	}
	if _, err := (*Register)(nil).Claim("missing", time.Now()); !errors.Is(err, ErrInvalidAssessment) {
		t.Fatalf("nil claim=%v", err)
	}
	r := NewRegister()
	a := fixture(time.Now().UTC())
	if err := r.Add(a, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(a, time.Now().UTC()); !errors.Is(err, ErrInvalidAssessment) {
		t.Fatalf("duplicate add=%v", err)
	}
	if _, err := r.Claim("missing", time.Now().UTC()); !errors.Is(err, ErrInvalidAssessment) {
		t.Fatalf("unknown claim=%v", err)
	}
	decision := r.Gate(time.Now().UTC().Add(31 * 24 * time.Hour))
	if decision.Allowed || len(decision.AssessmentIDs) != 1 || len(decision.Reasons) == 0 {
		t.Fatalf("expired gate=%+v", decision)
	}
}

func TestAssessmentValidateAllowsNonCriticalFindingStatuses(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	for _, status := range []FindingStatus{FindingOpen, FindingRemediated, FindingAcceptedRisk} {
		a := fixture(now)
		a.Findings[0].Severity = SeverityLow
		a.Findings[0].Status = status
		a.Findings[0].Retest = nil
		if err := a.Validate(now); err != nil {
			t.Fatalf("status %s rejected: %v", status, err)
		}
	}
}
