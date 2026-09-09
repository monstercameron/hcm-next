package execution

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

func (a executeDriverAdapter) ResumeTimer(ctx context.Context, req app.ExecutionTimerResumeRequest) (app.ExecutionResult, error) {
	result, err := a.driver.ResumeTimer(ctx, execute.ResumeTimerRequest{
		Start: req.Start, InstanceID: req.InstanceID,
		ExpectedInstanceVersion: req.ExpectedInstanceVersion, TimerID: req.TimerID,
		Outcome: req.Outcome, Refs: req.Refs, RecordedAt: req.RecordedAt,
	})
	if err != nil {
		return app.ExecutionResult{}, err
	}
	return adaptExecutionResult(result, req.InstanceID.String()), nil
}
