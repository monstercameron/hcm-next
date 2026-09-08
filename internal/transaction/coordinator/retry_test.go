package coordinator_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/transaction/coordinator"
)

type retryStateError struct{ state string }

func (e retryStateError) Error() string    { return e.state }
func (e retryStateError) SQLState() string { return e.state }

type retryDB struct {
	mu       sync.Mutex
	attempts int
	commits  []error
}

func (d *retryDB) Begin(context.Context) (dbport.Tx, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	i := d.attempts
	d.attempts++
	var commit error
	if i < len(d.commits) {
		commit = d.commits[i]
	}
	return &retryTx{commit: commit}, nil
}
func (d *retryDB) BeginSerializable(ctx context.Context) (dbport.Tx, error) { return d.Begin(ctx) }

type retryTx struct{ commit error }

func admit(context.Context) error { return nil }

func (t *retryTx) Exec(context.Context, string, ...any) (int64, error)        { return 1, nil }
func (t *retryTx) Query(context.Context, string, ...any) (dbport.Rows, error) { return nil, nil }
func (t *retryTx) QueryRow(context.Context, string, ...any) dbport.Row        { return nil }
func (t *retryTx) Commit(context.Context) error                               { return t.commit }
func (t *retryTx) Rollback(context.Context) error                             { return nil }

func TestSerializableRetryRestartsWholeClosureAndNeverRetriesAmbiguousCommit(t *testing.T) {
	db := &retryDB{commits: []error{retryStateError{"40001"}, nil}}
	c := coordinator.New(db)
	r := resolution(t)
	calls := 0
	req := coordinator.CommitRequest{Plan: r, Writes: writes(&calls)}
	_, err := c.CommitWithRetry(context.Background(), req, coordinator.RetryOptions{MaxAttempts: 2, Admit: admit, Prepare: func(context.Context, dbport.Tx) (coordinator.CommitRequest, error) { return req, nil }, Sleep: func(context.Context, time.Duration) error { return nil }})
	if err != nil || db.attempts != 2 || calls != 4 {
		t.Fatalf("err=%v attempts=%d closure calls=%d, want retry of complete closure", err, db.attempts, calls)
	}

	db = &retryDB{commits: []error{errors.New("connection lost")}}
	c = coordinator.New(db)
	published := false
	req = coordinator.CommitRequest{Plan: r, Writes: writes(&calls), Publish: func(context.Context, coordinator.Receipt) error { published = true; return nil }}
	_, err = c.CommitWithRetry(context.Background(), req, coordinator.RetryOptions{MaxAttempts: 3, Admit: admit, Prepare: func(context.Context, dbport.Tx) (coordinator.CommitRequest, error) { return req, nil }})
	if !errors.Is(err, coordinator.ErrCommitAmbiguous) || db.attempts != 1 || published {
		t.Fatalf("ambiguous err=%v attempts=%d published=%v", err, db.attempts, published)
	}
}

func TestTodo_DB_EDGE_003_Property(t *testing.T) {
	db := &retryDB{commits: []error{retryStateError{"23505"}, nil}}
	c := coordinator.New(db)
	req := coordinator.CommitRequest{Plan: resolution(t), Writes: writes(new(int))}
	_, err := c.CommitWithRetry(context.Background(), req, coordinator.RetryOptions{MaxAttempts: 3, Admit: admit, Prepare: func(context.Context, dbport.Tx) (coordinator.CommitRequest, error) { return req, nil }})
	if err == nil || db.attempts != 1 {
		t.Fatalf("unique conflict err=%v attempts=%d, want no retry", err, db.attempts)
	}
}

