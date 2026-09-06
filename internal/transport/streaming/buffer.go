package streaming

import (
	"context"
	"errors"
)

// ErrBufferClosed reports that [Buffer.Pop] drained the last chunk a closed
// buffer will ever hold.
var ErrBufferClosed = errors.New("streaming: buffer is closed")

// Buffer is a fixed-capacity, ordered queue of chunks between one producer
// and one consumer. Its capacity is the declared backpressure bound: [Push]
// blocks the producer once the buffer holds that many undelivered chunks,
// until the consumer drains one, the context is cancelled, or the buffer is
// closed. Nothing about it grows past that bound, which is the whole
// property this type exists to make true by construction rather than by
// convention - a producer that computes chunks faster than a slow consumer
// reads them is throttled, never allowed to pile up unbounded memory.
//
// A Buffer is single-producer, single-consumer: it is the plumbing under one
// stream, not a general work queue. Concurrent producers or concurrent
// consumers would still work individually (a Go channel tolerates it) but
// the ordering half of the contract stops meaning anything once more than
// one goroutine can send or receive.
type Buffer[T any] struct {
	ch chan Chunk[T]
}

// NewBuffer returns a [Buffer] holding up to capacity chunks before [Push]
// blocks. A capacity below 1 is raised to 1: a buffer of zero would either
// forbid every send outright or (with an unbuffered channel) require a
// concurrently-blocked receiver for every single push, neither of which is a
// bound a producer can reason about as "backpressure" rather than "broken".
func NewBuffer[T any](capacity int) *Buffer[T] {
	if capacity < 1 {
		capacity = 1
	}
	return &Buffer[T]{ch: make(chan Chunk[T], capacity)}
}

// Push enqueues c, blocking until there is room, ctx is done, or the buffer
// is closed. It never queues more than the buffer's declared capacity.
func (b *Buffer[T]) Push(ctx context.Context, c Chunk[T]) error {
	select {
	case b.ch <- c:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Pop dequeues the next chunk, blocking until one arrives, ctx is done, or
// the buffer is closed and drained ([ErrBufferClosed]).
func (b *Buffer[T]) Pop(ctx context.Context) (Chunk[T], error) {
	select {
	case c, ok := <-b.ch:
		if !ok {
			return Chunk[T]{}, ErrBufferClosed
		}
		return c, nil
	case <-ctx.Done():
		var zero Chunk[T]
		return zero, ctx.Err()
	}
}

// Close reports no more chunks will be pushed. A [Push] racing a concurrent
// Close is the caller's own synchronization bug, exactly as sending on a
// closed Go channel is; Close itself never blocks and never panics.
func (b *Buffer[T]) Close() { close(b.ch) }

// Len reports how many chunks are queued right now.
func (b *Buffer[T]) Len() int { return len(b.ch) }

// Cap reports the buffer's declared capacity, i.e. the backpressure bound
// passed to [NewBuffer].
func (b *Buffer[T]) Cap() int { return cap(b.ch) }
