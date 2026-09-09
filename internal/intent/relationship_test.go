package intent_test

import (
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// relationship builds a valid CHILD relationship between two intents. Tests
// copy it and break exactly one thing.
func relationship(id, parent, child string, kind intent.RelationKind, ordinal uint32) intent.RelationshipRevision {
	rev := intent.RelationshipRevision{
		RelationshipID: id,
		Revision:       1,
		Tenant:         values.TenantId("acme-eu"),
		Kind:           kind,
		Parent: intent.IntentVersionRef{
			IntentID:        parent,
			InstanceVersion: 1,
		},
		Child: intent.IntentVersionRef{
			IntentID:        child,
			InstanceVersion: 1,
		},
		Ordinal:     ordinal,
		CauseRef:    "cause:workflow_node.emit_child/v1",
		Purpose:     "promotion.annual_cycle",
		Cardinality: intent.CardinalityZeroOrMore,
		Propagation: intent.PropagationBlockClosure,
		Completion:  intent.CompletionMandatory,
		Material:    true,
		Disclosed:   true,
		RecordedAt:  fixedClock()(),
	}
	if kind.BindsProposal() {
		rev.Parent.ProposalRevisionID = "proposal:" + parent + ":1"
		rev.Parent.ProposalRevision = 1
	}
	return rev
}

// TestIntentRelationshipGraphRejectsAmbiguity is the PRIMARY test for
// INTENT-015.
//
// RED: a missing tenant, cause or purpose, a cycle where cycles are
// prohibited, mutable parentage, a duplicate ordinal, a hidden material child
// and a conflation of the nine relation kinds are all rejected.
//
// GREEN: relationship revisions bind exact intent and proposal versions,
// ordering, cardinality, lifecycle propagation and completion policy, and a
// composite can expose bounded child intents without erasing each child's own
// governance.
func TestIntentRelationshipGraphRejectsAmbiguity(t *testing.T) {
	tenant := values.TenantId("acme-eu")

	t.Run("RED: a relationship with no tenant, cause or purpose", func(t *testing.T) {
		cases := []struct {
			name   string
			break_ func(*intent.RelationshipRevision)
		}{
			{"tenant", func(r *intent.RelationshipRevision) { r.Tenant = "" }},
			{"cause", func(r *intent.RelationshipRevision) { r.CauseRef = "" }},
			{"purpose", func(r *intent.RelationshipRevision) { r.Purpose = "" }},
			{"cardinality", func(r *intent.RelationshipRevision) { r.Cardinality = intent.CardinalityUnspecified }},
			{"propagation", func(r *intent.RelationshipRevision) { r.Propagation = intent.PropagationUnspecified }},
			{"completion", func(r *intent.RelationshipRevision) { r.Completion = intent.CompletionUnspecified }},
			{"kind", func(r *intent.RelationshipRevision) { r.Kind = intent.RelationUnspecified }},
			{"parent instance version", func(r *intent.RelationshipRevision) { r.Parent.InstanceVersion = 0 }},
			{"child instance version", func(r *intent.RelationshipRevision) { r.Child.InstanceVersion = 0 }},
			{"parent proposal revision", func(r *intent.RelationshipRevision) { r.Parent.ProposalRevisionID = "" }},
			{"recorded time", func(r *intent.RelationshipRevision) { r.RecordedAt = values.Instant{} }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				rev := relationship("rel:1", "intent:parent", "intent:child", intent.RelationChild, 1)
				tc.break_(&rev)
				if _, err := intent.NewRelationshipGraph(tenant, []intent.RelationshipRevision{rev}); !errors.Is(err, intent.ErrInvalidRelationship) {
					t.Fatalf("a relationship with no %s was accepted: %v", tc.name, err)
				}
			})
		}
	})

	t.Run("RED: a cycle where cycles are prohibited", func(t *testing.T) {
		for _, kind := range intent.RelationKinds() {
			revs := []intent.RelationshipRevision{
				relationship("rel:a", "intent:1", "intent:2", kind, 1),
				relationship("rel:b", "intent:2", "intent:3", kind, 1),
				relationship("rel:c", "intent:3", "intent:1", kind, 1),
			}
			_, err := intent.NewRelationshipGraph(tenant, revs)
			if kind.ProhibitsCycles() {
				if !errors.Is(err, intent.ErrRelationshipCycle) {
					t.Fatalf("a %s cycle was accepted: %v", kind, err)
				}
				continue
			}
			if err != nil {
				t.Fatalf("a %s cycle is legal and was refused: %v", kind, err)
			}
		}
	})

	t.Run("RED: an intent may not relate to itself", func(t *testing.T) {
		rev := relationship("rel:1", "intent:1", "intent:1", intent.RelationChild, 1)
		if _, err := intent.NewRelationshipGraph(tenant, []intent.RelationshipRevision{rev}); !errors.Is(err, intent.ErrRelationshipCycle) {
			t.Fatalf("an intent became its own child: %v", err)
		}
	})

	t.Run("RED: parentage is immutable", func(t *testing.T) {
		base := relationship("rel:1", "intent:1", "intent:2", intent.RelationChild, 1)
		cases := []struct {
			name   string
			break_ func(*intent.RelationshipRevision)
		}{
			{"re-parent", func(r *intent.RelationshipRevision) { r.Parent.IntentID = "intent:9" }},
			{"re-child", func(r *intent.RelationshipRevision) { r.Child.IntentID = "intent:9" }},
			{"re-kind", func(r *intent.RelationshipRevision) { r.Kind = intent.RelationDependency }},
			{"re-order", func(r *intent.RelationshipRevision) { r.Ordinal = 7 }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				second := base
				second.Revision = 2
				tc.break_(&second)
				_, err := intent.NewRelationshipGraph(tenant, []intent.RelationshipRevision{base, second})
				if !errors.Is(err, intent.ErrImmutableRelationship) {
					t.Fatalf("revision 2 changed its %s: %v", tc.name, err)
				}
			})
		}

		t.Run("a revision history must be numbered 1..N", func(t *testing.T) {
			second := base
			second.Revision = 3
			if _, err := intent.NewRelationshipGraph(tenant, []intent.RelationshipRevision{base, second}); !errors.Is(err, intent.ErrImmutableRelationship) {
				t.Fatalf("a gap in the revision history was accepted: %v", err)
			}
		})

		t.Run("policy alone may be refined", func(t *testing.T) {
			second := base
			second.Revision = 2
			second.Propagation = intent.PropagationHold
			second.Completion = intent.CompletionOptional
			g, err := intent.NewRelationshipGraph(tenant, []intent.RelationshipRevision{base, second})
			if err != nil {
				t.Fatalf("a policy refinement was refused: %v", err)
			}
			current, ok := g.Current("rel:1")
			if !ok || current.Revision != 2 || current.Completion != intent.CompletionOptional {
				t.Fatalf("the current revision is not the refined one: %+v", current)
			}
			if len(g.History("rel:1")) != 2 {
				t.Fatalf("the revision history was collapsed: %d", len(g.History("rel:1")))
			}
		})
	})

	t.Run("RED: a duplicate ordinal under one parent and kind", func(t *testing.T) {
		revs := []intent.RelationshipRevision{
			relationship("rel:1", "intent:1", "intent:2", intent.RelationChild, 1),
			relationship("rel:2", "intent:1", "intent:3", intent.RelationChild, 1),
		}
		if _, err := intent.NewRelationshipGraph(tenant, revs); !errors.Is(err, intent.ErrAmbiguousRelationship) {
			t.Fatalf("two children share ordinal 1: %v", err)
		}
	})

	t.Run("RED: a hidden material child", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			break_ func(*intent.RelationshipRevision)
		}{
			{"undisclosed", func(r *intent.RelationshipRevision) { r.Disclosed = false }},
			{"detached from completion", func(r *intent.RelationshipRevision) { r.Completion = intent.CompletionDetached }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				rev := relationship("rel:1", "intent:1", "intent:2", intent.RelationChild, 1)
				tc.break_(&rev)
				if _, err := intent.NewRelationshipGraph(tenant, []intent.RelationshipRevision{rev}); !errors.Is(err, intent.ErrHiddenChildIntent) {
					t.Fatalf("a material child was %s: %v", tc.name, err)
				}
			})
		}
	})

	t.Run("RED: the nine kinds are never conflated", func(t *testing.T) {
		kinds := intent.RelationKinds()
		if len(kinds) != 9 {
			t.Fatalf("relation kinds = %d, want the nine declared kinds", len(kinds))
		}
		seen := map[string]bool{}
		for _, kind := range kinds {
			name := kind.String()
			if seen[name] {
				t.Fatalf("%s is not a distinct name", name)
			}
			seen[name] = true
			parsed, err := intent.ParseRelationKind(name)
			if err != nil || parsed != kind {
				t.Fatalf("ParseRelationKind(%q) = %v, %v", name, parsed, err)
			}
		}
		for _, name := range []string{
			"CHILD", "DEPENDENCY", "FOLLOW_UP", "CORRECTION", "COMPENSATION",
			"REPAIR", "SUPERSEDES", "ALTERNATIVE", "TRIGGERED_BY",
		} {
			if !seen[name] {
				t.Fatalf("%s is not one of the recorded kinds", name)
			}
		}
		if _, err := intent.ParseRelationKind("RELATED"); !errors.Is(err, intent.ErrInvalidRelationship) {
			t.Fatalf("a generic relation kind was accepted: %v", err)
		}

		// One ordered pair carries exactly one kind. A pair recorded as both a
		// correction and a compensation is two different stories about what
		// happened to business truth.
		revs := []intent.RelationshipRevision{
			relationship("rel:1", "intent:1", "intent:2", intent.RelationCorrection, 1),
			relationship("rel:2", "intent:1", "intent:2", intent.RelationCompensation, 1),
		}
		if _, err := intent.NewRelationshipGraph(tenant, revs); !errors.Is(err, intent.ErrAmbiguousRelationship) {
			t.Fatalf("one pair carried two kinds: %v", err)
		}
	})

	t.Run("RED: a bounded cardinality is enforced", func(t *testing.T) {
		first := relationship("rel:1", "intent:1", "intent:2", intent.RelationSupersedes, 1)
		first.Cardinality = intent.CardinalityAtMostOne
		second := relationship("rel:2", "intent:1", "intent:3", intent.RelationSupersedes, 2)
		second.Cardinality = intent.CardinalityAtMostOne
		if _, err := intent.NewRelationshipGraph(tenant, []intent.RelationshipRevision{first, second}); !errors.Is(err, intent.ErrAmbiguousRelationship) {
			t.Fatalf("an intent was superseded twice under AT_MOST_ONE: %v", err)
		}
	})

	t.Run("GREEN: a composite exposes bounded children with their own governance", func(t *testing.T) {
		// A Hire composite: one child per bounded piece of work, each pinned to
		// the exact parent proposal revision it was emitted from, each with its
		// own ordinal, and one detached follow-up that the parent never waits
		// on.
		revs := []intent.RelationshipRevision{
			relationship("rel:hire:1", "intent:hire", "intent:create_person", intent.RelationChild, 1),
			relationship("rel:hire:2", "intent:hire", "intent:create_employment", intent.RelationChild, 2),
			relationship("rel:hire:3", "intent:hire", "intent:reserve_budget", intent.RelationDependency, 1),
		}
		followUp := relationship("rel:hire:4", "intent:hire", "intent:send_welcome", intent.RelationFollowUp, 1)
		followUp.Material = false
		followUp.Completion = intent.CompletionDetached
		followUp.Propagation = intent.PropagationNone
		revs = append(revs, followUp)

		g, err := intent.NewRelationshipGraph(tenant, revs)
		if err != nil {
			t.Fatalf("a valid composite graph was refused: %v", err)
		}
		if g.Len() != 4 {
			t.Fatalf("graph holds %d relationships, want 4", g.Len())
		}
		children := g.Related("intent:hire", intent.RelationChild)
		if len(children) != 2 {
			t.Fatalf("the composite exposes %d children, want 2", len(children))
		}
		for _, child := range children {
			if child.Parent.ProposalRevisionID == "" {
				t.Fatalf("child %s does not pin the parent proposal revision", child.RelationshipID)
			}
			if !child.Disclosed {
				t.Fatalf("child %s is not disclosed", child.RelationshipID)
			}
		}
		blocking := g.BlocksCompletionOf("intent:hire")
		if len(blocking) != 3 {
			t.Fatalf("%d relationships block completion, want the two children and the dependency", len(blocking))
		}
		for _, rev := range blocking {
			if rev.Kind == intent.RelationFollowUp {
				t.Fatal("a detached follow-up blocks the parent's completion")
			}
		}
		if all := g.Related("intent:hire", intent.RelationUnspecified); len(all) != 4 {
			t.Fatalf("Related with no kind returned %d, want every relationship", len(all))
		}
	})

	t.Run("GREEN: a relationship graph is chronology, not containment", func(t *testing.T) {
		// The refactor rule: a relationship never expresses workflow-node
		// containment or foreign-key cascade ownership. Neither the revision
		// nor its propagation vocabulary may name one.
		typ := reflect.TypeOf(intent.RelationshipRevision{})
		for i := range typ.NumField() {
			name := strings.ToLower(typ.Field(i).Name)
			for _, bad := range []string{"node", "cascade", "ownedby", "owner", "foreignkey", "delete"} {
				if strings.Contains(name, bad) {
					t.Fatalf("RelationshipRevision.%s expresses containment or ownership",
						typ.Field(i).Name)
				}
			}
		}
		for _, p := range []intent.Propagation{
			intent.PropagationNone, intent.PropagationRequestCancellation,
			intent.PropagationHold, intent.PropagationBlockClosure,
		} {
			if strings.Contains(p.String(), "DELETE") {
				t.Fatalf("propagation %s deletes", p)
			}
		}
	})
}

