package ledger_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

func multiRequest(f fixture, stream string, expected int64, key string) ledger.AppendRequest {
	req := f.request(expected)
	req.StreamKey = stream
	req.IdempotencyKey = key
	return req
}

func registerMultiStreams(t *testing.T, f fixture, streams ...string) {
	t.Helper()
	f.inTx(t, func(tx dbport.Tx) error {
		for _, stream := range streams {
			if err := ledger.EnsureStream(context.Background(), tx, f.tenant, stream, "TRANSACTION", stream); err != nil {
				return err
			}
		}
		return nil
	})
}

// TestTodo_LEDGER_003 proves the all-or-none multi-stream append and the
// receipt's canonical stream-key ordering.
func TestTodo_LEDGER_003(t *testing.T) {
	f := newFixture(t)
	registerMultiStreams(t, f, "worker:0", "worker:2")
	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := ledger.AppendMulti(ctx, tx, ledger.MultiStreamAppendRequest{
		Tenant: f.tenant,
		Streams: []ledger.StreamAppend{
			{StreamKey: "worker:2", ExpectedHead: 0, Events: []ledger.AppendRequest{multiRequest(f, "worker:2", 0, "multi:2")}},
			{StreamKey: "worker:0", ExpectedHead: 0, Events: []ledger.AppendRequest{multiRequest(f, "worker:0", 0, "multi:0")}},
			{StreamKey: streamKey, ExpectedHead: 0, Events: []ledger.AppendRequest{f.request(0)}},
		},
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("AppendMulti: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	want := []string{"worker:0", "worker:1", "worker:2"}
	if len(receipt.Heads) != len(want) {
		t.Fatalf("heads = %+v, want %d heads", receipt.Heads, len(want))
	}
	for i, head := range receipt.Heads {
		if head.StreamKey != want[i] || head.Before != 0 || head.After != 1 {
			t.Fatalf("head[%d] = %+v, want %s 0->1", i, head, want[i])
		}
	}
	if len(receipt.Events) != 3 {
		t.Fatalf("event receipts = %d, want 3", len(receipt.Events))
	}
	for i, event := range receipt.Events {
		if event.StreamKey != want[i] || event.Sequence != 1 {
			t.Fatalf("event[%d] = %+v, want %s@1", i, event, want[i])
		}
	}
}

// TestTodo_LEDGER_003_Golden proves a stale later stream is rejected during
// preflight, before any earlier stream can receive an event, even if the
// caller chooses to commit the transaction after the refusal.
func TestTodo_LEDGER_003_Golden(t *testing.T) {
	f := newFixture(t)
	registerMultiStreams(t, f, "worker:0", "worker:2")
	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ledger.AppendMulti(ctx, tx, ledger.MultiStreamAppendRequest{
		Tenant: f.tenant,
		Streams: []ledger.StreamAppend{
			{StreamKey: "worker:0", ExpectedHead: 0, Events: []ledger.AppendRequest{multiRequest(f, "worker:0", 0, "stale:0")}},
			{StreamKey: "worker:2", ExpectedHead: 4, Events: []ledger.AppendRequest{multiRequest(f, "worker:2", 4, "stale:2")}},
		},
	})
	var stale ledger.ErrStaleStream
	if !errors.As(err, &stale) || stale.StreamKey != "worker:2" || stale.Expected != 4 || stale.Actual != 0 {
		_ = tx.Rollback(ctx)
		t.Fatalf("error = %v, want stale worker:2 expected 4 actual 0", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.db.Conn.QueryRow(ctx, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key IN ('worker:0', 'worker:2')`, f.tenant).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("events after stale preflight = %d, want 0", count)
	}
}

// TestTodo_LEDGER_003_Race proves two plans cannot both win the same set of
// stream heads and that the canonical lock order gives the loser a named
// stale stream rather than a deadlock or a partial batch.
func TestTodo_LEDGER_003_Race(t *testing.T) {
	f := newFixture(t)
	registerMultiStreams(t, f, "worker:0", "worker:2")
	connections := []*pgxadapter.Conn{f.db.NewConn(t), f.db.NewConn(t)}
	start := make(chan struct{})
	results := make(chan error, len(connections))
	for i, conn := range connections {
		i, conn := i, conn
		go func() {
			<-start
			ctx := context.Background()
			tx, beginErr := conn.Begin(ctx)
			if beginErr != nil {
				results <- beginErr
				return
			}
			_, appendErr := ledger.AppendMulti(ctx, tx, ledger.MultiStreamAppendRequest{
				Tenant: f.tenant,
				Streams: []ledger.StreamAppend{
					{StreamKey: "worker:2", ExpectedHead: 0, Events: []ledger.AppendRequest{multiRequest(f, "worker:2", 0, "race:"+string(rune('a'+i))+":2")}},
					{StreamKey: "worker:0", ExpectedHead: 0, Events: []ledger.AppendRequest{multiRequest(f, "worker:0", 0, "race:"+string(rune('a'+i))+":0")}},
				},
			})
			if appendErr == nil {
				appendErr = tx.Commit(ctx)
			} else {
				_ = tx.Rollback(ctx)
			}
			results <- appendErr
		}()
	}
	close(start)
	var winners, losers int
	for range connections {
		err := <-results
		if err == nil {
			winners++
			continue
		}
		var stale ledger.ErrStaleStream
		if !errors.As(err, &stale) || (stale.StreamKey != "worker:0" && stale.StreamKey != "worker:2") {
			t.Fatalf("race error = %v, want named stale stream", err)
		}
		losers++
	}
	if winners != 1 || losers != 1 {
		t.Fatalf("race winners/losers = %d/%d, want 1/1", winners, losers)
	}
	var count int
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key IN ('worker:0', 'worker:2')`, f.tenant).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("race events = %d, want exactly one per stream", count)
	}
}

// TestTodo_LEDGER_003_Mutation proves duplicate stream declarations are
// refused before the database is touched.
func TestTodo_LEDGER_003_Mutation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ledger.AppendMulti(ctx, tx, ledger.MultiStreamAppendRequest{
		Tenant: f.tenant,
		Streams: []ledger.StreamAppend{
			{StreamKey: streamKey, ExpectedHead: 0, Events: []ledger.AppendRequest{f.request(0)}},
			{StreamKey: streamKey, ExpectedHead: 0, Events: []ledger.AppendRequest{multiRequest(f, streamKey, 0, "duplicate")}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate stream") {
		_ = tx.Rollback(ctx)
		t.Fatalf("duplicate stream error = %v", err)
	}
	_ = tx.Rollback(ctx)
}
