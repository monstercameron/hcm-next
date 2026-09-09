package streaming_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/streaming"
)

// TestTodo_PROTO_007 is PROTO-007's primary proof, end to end within this
// package: a resumable, ordered stream whose reconnect delivers exactly the
// chunks after the presented cursor - no duplicate, no gap - whose forged,
// expired or foreign cursor is refused rather than accepted, whose buffer
// never grows past its declared bound under a slow consumer, and whose
// authorization revocation ends with the typed [streaming.ErrRevoked]
// status rather than an ordinary read failure.
func TestTodo_PROTO_007(t *testing.T) {
	key := []byte("proto-007-primary-signing-key-32")
	signer, err := streaming.NewSigner(key)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	const tenant = "tenant-primary"
	const streamID = "journey/intent-primary"
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	t.Run("reconnect resumes with no gap and no duplicate", func(t *testing.T) {
		producer := streaming.NewProducer[string](signer, tenant, streamID, 5*time.Minute, clock)
		var tracker streaming.OrderTracker
		var lastCursor string
		for i := 0; i < 3; i++ {
			chunk, err := producer.Next("payload", false)
			if err != nil {
				t.Fatalf("Next: %v", err)
			}
			if err := tracker.Accept(chunk.Sequence); err != nil {
				t.Fatalf("Accept(%d): %v", chunk.Sequence, err)
			}
			lastCursor = chunk.Cursor
		}
		if got, _ := tracker.Last(); got != 3 {
			t.Fatalf("delivered through sequence %d, want 3", got)
		}

		// Reconnect: a fresh producer for the same stream, positioned by the
		// cursor the consumer last actually held, must continue the
		// sequence rather than restart or skip it.
		resumed := streaming.NewProducer[string](signer, tenant, streamID, 5*time.Minute, clock)
		if err := resumed.Resume(lastCursor); err != nil {
			t.Fatalf("Resume: %v", err)
		}
		if got := resumed.Sequence(); got != 3 {
			t.Fatalf("Resume positioned the producer at sequence %d, want 3", got)
		}
		for i := 0; i < 2; i++ {
			chunk, err := resumed.Next("payload", i == 1)
			if err != nil {
				t.Fatalf("Next after resume: %v", err)
			}
			if err := tracker.Accept(chunk.Sequence); err != nil {
				t.Fatalf("resumed Accept(%d): %v", chunk.Sequence, err)
			}
		}
		if got, _ := tracker.Last(); got != 5 {
			t.Fatalf("after resume, delivered through sequence %d, want 5 (no gap, no duplicate across reconnect)", got)
		}
	})

	t.Run("a forged, expired or foreign cursor refuses to resume", func(t *testing.T) {
		producer := streaming.NewProducer[string](signer, tenant, streamID, time.Minute, clock)
		chunk, err := producer.Next("v", false)
		if err != nil {
			t.Fatalf("Next: %v", err)
		}

		forged := chunk.Cursor[:len(chunk.Cursor)-1] + flipLastRune(chunk.Cursor)
		forgedErr := streaming.NewProducer[string](signer, tenant, streamID, time.Minute, clock).Resume(forged)
		if !errors.Is(forgedErr, streaming.ErrCursorForged) && !errors.Is(forgedErr, streaming.ErrCursorMalformed) {
			t.Fatalf("Resume(forged) = %v, want a decode refusal", forgedErr)
		}

		expiredProducer := streaming.NewProducer[string](signer, tenant, streamID, -time.Second, clock)
		expiredChunk, err := expiredProducer.Next("v", false)
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if err := streaming.NewProducer[string](signer, tenant, streamID, time.Minute, clock).Resume(expiredChunk.Cursor); !errors.Is(err, streaming.ErrCursorExpired) {
			t.Fatalf("Resume(expired) = %v, want ErrCursorExpired", err)
		}

		if err := streaming.NewProducer[string](signer, "another-tenant", streamID, time.Minute, clock).Resume(chunk.Cursor); !errors.Is(err, streaming.ErrCursorForeign) {
			t.Fatalf("Resume(foreign tenant) = %v, want ErrCursorForeign", err)
		}
		if err := streaming.NewProducer[string](signer, tenant, "another-stream", time.Minute, clock).Resume(chunk.Cursor); !errors.Is(err, streaming.ErrCursorForeign) {
			t.Fatalf("Resume(foreign stream) = %v, want ErrCursorForeign", err)
		}

		// A refused Resume must not move the producer's own sequence: it
		// still mints its very next chunk as sequence 1, not 4 (which would
		// mean a bad cursor could fast-forward a stream that never issued
		// it) and not chunk.Sequence (which would mean it silently rewound).
		untouched := streaming.NewProducer[string](signer, tenant, streamID, time.Minute, clock)
		_ = untouched.Resume(forged)
		next, err := untouched.Next("v", false)
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if next.Sequence != 1 {
			t.Fatalf("after a refused Resume, Next() minted sequence %d, want 1", next.Sequence)
		}
	})

	t.Run("backpressure bounds the buffer under a slow consumer", func(t *testing.T) {
		producer := streaming.NewProducer[int](signer, tenant, streamID, time.Minute, clock)
		buf := streaming.NewBuffer[int](2)
		ctx := context.Background()
		for i := 0; i < 2; i++ {
			chunk, err := producer.Next(i, false)
			if err != nil {
				t.Fatalf("Next: %v", err)
			}
			if err := buf.Push(ctx, chunk); err != nil {
				t.Fatalf("Push: %v", err)
			}
		}
		chunk, err := producer.Next(2, false)
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		blocked, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
		defer cancel()
		if err := buf.Push(blocked, chunk); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Push past the declared capacity = %v, want it to block until the deadline (never unbounded growth)", err)
		}
	})

	t.Run("revocation terminates with a typed status distinct from an ordinary refusal", func(t *testing.T) {
		check := func() error { return errors.New("credential revoked mid-stream") }
		terminated := streaming.Terminate(check())
		if !errors.Is(terminated, streaming.ErrRevoked) {
			t.Fatalf("Terminate(revoked) = %v, want ErrRevoked", terminated)
		}
		if err := streaming.Terminate(nil); err != nil {
			t.Fatalf("Terminate(nil) = %v, want nil - authorization still holds", err)
		}
	})
}

// flipLastRune returns a replacement for token's last character that is
// different from it, so appending it after truncating the original produces
// a token that differs by exactly one character - a tamper, not a coincidental
// no-op.
func flipLastRune(token string) string {
	if len(token) == 0 {
		return "x"
	}
	if token[len(token)-1] == 'x' {
		return "y"
	}
	return "x"
}
