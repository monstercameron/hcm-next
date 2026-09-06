package configregistry

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	platformconfig "github.com/monstercameron/hcm-next/internal/platform/configregistry"
)

// DB is the database capability this adapter needs, stated in [dbport]'s
// driver-free terms: every operation this [Store] performs runs inside a
// transaction it opens itself (so [withTenant] can establish migration
// 00027's row level security context first), so a plain [dbport.Beginner] is
// enough — a pooled handle or a single connection both satisfy it.
type DB interface {
	dbport.Beginner
}

// Store implements [platformconfig.Store] over PostgreSQL.
type Store struct {
	db DB
}

var _ platformconfig.Store = (*Store)(nil)

// New returns a [Store] over db. db is typically a pgxadapter connection or
// pool already able to SET ROLE hcmnext_app, the same expectation every
// other adapter in internal/data/* carries.
func New(db DB) *Store {
	return &Store{db: db}
}

// withTenant opens a transaction, establishes migration 00027's row level
// security tenant context via [tenancy.WithTenant], runs fn, and commits.
// fn's error (or a failure setting the tenant context) rolls the
// transaction back instead.
func (s *Store) withTenant(ctx context.Context, tenantID string, fn func(tx dbport.Tx) error) error {
	tid, err := uuid.Parse(tenantID)
	if err != nil {
		return fmt.Errorf("configregistry: scope tenant id %q is not a valid uuid: %w", tenantID, err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("configregistry: begin transaction: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tid); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}
