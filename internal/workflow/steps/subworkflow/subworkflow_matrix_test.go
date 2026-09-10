package subworkflow

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestTodo_WF_STEP_009_Golden(t *testing.T) {
	expanded, err := Expand(validExpansion())
	if err != nil {
		t.Fatal(err)
	}
	report, err := PropagateCancellation(CancellableChild{Ref: expanded.Child, State: StateRunning, Cancellable: true})
	if err != nil {
		t.Fatal(err)
	}
	completion, err := CompleteParent([]ChildTruth{{Child: expanded.Child, Outcome: report, Mandatory: true}}, []ChildRef{expanded.Child})
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(struct {
		Child      ExpandedChild    `json:"child"`
		Report     ChildReport      `json:"report"`
		Completion ParentCompletion `json:"completion"`
	}{expanded, report, completion}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const goldenPath = "testdata/golden.json"
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

func TestTodo_WF_STEP_009_Mutation(t *testing.T) {
	singles := []struct {
		name   string
		mutate func(*Expansion)
		code   error
	}{
		{"version drift", func(e *Expansion) { e.PinnedVersion = "v0.0.1" }, ErrVersionChanged},
		{"scope smuggling", func(e *Expansion) { e.ChildPolicyScope = []string{"leave:read", "admin:all"} }, ErrAuthorityExpansion},
		{"depth overflow", func(e *Expansion) { e.Depth = e.MaxDepth + 1 }, ErrDepthExceeded},
		{"fanout overflow", func(e *Expansion) { e.SiblingOrdinal = e.MaxFanout }, ErrFanoutExceeded},
		{"self recursion", func(e *Expansion) { e.Ancestors = append(e.Ancestors, e.Child) }, ErrRecursiveCycle},
		{"uncertified expansion", func(e *Expansion) { e.Certificate = "" }, ErrMissingCertificate},
	}
	for _, tc := range singles {
		t.Run(tc.name, func(t *testing.T) {
			e := validExpansion()
			tc.mutate(&e)
			if _, err := Expand(e); !IsCode(err, tc.code) {
				t.Fatalf("mutant survived: got %v, want %v", err, tc.code)
			}
		})
	}
	t.Run("attenuated caller scope still expands", func(t *testing.T) {
		e := validExpansion()
		e.CallerScope = []string{"leave:read"}
		expanded, err := Expand(e)
		if err != nil {
			t.Fatalf("attenuated expansion rejected: %v", err)
		}
		if len(expanded.EffectiveScope) != 1 {
			t.Fatalf("effective scope wrong: %+v", expanded)
		}
	})
	t.Run("detached child with obligation completes", func(t *testing.T) {
		e := validExpansion()
		e.WaitMode = WaitModeDetachWithObligation
		expanded, err := Expand(e)
		if err != nil {
			t.Fatal(err)
		}
		obligation, err := Detach(expanded, "corr/1")
		if err != nil {
			t.Fatal(err)
		}
		if obligation.IdempotencyKey != expanded.IdempotencyKey {
			t.Fatal("obligation lost the idempotency link")
		}
	})
}
