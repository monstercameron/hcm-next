package execution

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
)

func TestTodo_PROMO_EXEC_TIMERDISPATCH_ExecuteDriverAdapterSatisfiesTimerPort(t *testing.T) {
	var _ interface {
		ResumeTimer(context.Context, app.ExecutionTimerResumeRequest) (app.ExecutionResult, error)
	} = executeDriverAdapter{}
}