// TestTodo_INTENT_015_Property generates relationship graphs and checks the two
// properties the compiler must always hold: an accepted graph has no cycle in
// any cycle-prohibiting kind, and a refused one is refused for a typed reason
// rather than a panic or a bare error.
func TestTodo_INTENT_015_Property(t *testing.T) {
	tenant := values.TenantId("acme-eu")
	typed := []error{
		intent.ErrInvalidRelationship,
		intent.ErrRelationshipCycle,
		intent.ErrImmutableRelationship,
		intent.ErrAmbiguousRelationship,
		intent.ErrHiddenChildIntent,
	}
	kinds := intent.RelationKinds()

	rng := rand.New(rand.NewSource(0x15))
	for round := range 400 {
		nodes := 2 + rng.Intn(5)
		edges := 1 + rng.Intn(6)
		revs := make([]intent.RelationshipRevision, 0, edges)
		for e := range edges {
			from := fmt.Sprintf("intent:%d", rng.Intn(nodes))
			to := fmt.Sprintf("intent:%d", rng.Intn(nodes))
			kind := kinds[rng.Intn(len(kinds))]
			rev := relationship(fmt.Sprintf("rel:%d:%d", round, e), from, to, kind, uint32(e+1))
			revs = append(revs, rev)
		}

		g, err := intent.NewRelationshipGraph(tenant, revs)
		if err != nil {
			matched := false
			for _, sentinel := range typed {
				if errors.Is(err, sentinel) {
					matched = true
					break
				}
			}
			if !matched {
				t.Fatalf("round %d refused for an untyped reason: %v", round, err)
			}
			continue
		}

		// The graph was accepted, so no cycle-prohibiting kind may contain one.
		for _, kind := range kinds {
			if !kind.ProhibitsCycles() {
				continue
			}
			adjacency := map[string][]string{}
			for _, id := range relationshipIDs(revs) {
				rev, ok := g.Current(id)
				if !ok || rev.Kind != kind {
					continue
				}
				adjacency[rev.Parent.IntentID] = append(adjacency[rev.Parent.IntentID], rev.Child.IntentID)
			}
			if hasCycle(adjacency) {
				t.Fatalf("round %d: an accepted graph has a %s cycle", round, kind)
			}
		}
	}
}

