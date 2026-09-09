package pgstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
)

// The correlation reader is an additive port the timer-resume path type-asserts
// against the store; it must keep this exact shape.
func TestTodo_PROMO_EXEC_TIMERDISPATCH_CorrelationReaderIsAdditive(t *testing.T) {
	var _ interface {
		LoadIntentByCorrelation(ctx context.Context, tenant, correlation string) (app.IntentRecord, error)
	} = (*Store)(nil)
}

// An empty correlation can never name an intent: it is refused as not found
// before any statement runs, so a caller cannot accidentally resolve "the
// first intent with no correlation".
func TestLoadIntentByCorrelationRefusesAnEmptyCorrelationBeforeAnyQuery(t *testing.T) {
	t.Parallel()
	s := &Store{}
	_, err := s.LoadIntentByCorrelation(context.Background(), "acme", "")
	if !errors.Is(err, app.ErrIntentNotFound) {
		t.Fatalf("err = %v, want ErrIntentNotFound", err)
	}
}
