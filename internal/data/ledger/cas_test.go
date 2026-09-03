package ledger_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/hcm-next/internal/data/ledger"
)

// TestTodo_DATA_002 proves the stream-head compare-and-swap: concurrent
// appenders that expect the same head produce exactly one winner, every loser
// gets a typed conflict carrying the expected and actual sequences, and the
// stream is left with no gap.
func TestTodo_DATA_002(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	const appenders = 6

	// Each appender gets its own connection, so these are genuinely separate
	// sessions racing for the same head.
	conns := make([]*pgx.Conn, appenders)
	for i := range conns {
		conns[i] = f.db.NewConn(t)
	}

	type outcome struct {
		receipt ledger.AppendReceipt
		err     error
	}
	results := make([]outcome, appenders)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range appenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := f.request(0)
			<-start
			var receipt ledger.AppendReceipt
			err := f.inTxErr(conns[i], func(tx pgx.Tx) error {
				var appendErr error
				receipt, appendErr = ledger.Append(context.Background(), tx, req)
				return appendErr
			})
			results[i] = outcome{receipt: receipt, err: err}
		}()
	}
	close(start)
	wg.Wait()

	var winners int
	for i, got := range results {
		if got.err == nil {
			winners++
			if got.receipt.Sequence != 1 {
				t.Fatalf("appender %d won with sequence %d, want 1", i, got.receipt.Sequence)
			}
			continue
		}
		var stale ledger.ErrStaleStream
		if !errors.As(got.err, &stale) {
			t.Fatalf("appender %d failed with %v, want ErrStaleStream", i, got.err)
		}
		if stale.Expected != 0 {
			t.Fatalf("appender %d reports expected head %d, want 0", i, stale.Expected)
		}
		if stale.Actual != 1 {
			t.Fatalf("appender %d reports actual head %d, want 1", i, stale.Actual)
		}
		if stale.StreamKey != streamKey {
			t.Fatalf("appender %d reports stream %q, want %q", i, stale.StreamKey, streamKey)
		}
	}
	if winners != 1 {
		t.Fatalf("%d appenders won the same head, want exactly 1", winners)
	}

	if got := f.sequences(t); len(got) != 1 || got[0] != 1 {
		t.Fatalf("stream holds sequences %v, want [1]", got)
	}
	if head, _ := f.head(t); head != 1 {
		t.Fatalf("head is at %d, want 1", head)
	}
}

// TestTodo_DATA_002_Race drives a chain of contended rounds: in each round every
// appender expects the current head, one wins, and the sequence stays dense.
func TestTodo_DATA_002_Race(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	const rounds = 5
	const appenders = 4

	conns := make([]*pgx.Conn, appenders)
	for i := range conns {
		conns[i] = f.db.NewConn(t)
	}

	for round := range rounds {
		expected := int64(round)

		var mu sync.Mutex
		var winners int

		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range appenders {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := f.request(expected)
				<-start
				err := f.inTxErr(conns[i], func(tx pgx.Tx) error {
					_, appendErr := ledger.Append(context.Background(), tx, req)
					return appendErr
				})
				mu.Lock()
				defer mu.Unlock()
				if err == nil {
					winners++
					return
				}
				var stale ledger.ErrStaleStream
				if !errors.As(err, &stale) {
					t.Errorf("round %d: appender failed with %v, want ErrStaleStream", round, err)
					return
				}
				if stale.Expected != expected || stale.Actual != expected+1 {
					t.Errorf("round %d: conflict reports expected %d actual %d, want %d and %d",
						round, stale.Expected, stale.Actual, expected, expected+1)
				}
			}()
		}
		close(start)
		wg.Wait()

		if winners != 1 {
			t.Fatalf("round %d had %d winners, want 1", round, winners)
		}
	}

	got := f.sequences(t)
	if len(got) != rounds {
		t.Fatalf("stream holds %d events, want %d", len(got), rounds)
	}
	for i, sequence := range got {
		if sequence != int64(i+1) {
			t.Fatalf("sequence %d in the stream is %d; the stream has a gap", i, sequence)
		}
	}
	if head, _ := f.head(t); head != int64(rounds) {
		t.Fatalf("head is at %d, want %d", head, rounds)
	}
}

// TestTodo_DATA_002_Golden proves the head digest is the digest of the event the
// head points at, so a reader can tell which assertion the head describes.
func TestTodo_DATA_002_Golden(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	first := f.mustAppend(t, f.request(0))
	head, digest := f.head(t)
	if head != first.Sequence || digest != first.Digest {
		t.Fatalf("head %d/%s does not describe event %d/%s", head, digest, first.Sequence, first.Digest)
	}

	second := f.mustAppend(t, f.request(1))
	head, digest = f.head(t)
	if head != second.Sequence || digest != second.Digest {
		t.Fatalf("head %d/%s does not describe event %d/%s", head, digest, second.Sequence, second.Digest)
	}
}

// TestTodo_DATA_002_Mutation proves the compare-and-swap is exact: expecting a
// head above or below the real one is refused, not rounded to the nearest.
func TestTodo_DATA_002_Mutation(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.mustAppend(t, f.request(0))
	f.mustAppend(t, f.request(1))

	for _, expected := range []int64{0, 1, 3, 99} {
		_, err := f.append(t, f.request(expected))
		var stale ledger.ErrStaleStream
		if !errors.As(err, &stale) {
			t.Fatalf("expecting head %d returned %v, want ErrStaleStream", expected, err)
		}
		if stale.Actual != 2 {
			t.Fatalf("expecting head %d reports actual %d, want 2", expected, stale.Actual)
		}
	}

	if got := f.sequences(t); len(got) != 2 {
		t.Fatalf("stream holds %v after refused appends, want two events", got)
	}
}
