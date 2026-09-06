package shadow_test

import (
	"context"
	"os"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/shadow"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type pgRowCounter struct{ db *pgtest.DB }

func (c pgRowCounter) Counts(ctx context.Context) (shadow.RowCounts, error) {
	var counts shadow.RowCounts
	queries := []struct {
		query string
		into  *int64
	}{
		{"SELECT count(*) FROM ledger_event", &counts.LedgerEvent},
		{"SELECT count(*) FROM work_item", &counts.WorkItem},
		{"SELECT count(*) FROM workflow_timer", &counts.WorkflowTimer},
		{"SELECT count(*) FROM outbox", &counts.Outbox},
	}
	for _, item := range queries {
		if err := c.db.QueryRow(ctx, item.query).Scan(item.into); err != nil {
			return shadow.RowCounts{}, err
		}
	}
	return counts, nil
}

func TestTodo_WF_RUN_014_PGTest(t *testing.T) {
	if os.Getenv(pgtest.EnvDatabaseURL) == "" {
		t.Skip("embedded PostgreSQL is unavailable on this Windows host; set HCMNEXT_TEST_DATABASE_URL to run the proof")
	}
	db := pgtest.New(t)
	contract, err := shadow.ContractFor(intent.EnvironmentTest)
	if err != nil {
		t.Fatal(err)
	}
	result, err := shadow.Run(context.Background(), plan(t), shadow.Options{
		Contract: contract,
		Steps: shadow.StepRunnerFunc(func(_ context.Context, req shadow.StepRequest) (shadow.StepResult, error) {
			if req.Node.Type == workflow.StepEnd {
				return shadow.StepResult{TerminalCode: "PROMOTION_WOULD_COMMIT"}, nil
			}
			if req.Node.Type == workflow.StepDecision {
				return shadow.StepResult{RouteKey: req.Node.Routes[0]}, nil
			}
			return shadow.StepResult{Outcome: workflow.OutcomeSucceeded}, nil
		}),
		RowCounter: pgRowCounter{db: db},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.RowCountsBefore.Equal(result.RowCountsAfter) {
		t.Fatalf("shadow changed durable counts: before=%#v after=%#v", result.RowCountsBefore, result.RowCountsAfter)
	}
	if result.Terminal.Code != shadow.TerminalNotExecuted {
		t.Fatalf("terminal code = %q, want %q", result.Terminal.Code, shadow.TerminalNotExecuted)
	}
}
