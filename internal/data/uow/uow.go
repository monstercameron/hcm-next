package uow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
)

// Version is a 1-based count of the revisions an aggregate has ever had; zero
// means no revision has been appended yet. See doc.go's "Version, without a
// version column" section for why a count, rather than a stored column, is
// this package's compare-and-swap fence.
type Version uint64

// AggregateRepository is the contract every DB-018 repository implements:
// load the revision current at a business instant together with the version
// it was read at, append a new revision under compare-and-swap, and list
// every revision ever recorded. T is the aggregate's own Go shape (for
// example [github.com/monstercameron/hcm-next/internal/data/aggregates.Worker]);
// this package never inspects T's fields, so a repository over any other
// DB-008/009/010 table can implement the same contract.
//
// ex is a [dbport.Conn] rather than a caller-opened [dbport.Tx] on purpose:
// both satisfy it, so a repository used standalone can read over a bare
// connection, while [Stage] always passes the unit of work's own transaction
// so a Save participates in that transaction's atomicity. A repository must
// not begin, commit or roll back a transaction of its own -- doing so would
// be exactly the nested unit of work this package's own API has no way to
// express.
type AggregateRepository[T any] interface {
	// Load returns the revision live at businessAt together with its version
	// (the count of revisions recorded for entityID up to and including the
	// one returned). It returns [aggregates.ErrNotFound]-shaped errors from
	// the underlying store when no such revision exists; a repository over a
	// table with no rows yet for entityID returns version 0 and that error.
	Load(ctx context.Context, ex dbport.Conn, tenant, entityID uuid.UUID, businessAt time.Time) (T, Version, error)

	// Save appends aggregate as entityID's next revision, refusing with
	// [*ErrStaleVersion] when expectedVersion no longer matches the
	// aggregate's current version. It never updates a stored revision in
	// place; the only mutation any DB-018 table accepts is superseding
	// exactly the row this Save's own compare-and-swap targets, which is a
	// side effect of the append, not a rewrite of it.
	Save(ctx context.Context, ex dbport.Conn, tenant, entityID uuid.UUID, aggregate T, expectedVersion Version) error

	// ListRevisions returns every revision ever recorded for entityID, oldest
	// first -- the Nth element is the aggregate as it stood at Version(N+1).
	// It never mutates anything it reads.
	ListRevisions(ctx context.Context, ex dbport.Conn, tenant, entityID uuid.UUID) ([]T, error)
}

// unitState tracks whether a UnitOfWork may still accept Register/Stage
// calls or run Commit.
type unitState int

const (
	unitOpen unitState = iota
	unitCommitted
	unitRolledBack
)

// stagedWrite is one queued aggregate append, closed over its own kind and
// repository so Commit can run every staged write with a single, uniform
// loop regardless of how many distinct aggregate types were registered.
type stagedWrite struct {
	kind  string
	apply func(ctx context.Context, tx dbport.Tx) error
}

// UnitOfWork coordinates one or more [AggregateRepository] writes inside
// exactly one PostgreSQL transaction, bound to exactly one tenant. See doc.go.
type UnitOfWork struct {
	tx     dbport.Tx
	tenant uuid.UUID
	repos  map[string]any
	writes []stagedWrite
	state  unitState
}

// Begin opens a transaction on beginner, binds it to tenant via
// [tenancy.WithTenant] as its first statement, and returns a UnitOfWork ready
// for [Register] and [Stage] calls. Begin is a package-level function, not a
// method on [UnitOfWork]: there is no way to obtain a UnitOfWork nested
// inside another one's transaction.
func Begin(ctx context.Context, beginner dbport.Beginner, tenant uuid.UUID) (*UnitOfWork, error) {
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("uow: begin: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("uow: bind tenant: %w", err)
	}
	return &UnitOfWork{tx: tx, tenant: tenant, repos: make(map[string]any)}, nil
}

