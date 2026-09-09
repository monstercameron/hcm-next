package eligibility_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/eligibility"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
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

// TestTodo_ELIG_007_Golden pins the provenance carried by a result.  A
// result is useful for replay only when it names the exact population, fact,
// and rule snapshots, effective interval, and knowledge time used to derive
// it; this fixture is the stable wire-level example for that contract.
func TestTodo_ELIG_007_Golden(t *testing.T) {
	req := validRequest()
	known, err := values.NewKnownAt(mustInstant(t, 1_500_000))
	if err != nil {
		t.Fatal(err)
	}
	req.KnownAt = known
	plan := mustPlan(t, validCriteria())
	s1 := subject(1)
	facts := newFakeFacts().with(s1, "grade", values.Value("P3"))
	rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)
	result, err := eligibility.Evaluate(context.Background(), facts, rules, req, plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.PopulationSnapshotRef != "pop-snap-1" || result.FactSnapshotRef != "fact-snap-1" || result.RuleSnapshotRef != "rule-snap-1" {
		t.Fatalf("snapshot provenance = (%q, %q, %q), want pinned fixture refs", result.PopulationSnapshotRef, result.FactSnapshotRef, result.RuleSnapshotRef)
	}
	if result.EffectiveInterval != req.EffectiveInterval || result.KnownAt != known {
		t.Fatalf("time provenance lost: effective=%v known_at=%v", result.EffectiveInterval, result.KnownAt)
	}
}

// TestTodo_ELIG_007_Property proves that a result remains bound to one
// immutable context: equivalent requests validate, while each independent
// population/fact/rule/time mutation is rejected as a restatement.
func TestTodo_ELIG_007_Property(t *testing.T) {
	req := validRequest()
	known, err := values.NewKnownAt(mustInstant(t, 1_500_000))
	if err != nil {
		t.Fatal(err)
	}
	req.KnownAt = known
	plan := mustPlan(t, validCriteria())
	s1 := subject(1)
	facts := newFakeFacts().with(s1, "grade", values.Value("P3"))
	rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)
	result, err := eligibility.Evaluate(context.Background(), facts, rules, req, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := eligibility.ValidateBinding(req, result); err != nil {
		t.Fatalf("baseline binding: %v", err)
	}
	mutations := []func(*eligibility.Request){
		func(r *eligibility.Request) { r.Snapshots.FactSnapshotRef = "fact-snap-restated" },
		func(r *eligibility.Request) { r.Snapshots.RuleSnapshotRef = "rule-snap-restated" },
		func(r *eligibility.Request) { r.EffectiveInterval = mustInterval(t, 2_000_000, 3_000_000) },
		func(r *eligibility.Request) { r.KnownAt, _ = values.NewKnownAt(mustInstant(t, 1_600_000)) },
	}
	for i, mutate := range mutations {
		changed := req
		mutate(&changed)
		if err := eligibility.ValidateBinding(changed, result); !errors.Is(err, eligibility.ErrBindingMismatch) {
			t.Fatalf("mutation %d error=%v, want binding mismatch", i, err)
		}
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
