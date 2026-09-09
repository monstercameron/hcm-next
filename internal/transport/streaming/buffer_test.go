package streaming_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/streaming"
)

func TestBufferDeliversInFIFOOrder(t *testing.T) {
	b := streaming.NewBuffer[int](4)
	ctx := context.Background()
	for i := 1; i <= 4; i++ {
		if err := b.Push(ctx, streaming.Chunk[int]{Sequence: uint64(i), Value: i}); err != nil {
			t.Fatalf("Push(%d): %v", i, err)
		}
	}
	for i := 1; i <= 4; i++ {
		c, err := b.Pop(ctx)
		if err != nil {
			t.Fatalf("Pop: %v", err)
		}
		if c.Value != i {
			t.Fatalf("Pop() = %d, want %d", c.Value, i)
		}
	}
}

// TestBufferBlocksTheProducerAtItsDeclaredCapacity is the backpressure bound
// made checkable: a producer racing ahead of a consumer is throttled at the
// buffer's capacity rather than allowed to queue without limit. Pushing one
// chunk past capacity must not complete until the consumer drains one.
func TestBufferBlocksTheProducerAtItsDeclaredCapacity(t *testing.T) {
	const capacity = 3
	b := streaming.NewBuffer[int](capacity)
	ctx := context.Background()

	for i := 0; i < capacity; i++ {
		if err := b.Push(ctx, streaming.Chunk[int]{Sequence: uint64(i)}); err != nil {
			t.Fatalf("Push(%d): %v", i, err)
		}
	}
	if got := b.Len(); got != capacity {
		t.Fatalf("Len() = %d, want %d", got, capacity)
	}

	// The next push must block: prove it with a short deadline that expires
	// while nobody is draining.
	blockedCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if err := b.Push(blockedCtx, streaming.Chunk[int]{Sequence: capacity}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Push past capacity with no consumer = %v, want DeadlineExceeded", err)
	}
	if got := b.Len(); got != capacity {
		t.Fatalf("Len() after the blocked push = %d, want it to stay at the bound %d", got, capacity)
	}

	// Draining one slot unblocks exactly one further push - the bound holds,
	// it is not simply gone once touched once.
	if _, err := b.Pop(ctx); err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if err := b.Push(ctx, streaming.Chunk[int]{Sequence: capacity}); err != nil {
		t.Fatalf("Push after draining one slot: %v", err)
	}
	if got := b.Len(); got != capacity {
		t.Fatalf("Len() after refilling = %d, want %d", got, capacity)
	}
}

func TestBufferPopReportsClosed(t *testing.T) {
	b := streaming.NewBuffer[int](1)
	ctx := context.Background()
	if err := b.Push(ctx, streaming.Chunk[int]{Sequence: 1, Value: 7}); err != nil {
		t.Fatalf("Push: %v", err)
	}
	b.Close()

	// The chunk already queued is still delivered after Close.
	c, err := b.Pop(ctx)
	if err != nil {
		t.Fatalf("Pop after Close (queued chunk): %v", err)
	}
	if c.Value != 7 {
		t.Fatalf("Pop() = %d, want 7", c.Value)
	}
	// Once drained, a closed buffer reports ErrBufferClosed rather than
	// blocking forever.
	if _, err := b.Pop(ctx); !errors.Is(err, streaming.ErrBufferClosed) {
		t.Fatalf("Pop on a drained, closed buffer = %v, want ErrBufferClosed", err)
	}
}

func TestBufferPopHonoursContextCancellation(t *testing.T) {
	b := streaming.NewBuffer[int](1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.Pop(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Pop with a cancelled context = %v, want Canceled", err)
	}
}

func TestNewBufferRaisesANonPositiveCapacityToOne(t *testing.T) {
	for _, capacity := range []int{0, -5} {
		b := streaming.NewBuffer[int](capacity)
		if got := b.Cap(); got != 1 {
			t.Fatalf("NewBuffer(%d).Cap() = %d, want 1", capacity, got)
		}
	}
}
