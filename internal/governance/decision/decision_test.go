package decision_test

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

import "github.com/monstercameron/hcm-next/internal/governance/decision"

func promotionInputs() decision.Inputs {
	return decision.Inputs{
		ProposalRevisionDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000001",
		Context: decision.Context{
			Principal:           "principal:promotion-manager",
			Delegation:          "none",
			Capability:          "promotion.propose/v1",
			Resource:            "worker:22222222-2222-4222-8222-222222222222",
			Fields:              []string{"assignment.grade", "assignment.job_code", "rewards.compensation.annualized_base_pay"},
			CurrentOrganization: "people-ops",
			TargetOrganization:  "people-ops",
			Purpose:             "internal_promotion",
			Risk:                "LOW",
			Authority:           "authority.assignment/v1",
			Legal:               "legal.harborcare-demo/v1",
		},
		ControlSnapshot: decision.ControlSnapshot{
			Digest:          "sha256:eaac7c7d44615f90840dd86ca52c6785bdb6653fcd8c7d4116d9d2101c69247a",
			PolicyBundle:    "policy.promotion/v1",
			LegalContext:    "legal.harborcare-demo/v1",
			ReferenceData:   "reference.pay-band/2026.1",
			SourceWatermark: "rev:v1:people.worker.22222222:s17",
		},
		RulePackVersions: []decision.RulePackVersion{
			{ID: "legal.harborcare-demo", Version: "2026.1", Digest: "sha256:legal"},
			{ID: "rules.promotion-approval", Version: "1", Digest: "sha256:rules"},
		},
		ApprovalRequirements: []decision.ApprovalRequirement{
			{ID: "approval.finance_partner", Version: "1", Satisfaction: decision.ApprovalSatisfied},
		},
		SoDVerdicts: []decision.SoDVerdict{
			{RuleID: "sod.promotion", Version: "1", State: decision.Allow, Satisfied: true},
		},
		Subdecisions: []decision.Subdecision{
			{ID: "authz", Source: "authz", State: decision.Allow, FiredRules: []decision.RuleRef{{ID: "authz.promotion", Version: "1"}}},
			{ID: "legal", Source: "legal", State: decision.Allow, FiredRules: []decision.RuleRef{{ID: "legal.promotion", Version: "2026.1"}}},
			{ID: "risk", Source: "risk", State: decision.Allow, FiredRules: []decision.RuleRef{{ID: "risk.promotion", Version: "1"}}},
		},
	}
}

func TestTodo_GOVERN_001(t *testing.T) {
	got, err := decision.Compose(promotionInputs())
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if got.State != decision.Allow {
		t.Fatalf("state = %s, want %s", got.State, decision.Allow)
	}
	if got.ProposalRevisionDigest == "" || got.ControlSnapshot.Digest == "" || got.Digest == "" {
		t.Fatalf("decision is missing immutable evidence: %+v", got)
	}
	if len(got.RulePackVersions) != 2 || len(got.FiredRules) != 4 {
		t.Fatalf("decision lost pinned inputs: packs=%v rules=%v", got.RulePackVersions, got.FiredRules)
	}
	if got.Explain() == "" {
		t.Fatal("Explain returned an empty explanation")
	}
}

func TestTodo_GOVERN_001_Golden(t *testing.T) {
	got, err := decision.Compose(promotionInputs())
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if got.Digest != got.CanonicalDigest() {
		t.Fatalf("digest = %q, CanonicalDigest = %q", got.Digest, got.CanonicalDigest())
	}
	rendered := renderDecision(got)
	want, err := os.ReadFile("testdata/golden/promotion.decision")
	if err != nil {
		t.Logf("golden candidate:\n%s", rendered)
		t.Fatalf("read promotion decision golden: %v", err)
	}
	if rendered != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("promotion decision golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, rendered)
	}
}

func TestTodo_GOVERN_001_Security(t *testing.T) {
	input := promotionInputs()
	input.ProposalRevisionDigest = ""
	got, err := decision.Compose(input)
	if !errors.Is(err, decision.ErrInvalidInput) {
		t.Fatalf("missing material digest error = %v, want ErrInvalidInput", err)
	}
	if got.State == decision.Allow || got.State == decision.AllowWithObligations {
		t.Fatalf("missing material evidence allowed: %+v", got)
	}

	input = promotionInputs()
	input.Context.Purpose = ""
	got, err = decision.Compose(input)
	if err != nil {
		t.Fatalf("Compose with incomplete context: %v", err)
	}
	if got.State == decision.Allow || got.State == decision.AllowWithObligations {
		t.Fatalf("unknown purpose allowed: %+v", got)
	}
	if got.Explain() == "" || containsAny(got.Explain(), input.Context.Principal, input.Context.Resource) {
		t.Fatalf("Explain disclosed protected context: %q", got.Explain())
	}
}

func TestTodo_GOVERN_001_Mutation(t *testing.T) {
	base, err := decision.Compose(promotionInputs())
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	mutated := promotionInputs()
	mutated.ControlSnapshot.PolicyBundle = "policy.promotion/v2"
	changed, err := decision.Compose(mutated)
	if err != nil {
		t.Fatalf("Compose mutated input: %v", err)
	}
	if changed.Digest == base.Digest {
		t.Fatal("control snapshot mutation did not change digest")
	}
}

