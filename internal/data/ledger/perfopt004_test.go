package ledger_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
)

func perfRequest(f fixture, stream string, expected int64, key string) ledger.AppendRequest {
	req := f.request(expected)
	req.StreamKey = stream
	req.IdempotencyKey = key
	return req
}

func perfRequestStreams(f fixture) []ledger.StreamAppend {
	return []ledger.StreamAppend{
		{StreamKey: "worker:2", ExpectedHead: 0, Events: []ledger.AppendRequest{
			perfRequest(f, "worker:2", 0, "perf:2:1"), perfRequest(f, "worker:2", 1, "perf:2:2"),
			perfRequest(f, "worker:2", 2, "perf:2:3"), perfRequest(f, "worker:2", 3, "perf:2:4"),
		}},
		{StreamKey: "worker:0", ExpectedHead: 0, Events: []ledger.AppendRequest{
			perfRequest(f, "worker:0", 0, "perf:0:1"), perfRequest(f, "worker:0", 1, "perf:0:2"),
			perfRequest(f, "worker:0", 2, "perf:0:3"),
		}},
		{StreamKey: streamKey, ExpectedHead: 0, Events: []ledger.AppendRequest{
			perfRequest(f, streamKey, 0, "perf:1:1"), perfRequest(f, streamKey, 1, "perf:1:2"),
			perfRequest(f, streamKey, 2, "perf:1:3"),
		}},
	}
}

