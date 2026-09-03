package assurance

import (
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
