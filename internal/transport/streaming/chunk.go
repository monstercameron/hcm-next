package streaming

import (
	"errors"
	"fmt"
)

// Chunk is one ordered unit of a resumable stream: the sequence number it
// occupies, the cursor a consumer presents to resume after it, whether it is
// the stream's last chunk, and the value it carries.
//
// Sequence and Cursor answer different questions on purpose. Sequence is
// what an [OrderTracker] compares to detect a gap or a duplicate; Cursor is
// the opaque, signed token a client actually holds onto and presents back -
// a client is never expected to reconnect quoting a bare sequence number,
// because a bare integer carries no proof it was ever issued to that client
// for that stream.
type Chunk[T any] struct {
	Sequence uint64
	Cursor   string
	Terminal bool
	Value    T
}

// Sentinel errors [OrderTracker.Accept] returns.
var (
	// ErrChunkDuplicate reports a sequence number the tracker has already
	// accepted (an exact repeat) or one behind the last it accepted (a
	// replay).
	ErrChunkDuplicate = errors.New("streaming: chunk sequence repeats one already delivered")
	// ErrChunkGap reports a sequence number that skips ahead of the next one
	// the tracker expects, meaning at least one chunk between them was never
	// delivered.
	ErrChunkGap = errors.New("streaming: chunk sequence skips ahead, leaving a gap")
)

// OrderTracker is the receive side of the ordering half of the Resume rule:
// a stream of accepted sequence numbers has no gap and no duplicate. It
// holds no buffer and inspects no payload; it is the one comparison a
// consumer (or a test standing in for one) needs to make per chunk to prove
// the property, independent of transport.
type OrderTracker struct {
	last    uint64
	started bool
}

// Accept records seq as delivered, or reports why it cannot be: a duplicate
// or a replay ([ErrChunkDuplicate]), or a chunk that leaves a gap
// ([ErrChunkGap]). The first call accepts any seq unconditionally, seeding
// the sequence a resumed tracker continues from.
func (o *OrderTracker) Accept(seq uint64) error {
	if !o.started {
		o.started = true
		o.last = seq
		return nil
	}
	switch {
	case seq <= o.last:
		return fmt.Errorf("%w: sequence %d, last delivered %d", ErrChunkDuplicate, seq, o.last)
	case seq != o.last+1:
		return fmt.Errorf("%w: sequence %d, last delivered %d", ErrChunkGap, seq, o.last)
	}
	o.last = seq
	return nil
}

// Last reports the last sequence number [Accept] recorded, and whether
// anything has been recorded at all.
func (o *OrderTracker) Last() (seq uint64, ok bool) {
	return o.last, o.started
}
