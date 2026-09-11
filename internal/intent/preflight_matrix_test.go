package intent_test

import (
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/governance"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

func TestTodo_INTENT_027_Property(t *testing.T) {
	levels := []intent.RiskLevel{intent.RiskLow, intent.RiskMedium, intent.RiskHigh, intent.RiskCritical, ""}
	rng := rand.New(rand.NewSource(0x2701))
	for round := range 300 {
		req := validPreflightPlanRequest()
		req.Cost.Units = int64(rng.Intn(100))
		req.Risk.Level = levels[rng.Intn(len(levels))]
		if rng.Intn(6) == 0 {
			req.Snapshot.Digest = ""
		}
		if rng.Intn(6) == 0 {
			req.EffectEdges = append(req.EffectEdges, intent.EffectEdge{From: "ledger/payroll", To: "shadow/x"})
		}
		if rng.Intn(8) == 0 {
			req.Reservations[0].Fence = 0
		}
		plan, err := intent.CompilePreflightPlan(req)
		if err != nil {
			var target *intent.Error
			if !errors.As(err, &target) || target.Cause == nil {
				t.Fatalf("round %d: untyped refusal %v", round, err)
			}
			continue
		}
		again, err := intent.CompilePreflightPlan(req)
		if err != nil || again.Digest != plan.Digest {
			t.Fatalf("round %d: unstable identity %v", round, err)
		}
		if plan.Governance.Digest == "" {
			t.Fatalf("round %d: governance composition missing", round)
		}
	}
}

func TestTodo_INTENT_027_Golden(t *testing.T) {
	plan, err := intent.CompilePreflightPlan(validPreflightPlanRequest())
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const goldenPath = "testdata/intent027_golden.json"
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

func TestTodo_INTENT_027_Race(t *testing.T) {
	const workers = 16
	digests := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			plan, err := intent.CompilePreflightPlan(validPreflightPlanRequest())
			if err != nil {
				errs <- err
				return
			}
			digests <- plan.Digest
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

func TestTodo_INTENT_027_Fault(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*intent.PreflightPlanRequest)
		cause  error
	}{
		{"empty definition", func(r *intent.PreflightPlanRequest) { r.Definition = intent.Ref{} }, intent.ErrInvalidPreflight},
		{"empty write target", func(r *intent.PreflightPlanRequest) { r.Writes[0].Target = "" }, intent.ErrInvalidPreflight},
		{"bad approval kind", func(r *intent.PreflightPlanRequest) { r.Approvals[0].Kind = "WISH" }, intent.ErrInvalidPreflight},
		{"empty engine version", func(r *intent.PreflightPlanRequest) { r.EngineVersions["payroll-core"] = "" }, intent.ErrMismatchedSnapshot},
		{"negative cost", func(r *intent.PreflightPlanRequest) { r.Cost.Units = -1 }, intent.ErrMissingEstimate},
		{"empty reservation id", func(r *intent.PreflightPlanRequest) { r.Reservations[0].IntentID = "" }, intent.ErrUnfencedReservation},
		{"empty triggers", func(r *intent.PreflightPlanRequest) { r.RevalidationTriggers = nil }, intent.ErrMissingRepairPolicy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validPreflightPlanRequest()
			tc.mutate(&req)
			if _, err := intent.CompilePreflightPlan(req); !errors.Is(err, tc.cause) {
				t.Fatalf("want %v, got %v", tc.cause, err)
			}
		})
	}
}

func TestTodo_INTENT_027_Security(t *testing.T) {
	t.Run("redacted input never affirmed", func(t *testing.T) {
		req := validPreflightPlanRequest()
		req.Assumptions = append(req.Assumptions, intent.Assumption{Name: "salary-band", Unknown: true})
		if _, err := intent.CompilePreflightPlan(req); err != nil {
			t.Fatalf("unresolved redaction rejected: %v", err)
		}
		req.Assumptions[1].ResolvedValue = "band-9"
		if _, err := intent.CompilePreflightPlan(req); !errors.Is(err, intent.ErrUnsafeAffirmation) {
			t.Fatalf("redacted affirmation accepted: %v", err)
		}
	})
	t.Run("no guaranteed outcomes", func(t *testing.T) {
		req := validPreflightPlanRequest()
		req.Writes = append(req.Writes, intent.IntendedWrite{Target: "notify/send", Effect: "SEND", Guaranteed: true})
		if _, err := intent.CompilePreflightPlan(req); !errors.Is(err, intent.ErrGuaranteedOutcome) {
			t.Fatalf("guaranteed outcome accepted: %v", err)
		}
	})
	t.Run("snapshot mismatch fails closed", func(t *testing.T) {
		req := validPreflightPlanRequest()
		req.Snapshot.Digest = "sha256:stale"
		first, err := intent.CompilePreflightPlan(req)
		if err != nil {
			t.Fatal(err)
		}
		req.Snapshot.Digest = "sha256:current"
		second, err := intent.CompilePreflightPlan(req)
		if err != nil {
			t.Fatal(err)
		}
		if first.Digest == second.Digest {
			t.Fatal("different snapshots share one identity")
		}
	})
}

func TestTodo_INTENT_027_Conformance(t *testing.T) {
	req := validPreflightPlanRequest()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := intent.CompilePreflightPlan(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plan.Digest, "sha256:") {
		t.Fatalf("digest not content-addressed: %q", plan.Digest)
	}
	composed := governance.Compose(req.Governance)
	if plan.Governance.Digest != composed.Digest || plan.Governance.Decision != composed.Decision {
		t.Fatal("plan governance differs from the owning composition")
	}
	after, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(after) {
		t.Fatal("compilation mutated its request")
	}
}

func TestTodo_INTENT_027_Mutation(t *testing.T) {
	t.Run("unknown-but-unresolved redaction passes", func(t *testing.T) {
		req := validPreflightPlanRequest()
		req.Assumptions = append(req.Assumptions, intent.Assumption{Name: "bonus-pool", Unknown: true})
		if _, err := intent.CompilePreflightPlan(req); err != nil {
			t.Fatalf("unresolved redaction rejected: %v", err)
		}
	})
	t.Run("empty obligations pass", func(t *testing.T) {
		req := validPreflightPlanRequest()
		req.Obligations = nil
		if _, err := intent.CompilePreflightPlan(req); err != nil {
			t.Fatalf("no-obligation plan rejected: %v", err)
		}
	})
	t.Run("branch edge between declared writes passes", func(t *testing.T) {
		req := validPreflightPlanRequest()
		req.Writes = append(req.Writes, intent.IntendedWrite{Target: "notify/send", Effect: "SEND"})
		req.EffectEdges = append(req.EffectEdges, intent.EffectEdge{From: "ledger/payroll", To: "notify/send"})
		if _, err := intent.CompilePreflightPlan(req); err != nil {
			t.Fatalf("declared branch rejected: %v", err)
		}
	})
}
