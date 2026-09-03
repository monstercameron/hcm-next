package leave

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/governance/legal"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TestTodo_LEAVE_002_Conformance is the registry's exact conformance symbol.
func TestTodo_LEAVE_002_Conformance(t *testing.T) {
	p, err := NewProgramRevision(testProgram(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != StatutoryProgram || !p.Protected || !p.Paid || p.PayTreatment == "" {
		t.Fatalf("program semantics not retained: %+v", p)
	}
	if p.Authority.Kind == evidence.AuthorityUnspecified || p.Jurisdiction.Country == "" || len(p.Scope) == 0 || len(p.EligibilityRuleRefs) == 0 {
		t.Fatalf("source and eligibility metadata incomplete: %+v", p)
	}
	if len(p.EvidenceRequirements) == 0 || len(p.NoticeRequirements) == 0 || p.Interaction.ConcurrencyGroup == "" || p.BalanceSource == "" || len(p.ReturnObligations) == 0 {
		t.Fatalf("interaction/obligation metadata incomplete: %+v", p)
	}
	if p.UnknownPolicy != UnknownReview || p.Digest == "" || !p.Verify() {
		t.Fatalf("unknown policy or digest not bound: %+v", p)
	}
}

// TestTodo_LEAVE_002_Fault is the registry's exact fault symbol.
func TestTodo_LEAVE_002_Fault(t *testing.T) {
	cases := []struct {
		name string
		edit func(*LeaveProgramRevision)
	}{
		{"missing authority", func(p *LeaveProgramRevision) { p.Authority = evidence.SourceAuthority{} }},
		{"missing jurisdiction", func(p *LeaveProgramRevision) { p.Jurisdiction = legal.Jurisdiction{} }},
		{"missing effective interval", func(p *LeaveProgramRevision) { p.Effective = values.EffectiveInterval{} }},
		{"paid without treatment", func(p *LeaveProgramRevision) { p.PayTreatment = "" }},
		{"missing evidence", func(p *LeaveProgramRevision) { p.EvidenceRequirements = nil }},
		{"missing notices", func(p *LeaveProgramRevision) { p.NoticeRequirements = nil }},
		{"missing interaction group", func(p *LeaveProgramRevision) { p.Interaction.ConcurrencyGroup = "" }},
		{"missing balance source", func(p *LeaveProgramRevision) { p.BalanceSource = "" }},
		{"missing return obligation", func(p *LeaveProgramRevision) { p.ReturnObligations = nil }},
		{"unknown policy", func(p *LeaveProgramRevision) { p.UnknownPolicy = "UNSAFE" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := testProgram(t, 1)
			tc.edit(&p)
			if _, err := NewProgramRevision(p); !errors.Is(err, ErrInvalidProgram) {
				t.Fatalf("error = %v, want ErrInvalidProgram", err)
			}
		})
	}
}

// TestTodo_LEAVE_002_Mutation is the registry's exact mutation symbol.
func TestTodo_LEAVE_002_Mutation(t *testing.T) {
	base, err := NewProgramRevision(testProgram(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name string
		edit func(*LeaveProgramRevision)
	}{
		{"name", func(p *LeaveProgramRevision) { p.Name = "Other leave" }},
		{"kind", func(p *LeaveProgramRevision) { p.Kind = CompanyProgram }},
		{"scope", func(p *LeaveProgramRevision) { p.Scope[0] = "employment" }},
		{"eligibility", func(p *LeaveProgramRevision) { p.EligibilityRuleRefs[0] = "eligibility:other" }},
		{"protected", func(p *LeaveProgramRevision) { p.Protected = false }},
		{"paid", func(p *LeaveProgramRevision) { p.Paid = false; p.PayTreatment = "" }},
		{"interaction", func(p *LeaveProgramRevision) { p.Interaction.Priority++ }},
		{"balance", func(p *LeaveProgramRevision) { p.BalanceSource = "balance:other" }},
		{"unknown policy", func(p *LeaveProgramRevision) { p.UnknownPolicy = UnknownBlocks }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			candidate := base.copy()
			tc.edit(&candidate)
			minted, err := NewProgramRevision(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if minted.Digest == base.Digest {
				t.Fatal("material revision mutation retained the original digest")
			}
		})
	}
}

// TestTodo_LEAVE_002_Property is the registry's exact property symbol.
func TestTodo_LEAVE_002_Property(t *testing.T) {
	a, err := NewProgramRevision(testProgram(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewProgramRevision(testProgram(t, 1))
	if err != nil || a.Digest != b.Digest || string(a.Canonical()) != string(b.Canonical()) {
		t.Fatalf("equivalent revisions are not deterministic: %v", err)
	}
	a.Scope[0] = "caller mutation"
	if b.Scope[0] == "caller mutation" {
		t.Fatal("minted revisions share mutable slices")
	}
}

// TestTodo_LEAVE_002_Security is the registry's exact security symbol.
func TestTodo_LEAVE_002_Security(t *testing.T) {
	p := testProgram(t, 1)
	p.Digest = "caller-supplied-digest"
	minted, err := NewProgramRevision(p)
	if err != nil {
		t.Fatal(err)
	}
	if minted.Digest == p.Digest || !minted.Verify() {
		t.Fatal("caller-supplied digest was trusted")
	}
	p.Authority = evidence.SourceAuthority{Kind: evidence.AuthorityUnspecified}
	if _, err := NewProgramRevision(p); !errors.Is(err, ErrInvalidProgram) {
		t.Fatalf("unknown source authority error = %v", err)
	}
}
