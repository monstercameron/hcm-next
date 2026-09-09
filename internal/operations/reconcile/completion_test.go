package reconcile_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile"
)

var completionAt = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func completionJob(repair string) reconcile.Job {
	tenant := uuid.MustParse("55555555-5555-4555-8555-555555555555")
	return reconcile.Job{
		TenantID: tenant, JobID: reconcile.JobID(tenant, "effect:promotion", "policy:promotion-v1"),
		EffectRef: "effect:promotion", PolicyRef: "policy:promotion-v1",
		RequiredFreshness: observe.FreshnessFresh, Deadline: completionAt.Add(time.Hour), RepairPolicy: repair,
	}
}

func matchingComparison(t *testing.T) observe.PilotComparison {
	t.Helper()
	fields := observe.NewPilotFields("OPS-HRBP3", "P3", "position-42", "125000.00 USD")
	result, err := observe.Compare(fields, fields, fields)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func completionRequest(t *testing.T) reconcile.CompletionRequest {
	t.Helper()
	return reconcile.CompletionRequest{
		Job: completionJob("NONE"), Found: true, Freshness: observe.FreshnessFresh,
		Complete: true, Comparison: matchingComparison(t), Now: completionAt.Add(time.Minute),
	}
}

func TestTodo_RECON_002(t *testing.T) {
	request := completionRequest(t)
	got, err := reconcile.EvaluateCompletion(request)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != reconcile.CompletionPass || !got.Terminal || got.Route != reconcile.RouteConsistent {
		t.Fatalf("matching promotion evidence = %+v; want terminal PASS/CONSISTENT", got)
	}
	if got.TerminalContribution != reconcile.CompletionDimension || got.NextAction != reconcile.ActionNone {
		t.Fatalf("matching completion dimensions = %+v", got)
	}

	request.Comparison.Fields[0].Verdict = observe.Mismatch
	request.Comparison.Verdict = observe.Mismatch
	got, err = reconcile.Evaluate(request)
	if err != nil || got.Status != reconcile.CompletionMismatch || got.Route != reconcile.RouteDegraded {
		t.Fatalf("mismatching promotion evidence = %+v, %v; want MISMATCH/DEGRADED", got, err)
	}
}

func TestTodo_RECON_002_Golden(t *testing.T) {
	got, err := reconcile.EvaluateCompletion(completionRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	want := "reconciliation completion status=PASS terminal=true route=CONSISTENT contribution=EXTERNAL_CONSISTENCY next=NONE reason=fresh complete observation matches intended and canonical values"
	if explanation := reconcile.Explain(got); explanation != want {
		t.Fatalf("explanation = %q, want %q", explanation, want)
	}
}

func TestTodo_RECON_002_Race(t *testing.T) {
	request := completionRequest(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := reconcile.EvaluateCompletion(request)
			if err != nil || got.Route != reconcile.RouteConsistent {
				t.Errorf("concurrent evaluation = %+v, %v", got, err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_RECON_002_Integration(t *testing.T) {
	// This is the promotion fixture used by the observe_reconciliation port:
	// all four durable pilot fields are covered, fresh, and equal on all three
	// sides, so the only legal route is CONSISTENT.
	got, err := reconcile.EvaluateCompletion(completionRequest(t))
	if err != nil || got.Route != reconcile.RouteConsistent || got.Status != reconcile.CompletionPass {
		t.Fatalf("promotion fixture route = %+v, %v", got, err)
	}

	request := completionRequest(t)
	request.Comparison.Fields[3].Observed = observe.Known("125001.00 USD")
	request.Comparison.Fields[3].Verdict = observe.Mismatch
	request.Comparison.Verdict = observe.Mismatch
	got, err = reconcile.EvaluateCompletion(request)
	if err != nil || got.Route != reconcile.RouteDegraded {
		t.Fatalf("promotion drift route = %+v, %v; want DEGRADED", got, err)
	}
}

func TestTodo_RECON_002_Fault(t *testing.T) {
	cases := []struct {
		name string
		edit func(*reconcile.CompletionRequest)
		want reconcile.CompletionStatus
	}{
		{"stale evidence", func(r *reconcile.CompletionRequest) { r.Freshness = observe.FreshnessStale }, reconcile.StatusToCompletion(reconcile.StatusObserving)},
		{"unknown evidence", func(r *reconcile.CompletionRequest) {
			r.Comparison = observe.PilotComparison{Verdict: observe.Unknown, Fields: make([]observe.FieldComparison, 4)}
		}, reconcile.CompletionUnknown},
		{"insufficient coverage", func(r *reconcile.CompletionRequest) { r.Complete = false }, reconcile.CompletionPartial},
		{"expired without repair", func(r *reconcile.CompletionRequest) { r.Now = completionAt.Add(2 * time.Hour) }, reconcile.CompletionExpired},
		{"expired with repair", func(r *reconcile.CompletionRequest) {
			r.Job.RepairPolicy = "repair/promotion"
			r.Now = completionAt.Add(2 * time.Hour)
		}, reconcile.CompletionRepairRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := completionRequest(t)
			tc.edit(&request)
			got, err := reconcile.EvaluateCompletion(request)
			if err != nil || got.Status != tc.want || got.Route != reconcile.RouteDegraded {
				t.Fatalf("decision = %+v, %v; want %s/DEGRADED", got, err, tc.want)
			}
		})
	}
}

func TestTodo_RECON_002_Mutation(t *testing.T) {
	request := completionRequest(t)
	request.Comparison.Verdict = observe.Mismatch
	request.Comparison.Fields[0].Verdict = observe.Mismatch
	got, err := reconcile.EvaluateCompletion(request)
	if err != nil || got.Status != reconcile.CompletionMismatch || got.Route != reconcile.RouteDegraded {
		t.Fatalf("mutated comparison = %+v, %v; want MISMATCH/DEGRADED", got, err)
	}

	request = completionRequest(t)
	request.Freshness = observe.FreshnessUnknown
	got, err = reconcile.EvaluateCompletion(request)
	if err != nil || got.Terminal || !strings.Contains(got.Reason, "freshness") {
		t.Fatalf("mutated freshness = %+v, %v; want non-terminal freshness decision", got, err)
	}
}