// Register binds repo under kind for the lifetime of u. kind must not already
// be registered on u. Register itself performs no I/O; it only makes repo
// available to a later [Stage] call naming the same kind.
func Register[T any](u *UnitOfWork, kind string, repo AggregateRepository[T]) error {
	if u.state != unitOpen {
		return ErrClosed
	}
	if kind == "" {
		return fmt.Errorf("%w: a registered aggregate needs a non-empty kind", ErrUnitOfWork)
	}
	if _, exists := u.repos[kind]; exists {
		return fmt.Errorf("%w: kind %q is already registered on this unit of work", ErrUnitOfWork, kind)
	}
	u.repos[kind] = repo
	return nil
}

// Stage queues one aggregate append against the repository registered under
// kind (see [Register]), to be attempted -- with every other staged write --
// inside [UnitOfWork.Commit]'s single transaction. Staging performs no I/O:
// nothing is written, and no version is checked, until Commit runs.
func Stage[T any](u *UnitOfWork, kind string, entityID uuid.UUID, aggregate T, expectedVersion Version) error {
	if u.state != unitOpen {
		return ErrClosed
	}
	raw, ok := u.repos[kind]
	if !ok {
		return fmt.Errorf("%w: kind %q was never registered on this unit of work", ErrUnitOfWork, kind)
	}
	repo, ok := raw.(AggregateRepository[T])
	if !ok {
		return fmt.Errorf("%w: kind %q was registered with a different aggregate type", ErrUnitOfWork, kind)
	}
	tenant := u.tenant
	u.writes = append(u.writes, stagedWrite{
		kind: kind,
		apply: func(ctx context.Context, tx dbport.Tx) error {
			err := repo.Save(ctx, tx, tenant, entityID, aggregate, expectedVersion)
			if err == nil {
				return nil
			}
			var stale *ErrStaleVersion
			if errors.As(err, &stale) {
				return &ConflictError{
					Kind: kind, EntityID: entityID,
					ExpectedVersion: expectedVersion, ActualVersion: stale.Actual,
				}
			}
			return fmt.Errorf("uow: save %s %s: %w", kind, entityID, err)
		},
	})
	return nil
}

// Commit runs every staged write against the transaction [Begin] opened, in
// staging order, and commits only if all of them succeed. The first failure
// -- a [*ConflictError] or any other error a repository's Save returns --
// rolls the whole transaction back and is returned as-is; no revision from
// any other staged write in this unit of work is left partially applied.
// Commit may be called exactly once; a second call (whether or not the first
// succeeded) returns [ErrClosed].
func (u *UnitOfWork) Commit(ctx context.Context) error {
	if u.state != unitOpen {
		return ErrClosed
	}
	for _, w := range u.writes {
		if err := w.apply(ctx, u.tx); err != nil {
			_ = u.tx.Rollback(ctx)
			u.state = unitRolledBack
			return err
		}
	}
	if err := u.tx.Commit(ctx); err != nil {
		_ = u.tx.Rollback(ctx)
		u.state = unitRolledBack
		return fmt.Errorf("uow: commit: %w", err)
	}
	u.state = unitCommitted
	return nil
}

// Rollback discards the unit of work's transaction. It is idempotent (like
// [dbport.Tx.Rollback]): calling it after Commit has already finished the
// unit of work, one way or the other, is not an error.
func (u *UnitOfWork) Rollback(ctx context.Context) error {
	if u.state != unitOpen {
		return nil
	}
	u.state = unitRolledBack
	return u.tx.Rollback(ctx)
}

// Tx exposes the unit of work's own transaction so a caller can Load through
// it (Load takes no version and stages no write, so it needs no [Stage] call)
// before deciding what to [Stage]. It is nil once the unit of work has
// finished.
func (u *UnitOfWork) Tx() dbport.Tx {
	if u.state != unitOpen {
		return nil
	}
	return u.tx
}
