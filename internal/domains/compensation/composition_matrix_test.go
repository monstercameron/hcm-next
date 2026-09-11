package compensation_test

import (
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/compensation"
)

func TestTodo_COMP_005_Property(t *testing.T) {
	kinds := []compensation.ComponentType{
		compensation.ComponentBase, compensation.ComponentBonusTarget, compensation.ComponentAllowance,
	}
	ops := []compensation.ComponentOp{
		compensation.OpAdd, compensation.OpRevise, compensation.OpEnd, compensation.OpCorrect,
	}
	rng := rand.New(rand.NewSource(0xC005))
	for round := range 300 {
		current := []compensation.CurrentComponent{
			{Type: compensation.ComponentBase, Revision: 1 + uint64(rng.Intn(5))},
			{Type: compensation.ComponentBonusTarget, Revision: 1 + uint64(rng.Intn(5))},
		}
		proposal := compProposal(t)
		proposal.Components = nil
		count := rng.Intn(3)
		for i := range count {
			kind := kinds[rng.Intn(len(kinds))]
			op := ops[rng.Intn(len(ops))]
			component := compensation.ComponentProposal{Type: kind, Op: op, HasAmount: rng.Intn(2) == 0,
				Currency: "USD", Frequency: "annual", Revision: 1}
			if op == compensation.OpEnd {
				component.EndCondition = "term"
			}
			if op == compensation.OpCorrect {
				component.CorrectionOf = "rev-1"
				component.RetroactiveReason = "backpay"
			}
			proposal.Components = append(proposal.Components, component)
			_ = i
		}
		composed, err := compensation.ComposePackage(current, proposal, compPolicy())
		if err != nil {
			continue
		}
		again, err := compensation.ComposePackage(current, proposal, compPolicy())
		if err != nil || again.Digest != composed.Digest {
			t.Fatalf("round %d: unstable identity %v", round, err)
		}
		for _, child := range composed.Children {
			if child.ApprovalRef == "" {
				t.Fatalf("round %d: child without approval", round)
			}
		}
	}
}

func TestTodo_COMP_005_Golden(t *testing.T) {
	composed, err := compensation.ComposePackage(compCurrent(), compProposal(t), compPolicy())
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(composed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const goldenPath = "testdata/composition_golden.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
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

func TestTodo_COMP_005_Race(t *testing.T) {
	const workers = 16
	digests := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			composed, err := compensation.ComposePackage(compCurrent(), compProposal(t), compPolicy())
			if err != nil {
				errs <- err
				return
			}
			digests <- composed.Digest
		}()
	}
	wg.Wait()
	close(digests)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent compose failed: %v", err)
	}
	var first string
	for digest := range digests {
		if first == "" {
			first = digest
		} else if digest != first {
			t.Fatal("concurrent compositions diverged")
		}
	}
}

func TestTodo_COMP_005_Fault(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*compensation.PackageProposal)
		cause  error
	}{
		{"empty package ref", func(p *compensation.PackageProposal) { p.PackageRef = "" }, compensation.ErrInvalidPackage},
		{"zero fence", func(p *compensation.PackageProposal) { p.Fence = 0 }, compensation.ErrInvalidPackage},
		{"stale revision", func(p *compensation.PackageProposal) { p.Components[0].Revision = 1 }, compensation.ErrInvalidPackage},
		{"too many children", func(p *compensation.PackageProposal) {}, compensation.ErrInvalidPackage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proposal := compProposal(t)
			policy := compPolicy()
			if tc.name == "too many children" {
				policy.MaxChildren = 0
			} else {
				tc.mutate(&proposal)
			}
			if _, err := compensation.ComposePackage(compCurrent(), proposal, policy); !errors.Is(err, tc.cause) {
				t.Fatalf("want %v, got %v", tc.cause, err)
			}
		})
	}
}

func TestTodo_COMP_005_Security(t *testing.T) {
	t.Run("omitted components keep exact revisions", func(t *testing.T) {
		composed, err := compensation.ComposePackage(compCurrent(), compProposal(t), compPolicy())
		if err != nil {
			t.Fatal(err)
		}
		for _, component := range composed.Components {
			if !component.Carried {
				continue
			}
			for _, current := range compCurrent() {
				if current.Type == component.Type && current.Revision != component.Revision {
					t.Fatalf("carried component %s moved %d -> %d", component.Type, current.Revision, component.Revision)
				}
			}
		}
	})
	t.Run("children bind proposal approvals only", func(t *testing.T) {
		composed, err := compensation.ComposePackage(compCurrent(), compProposal(t), compPolicy())
		if err != nil {
			t.Fatal(err)
		}
		for _, child := range composed.Children {
			if child.ApprovalRef != "approval/captains-1" {
				t.Fatalf("child bound foreign approval: %+v", child)
			}
		}
	})
	t.Run("boundary escape refused", func(t *testing.T) {
		proposal := compProposal(t)
		policy := compPolicy()
		policy.AtomicBoundary = []compensation.ComponentType{compensation.ComponentBonusTarget}
		if _, err := compensation.ComposePackage(compCurrent(), proposal, policy); !errors.Is(err, compensation.ErrOutsideAtomicBoundary) {
			t.Fatalf("base revision escaped its boundary: %v", err)
		}
	})
}

func TestTodo_COMP_005_Conformance(t *testing.T) {
	composed, err := compensation.ComposePackage(compCurrent(), compProposal(t), compPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(composed.Digest, "sha256:") {
		t.Fatalf("digest not content-addressed: %q", composed.Digest)
	}
	if len(composed.Children) > compPolicy().MaxChildren {
		t.Fatal("children escaped the bound")
	}
	raw, err := json.Marshal(composed)
	if err != nil {
		t.Fatal(err)
	}
	var decoded compensation.ComposedPackage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("composed package does not survive serialization: %v", err)
	}
	if decoded.Digest != composed.Digest {
		t.Fatal("serialization changed the identity")
	}
}

func TestTodo_COMP_005_Mutation(t *testing.T) {
	t.Run("end with condition passes", func(t *testing.T) {
		proposal := compProposal(t)
		proposal.Components[0].Op = compensation.OpEnd
		proposal.Components[0].EndCondition = "termination-2026-11-01"
		composed, err := compensation.ComposePackage(compCurrent(), proposal, compPolicy())
		if err != nil {
			t.Fatalf("conditioned end rejected: %v", err)
		}
		if composed.Components[0].Op != compensation.OpEnd {
			t.Fatalf("end lost: %+v", composed.Components[0])
		}
	})
	t.Run("add of new component passes", func(t *testing.T) {
		proposal := compProposal(t)
		proposal.Components = []compensation.ComponentProposal{{
			Type: compensation.ComponentCommission, Op: compensation.OpAdd,
			Amount: compDecimal(t, "1200.00"), HasAmount: true, Currency: "USD", Frequency: "annual",
		}}
		policy := compPolicy()
		policy.AtomicBoundary = append(policy.AtomicBoundary, compensation.ComponentCommission)
		composed, err := compensation.ComposePackage(compCurrent(), proposal, policy)
		if err != nil {
			t.Fatalf("add rejected: %v", err)
		}
		found := false
		for _, component := range composed.Components {
			if component.Type == compensation.ComponentCommission {
				found = true
				if component.Revision != 1 || component.Carried {
					t.Fatalf("added component wrong: %+v", component)
				}
			}
		}
		if !found {
			t.Fatal("added component missing from output")
		}
	})
}
