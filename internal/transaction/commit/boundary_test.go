package commit_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transactioncancel "github.com/monstercameron/human-capital-management-suite/internal/transaction/cancel"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/plan"
)

type retryableBoundaryError struct{}

func (retryableBoundaryError) Error() string    { return "serialization" }
func (retryableBoundaryError) SQLState() string { return "40001" }

func TestCommitGovernedContract(t *testing.T) {
	_ = context.Background()
	_ = pgtest.New
	_ = uuid.Nil
	_ = strings.Repeat
	_ = time.Time{}
	_ = values.NewInstant
	_ = transactioncancel.Version
	_ = transactioncommit.Version
	_ = plan.Version
}

func TestRetryClosureBoundary(t *testing.T) {
	t.Run("fresh admitted attempts", func(t *testing.T) {
		attempts, admissions := 0, 0
		err := transactioncommit.RetryClosure(context.Background(), transactioncommit.RetryOptions{
			MaxAttempts: 2,
			Admit: func(context.Context) error {
				admissions++
				return nil
			},
			Sleep: func(context.Context, time.Duration) error { return nil },
		}, func(context.Context) error {
			attempts++
			if attempts == 1 {
				return retryableBoundaryError{}
			}
			return nil
		})
		if err != nil || attempts != 2 || admissions != 2 {
			t.Fatalf("err=%v attempts=%d admissions=%d", err, attempts, admissions)
		}
	})

	t.Run("ambiguity is terminal even when wrapping retryable state", func(t *testing.T) {
		attempts := 0
		err := transactioncommit.RetryClosure(context.Background(), transactioncommit.RetryOptions{
			MaxAttempts: 3,
			Admit:       func(context.Context) error { return nil },
		}, func(context.Context) error {
			attempts++
			return fmt.Errorf("%w: %w", transactioncommit.ErrCommitAmbiguous, retryableBoundaryError{})
		})
		if !errors.Is(err, transactioncommit.ErrCommitAmbiguous) || attempts != 1 {
			t.Fatalf("err=%v attempts=%d", err, attempts)
		}
	})
}