func TestTodo_DB_EDGE_003_Fault(t *testing.T) {
	db := &retryDB{commits: []error{retryStateError{"40001"}, retryStateError{"40001"}}}
	c := coordinator.New(db)
	req := coordinator.CommitRequest{Plan: resolution(t), Writes: writes(new(int))}
	_, err := c.CommitWithRetry(context.Background(), req, coordinator.RetryOptions{MaxAttempts: 2, Admit: admit, Prepare: func(context.Context, dbport.Tx) (coordinator.CommitRequest, error) { return req, nil }})
	if err == nil || db.attempts != 2 {
		t.Fatalf("bounded retry err=%v attempts=%d", err, db.attempts)
	}
	t.Run("jitter cannot exceed bound", func(t *testing.T) {
		db := &retryDB{commits: []error{retryStateError{"40001"}}}
		c := coordinator.New(db)
		r := resolution(t)
		slept := false
		_, err := c.CommitWithRetry(context.Background(), coordinator.CommitRequest{}, coordinator.RetryOptions{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, Admit: admit, Prepare: func(context.Context, dbport.Tx) (coordinator.CommitRequest, error) {
			return coordinator.CommitRequest{Plan: r, Writes: writes(new(int))}, nil
		}, Jitter: func(int, time.Duration) time.Duration { return 2 * time.Millisecond }, Sleep: func(context.Context, time.Duration) error { slept = true; return nil }})
		if !errors.Is(err, coordinator.ErrInvalidRequest) || slept || db.attempts != 1 {
			t.Fatalf("err=%v slept=%v attempts=%d", err, slept, db.attempts)
		}
	})
	t.Run("ambiguous resolver must bind receipt", func(t *testing.T) {
		db := &retryDB{commits: []error{errors.New("lost")}}
		c := coordinator.New(db)
		r := resolution(t)
		_, err := c.CommitWithRetry(context.Background(), coordinator.CommitRequest{}, coordinator.RetryOptions{MaxAttempts: 2, Admit: admit, Prepare: func(context.Context, dbport.Tx) (coordinator.CommitRequest, error) {
			return coordinator.CommitRequest{Plan: r, Writes: writes(new(int))}, nil
		}, ResolveAmbiguous: func(context.Context, coordinator.Receipt) (coordinator.Receipt, error) {
			return coordinator.Receipt{PlanID: "forged"}, nil
		}})
		if !errors.Is(err, coordinator.ErrCommitAmbiguous) || db.attempts != 1 {
			t.Fatalf("err=%v attempts=%d", err, db.attempts)
		}
	})
	t.Run("resolver cannot mutate its input into a matching forgery", func(t *testing.T) {
		db := &retryDB{commits: []error{errors.New("lost")}}
		c := coordinator.New(db)
		r := resolution(t)
		_, err := c.CommitWithRetry(context.Background(), coordinator.CommitRequest{}, coordinator.RetryOptions{MaxAttempts: 2, Admit: admit, Prepare: func(context.Context, dbport.Tx) (coordinator.CommitRequest, error) {
			return coordinator.CommitRequest{Plan: r, Writes: writes(new(int))}, nil
		}, ResolveAmbiguous: func(_ context.Context, receipt coordinator.Receipt) (coordinator.Receipt, error) {
			receipt.Participants[0] = "forged"
			return receipt, nil
		}})
		if !errors.Is(err, coordinator.ErrCommitAmbiguous) || db.attempts != 1 {
			t.Fatalf("err=%v attempts=%d", err, db.attempts)
		}
	})
	t.Run("retryable resolver failure cannot replay ambiguous commit", func(t *testing.T) {
		db := &retryDB{commits: []error{errors.New("lost")}}
		c := coordinator.New(db)
		r := resolution(t)
		writesApplied := 0
		_, err := c.CommitWithRetry(context.Background(), coordinator.CommitRequest{}, coordinator.RetryOptions{MaxAttempts: 3, Admit: admit, Prepare: func(context.Context, dbport.Tx) (coordinator.CommitRequest, error) {
			return coordinator.CommitRequest{Plan: r, Writes: writes(&writesApplied)}, nil
		}, ResolveAmbiguous: func(context.Context, coordinator.Receipt) (coordinator.Receipt, error) {
			return coordinator.Receipt{}, retryStateError{"40001"}
		}})
		if !errors.Is(err, coordinator.ErrCommitAmbiguous) || db.attempts != 1 || writesApplied != 2 {
			t.Fatalf("err=%v attempts=%d writes=%d", err, db.attempts, writesApplied)
		}
	})
}

func TestTodo_DB_EDGE_003_Mutation(t *testing.T) {
	db := &retryDB{}
	c := coordinator.New(db)
	req := coordinator.CommitRequest{Plan: resolution(t), Writes: writes(new(int))}
	_, err := c.CommitWithRetry(context.Background(), req, coordinator.RetryOptions{MaxAttempts: 2})
	if !errors.Is(err, coordinator.ErrInvalidRequest) || db.attempts != 0 {
		t.Fatalf("missing preparation err=%v attempts=%d, want fail closed before transaction", err, db.attempts)
	}
}

