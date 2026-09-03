package effectgraph

import "testing"

func node(id string) EffectNode {
	return EffectNode{ID: id, ProposalRef: "proposal/1", TransactionRef: "tx/1", CapabilityRef: "payroll.write/v1", ResourceKey: "worker/1", Ordering: Strict, IdempotencyKey: "effect/" + id, DispatchCondition: "approved", Deadline: "2026-12-01T00:00:00Z", FailurePolicy: "RETRY_THEN_QUARANTINE", RepairPolicy: "repair/" + id, Observation: ObservationContract{Required: true, Profile: "provider-state/v1"}}
}

func TestCompileCanonicalOrderAndDigest(t *testing.T) {
	a, b := node("a"), node("b")
	b.Prerequisites = []string{"a"}
	c, err := Compile(Graph{Nodes: []EffectNode{b, a}})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Order) != 2 || c.Order[0] != "a" || c.Order[1] != "b" {
		t.Fatalf("order=%v", c.Order)
	}
	if c.Digest == "" {
		t.Fatal("empty digest")
	}
	if err := c.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestCompileRejectsSafetyViolations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*EffectNode)
		want   string
	}{
		{"missing dependency", func(n *EffectNode) { n.Prerequisites = []string{"nope"} }, "UNDECLARED_DEPENDENCY"},
		{"irreversible without observation", func(n *EffectNode) { n.Irreversible = true; n.Observation = ObservationContract{} }, "IRREVERSIBLE_NEEDS_OBSERVATION"},
		{"missing idempotency", func(n *EffectNode) { n.IdempotencyKey = "" }, "MISSING_IDEMPOTENCY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := node("a")
			tt.mutate(&n)
			_, err := Compile(Graph{Nodes: []EffectNode{n}})
			if err == nil || err.Error() == "" {
				t.Fatal("expected rejection")
			}
			if want := tt.want; !contains(err.Error(), want) {
				t.Fatalf("error=%v want %s", err, want)
			}
		})
	}
}
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestCompileRejectsCycleAndOrderingConflict(t *testing.T) {
	a, b := node("a"), node("b")
	a.Prerequisites = []string{"b"}
	b.Prerequisites = []string{"a"}
	if _, err := Compile(Graph{Nodes: []EffectNode{a, b}}); err == nil {
		t.Fatal("cycle accepted")
	}
	a, b = node("a"), node("b")
	b.Ordering = Independent
	if _, err := Compile(Graph{Nodes: []EffectNode{a, b}}); err == nil {
		t.Fatal("ordering conflict accepted")
	}
}
