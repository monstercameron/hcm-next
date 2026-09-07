package dbport

import "context"

// Statement is one SQL statement and its positional arguments.
type Statement struct {
	SQL  string
	Args []any
}

// Batch queues statements for one round trip. It deliberately exposes only
// queueing: result interpretation stays with BatchResults and callers do not
// need to know which driver implements the capability.
type Batch interface {
	Queue(sql string, args ...any)
}

// Batcher is an optional write capability. Implementations execute queued
// statements in order and return one result per queued statement.
type Batcher interface {
	SendBatch(ctx context.Context, fn func(Batch)) (BatchResults, error)
}

// BatchResults is the small result surface shared by batched drivers.
type BatchResults interface {
	Exec() (rowsAffected int64, err error)
	QueryRow() Row
	Close() error
}

// ExecAll executes statements as one batch when ex supports Batcher and falls
// back to ordinary Exec calls otherwise. The returned counts contain every
// statement that completed before an error, which lets callers retain the
// original per-row error context.
func ExecAll(ctx context.Context, ex Execer, stmts []Statement) ([]int64, error) {
	if len(stmts) == 0 {
		return nil, nil
	}
	if batcher, ok := ex.(Batcher); ok {
		results, err := batcher.SendBatch(ctx, func(batch Batch) {
			for _, stmt := range stmts {
				batch.Queue(stmt.SQL, stmt.Args...)
			}
		})
		if err != nil {
			return nil, err
		}
		counts := make([]int64, 0, len(stmts))
		for range stmts {
			count, execErr := results.Exec()
			if execErr != nil {
				_ = results.Close()
				return counts, execErr
			}
			counts = append(counts, count)
		}
		if err := results.Close(); err != nil {
			return counts, err
		}
		return counts, nil
	}

	counts := make([]int64, 0, len(stmts))
	for _, stmt := range stmts {
		count, err := ex.Exec(ctx, stmt.SQL, stmt.Args...)
		if err != nil {
			return counts, err
		}
		counts = append(counts, count)
	}
	return counts, nil
}

// FailedStatement returns the zero-based index of the statement that did not
// complete when ExecAll returned counts for the statements before the error.
// When every statement returned a count (the error came from closing the
// batch), the last index is returned; when nothing was queued it is -1.
func FailedStatement(counts []int64, total int) int {
	if total <= 0 {
		return -1
	}
	if len(counts) >= total {
		return total - 1
	}
	return len(counts)
}