func TestTodo_DB_EDGE_003_Conformance(t *testing.T) {
	t.Run("publish SQLSTATE is never retried", func(t *testing.T) {
		db := &retryDB{}
		c := coordinator.New(db)
		publishes := 0
		callbacks := 0
		r := resolution(t)
		req := coordinator.CommitRequest{Plan: r, Writes: writes(new(int)), Publish: func(context.Context, coordinator.Receipt) error { publishes++; return retryStateError{"40001"} }}
		_, err := c.CommitWithRetry(context.Background(), coordinator.CommitRequest{}, coordinator.RetryOptions{MaxAttempts: 3, Admit: admit, Prepare: func(context.Context, dbport.Tx) (coordinator.CommitRequest, error) { return req, nil }, OnRetry: func(context.Context, coordinator.RetryAttempt) error { callbacks++; return nil }})
		if err == nil || db.attempts != 1 || publishes != 1 || callbacks != 0 {
			t.Fatalf("err=%v attempts=%d publishes=%d callbacks=%d", err, db.attempts, publishes, callbacks)
		}
	})
	t.Run("prepare and writes share transaction", func(t *testing.T) {
		db := &retryDB{}
		c := coordinator.New(db)
		r := resolution(t)
		var prepared dbport.Tx
		prepare := func(_ context.Context, tx dbport.Tx) (coordinator.CommitRequest, error) {
			prepared = tx
			ws := writes(new(int))
			for i := range ws {
				old := ws[i].Apply
				ws[i].Apply = func(ctx context.Context, got dbport.Tx) error {
					if got != prepared {
						return errors.New("different tx")
					}
					return old(ctx, got)
				}
			}
			return coordinator.CommitRequest{Plan: r, Writes: ws}, nil
		}
		if _, err := c.CommitWithRetry(context.Background(), coordinator.CommitRequest{}, coordinator.RetryOptions{MaxAttempts: 1, Admit: admit, Prepare: prepare}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("serializable capability required", func(t *testing.T) {
		db := &fakeDB{tx: &fakeTx{}}
		c := coordinator.New(db)
		called := false
		_, err := c.CommitWithRetry(context.Background(), coordinator.CommitRequest{}, coordinator.RetryOptions{MaxAttempts: 1, Admit: admit, Prepare: func(context.Context, dbport.Tx) (coordinator.CommitRequest, error) {
			called = true
			return coordinator.CommitRequest{}, nil
		}})
		if !errors.Is(err, coordinator.ErrInvalidRequest) || called {
			t.Fatalf("err=%v prepare=%v", err, called)
		}
	})
}

func TestTodo_DB_EDGE_003_Race(t *testing.T) {
	c := coordinator.New(&retryDB{})
	r := resolution(t)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := coordinator.CommitRequest{Plan: r, Writes: writes(new(int))}
			if _, err := c.CommitWithRetry(context.Background(), coordinator.CommitRequest{}, coordinator.RetryOptions{MaxAttempts: 1, Admit: admit, Prepare: func(context.Context, dbport.Tx) (coordinator.CommitRequest, error) { return req, nil }}); err != nil {
				t.Errorf("commit: %v", err)
			}
		}()
	}
	wg.Wait()
	for i := range 16 {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			attempts, callback := 0, coordinator.RetryAttempt{}
			err := coordinator.RetryClosure(context.Background(), coordinator.RetryOptions{
				MaxAttempts: 2, Admit: admit,
				Sleep:   func(context.Context, time.Duration) error { return nil },
				OnRetry: func(_ context.Context, got coordinator.RetryAttempt) error { callback = got; return nil },
			}, func(context.Context) error {
				attempts++
				if attempts == 1 {
					return retryStateError{"40001"}
				}
				return nil
			})
			if err != nil || callback.Attempt != 2 || callback.SQLState != "40001" {
				t.Errorf("worker %d err=%v callback=%+v", worker, err, callback)
			}
		}(i)
	}
	wg.Wait()
}

