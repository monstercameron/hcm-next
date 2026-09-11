package dbport

import (
	"context"
	"errors"
	"testing"
)

type batchTestExecer struct {
	args [][]any
}

func (e *batchTestExecer) Exec(_ context.Context, sql string, args ...any) (int64, error) {
	e.args = append(e.args, append([]any{sql}, args...))
	return int64(len(e.args)), nil
}

type batchTestResults struct {
	counts []int64
	errAt  int
	closed bool
	index  int
}

func (r *batchTestResults) Exec() (int64, error) {
	if r.errAt >= 0 && r.index == r.errAt {
		return 0, errors.New("batch exec failed")
	}
	count := r.counts[r.index]
	r.index++
	return count, nil
}

func (r *batchTestResults) QueryRow() Row { return mockRow{} }

func (r *batchTestResults) Close() error {
	r.closed = true
	return nil
}

type batchTestBatcher struct {
	batchTestExecer
	queued []Statement
	res    *batchTestResults
}

func (b *batchTestBatcher) SendBatch(_ context.Context, fn func(Batch)) (BatchResults, error) {
	fn(batchQueueFunc(func(sql string, args ...any) {
		b.queued = append(b.queued, Statement{SQL: sql, Args: args})
	}))
	return b.res, nil
}

type batchQueueFunc func(string, ...any)

func (f batchQueueFunc) Queue(sql string, args ...any) { f(sql, args...) }

func TestTodo_PERFOPT_004_ExecAllFallback(t *testing.T) {
	ex := &batchTestExecer{}
	counts, err := ExecAll(context.Background(), ex, []Statement{
		{SQL: "one", Args: []any{1}},
		{SQL: "two", Args: []any{2}},
	})
	if err != nil {
		t.Fatalf("ExecAll fallback: %v", err)
	}
	if len(counts) != 2 || counts[0] != 1 || counts[1] != 2 {
		t.Fatalf("counts = %v, want [1 2]", counts)
	}
	if len(ex.args) != 2 || ex.args[0][0] != "one" || ex.args[1][0] != "two" {
		t.Fatalf("fallback calls = %v, want two ordered calls", ex.args)
	}
}

func TestTodo_PERFOPT_004_ExecAllBatch(t *testing.T) {
	res := &batchTestResults{counts: []int64{3, 4}, errAt: -1}
	ex := &batchTestBatcher{res: res}
	counts, err := ExecAll(context.Background(), ex, []Statement{{SQL: "one"}, {SQL: "two", Args: []any{"arg"}}})
	if err != nil {
		t.Fatalf("ExecAll batch: %v", err)
	}
	if len(ex.queued) != 2 || ex.queued[1].SQL != "two" || ex.queued[1].Args[0] != "arg" {
		t.Fatalf("queued = %+v, want ordered batch", ex.queued)
	}
	if len(counts) != 2 || counts[0] != 3 || counts[1] != 4 || !res.closed {
		t.Fatalf("counts/close = %v/%v, want [3 4]/true", counts, res.closed)
	}
}

func TestTodo_PERFOPT_004_ExecAllBatchError(t *testing.T) {
	res := &batchTestResults{counts: []int64{3, 4}, errAt: 1}
	ex := &batchTestBatcher{res: res}
	counts, err := ExecAll(context.Background(), ex, []Statement{{SQL: "one"}, {SQL: "two"}})
	if err == nil || err.Error() != "batch exec failed" {
		t.Fatalf("error = %v, want batch exec failed", err)
	}
	if len(counts) != 1 || counts[0] != 3 || !res.closed {
		t.Fatalf("partial counts/close = %v/%v, want [3]/true", counts, res.closed)
	}
}
