package intent_test

import (
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

func TestTodo_INTENT_026_Property(t *testing.T) {
	defs := []string{"payroll.run.a", "payroll.run.b", "payroll.run.c"}
	systems := []string{"payroll-core", "notify"}
	rng := rand.New(rand.NewSource(0x2601))
	for round := range 400 {
		plan := intent.CompositionPlan{
			ParentDefinition: intent.Ref{TypeID: "payroll.run", Version: 1},
			ParentProposal:   "proposal/p",
			ParentTenant:     "tenant-1",
			ParentOrg:        "org:acme",
			ParentPurpose:    "payroll.run",
			ParentDelegation: []string{"payroll:run", "payroll:read"},
			Boundary:         intent.BoundarySaga,
			Wait:             intent.WaitForChildren,
			Failure:          intent.FailParent,
			Correction:       intent.CorrectInPlace,
			Cancellation:     intent.PropagateCancellation,
			MaxCost:          1000,
			MaxChildren:      4,
		}
		nodes := 1 + rng.Intn(3)
		for i := range nodes {
			def := defs[rng.Intn(len(defs))]
			child := intent.ChildTemplate{
				Definition: intent.Ref{TypeID: def, Version: uint32(1 + rng.Intn(2))},
				Ordinal:    uint32(i),
				Owner:      "engine",
				System:     systems[rng.Intn(len(systems))],
				Tenant:     "tenant-1",
				Org:        "org:acme",
				Purpose:    "payroll.run." + def[strings.LastIndex(def, ".")+1:],
				Delegation: []string{"payroll:run"},
				Cost:       int64(rng.Intn(10)),
			}
			if rng.Intn(8) == 0 {
				child.Tenant = "tenant-9"
			}
			plan.Children = append(plan.Children, child)
		}
		for e := 0; e < rng.Intn(4); e++ {
			plan.Edges = append(plan.Edges, intent.DependencyEdge{
				Before: defs[rng.Intn(len(defs))],
				After:  defs[rng.Intn(len(defs))],
			})
		}
		compiled, err := intent.CompileComposition(plan)
		if err != nil {
			var target *intent.Error
			if !errors.As(err, &target) || target.Cause == nil {
				t.Fatalf("round %d: untyped refusal %v", round, err)
			}
			continue
		}
		again, err := intent.CompileComposition(plan)
		if err != nil || again.Digest != compiled.Digest {
			t.Fatalf("round %d: unstable identity %v", round, err)
		}
		for _, child := range compiled.Plan.Children {
			if child.Tenant != plan.ParentTenant {
				t.Fatalf("round %d: tenant broadened", round)
			}
		}
		bundle := intent.BundleComposition(compiled, nil)
		if err := intent.VerifyBundle(bundle, plan); err != nil {
			t.Fatalf("round %d: bundle mismatch %v", round, err)
		}
	}
}

func TestTodo_INTENT_026_Golden(t *testing.T) {
	compiled, err := intent.CompileComposition(validCompositionPlan())
	if err != nil {
		t.Fatal(err)
	}
	bundle := intent.BundleComposition(compiled, []string{"evidence/audit-1"})
	got, err := json.MarshalIndent(struct {
		Plan   intent.CompositionPlan `json:"plan"`
		Digest string                 `json:"digest"`
		Bundle intent.IntentBundle    `json:"bundle"`
	}{compiled.Plan, compiled.Digest, bundle}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const goldenPath = "testdata/intent026_golden.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestTodo_INTENT_026_Race(t *testing.T) {
	const workers = 16
	digests := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			compiled, err := intent.CompileComposition(validCompositionPlan())
			if err != nil {
				errs <- err
				return
			}
			digests <- compiled.Digest
		}()
	}
	wg.Wait()
	close(digests)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent compile failed: %v", err)
	}
	var first string
	for digest := range digests {
		if first == "" {
			first = digest
		} else if digest != first {
			t.Fatal("concurrent compilations diverged")
		}
	}
}

func TestTodo_INTENT_026_Fault(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*intent.CompositionPlan)
		cause  error
	}{
		{"empty parent", func(p *intent.CompositionPlan) { p.ParentDefinition = intent.Ref{} }, intent.ErrInvalidComposition},
		{"no children", func(p *intent.CompositionPlan) { p.Children = nil }, intent.ErrInvalidComposition},
		{"invalid boundary", func(p *intent.CompositionPlan) { p.Boundary = "CHAOS" }, intent.ErrInvalidComposition},
		{"empty system", func(p *intent.CompositionPlan) { p.Children[0].System = "" }, intent.ErrInvalidComposition},
		{"undeclared atomic ref", func(p *intent.CompositionPlan) {
			p.AtomicGroups = [][]string{{"payroll.run.ghost"}}
		}, intent.ErrHiddenChildIntent},
		{"zero cost limit", func(p *intent.CompositionPlan) { p.MaxCost = 0 }, intent.ErrBudgetExceeded},
		{"zero child limit", func(p *intent.CompositionPlan) { p.MaxChildren = 0 }, intent.ErrBudgetExceeded},
		{"empty wait policy", func(p *intent.CompositionPlan) { p.Wait = "" }, intent.ErrMissingCompositionPolicy},
		{"empty correction policy", func(p *intent.CompositionPlan) { p.Correction = "" }, intent.ErrMissingCompositionPolicy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := validCompositionPlan()
			tc.mutate(&plan)
			if _, err := intent.CompileComposition(plan); !errors.Is(err, tc.cause) {
				t.Fatalf("want %v, got %v", tc.cause, err)
			}
		})
	}
}