func TestRetryClosure_OnRetryChargesOnlyConsumedRetries(t *testing.T) {
	t.Run("exact next ordinal and SQLSTATE", func(t *testing.T) {
		attempts := 0
		var got []coordinator.RetryAttempt
		err := coordinator.RetryClosure(context.Background(), coordinator.RetryOptions{
			MaxAttempts: 3,
			Admit:       admit,
			Sleep:       func(context.Context, time.Duration) error { return nil },
			OnRetry: func(_ context.Context, retry coordinator.RetryAttempt) error {
				got = append(got, retry)
				return nil
			},
		}, func(context.Context) error {
			attempts++
			switch attempts {
			case 1:
				return retryStateError{"40001"}
			case 2:
				return retryStateError{"40P01"}
			default:
				return nil
			}
		})
		want := []coordinator.RetryAttempt{{Attempt: 2, SQLState: "40001"}, {Attempt: 3, SQLState: "40P01"}}
		if err != nil || !reflect.DeepEqual(got, want) || attempts != 3 {
			t.Fatalf("err=%v callbacks=%+v attempts=%d", err, got, attempts)
		}
	})

	t.Run("callback denial is terminal", func(t *testing.T) {
		denied := retryStateError{"40001"}
		attempts, callbacks := 0, 0
		err := coordinator.RetryClosure(context.Background(), coordinator.RetryOptions{
			MaxAttempts: 3,
			Admit:       admit,
			Sleep:       func(context.Context, time.Duration) error { return nil },
			OnRetry: func(context.Context, coordinator.RetryAttempt) error {
				callbacks++
				return denied
			},
		}, func(context.Context) error { attempts++; return retryStateError{"40001"} })
		if !errors.Is(err, denied) || attempts != 1 || callbacks != 1 {
			t.Fatalf("err=%v attempts=%d callbacks=%d", err, attempts, callbacks)
		}
	})

	for _, tc := range []struct {
		name string
		err  error
	}{
		{"unique", errors.New("unique")},
		{"ambiguous", coordinator.ErrCommitAmbiguous},
	} {
		t.Run("no callback for "+tc.name, func(t *testing.T) {
			callbacks := 0
			err := coordinator.RetryClosure(context.Background(), coordinator.RetryOptions{MaxAttempts: 3, Admit: admit, OnRetry: func(context.Context, coordinator.RetryAttempt) error { callbacks++; return nil }}, func(context.Context) error { return tc.err })
			if !errors.Is(err, tc.err) || callbacks != 0 {
				t.Fatalf("err=%v callbacks=%d", err, callbacks)
			}
		})
	}

	t.Run("exhaustion does not charge an unavailable attempt", func(t *testing.T) {
		callbacks, attempts := 0, 0
		err := coordinator.RetryClosure(context.Background(), coordinator.RetryOptions{MaxAttempts: 1, Admit: admit, OnRetry: func(context.Context, coordinator.RetryAttempt) error { callbacks++; return nil }}, func(context.Context) error { attempts++; return retryStateError{"40001"} })
		if err == nil || attempts != 1 || callbacks != 0 {
			t.Fatalf("err=%v attempts=%d callbacks=%d", err, attempts, callbacks)
		}
	})

	t.Run("failed backoff does not charge", func(t *testing.T) {
		callbacks := 0
		stopped := errors.New("sleep stopped")
		err := coordinator.RetryClosure(context.Background(), coordinator.RetryOptions{MaxAttempts: 2, Admit: admit, Sleep: func(context.Context, time.Duration) error { return stopped }, OnRetry: func(context.Context, coordinator.RetryAttempt) error { callbacks++; return nil }}, func(context.Context) error { return retryStateError{"40001"} })
		if !errors.Is(err, stopped) || callbacks != 0 {
			t.Fatalf("err=%v callbacks=%d", err, callbacks)
		}
	})

	t.Run("next admission denial does not charge", func(t *testing.T) {
		admissions, callbacks := 0, 0
		denied := errors.New("admission denied")
		err := coordinator.RetryClosure(context.Background(), coordinator.RetryOptions{MaxAttempts: 2, Sleep: func(context.Context, time.Duration) error { return nil }, Admit: func(context.Context) error {
			admissions++
			if admissions == 2 {
				return denied
			}
			return nil
		}, OnRetry: func(context.Context, coordinator.RetryAttempt) error { callbacks++; return nil }}, func(context.Context) error { return retryStateError{"40001"} })
		if !errors.Is(err, denied) || callbacks != 0 {
			t.Fatalf("err=%v callbacks=%d", err, callbacks)
		}
	})
}