func TestTodo_GOVERN_002(t *testing.T) {
	a := promotionInputs()
	b := promotionInputs()
	slices.Reverse(b.RulePackVersions)
	slices.Reverse(b.ApprovalRequirements)
	slices.Reverse(b.Subdecisions)
	for _, sub := range b.Subdecisions {
		slices.Reverse(sub.FiredRules)
	}
	b.Context.Fields = []string{"rewards.compensation.annualized_base_pay", "assignment.job_code", "assignment.grade"}

	left, err := decision.Compose(a)
	if err != nil {
		t.Fatalf("Compose left: %v", err)
	}
	right, err := decision.Compose(b)
	if err != nil {
		t.Fatalf("Compose right: %v", err)
	}
	if left.State != right.State || left.Digest != right.Digest {
		t.Fatalf("order changed composition: left=%+v right=%+v", left, right)
	}

	deny := promotionInputs()
	deny.Subdecisions = append(deny.Subdecisions, decision.Subdecision{
		ID: "deny", Source: "legal", State: decision.Deny,
		FiredRules: []decision.RuleRef{{ID: "legal.mandatory-deny", Version: "2026.1"}},
	})
	denied, err := decision.Compose(deny)
	if err != nil {
		t.Fatalf("Compose deny: %v", err)
	}
	if denied.State != decision.Deny {
		t.Fatalf("deny did not dominate allow: %s", denied.State)
	}

	conflict := promotionInputs()
	conflict.Subdecisions = []decision.Subdecision{
		{ID: "a", Source: "a", State: decision.Allow, FiredRules: []decision.RuleRef{{ID: "rule.a", Version: "1"}}},
		{ID: "d", Source: "d", State: decision.Deny, FiredRules: []decision.RuleRef{{ID: "rule.d", Version: "1"}}},
	}
	conflict.Precedence = decision.PrecedenceTable{{Key: string(decision.Deny), Priority: 20}, {Key: string(decision.Allow), Priority: 10}}
	resolved, err := decision.Compose(conflict)
	if err != nil || resolved.State != decision.Deny {
		t.Fatalf("declared precedence did not resolve conflict: state=%s err=%v", resolved.State, err)
	}

	conflict.Precedence = decision.PrecedenceTable{{Key: string(decision.Allow), Priority: 10}}
	refused, err := decision.Compose(conflict)
	var typed decision.Refusal
	if !errors.As(err, &typed) || !errors.Is(err, decision.ErrUnresolvedConflict) || refused.State != decision.ContradictoryRequirements {
		t.Fatalf("unresolved conflict = state %s err %v refusal %+v", refused.State, err, typed)
	}
}

func TestTodo_GOVERN_002_Race(t *testing.T) {
	input := promotionInputs()
	results := make(chan string, 16)
	for i := 0; i < cap(results); i++ {
		go func() {
			got, err := decision.Compose(input)
			if err != nil {
				results <- err.Error()
				return
			}
			results <- got.Digest
		}()
	}
	want := <-results
	for i := 1; i < cap(results); i++ {
		if got := <-results; got != want {
			t.Fatalf("concurrent composition mismatch: %q vs %q", got, want)
		}
	}
}

func TestTodo_GOVERN_002_Mutation(t *testing.T) {
	input := promotionInputs()
	base, err := decision.Compose(input)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	input.Obligations = []decision.Obligation{{ID: "audit.promotion", Version: "1", Scope: "worker"}}
	changed, err := decision.Compose(input)
	if err != nil {
		t.Fatalf("Compose with obligation: %v", err)
	}
	if changed.State != decision.AllowWithObligations || changed.Digest == base.Digest {
		t.Fatalf("obligation mutation was lost: base=%+v changed=%+v", base, changed)
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && len(value) >= len(needle) {
			for i := 0; i+len(needle) <= len(value); i++ {
				if value[i:i+len(needle)] == needle {
					return true
				}
			}
		}
	}
	return false
}

func renderDecision(d decision.Decision) string {
	var b strings.Builder
	fmt.Fprintf(&b, "state: %s\n", d.State)
	fmt.Fprintf(&b, "proposal revision digest: %s\n", d.ProposalRevisionDigest)
	fmt.Fprintf(&b, "control snapshot digest: %s\n", d.ControlSnapshot.Digest)
	b.WriteString("rule packs:\n")
	for _, p := range d.RulePackVersions {
		fmt.Fprintf(&b, "- %s@%s %s\n", p.ID, p.Version, p.Digest)
	}
	b.WriteString("approvals:\n")
	for _, a := range d.ApprovalRequirements {
		fmt.Fprintf(&b, "- %s@%s %s\n", a.ID, a.Version, a.Satisfaction)
	}
	b.WriteString("sod:\n")
	for _, v := range d.SoDVerdicts {
		fmt.Fprintf(&b, "- %s@%s %s\n", v.RuleID, v.Version, v.State)
	}
	b.WriteString("fired rules:\n")
	for _, r := range d.FiredRules {
		fmt.Fprintf(&b, "- %s@%s\n", r.ID, r.Version)
	}
	fmt.Fprintf(&b, "restrictions: %s\n", strings.Join(d.Restrictions, ","))
	fmt.Fprintf(&b, "obligations: %d\n", len(d.Obligations))
	fmt.Fprintf(&b, "digest: %s\n", d.Digest)
	fmt.Fprintf(&b, "explanation: %s\n", d.Explanation)
	return b.String()
}
