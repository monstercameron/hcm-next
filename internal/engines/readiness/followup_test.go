package readiness_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/readiness"
)

func followupFixture(t *testing.T) (readiness.ReadinessRequirement, readiness.Evaluation, readiness.FollowUpPolicy) {
	t.Helper()
	r := requirement(t, readiness.EvidenceAuthorization)
	evidence := descriptor(t, readiness.EvidenceSatisfied)
	evidence.ObservedAt = instant(t, 1)
	evidence.FreshUntil = instant(t, 1)
	resolution := resolveForEvaluation(t, r, evidence)
	evaluation, err := readiness.Evaluate(r, resolution)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := readiness.NewFollowUpPolicy(readiness.FollowUpPolicy{Domain: r.Origin.Domain, Version: 7, Owner: "workforce-readiness", Scope: "worker:return-to-work", Risk: "HIGH", Kinds: []readiness.FollowUpKind{readiness.FollowUpTask, readiness.FollowUpObligation}, RequiredGates: []readiness.FollowUpGate{readiness.GateHumanReview, readiness.GateGovernanceApproval}})
	if err != nil {
		t.Fatal(err)
	}
	return r, evaluation, policy
}

func TestTodo_READINESS_004(t *testing.T) {
	r, evaluation, policy := followupFixture(t)
	got, err := readiness.CompileFollowUps(r, evaluation, policy)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != readiness.StatusNotReady || len(got.Drafts) != 4 {
		t.Fatalf("compilation = %+v, want bounded drafts for each safe blocker", got)
	}
	for _, draft := range got.Drafts {
		if err := draft.Validate(); err != nil {
			t.Fatal(err)
		}
		if draft.RequirementDigest != r.CanonicalDigest || draft.EvaluationDigest != evaluation.Digest || draft.EvidenceDigest != evaluation.ResolutionDigest || draft.Owner != policy.Owner || draft.Scope != policy.Scope || draft.Risk != policy.Risk {
			t.Fatalf("draft pins/config = %+v", draft)
		}
		if len(draft.RequiredGates) != 2 {
			t.Fatalf("draft gates = %v", draft.RequiredGates)
		}
	}
	readyEvidence := descriptor(t, readiness.EvidenceSatisfied)
	ready, err := readiness.Evaluate(r, resolveForEvaluation(t, r, readyEvidence))
	if err != nil {
		t.Fatal(err)
	}
	readyCompilation, err := readiness.CompileFollowUps(r, ready, policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(readyCompilation.Drafts) != 0 {
		t.Fatalf("READY unexpectedly emitted drafts: %+v", readyCompilation.Drafts)
	}
	conditionalEvidence := descriptor(t, readiness.EvidenceConditional)
	conditional, err := readiness.Evaluate(r, resolveForEvaluation(t, r, conditionalEvidence))
	if err != nil {
		t.Fatal(err)
	}
	conditionalCompilation, err := readiness.CompileFollowUps(r, conditional, policy)
	if err != nil {
		t.Fatal(err)
	}
	if conditionalCompilation.Status != readiness.StatusConditional || len(conditionalCompilation.Drafts) != len(policy.Kinds) {
		t.Fatalf("conditional compilation = %+v", conditionalCompilation)
	}
}

func TestTodo_READINESS_004_Golden(t *testing.T) {
	r, evaluation, policy := followupFixture(t)
	one, err := readiness.CompileFollowUps(r, evaluation, policy)
	if err != nil {
		t.Fatal(err)
	}
	const wantDigest = "sha256:673225c23d429c2de53d30c748de87a5e61d54fc44c1f4c4a4d70d675e464952"
	wantIDs := []string{
		"sha256:11cd2bfc8deaafe37fca174f912d0d691b7d72996ef6bd6ded226ae9807e282a",
		"sha256:8d65928ea53ab64a114f405bd7bcd7fc3ed6ed5280fbc8a9b5742692a9ead709",
		"sha256:ca608148957c48130c34315daa2ee4c9ff8b4f00f2ec2bb4e35d4ca39ac5c0ff",
		"sha256:cbebcdac506be44f69118ce327d07e2f78f277a78c308c54ec3ec54f4e0f4214",
	}
	if one.Digest != wantDigest || len(one.Drafts) != len(wantIDs) {
		t.Fatalf("golden compilation = %q ids=%v", one.Digest, draftIDs(one.Drafts))
	}
	for i := range wantIDs {
		if one.Drafts[i].SemanticID != wantIDs[i] {
			t.Fatalf("golden compilation = %q ids=%v", one.Digest, draftIDs(one.Drafts))
		}
	}
	if err := one.Validate(); err != nil {
		t.Fatal(err)
	}
	reordered, err := readiness.NewFollowUpPolicy(readiness.FollowUpPolicy{Domain: policy.Domain, Version: policy.Version, Owner: policy.Owner, Scope: policy.Scope, Risk: policy.Risk, Kinds: []readiness.FollowUpKind{readiness.FollowUpObligation, readiness.FollowUpTask}, RequiredGates: []readiness.FollowUpGate{readiness.GateGovernanceApproval, readiness.GateHumanReview}})
	if err != nil {
		t.Fatal(err)
	}
	two, err := readiness.CompileFollowUps(r, evaluation, reordered)
	if err != nil {
		t.Fatal(err)
	}
	if two.Digest != one.Digest {
		t.Fatalf("input order changed compilation: %q != %q", two.Digest, one.Digest)
	}
}

func draftIDs(drafts []readiness.DraftFollowUp) []string {
	ids := make([]string, len(drafts))
	for i := range drafts {
		ids[i] = drafts[i].SemanticID
	}
	return ids
}

func TestTodo_READINESS_004_Race(t *testing.T) {
	r, evaluation, policy := followupFixture(t)
	expected, err := readiness.CompileFollowUps(r, evaluation, policy)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 12
	results := make(chan string, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			got, err := readiness.CompileFollowUps(r, evaluation, policy)
			if err != nil {
				results <- err.Error()
				return
			}
			results <- got.Digest
		}()
	}
	wg.Wait()
	close(results)
	for got := range results {
		if got != expected.Digest {
			t.Fatalf("race compilation failed or diverged: %q != %q", got, expected.Digest)
		}
	}
}

