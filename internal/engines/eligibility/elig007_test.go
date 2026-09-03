package eligibility_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/eligibility"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestTodo_ELIG_007(t *testing.T) {
	req := validRequest()
	known, err := values.NewKnownAt(mustInstant(t, 1_500_000))
	if err != nil {
		t.Fatal(err)
	}
	req.KnownAt = known
	plan := mustPlan(t, validCriteria())
	facts := newFakeFacts().with(subject(1), "grade", values.Value("P3"))
	rules := newFakeRules().with(subject(1), "manager-attestation", "1", eligibility.RuleOutcomePass)
	result, err := eligibility.Evaluate(context.Background(), facts, rules, req, plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.PopulationSnapshotRef != req.Snapshots.PopulationSnapshotRef || result.FactSnapshotRef != req.Snapshots.FactSnapshotRef || result.RuleSnapshotRef != req.Snapshots.RuleSnapshotRef || result.KnownAt != known {
		t.Fatalf("result lost pinned provenance: %#v", result)
	}
	if err := eligibility.ValidateBinding(req, result); err != nil {
		t.Fatalf("binding: %v", err)
	}
}

func TestTodo_ELIG_007_Mutation(t *testing.T) {
	req := validRequest()
	plan := mustPlan(t, validCriteria())
	facts := newFakeFacts().with(subject(1), "grade", values.Value("P3"))
	rules := newFakeRules().with(subject(1), "manager-attestation", "1", eligibility.RuleOutcomePass)
	result, err := eligibility.Evaluate(context.Background(), facts, rules, req, plan)
	if err != nil {
		t.Fatal(err)
	}
	changed := req
	changed.Snapshots.PopulationSnapshotRef = "pop-snap-restated"
	if err := eligibility.ValidateBinding(changed, result); !errors.Is(err, eligibility.ErrBindingMismatch) {
		t.Fatalf("err=%v, want binding mismatch", err)
	}
	changed = req
	changed.EffectiveInterval = mustInterval(t, 2_000_000, 3_000_000)
	if err := eligibility.ValidateBinding(changed, result); !errors.Is(err, eligibility.ErrBindingMismatch) {
		t.Fatalf("interval err=%v, want binding mismatch", err)
	}
}

func mustInstant(t *testing.T, seconds int64) values.Instant {
	t.Helper()
	i, err := values.NewInstantFromUnix(seconds, 0)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func mustInterval(t *testing.T, start, end int64) values.EffectiveInterval {
	t.Helper()
	i, err := values.NewInstantInterval(mustInstant(t, start), mustInstant(t, end))
	if err != nil {
		t.Fatal(err)
	}
	return i
}
