package streaming_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/streaming"
)

func TestOperationStateTerminal(t *testing.T) {
	cases := map[streaming.OperationState]bool{
		streaming.OperationPending:               false,
		streaming.OperationRunning:               false,
		streaming.OperationCancellationRequested: false,
		streaming.OperationSucceeded:             true,
		streaming.OperationFailed:                true,
		streaming.OperationCancelled:             true,
	}
	for state, want := range cases {
		if got := state.Terminal(); got != want {
			t.Errorf("%s.Terminal() = %v, want %v", state, got, want)
		}
	}
}

func TestOperationValidateRequiresAnID(t *testing.T) {
	op := streaming.Operation{State: streaming.OperationRunning, RetryAfter: time.Second}
	if err := op.Validate(); !errors.Is(err, streaming.ErrOperationNoID) {
		t.Fatalf("Validate() = %v, want ErrOperationNoID", err)
	}
}

func TestOperationValidateRejectsAnUnknownState(t *testing.T) {
	op := streaming.Operation{ID: "op-1", State: "SOMETHING_ELSE", RetryAfter: time.Second}
	if err := op.Validate(); !errors.Is(err, streaming.ErrOperationUnknownState) {
		t.Fatalf("Validate() = %v, want ErrOperationUnknownState", err)
	}
}

func TestOperationValidateRequiresAPositiveRetryAfterWhileNonTerminal(t *testing.T) {
	op := streaming.Operation{ID: "op-1", State: streaming.OperationRunning, RetryAfter: 0}
	if err := op.Validate(); !errors.Is(err, streaming.ErrOperationNoRetryAfter) {
		t.Fatalf("Validate() = %v, want ErrOperationNoRetryAfter", err)
	}
}

func TestOperationValidateForbidsARetryAfterOnATerminalState(t *testing.T) {
	op := streaming.Operation{ID: "op-1", State: streaming.OperationSucceeded, RetryAfter: time.Second}
	if err := op.Validate(); !errors.Is(err, streaming.ErrOperationRetryOnTerminal) {
		t.Fatalf("Validate() = %v, want ErrOperationRetryOnTerminal", err)
	}
}

func TestOperationValidateAcceptsAWellFormedAnswer(t *testing.T) {
	running := streaming.Operation{ID: "op-1", State: streaming.OperationRunning, RetryAfter: time.Second}
	if err := running.Validate(); err != nil {
		t.Fatalf("Validate(running) = %v, want nil", err)
	}
	done := streaming.Operation{ID: "op-1", State: streaming.OperationSucceeded}
	if err := done.Validate(); err != nil {
		t.Fatalf("Validate(succeeded) = %v, want nil", err)
	}
}

func TestRetryAfterBoundClamp(t *testing.T) {
	b := streaming.RetryAfterBound{Min: 2 * time.Second, Max: 10 * time.Second}
	cases := []struct {
		in   time.Duration
		want time.Duration
	}{
		{time.Second, 2 * time.Second},
		{5 * time.Second, 5 * time.Second},
		{time.Minute, 10 * time.Second},
	}
	for _, c := range cases {
		if got := b.Clamp(c.in); got != c.want {
			t.Errorf("Clamp(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestRetryAfterBoundClampFallsBackToDefaults(t *testing.T) {
	var b streaming.RetryAfterBound
	if got := b.Clamp(0); got != streaming.DefaultMinRetryAfter {
		t.Fatalf("Clamp(0) with a zero-value bound = %v, want %v", got, streaming.DefaultMinRetryAfter)
	}
	if got := b.Clamp(time.Hour); got != streaming.DefaultMaxRetryAfter {
		t.Fatalf("Clamp(1h) with a zero-value bound = %v, want %v", got, streaming.DefaultMaxRetryAfter)
	}
}
