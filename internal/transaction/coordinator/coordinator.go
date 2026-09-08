package coordinator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/transaction"
)

var (
	ErrInvalidRequest      = errors.New("transaction coordinator: invalid commit request")
	ErrParticipantMismatch = errors.New("transaction coordinator: participant write mismatch")
	ErrCommitAmbiguous     = errors.New("transaction coordinator: commit outcome is ambiguous")
)

// TxFactory is the only capability the coordinator needs from a database
// pool. The coordinator never opens a second transaction or commits a caller's
// transaction on its behalf.
type TxFactory interface {
	Begin(context.Context) (dbport.Tx, error)
}

type serializableTxFactory interface {
	BeginSerializable(context.Context) (dbport.Tx, error)
}

func beginSerializable(ctx context.Context, db TxFactory) (dbport.Tx, error) {
	if serializable, ok := db.(serializableTxFactory); ok {
		return serializable.BeginSerializable(ctx)
	}
	return nil, fmt.Errorf("%w: serializable transactions unsupported", ErrInvalidRequest)
}

// Write is one correctness-bearing local operation. Apply must issue all its
// statements through tx and must not call an external service.
type Write struct {
	ParticipantID string
	Apply         func(context.Context, dbport.Tx) error
}

// Publish is an after-commit notification. It is never called if the local
// transaction rolls back or fails to commit.
type Publish func(context.Context, Receipt) error

type publishError struct{ err error }

func (e publishError) Error() string { return "publish after commit: " + e.err.Error() }
func (e publishError) Unwrap() error { return e.err }

type CommitRequest struct {
	Plan    transaction.Resolution
	Writes  []Write
	Publish Publish
}

type Receipt struct {
	PlanID           string
	ResolutionDigest string
	Participants     []string
}

// RetryOptions bounds restartable serialization/deadlock failures. Apply
// closures are rerun from a fresh transaction; callers must keep external
// effects in Publish, which runs only after a successful commit.
type RetryOptions struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Admit       func(context.Context) error
	Sleep       func(context.Context, time.Duration) error
	Jitter      func(attempt int, delay time.Duration) time.Duration
	// OnRetry is invoked only after a known-abort retryable failure and before
	// the next closure attempt. Attempt is the next ordinal; SQLState is the
	// exact classified serialization/deadlock state. It is never called for
	// the initial attempt, ambiguity, unique conflicts or exhaustion.
	OnRetry func(context.Context, RetryAttempt) error
	// ResolveAmbiguous determines the outcome after a commit connection error.
	// It may consult a durable receipt, but CommitWithRetry never replays the
	// closure after ErrCommitAmbiguous.
	ResolveAmbiguous func(context.Context, Receipt) (Receipt, error)
	// Prepare rebuilds the request from a fresh snapshot for every attempt.
	Prepare func(context.Context, dbport.Tx) (CommitRequest, error)
}

type RetryAttempt struct {
	Attempt  int
	SQLState string
}

func (o RetryOptions) normalized() RetryOptions {
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 3
	}
	if o.BaseDelay <= 0 {
		o.BaseDelay = 10 * time.Millisecond
	}
	if o.MaxDelay <= 0 {
		o.MaxDelay = time.Second
	}
	if o.MaxDelay < o.BaseDelay {
		o.MaxDelay = o.BaseDelay
	}
	if o.Sleep == nil {
		o.Sleep = func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		}
	}
	if o.Jitter == nil {
		o.Jitter = func(_ int, d time.Duration) time.Duration {
			// Full jitter is intentionally bounded and uses no shared mutable
			// random source, so concurrent coordinators cannot contend on it.
			half := d / 2
			if half <= 0 {
				return d
			}
			return half + time.Duration(time.Now().UnixNano()%int64(half+1))
		}
	}
	return o
}

type sqlStateError interface{ SQLState() string }

func retryable(err error) bool {
	var state sqlStateError
	if !errors.As(err, &state) {
		return false
	}
	return state.SQLState() == "40001" || state.SQLState() == "40P01"
}

func retryDelay(base, max time.Duration, attempt int) time.Duration {
	d := base
	for i := 1; i < attempt && d < max; i++ {
		if d > max/2 {
			return max
		}
		d *= 2
	}
	if d > max {
		return max
	}
	return d
}

