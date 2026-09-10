package intent_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

func validCompositionPlan() intent.CompositionPlan {
	return intent.CompositionPlan{
		ParentDefinition: intent.Ref{TypeID: "payroll.run", Version: 2},
		ParentProposal:   "proposal/payroll-run-88/r9",
		ParentTenant:     "tenant-1",
		ParentOrg:        "org:acme",
		ParentPurpose:    "payroll.run",
		ParentDelegation: []string{"payroll:run", "payroll:read"},
		Children: []intent.ChildTemplate{
			{Definition: intent.Ref{TypeID: "payroll.run.overtime", Version: 1}, Ordinal: 0,
				Owner: "payroll-engine", System: "payroll-core", Tenant: "tenant-1",
				Org: "org:acme", Purpose: "payroll.run.overtime",
				Delegation: []string{"payroll:run"}, Cost: 10, Material: true},
			{Definition: intent.Ref{TypeID: "payroll.run.notify", Version: 3}, Ordinal: 1,
				Owner: "notify-engine", System: "notify", Tenant: "tenant-1",
				Org: "org:acme", Purpose: "payroll.run.notify",
				Delegation: []string{"payroll:read"}, Cost: 5, Material: false},
		},
		Edges: []intent.DependencyEdge{
			{Before: "payroll.run.overtime", After: "payroll.run.notify"},
		},
		AtomicGroups: [][]string{{"payroll.run.overtime"}},
		Boundary:     intent.BoundarySaga,
		Wait:         intent.WaitForChildren,
		Failure:      intent.FailParent,
		Correction:   intent.CorrectInPlace,
		Cancellation: intent.PropagateCancellation,
		MaxCost:      100,
		MaxChildren:  8,
	}
}

// TestIntentCompositionPlanRejectsCyclesHiddenChildrenAndFalseAtomicity
// proves one immutable composition plan binds its parent, children, DAG,
// attenuated authority, consistency boundaries and completion policies, or
// fails with an exact cause.
func TestIntentCompositionPlanRejectsCyclesHiddenChildrenAndFalseAtomicity(t *testing.T) {
	compiled, err := intent.CompileComposition(validCompositionPlan())
	if err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}
	if compiled.Digest == "" {
		t.Fatal("compiled plan carries no digest")
	}
	again, err := intent.CompileComposition(validCompositionPlan())
	if err != nil {
		t.Fatal(err)
	}
	if again.Digest != compiled.Digest {
		t.Fatal("duplicate compilation created a different semantic identity")
	}

	adversaries := []struct {
		name   string
		mutate func(*intent.CompositionPlan)
		cause  error
	}{
		{"cyclic graph", func(p *intent.CompositionPlan) {
			p.Edges = append(p.Edges, intent.DependencyEdge{Before: "payroll.run.notify", After: "payroll.run.overtime"})
		}, intent.ErrCompositionCycle},
		{"duplicate child", func(p *intent.CompositionPlan) {
			p.Children = append(p.Children, p.Children[0])
		}, intent.ErrDuplicateChild},
		{"hidden child in edge", func(p *intent.CompositionPlan) {
			p.Edges = append(p.Edges, intent.DependencyEdge{Before: "payroll.run.ghost", After: "payroll.run.notify"})
		}, intent.ErrHiddenChildIntent},
		{"ownerless child", func(p *intent.CompositionPlan) { p.Children[0].Owner = "" }, intent.ErrInvalidComposition},
		{"tenant broadening", func(p *intent.CompositionPlan) { p.Children[0].Tenant = "tenant-2" }, intent.ErrAuthorityBroadening},
		{"purpose broadening", func(p *intent.CompositionPlan) { p.Children[0].Purpose = "payroll" }, intent.ErrAuthorityBroadening},
		{"delegation broadening", func(p *intent.CompositionPlan) {
			p.Children[0].Delegation = []string{"hr:admin"}
		}, intent.ErrAuthorityBroadening},
		{"cross-system atomicity", func(p *intent.CompositionPlan) {
			p.AtomicGroups = [][]string{{"payroll.run.overtime", "payroll.run.notify"}}
		}, intent.ErrCrossSystemAtomicity},
		{"duplicate ordinals", func(p *intent.CompositionPlan) { p.Children[1].Ordinal = 0 }, intent.ErrUnorderedChildren},
		{"budget exceeded", func(p *intent.CompositionPlan) { p.MaxCost = 1 }, intent.ErrBudgetExceeded},
		{"too many children", func(p *intent.CompositionPlan) { p.MaxChildren = 1 }, intent.ErrBudgetExceeded},
		{"missing policy", func(p *intent.CompositionPlan) { p.Failure = "" }, intent.ErrMissingCompositionPolicy},
	}
	for _, tc := range adversaries {
		t.Run(tc.name, func(t *testing.T) {
			plan := validCompositionPlan()
			tc.mutate(&plan)
			if _, err := intent.CompileComposition(plan); !errors.Is(err, tc.cause) {
				t.Fatalf("want %v, got %v", tc.cause, err)
			}
		})
	}

	t.Run("bundle verifies against its plan", func(t *testing.T) {
		bundle := intent.BundleComposition(compiled, []string{"sha256:evidence"})
		if bundle.PlanDigest != compiled.Digest {
			t.Fatal("bundle lost its plan digest")
		}
		if err := intent.VerifyBundle(bundle, validCompositionPlan()); err != nil {
			t.Fatalf("bundle failed against its own plan: %v", err)
		}
		mutated := validCompositionPlan()
		mutated.MaxCost = 101
		if err := intent.VerifyBundle(bundle, mutated); !errors.Is(err, intent.ErrBundleMismatch) {
			t.Fatalf("mutated plan verified against its bundle: %v", err)
		}
	})
}