// relationshipIDs returns the relationship ids in a revision list, in order and
// without duplicates.
func relationshipIDs(revs []intent.RelationshipRevision) []string {
	seen := map[string]bool{}
	var out []string
	for _, rev := range revs {
		if seen[rev.RelationshipID] {
			continue
		}
		seen[rev.RelationshipID] = true
		out = append(out, rev.RelationshipID)
	}
	return out
}

// hasCycle is an independent cycle check, deliberately written differently from
// the one under test: a property test that reuses the implementation proves
// nothing.
func hasCycle(adjacency map[string][]string) bool {
	const (
		unvisited = 0
		onStack   = 1
		done      = 2
	)
	state := map[string]int{}
	var walk func(string) bool
	walk = func(node string) bool {
		state[node] = onStack
		for _, next := range adjacency[node] {
			switch state[next] {
			case onStack:
				return true
			case unvisited:
				if walk(next) {
					return true
				}
			}
		}
		state[node] = done
		return false
	}
	for node := range adjacency {
		if state[node] == unvisited && walk(node) {
			return true
		}
	}
	return false
}

// TestTodo_INTENT_015_Security is the tenant and disclosure half: a
// relationship never crosses a tenant boundary, and a material child is never
// invisible to whoever can see its parent.
func TestTodo_INTENT_015_Security(t *testing.T) {
	tenant := values.TenantId("acme-eu")

	t.Run("a relationship from another tenant is refused", func(t *testing.T) {
		rev := relationship("rel:1", "intent:1", "intent:2", intent.RelationChild, 1)
		rev.Tenant = values.TenantId("globex")
		if _, err := intent.NewRelationshipGraph(tenant, []intent.RelationshipRevision{rev}); !errors.Is(err, intent.ErrInvalidRelationship) {
			t.Fatalf("a cross-tenant relationship was compiled: %v", err)
		}
	})

	t.Run("a graph compiled for no tenant is refused", func(t *testing.T) {
		rev := relationship("rel:1", "intent:1", "intent:2", intent.RelationChild, 1)
		if _, err := intent.NewRelationshipGraph("", []intent.RelationshipRevision{rev}); !errors.Is(err, intent.ErrInvalidRelationship) {
			t.Fatalf("a tenantless graph was compiled: %v", err)
		}
	})

	t.Run("a non-material relationship may be detached, a material one may not", func(t *testing.T) {
		detached := relationship("rel:1", "intent:1", "intent:2", intent.RelationFollowUp, 1)
		detached.Material = false
		detached.Completion = intent.CompletionDetached
		detached.Disclosed = false
		if _, err := intent.NewRelationshipGraph(tenant, []intent.RelationshipRevision{detached}); err != nil {
			t.Fatalf("a non-material follow-up was refused: %v", err)
		}
		detached.Material = true
		if _, err := intent.NewRelationshipGraph(tenant, []intent.RelationshipRevision{detached}); !errors.Is(err, intent.ErrHiddenChildIntent) {
			t.Fatalf("the same relationship carrying material work was accepted: %v", err)
		}
	})

	t.Run("History does not alias the compiled graph", func(t *testing.T) {
		base := relationship("rel:1", "intent:1", "intent:2", intent.RelationChild, 1)
		g, err := intent.NewRelationshipGraph(tenant, []intent.RelationshipRevision{base})
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		history := g.History("rel:1")
		history[0].Parent.IntentID = "intent:999"
		if again := g.History("rel:1"); again[0].Parent.IntentID != "intent:1" {
			t.Fatal("a caller mutated the compiled graph through History")
		}
	})
}