func TestTodo_PERFOPT_004(t *testing.T) {
	f := newFixture(t)
	registerMultiStreams(t, f, "worker:0", "worker:2")
	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := ledger.AppendMulti(ctx, tx, ledger.MultiStreamAppendRequest{
		Tenant: f.tenant, Streams: perfRequestStreams(f),
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("AppendMulti: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	wantBytes, err := os.ReadFile(filepath.Join("testdata", "perfopt004_receipts.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(string(wantBytes))
	if got := perfReceiptGolden(receipt); got != want {
		t.Fatalf("receipt golden mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func perfReceiptGolden(receipt ledger.MultiStreamAppendReceipt) string {
	var b strings.Builder
	for _, event := range receipt.Events {
		b.WriteString("event|")
		b.WriteString(event.StreamKey)
		b.WriteString("|")
		b.WriteString(formatInt(event.Sequence))
		b.WriteString("|")
		b.WriteString(formatInt(event.PreviousHead))
		b.WriteString("|")
		b.WriteString(event.Digest)
		b.WriteString("|")
		b.WriteString(event.DigestAlgorithm)
		b.WriteString("|")
		b.WriteString(formatInt(int64(event.CanonicalLength)))
		b.WriteString("|")
		b.WriteString(boolText(event.Replayed))
		b.WriteByte('\n')
	}
	for _, head := range receipt.Heads {
		b.WriteString("head|")
		b.WriteString(head.StreamKey)
		b.WriteString("|")
		b.WriteString(formatInt(head.Before))
		b.WriteString("|")
		b.WriteString(formatInt(head.After))
		b.WriteString("|")
		b.WriteString(head.BeforeDigest)
		b.WriteString("|")
		b.WriteString(head.AfterDigest)
		b.WriteByte('\n')
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func formatInt(n int64) string { return strconv.FormatInt(n, 10) }

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func TestTodo_PERFOPT_004_Integration(t *testing.T) {
	f := newFixture(t)
	registerMultiStreams(t, f, "worker:0", "worker:2")
	ctx := context.Background()

	t.Run("stale second stream rolls back first stream", func(t *testing.T) {
		tx, err := f.db.Conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = ledger.AppendMulti(ctx, tx, ledger.MultiStreamAppendRequest{Tenant: f.tenant, Streams: []ledger.StreamAppend{
			{StreamKey: "worker:0", ExpectedHead: 0, Events: []ledger.AppendRequest{perfRequest(f, "worker:0", 0, "perf:stale:0")}},
			{StreamKey: "worker:2", ExpectedHead: 9, Events: []ledger.AppendRequest{perfRequest(f, "worker:2", 9, "perf:stale:2")}},
		}})
		var stale ledger.ErrStaleStream
		if !errors.As(err, &stale) || stale.StreamKey != "worker:2" {
			_ = tx.Rollback(ctx)
			t.Fatalf("error = %v, want stale worker:2", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := f.db.QueryRow(ctx, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key = 'worker:0'`, f.tenant).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("worker:0 events = %d, want 0", count)
		}
	})

	t.Run("duplicate idempotency key rolls back the batch", func(t *testing.T) {
		tx, err := f.db.Conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		first := perfRequest(f, "worker:0", 0, "perf:duplicate")
		second := perfRequest(f, "worker:0", 1, "perf:duplicate")
		_, err = ledger.AppendMulti(ctx, tx, ledger.MultiStreamAppendRequest{Tenant: f.tenant, Streams: []ledger.StreamAppend{
			{StreamKey: "worker:0", ExpectedHead: 0, Events: []ledger.AppendRequest{first, second}},
		}})
		if err == nil {
			_ = tx.Rollback(ctx)
			t.Fatal("duplicate idempotency key was accepted")
		}
		if rollbackErr := tx.Commit(ctx); rollbackErr != nil {
			t.Fatal(rollbackErr)
		}
		var count int
		if err := f.db.QueryRow(ctx, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key = 'worker:0'`, f.tenant).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("worker:0 events after duplicate = %d, want 0", count)
		}
	})
}

func TestTodo_PERFOPT_004_Race(t *testing.T) {
	f := newFixture(t)
	registerMultiStreams(t, f, "worker:0", "worker:2")
	start := make(chan struct{})
	results := make(chan error, 2)
	connections := []*pgxadapter.Conn{f.db.NewConn(t), f.db.NewConn(t)}
	for i := 0; i < 2; i++ {
		i := i
		conn := connections[i]
		go func() {
			<-start
			tx, err := conn.Begin(context.Background())
			if err != nil {
				results <- err
				return
			}
			_, err = ledger.AppendMulti(context.Background(), tx, ledger.MultiStreamAppendRequest{Tenant: f.tenant, Streams: []ledger.StreamAppend{
				{StreamKey: "worker:2", ExpectedHead: 0, Events: []ledger.AppendRequest{perfRequest(f, "worker:2", 0, "perf:race:"+strconv.Itoa(i)+":2")}},
				{StreamKey: "worker:0", ExpectedHead: 0, Events: []ledger.AppendRequest{perfRequest(f, "worker:0", 0, "perf:race:"+strconv.Itoa(i)+":0")}},
			}})
			if err == nil {
				err = tx.Commit(context.Background())
			} else {
				_ = tx.Rollback(context.Background())
			}
			results <- err
		}()
	}
	close(start)
	var winners, losers int
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			winners++
		} else {
			var stale ledger.ErrStaleStream
			if !errors.As(err, &stale) {
				t.Fatalf("race error = %v, want stale stream", err)
			}
			losers++
		}
	}
	if winners != 1 || losers != 1 {
		t.Fatalf("race winners/losers = %d/%d, want 1/1", winners, losers)
	}
}

func BenchmarkTodo_PERFOPT_004(b *testing.B) {
	request := perfBenchmarkRequest()
	var totalStatements int64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := &perfBenchmarkTx{}
		if _, err := ledger.AppendMulti(context.Background(), tx, request); err != nil {
			b.Fatal(err)
		}
		totalStatements += int64(tx.statements + tx.queued)
	}
	b.ReportMetric(float64(totalStatements)/float64(b.N), "statements/op")
}

func perfBenchmarkRequest() ledger.MultiStreamAppendRequest {
	tenant := uuid.New()
	makeStream := func(key string, count int) ledger.StreamAppend {
		events := make([]ledger.AppendRequest, count)
		for i := range events {
			events[i] = ledger.AppendRequest{
				Tenant: tenant, StreamKey: key, ExpectedHead: int64(i),
				AssertionClass: ledger.TransactionFact, SourceRef: "benchmark",
				SchemaRef: schemaRef, Payload: []byte("promotion-proposed"),
				OccurredAt:    time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC),
				EffectiveAt:   time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
				CorrelationID: uuid.New(), IdempotencyKey: key + ":" + strconv.Itoa(i),
			}
		}
		return ledger.StreamAppend{StreamKey: key, ExpectedHead: 0, Events: events}
	}
	return ledger.MultiStreamAppendRequest{Tenant: tenant, Streams: []ledger.StreamAppend{
		makeStream("worker:2", 4), makeStream("worker:0", 3), makeStream("worker:1", 3),
	}}
}

type perfBenchmarkTx struct{ statements, queued int }

func (tx *perfBenchmarkTx) Exec(context.Context, string, ...any) (int64, error) {
	tx.statements++
	return 1, nil
}

func (tx *perfBenchmarkTx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	tx.statements++
	return &perfBenchmarkRows{keys: []string{"worker:0", "worker:1", "worker:2"}}, nil
}

func (tx *perfBenchmarkTx) QueryRow(context.Context, string, ...any) dbport.Row {
	tx.statements++
	return perfBenchmarkNoRows{}
}

func (tx *perfBenchmarkTx) Commit(context.Context) error   { return nil }
func (tx *perfBenchmarkTx) Rollback(context.Context) error { return nil }

func (tx *perfBenchmarkTx) SendBatch(_ context.Context, fn func(dbport.Batch)) (dbport.BatchResults, error) {
	b := &perfBenchmarkBatch{tx: tx}
	fn(b)
	return &perfBenchmarkResults{count: b.count}, nil
}

type perfBenchmarkBatch struct {
	tx    *perfBenchmarkTx
	count int
}

func (b *perfBenchmarkBatch) Queue(string, ...any) { b.count++; b.tx.queued++ }

type perfBenchmarkResults struct{ count, index int }

func (r *perfBenchmarkResults) Exec() (int64, error) {
	r.index++
	return 1, nil
}
func (r *perfBenchmarkResults) QueryRow() dbport.Row { return perfBenchmarkNoRows{} }
func (r *perfBenchmarkResults) Close() error         { return nil }

type perfBenchmarkNoRows struct{}

func (perfBenchmarkNoRows) Scan(...any) error { return dbport.ErrNoRows }

type perfBenchmarkRows struct {
	keys  []string
	index int
}

func (r *perfBenchmarkRows) Next() bool {
	return r.index < len(r.keys)
}

func (r *perfBenchmarkRows) Scan(dest ...any) error {
	if len(dest) != 3 {
		return errors.New("benchmark rows: want three destinations")
	}
	*(dest[0].(*string)) = r.keys[r.index]
	*(dest[1].(*int64)) = 0
	*(dest[2].(**string)) = nil
	r.index++
	return nil
}

func (r *perfBenchmarkRows) Err() error { return nil }
func (r *perfBenchmarkRows) Close()     {}