func TestTodo_READINESS_004_Mutation(t *testing.T) {
	r, evaluation, policy := followupFixture(t)
	evaluation.RequirementDigest = "tampered"
	if _, err := readiness.CompileFollowUps(r, evaluation, policy); err == nil {
		t.Fatal("tampered evaluation must be rejected")
	}
	_, evaluation, policy = followupFixture(t)
	policy.Scope = "other-domain"
	if _, err := readiness.CompileFollowUps(r, evaluation, policy); !errors.Is(err, readiness.ErrInvalidFollowUp) {
		t.Fatalf("mutated policy error = %v", err)
	}
	_, evaluation, policy = followupFixture(t)
	policy.Domain = readiness.DomainPayrollRelease
	policy, err := readiness.NewFollowUpPolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readiness.CompileFollowUps(r, evaluation, policy); !errors.Is(err, readiness.ErrInvalidFollowUp) {
		t.Fatalf("cross-domain policy error = %v", err)
	}
	_, evaluation, policy = followupFixture(t)
	compiled, err := readiness.CompileFollowUps(r, evaluation, policy)
	if err != nil {
		t.Fatal(err)
	}
	compiled.Status = readiness.StatusReady
	if err := compiled.Validate(); !errors.Is(err, readiness.ErrInvalidFollowUp) {
		t.Fatalf("mutated compilation error = %v", err)
	}
	_, evaluation, policy = followupFixture(t)
	compiled, err = readiness.CompileFollowUps(r, evaluation, policy)
	if err != nil {
		t.Fatal(err)
	}
	compiled.Drafts[0].RequiredGates[0] = readiness.GateHumanReview
	if err := compiled.Validate(); !errors.Is(err, readiness.ErrInvalidFollowUp) {
		t.Fatalf("mutated draft gates error = %v", err)
	}
	_, evaluation, policy = followupFixture(t)
	policy.RequiredGates = policy.RequiredGates[:1]
	if _, err := readiness.CompileFollowUps(r, evaluation, policy); !errors.Is(err, readiness.ErrInvalidFollowUp) {
		t.Fatalf("missing governance gate error = %v", err)
	}
}