// TestTodo_INTENT_015_Mutation perturbs one field at a time of a valid
// relationship and requires a typed rejection for each. A mutation that
// survives names a rule the relationship contract does not enforce.
func TestTodo_INTENT_015_Mutation(t *testing.T) {
	tenant := values.TenantId("acme-eu")
	valid := relationship("rel:1", "intent:1", "intent:2", intent.RelationChild, 1)
	if err := valid.Validate(); err != nil {
		t.Fatalf("the unmutated relationship was refused: %v", err)
	}

	cases := []struct {
		name   string
		break_ func(*intent.RelationshipRevision)
		cause  error
	}{
		{"no relationship id", func(r *intent.RelationshipRevision) { r.RelationshipID = "" }, intent.ErrInvalidRelationship},
		{"revision zero", func(r *intent.RelationshipRevision) { r.Revision = 0 }, intent.ErrInvalidRelationship},
		{"no parent intent", func(r *intent.RelationshipRevision) { r.Parent.IntentID = "" }, intent.ErrInvalidRelationship},
		{"no child intent", func(r *intent.RelationshipRevision) { r.Child.IntentID = "" }, intent.ErrInvalidRelationship},
		{"half a proposal pin", func(r *intent.RelationshipRevision) { r.Parent.ProposalRevision = 0 }, intent.ErrInvalidRelationship},
		{"child proposal pinned by id alone", func(r *intent.RelationshipRevision) {
			r.Child.ProposalRevisionID = "proposal:child:1"
		}, intent.ErrInvalidRelationship},
		{"self-relationship", func(r *intent.RelationshipRevision) { r.Child.IntentID = r.Parent.IntentID }, intent.ErrRelationshipCycle},
		{"undisclosed material child", func(r *intent.RelationshipRevision) { r.Disclosed = false }, intent.ErrHiddenChildIntent},
		{"non-canonical cause", func(r *intent.RelationshipRevision) { r.CauseRef = "cause\x07node" }, intent.ErrInvalidRelationship},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rev := valid
			tc.break_(&rev)
			if err := rev.Validate(); !errors.Is(err, tc.cause) {
				t.Fatalf("mutation %q survived: %v", tc.name, err)
			}
			if _, err := intent.NewRelationshipGraph(tenant, []intent.RelationshipRevision{rev}); !errors.Is(err, tc.cause) {
				t.Fatalf("mutation %q survived graph compilation: %v", tc.name, err)
			}
		})
	}

	t.Run("a kind that binds no proposal does not require one", func(t *testing.T) {
		rev := relationship("rel:2", "intent:1", "intent:2", intent.RelationDependency, 1)
		if rev.Parent.ProposalRevisionID != "" {
			t.Fatal("the fixture pinned a proposal for a kind that binds none")
		}
		if err := rev.Validate(); err != nil {
			t.Fatalf("a DEPENDENCY without a proposal pin was refused: %v", err)
		}
	})
}