func TestTodo_INTENT_026_Security(t *testing.T) {
	t.Run("cross-tenant child refused", func(t *testing.T) {
		plan := validCompositionPlan()
		plan.Children[1].Tenant = "tenant-evil"
		if _, err := intent.CompileComposition(plan); !errors.Is(err, intent.ErrAuthorityBroadening) {
			t.Fatalf("cross-tenant child accepted: %v", err)
		}
	})
	t.Run("delegation smuggling refused", func(t *testing.T) {
		plan := validCompositionPlan()
		plan.Children[1].Delegation = []string{"payroll:read", "treasury:move"}
		if _, err := intent.CompileComposition(plan); !errors.Is(err, intent.ErrAuthorityBroadening) {
			t.Fatalf("undelegated token accepted: %v", err)
		}
	})
	t.Run("cross-system atomicity refused", func(t *testing.T) {
		plan := validCompositionPlan()
		plan.AtomicGroups = [][]string{{"payroll.run.overtime", "payroll.run.notify"}}
		if _, err := intent.CompileComposition(plan); !errors.Is(err, intent.ErrCrossSystemAtomicity) {
			t.Fatalf("cross-system atomic group accepted: %v", err)
		}
	})
	t.Run("post-approval change breaks verification", func(t *testing.T) {
		compiled, err := intent.CompileComposition(validCompositionPlan())
		if err != nil {
			t.Fatal(err)
		}
		bundle := intent.BundleComposition(compiled, nil)
		mutated := validCompositionPlan()
		mutated.Children[0].Cost = 9999
		mutated.MaxCost = 99999
		if err := intent.VerifyBundle(bundle, mutated); !errors.Is(err, intent.ErrBundleMismatch) {
			t.Fatalf("post-approval change verified: %v", err)
		}
	})
}

func TestTodo_INTENT_026_Conformance(t *testing.T) {
	if n := reflect.TypeOf(intent.IntentBundle{}).NumMethod(); n != 0 {
		t.Fatalf("bundle exposes %d methods; it must be inspection evidence only", n)
	}
	compiled, err := intent.CompileComposition(validCompositionPlan())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(compiled.Digest, "sha256:") {
		t.Fatalf("digest not content-addressed: %q", compiled.Digest)
	}
	bundle := intent.BundleComposition(compiled, []string{"evidence/1"})
	if bundle.PlanDigest != compiled.Digest || !strings.HasPrefix(bundle.Digest, "sha256:") {
		t.Fatalf("bundle misbound: %+v", bundle)
	}
	if len(bundle.Children) != len(validCompositionPlan().Children) {
		t.Fatalf("bundle dropped children: %+v", bundle)
	}
	if err := intent.VerifyBundle(bundle, validCompositionPlan()); err != nil {
		t.Fatalf("bundle failed against its plan: %v", err)
	}
}

func TestTodo_INTENT_026_Mutation(t *testing.T) {
	t.Run("three-node cycle refused", func(t *testing.T) {
		plan := validCompositionPlan()
		plan.Children = append(plan.Children, intent.ChildTemplate{
			Definition: intent.Ref{TypeID: "payroll.run.audit", Version: 1}, Ordinal: 2,
			Owner: "audit-engine", System: "payroll-core", Tenant: "tenant-1",
			Org: "org:acme", Purpose: "payroll.run.audit", Delegation: []string{"payroll:read"},
		})
		plan.Edges = []intent.DependencyEdge{
			{Before: "payroll.run.overtime", After: "payroll.run.notify"},
			{Before: "payroll.run.notify", After: "payroll.run.audit"},
			{Before: "payroll.run.audit", After: "payroll.run.overtime"},
		}
		if _, err := intent.CompileComposition(plan); !errors.Is(err, intent.ErrCompositionCycle) {
			t.Fatalf("three-node cycle accepted: %v", err)
		}
	})
	t.Run("self edge refused", func(t *testing.T) {
		plan := validCompositionPlan()
		plan.Edges = append(plan.Edges, intent.DependencyEdge{Before: "payroll.run.notify", After: "payroll.run.notify"})
		if _, err := intent.CompileComposition(plan); !errors.Is(err, intent.ErrCompositionCycle) {
			t.Fatalf("self edge accepted: %v", err)
		}
	})
	t.Run("single-system atomic group passes", func(t *testing.T) {
		plan := validCompositionPlan()
		if _, err := intent.CompileComposition(plan); err != nil {
			t.Fatalf("single-system atomic group rejected: %v", err)
		}
	})
	t.Run("equal authority passes", func(t *testing.T) {
		plan := validCompositionPlan()
		plan.Children[0].Org = "org:acme"
		plan.Children[0].Purpose = "payroll.run"
		if _, err := intent.CompileComposition(plan); err != nil {
			t.Fatalf("equal authority rejected: %v", err)
		}
	})
}