// RetryClosure reruns one complete transaction-owned closure after a
// classified serialization/deadlock failure. The closure must open and close
// its own fresh transaction on every invocation. Commit ambiguity is terminal:
// callers may resolve it separately, but this helper never replays it.
func RetryClosure(ctx context.Context, opts RetryOptions, closure func(context.Context) error) error {
	if closure == nil {
		return fmt.Errorf("%w: retry closure is required", ErrInvalidRequest)
	}
	if opts.MaxAttempts < 0 || opts.BaseDelay < 0 || opts.MaxDelay < 0 {
		return fmt.Errorf("%w: retry bounds must not be negative", ErrInvalidRequest)
	}
	opts = opts.normalized()
	if opts.Admit == nil {
		return fmt.Errorf("%w: retry admission closure is required", ErrInvalidRequest)
	}
	pendingSQLState := ""
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := opts.Admit(ctx); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if attempt > 1 && opts.OnRetry != nil {
			if callErr := opts.OnRetry(ctx, RetryAttempt{Attempt: attempt, SQLState: pendingSQLState}); callErr != nil {
				return callErr
			}
		}
		err := closure(ctx)
		if err == nil {
			return nil
		}
		var published publishError
		if errors.As(err, &published) || errors.Is(err, ErrCommitAmbiguous) || !retryable(err) || attempt == opts.MaxAttempts {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		var sqlErr sqlStateError
		if errors.As(err, &sqlErr) {
			pendingSQLState = sqlErr.SQLState()
		}
		delay := opts.Jitter(attempt, retryDelay(opts.BaseDelay, opts.MaxDelay, attempt))
		if delay < 0 || delay > opts.MaxDelay {
			return fmt.Errorf("%w: retry delay out of bounds", ErrInvalidRequest)
		}
		if err := opts.Sleep(ctx, delay); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// CommitWithRetry restarts the complete transaction closure for classified
// serialization/deadlock failures only. Unique conflicts and all other
// failures are returned unchanged. A commit failure is ambiguous and is
// resolved explicitly when a resolver is supplied, never retried.
func (c *Coordinator) CommitWithRetry(ctx context.Context, req CommitRequest, opts RetryOptions) (Receipt, error) {
	if opts.MaxAttempts < 0 || opts.BaseDelay < 0 || opts.MaxDelay < 0 {
		return Receipt{}, fmt.Errorf("%w: retry bounds must not be negative", ErrInvalidRequest)
	}
	opts = opts.normalized()
	if opts.Prepare == nil || opts.Admit == nil {
		return Receipt{}, fmt.Errorf("%w: retry preparation and admission closures are required", ErrInvalidRequest)
	}
	var receipt Receipt
	err := RetryClosure(ctx, opts, func(ctx context.Context) error {
		var err error
		receipt, err = c.commit(ctx, req, true, opts.Prepare)
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrCommitAmbiguous) && opts.ResolveAmbiguous != nil {
			expected := receipt
			expected.Participants = append([]string(nil), receipt.Participants...)
			query := expected
			query.Participants = append([]string(nil), expected.Participants...)
			resolved, resolveErr := opts.ResolveAmbiguous(ctx, query)
			if resolveErr != nil {
				// Resolution happens only after an uncertain commit. Even a
				// retryable-looking resolver failure must retain that ambiguity
				// classification so RetryClosure never repeats the transaction.
				return fmt.Errorf("%w: durable resolution failed: %w", ErrCommitAmbiguous, resolveErr)
			}
			if resolved.PlanID != expected.PlanID || resolved.ResolutionDigest != expected.ResolutionDigest || !equalStrings(resolved.Participants, expected.Participants) {
				return fmt.Errorf("%w: ambiguous resolver returned mismatched receipt", ErrCommitAmbiguous)
			}
			resolved.Participants = append([]string(nil), resolved.Participants...)
			receipt = resolved
			return nil
		}
		return err
	})
	return receipt, err
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Commit atomically executes each admitted participant write. The write order
// is the resolution's canonical lock order (with participant order preserved
// within a stream), preventing two coordinators from deadlocking. Effects are
// not executed here; Publish runs only after a successful Commit.
func (c *Coordinator) Commit(ctx context.Context, req CommitRequest) (Receipt, error) {
	return c.commit(ctx, req, false, nil)
}

func (c *Coordinator) commit(ctx context.Context, req CommitRequest, serializable bool, prepare func(context.Context, dbport.Tx) (CommitRequest, error)) (Receipt, error) {
	if c == nil || c.db == nil {
		return Receipt{}, fmt.Errorf("%w: no database", ErrInvalidRequest)
	}
	var ordered []intent.PlanParticipant
	var byID map[string]Write
	var err error
	if prepare == nil {
		ordered, byID, err = validateRequest(req)
		if err != nil {
			return Receipt{}, err
		}
	}
	var tx dbport.Tx
	if serializable {
		tx, err = beginSerializable(ctx, c.db)
	} else {
		tx, err = c.db.Begin(ctx)
	}
	if err != nil {
		return Receipt{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if prepare != nil {
		prepared, prepareErr := prepare(ctx, tx)
		if prepareErr != nil {
			return Receipt{}, prepareErr
		}
		req = prepared
	}
	if prepare != nil {
		ordered, byID, err = validateRequest(req)
		if err != nil {
			return Receipt{}, err
		}
	}
	for _, p := range ordered {
		if err := byID[p.ParticipantID].Apply(ctx, tx); err != nil {
			return Receipt{}, fmt.Errorf("participant %s: %w", p.ParticipantID, err)
		}
	}
	receipt := Receipt{PlanID: req.Plan.PlanID, ResolutionDigest: req.Plan.Digest, Participants: participantIDs(ordered)}
	if err := tx.Commit(ctx); err != nil {
		// PostgreSQL reports a definite serialization/deadlock abort with a
		// SQLSTATE. The transaction is known not to have committed and is safe
		// for a complete closure restart. Other commit/connection failures are
		// deliberately ambiguous and require durable resolution.
		if retryable(err) {
			return receipt, fmt.Errorf("commit restartable: %w", err)
		}
		return receipt, fmt.Errorf("%w: %v", ErrCommitAmbiguous, err)
	}
	if req.Publish != nil {
		if err := req.Publish(ctx, receipt); err != nil {
			return receipt, publishError{err: err}
		}
	}
	return receipt, nil
}

func validateRequest(req CommitRequest) ([]intent.PlanParticipant, map[string]Write, error) {
	if err := req.Plan.VerifyDigest(); err != nil {
		return nil, nil, err
	}
	if req.Plan.PlanID == "" || len(req.Plan.Admitted) == 0 {
		return nil, nil, fmt.Errorf("%w: resolution has no admitted participants", ErrInvalidRequest)
	}
	byID := make(map[string]Write, len(req.Writes))
	for _, w := range req.Writes {
		if w.ParticipantID == "" || w.Apply == nil || byID[w.ParticipantID].Apply != nil {
			return nil, nil, fmt.Errorf("%w: invalid or duplicate write %q", ErrParticipantMismatch, w.ParticipantID)
		}
		byID[w.ParticipantID] = w
	}
	if len(byID) != len(req.Plan.Admitted) {
		return nil, nil, fmt.Errorf("%w: got %d writes for %d admitted participants", ErrParticipantMismatch, len(byID), len(req.Plan.Admitted))
	}
	ordered := orderedParticipants(req.Plan.Admitted, req.Plan.LockOrder)
	if len(ordered) != len(req.Plan.Admitted) {
		return nil, nil, fmt.Errorf("%w: writes do not cover admitted participants", ErrParticipantMismatch)
	}
	for _, p := range ordered {
		if byID[p.ParticipantID].Apply == nil {
			return nil, nil, fmt.Errorf("%w: missing participant %q", ErrParticipantMismatch, p.ParticipantID)
		}
	}
	return ordered, byID, nil
}

type Coordinator struct{ db TxFactory }

func New(db TxFactory) *Coordinator { return &Coordinator{db: db} }

func orderedParticipants(in []intent.PlanParticipant, lockOrder []string) []intent.PlanParticipant {
	out := make([]intent.PlanParticipant, 0, len(in))
	used := make(map[string]bool, len(in))
	for _, stream := range lockOrder {
		for _, p := range in {
			if !used[p.ParticipantID] && p.StreamID == stream {
				out = append(out, p)
				used[p.ParticipantID] = true
			}
		}
	}
	for _, p := range in {
		if !used[p.ParticipantID] {
			out = append(out, p)
		}
	}
	return out
}
func participantIDs(in []intent.PlanParticipant) []string {
	out := make([]string, len(in))
	for i, p := range in {
		out[i] = p.ParticipantID
	}
	return out
}
