package streaming_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/transport/streaming"
)

func TestOrderTrackerAcceptsTheFirstSequenceUnconditionally(t *testing.T) {
	var tr streaming.OrderTracker
	if err := tr.Accept(9); err != nil {
		t.Fatalf("Accept(9) on a fresh tracker = %v, want nil", err)
	}
	last, ok := tr.Last()
	if !ok || last != 9 {
		t.Fatalf("Last() = (%d, %v), want (9, true)", last, ok)
	}
}

func TestOrderTrackerAcceptsAConsecutiveRun(t *testing.T) {
	var tr streaming.OrderTracker
	for seq := uint64(1); seq <= 5; seq++ {
		if err := tr.Accept(seq); err != nil {
			t.Fatalf("Accept(%d) = %v, want nil", seq, err)
		}
	}
	if last, _ := tr.Last(); last != 5 {
		t.Fatalf("Last() = %d, want 5", last)
	}
}

func TestOrderTrackerRefusesADuplicate(t *testing.T) {
	var tr streaming.OrderTracker
	must(t, tr.Accept(1))
	must(t, tr.Accept(2))
	if err := tr.Accept(2); err == nil {
		t.Fatal("Accept(2) a second time succeeded, want ErrChunkDuplicate")
	} else if !errors.Is(err, streaming.ErrChunkDuplicate) {
		t.Fatalf("Accept(2) again = %v, want ErrChunkDuplicate", err)
	}
}

func TestOrderTrackerRefusesAReplay(t *testing.T) {
	var tr streaming.OrderTracker
	must(t, tr.Accept(5))
	if err := tr.Accept(3); !errors.Is(err, streaming.ErrChunkDuplicate) {
		t.Fatalf("Accept(3) after 5 = %v, want ErrChunkDuplicate", err)
	}
}

func TestOrderTrackerRefusesAGap(t *testing.T) {
	var tr streaming.OrderTracker
	must(t, tr.Accept(1))
	if err := tr.Accept(3); !errors.Is(err, streaming.ErrChunkGap) {
		t.Fatalf("Accept(3) after 1 = %v, want ErrChunkGap", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
