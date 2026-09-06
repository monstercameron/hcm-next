// Package truststore persists the trust metadata owned by PERSIST-TRUST-001.
// It owns no trust decisions: the step-up, JIT and access-review packages
// remain the policy authorities, while this package supplies tenant-scoped
// PostgreSQL storage with typed duplicate and compare-and-swap failures.
package truststore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/trust/stepup"
)

// DB is the only capability needed to open the short transactions used by a
// Store. pgx connections, pools and pgtest connections satisfy it.
type DB interface{ dbport.Beginner }

// Executor is the explicit-transaction capability accepted by helper methods.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// Store implements the PERSIST-TRUST-001 storage boundary.
type Store struct{ db DB }

var _ stepup.ProofStore = (*Store)(nil)

// New creates a trust metadata store over a caller-owned database handle.
func New(db DB) *Store { return &Store{db: db} }

func (s *Store) withTenant(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if s == nil || s.db == nil {
		return failure(CodeInvalid, "store", "", errors.New("database is required"))
	}
	if tenantID == uuid.Nil {
		return failure(CodeTenantRequired, "", "", errors.New("tenant is required"))
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return failure(CodeDatabase, "transaction", "", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return failure(CodeDatabase, "transaction", "", err)
	}
	return nil
}

func classifyWriteError(table, key string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return failure(CodeDuplicate, table, key, err)
		case "23514", "23502", "22P02":
			return failure(CodeInvalid, table, key, err)
		}
	}
	return failure(CodeDatabase, table, key, err)
}

func jsonValue(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal JSON: %w", err)
	}
	return raw, nil
}

func parseTenant(value string) (uuid.UUID, error) {
	tenant, err := uuid.Parse(value)
	if err != nil || tenant == uuid.Nil {
		return uuid.Nil, failure(CodeTenantRequired, "", value, errors.New("tenant must be a non-nil UUID"))
	}
	return tenant, nil
}

func requireText(table, field, value string) error {
	if value == "" {
		return invalid(table, field, "value is required")
	}
	return nil
}

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func requireDigest(table, field, value string) error {
	if !digestPattern.MatchString(value) {
		return invalid(table, field, "digest must be 64 lowercase hexadecimal characters")
	}
	return nil
}

// ContextWithTenant supplies the tenant used by Consume when the Store itself
// is passed to a stepup.Gate as its ProofStore. Prefer ForTenant when wiring a
// gate, because the wrapper makes the scope explicit at construction time.
func ContextWithTenant(ctx context.Context, tenantID uuid.UUID) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tenantID)
}

type tenantContextKey struct{}

func tenantFromContext(ctx context.Context) (uuid.UUID, bool) {
	tenant, ok := ctx.Value(tenantContextKey{}).(uuid.UUID)
	return tenant, ok && tenant != uuid.Nil
}

// ForTenant returns a step-up ProofStore bound to one tenant. Every Consume
// call scopes its transaction before it reads or writes the proof ledger.
func (s *Store) ForTenant(tenantID uuid.UUID) stepup.ProofStore {
	return scopedProofStore{store: s, tenantID: tenantID}
}

type scopedProofStore struct {
	store    *Store
	tenantID uuid.UUID
}

func (s scopedProofStore) Consume(ctx context.Context, proofID, outcome string) (bool, error) {
	return s.store.consume(ctx, s.tenantID, proofID, outcome)
}

// Consume implements stepup.ProofStore for callers that carry the tenant in
// ContextWithTenant. A missing scope is a typed refusal rather than a query
// against an unscoped connection.
func (s *Store) Consume(ctx context.Context, proofID, outcome string) (bool, error) {
	tenantID, ok := tenantFromContext(ctx)
	if !ok {
		return false, failure(CodeTenantRequired, "stepup_proof_log", proofID, errors.New("tenant context is required"))
	}
	return s.consume(ctx, tenantID, proofID, outcome)
}

func (s *Store) consume(ctx context.Context, tenantID uuid.UUID, proofID, outcome string) (bool, error) {
	if tenantID == uuid.Nil {
		return false, failure(CodeTenantRequired, "stepup_proof_log", proofID, errors.New("tenant is required"))
	}
	if err := requireText("stepup_proof_log", "proof_id", proofID); err != nil {
		return false, err
	}
	if err := requireText("stepup_proof_log", "outcome", outcome); err != nil {
		return false, err
	}
	var consumed bool
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		var existing string
		err := tx.QueryRow(ctx, `
			SELECT outcome FROM stepup_proof_log
			WHERE tenant_id=$1 AND proof_id=$2`, tenantID, proofID).Scan(&existing)
		if err == nil {
			consumed = true
			return nil
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return failure(CodeDatabase, "stepup_proof_log", proofID, err)
		}

		_, err = tx.Exec(ctx, `
            INSERT INTO stepup_proof_log
                (tenant_id, row_id, proof_id, outcome, consumed_at, event_sequence)
            VALUES ($1,$2,$3,$4,$5,1)`,
			tenantID, uuid.New(), proofID, outcome, time.Now().UTC())
		if err == nil {
			return nil
		}
		if classified := classifyWriteError("stepup_proof_log", proofID, err); CodeOf(classified) == CodeDuplicate {
			// A concurrent consumer won the proof identity. The transaction is
			// rolled back by the outer helper; the caller retries the read once.
			return classified
		}
		return classifyWriteError("stepup_proof_log", proofID, err)
	})
	if err == nil {
		return consumed, nil
	}
	if CodeOf(err) != CodeDuplicate {
		return false, err
	}
	// Resolve a proof-identity collision after the failed insert. This read
	// occurs in a new tenant-scoped transaction and makes a racing consumer
	// converge on the same replay result as a sequential consumer.
	var existing string
	err = s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT outcome FROM stepup_proof_log WHERE tenant_id=$1 AND proof_id=$2`, tenantID, proofID).Scan(&existing)
	})
	if err == nil {
		return true, nil
	}
	return false, err
}
