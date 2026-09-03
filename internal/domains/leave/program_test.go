package leave

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/governance/legal"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func testProgram(t *testing.T, revision uint64) LeaveProgramRevision {
	t.Helper()
	start := values.NewInstant(time.Unix(1700000000, 0).UTC())
	end := values.NewInstant(time.Unix(1800000000, 0).UTC())
	iv, err := values.NewInstantInterval(start, end)
	if err != nil {
		t.Fatal(err)
	}
	return LeaveProgramRevision{ProgramID: "medical", Revision: revision, Name: "Medical leave", Kind: StatutoryProgram, Sponsor: "policy-source", Authority: evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "policy-registry", PolicyRef: "policy:leave"}, Jurisdiction: legal.Jurisdiction{Country: "US", State: "CA"}, Scope: []string{"worker"}, EligibilityRuleRefs: []string{"eligibility:medical"}, Effective: iv, Protected: true, Paid: true, PayTreatment: "wage-replacement", EvidenceRequirements: []string{"certification"}, NoticeRequirements: []string{"worker-notice"}, Interaction: InteractionMetadata{ConcurrencyGroup: "medical", Priority: 1, Stacking: false, OffsetAllowed: true, MostProtective: true, ConflictPolicy: "MOST_PROTECTIVE"}, BalanceSource: "leave-ledger", ReturnObligations: []string{"readiness-review"}, UnknownPolicy: UnknownReview}
}

func TestTodo_LEAVE_002(t *testing.T) {
	p, err := NewProgramRevision(testProgram(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	if p.Digest == "" || !p.Verify() || p.Canonical() == nil {
		t.Fatal("revision was not immutably digested")
	}
	p.Scope[0] = "tampered"
	if p.Verify() {
		t.Fatal("mutation of a caller-owned slice changed a minted revision")
	}
	cat := NewProgramCatalog()
	if err := cat.Append(testProgram(t, 1)); err != nil {
		t.Fatal(err)
	}
	if err := cat.Append(testProgram(t, 1)); !errors.Is(err, ErrRevisionOrder) {
		t.Fatalf("duplicate revision error = %v", err)
	}
	if err := cat.Append(testProgram(t, 2)); err != nil {
		t.Fatal(err)
	}
	got, ok := cat.Current("medical")
	if !ok || got.Revision != 2 {
		t.Fatalf("current = %#v, %v", got, ok)
	}
	all := cat.Revisions("medical")
	all[0].Scope[0] = "tampered"
	again := cat.Revisions("medical")
	if again[0].Scope[0] == "tampered" {
		t.Fatal("catalog leaked mutable slices")
	}
}

func TestProgramValidationRequiresPolicyInputs(t *testing.T) {
	p := testProgram(t, 1)
	p.BalanceSource = ""
	if _, err := NewProgramRevision(p); !errors.Is(err, ErrInvalidProgram) {
		t.Fatalf("err = %v", err)
	}
}

func TestProgramValidationRejectsBlankAndDuplicatePolicyRefs(t *testing.T) {
	cases := []struct {
		name string
		edit func(*LeaveProgramRevision)
	}{
		{"blank scope", func(p *LeaveProgramRevision) { p.Scope = []string{"worker", " "} }},
		{"duplicate eligibility", func(p *LeaveProgramRevision) {
			p.EligibilityRuleRefs = []string{"eligibility:medical", "eligibility:medical"}
		}},
		{"blank evidence", func(p *LeaveProgramRevision) { p.EvidenceRequirements = []string{""} }},
		{"duplicate return obligation", func(p *LeaveProgramRevision) { p.ReturnObligations = []string{"readiness-review", "readiness-review"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := testProgram(t, 1)
			tc.edit(&p)
			if _, err := NewProgramRevision(p); !errors.Is(err, ErrInvalidProgram) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestProgramCatalogDuplicateRevisionHasTypedErrors(t *testing.T) {
	cat := NewProgramCatalog()
	if err := cat.Append(testProgram(t, 1)); err != nil {
		t.Fatal(err)
	}
	err := cat.Append(testProgram(t, 1))
	if !errors.Is(err, ErrDuplicateRevision) || !errors.Is(err, ErrRevisionOrder) {
		t.Fatalf("duplicate error = %v", err)
	}
}
