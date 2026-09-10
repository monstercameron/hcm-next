package intent_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

func TestTodo_INTENT_016_Golden(t *testing.T) {
	emitter := intent.NewEmitter()
	child, err := emitter.Emit(validEmissionRequest())
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := emitter.Emit(validEmissionRequest())
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(struct {
		Child     intent.EmittedChild `json:"child"`
		Duplicate intent.EmittedChild `json:"duplicate"`
	}{child, duplicate}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const goldenPath = "testdata/intent016_golden.json"
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

func TestTodo_INTENT_016_Race(t *testing.T) {
	emitter := intent.NewEmitter()
	const workers = 16
	type result struct {
		child intent.EmittedChild
		err   error
	}
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			child, err := emitter.Emit(validEmissionRequest())
			results <- result{child, err}
		}()
	}
	wg.Wait()
	close(results)
	first := true
	var key, id string
	winners := 0
	for r := range results {
		if r.err != nil {
			t.Fatalf("concurrent emission failed: %v", r.err)
		}
		if first {
			key, id = r.child.Key, r.child.ChildID
			first = false
		}
		if r.child.Key != key || r.child.ChildID != id {
			t.Fatal("concurrent deliveries named different children")
		}
		if !r.child.Duplicate {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("%d concurrent emissions claimed first delivery, want exactly 1", winners)
	}
	const fanout = 4
	distinct := make(chan error, fanout)
	for ordinal := 0; ordinal < fanout; ordinal++ {
		wg.Add(1)
		go func(ordinal int) {
			defer wg.Done()
			req := validEmissionRequest()
			req.Ordinal = ordinal
			_, err := emitter.Emit(req)
			distinct <- err
		}(ordinal)
	}
	wg.Wait()
	close(distinct)
	for err := range distinct {
		if err != nil {
			t.Fatalf("concurrent distinct emission failed: %v", err)
		}
	}
}

func TestTodo_INTENT_016_Mutation(t *testing.T) {
	singles := []struct {
		name   string
		mutate func(*intent.EmissionRequest)
		cause  error
	}{
		{"self recursion", func(r *intent.EmissionRequest) { r.ChildDefinition = r.ParentDefinition }, intent.ErrRecursiveEmission},
		{"fanout overflow", func(r *intent.EmissionRequest) { r.Ordinal = r.MaxFanout }, intent.ErrFanoutOverflow},
		{"depth overflow", func(r *intent.EmissionRequest) { r.Depth = r.MaxDepth + 1 }, intent.ErrDepthOverflow},
		{"budget overflow", func(r *intent.EmissionRequest) { r.Cost = r.MaxCost + 1 }, intent.ErrBudgetOverflow},
		{"scope smuggling", func(r *intent.EmissionRequest) { r.ChildScope = []string{"payroll:run", "audit:all"} }, intent.ErrScopeExpansion},
		{"purpose broadening", func(r *intent.EmissionRequest) { r.ChildPurpose = "payroll" }, intent.ErrPurposeBroadening},
	}
	for _, tc := range singles {
		t.Run(tc.name, func(t *testing.T) {
			req := validEmissionRequest()
			tc.mutate(&req)
			if _, err := intent.NewEmitter().Emit(req); !errors.Is(err, tc.cause) {
				t.Fatalf("mutant survived: got %v, want %v", err, tc.cause)
			}
		})
	}
	t.Run("narrowed purpose suffix emits", func(t *testing.T) {
		req := validEmissionRequest()
		req.ChildPurpose = "payroll.run.overtime.night"
		if _, err := intent.NewEmitter().Emit(req); err != nil {
			t.Fatalf("narrowed purpose rejected: %v", err)
		}
	})
	t.Run("follow-up kind emits", func(t *testing.T) {
		req := validEmissionRequest()
		req.Kind = intent.RelationFollowUp
		child, err := intent.NewEmitter().Emit(req)
		if err != nil {
			t.Fatalf("follow-up rejected: %v", err)
		}
		if child.Kind != intent.RelationFollowUp {
			t.Fatalf("kind lost: %+v", child)
		}
	})
}
