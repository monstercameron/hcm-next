package stepup

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SQLProofStore is a [ProofStore] over one shared consumption-log table, so
// any number of gate instances - in this process or any other - consume the
// same proofs against the same authority. The table shape is:
//
//	CREATE TABLE stepup_proof_log (
//	  proof_id    text primary key,
//	  outcome     text   not null,
//	  consumed_at timestamptz not null
//	);
//
// Schema ownership stays with the migrations of the owning package; this
// store only reads and writes rows in a table that already exists.
type SQLProofStore struct {
	db *sql.DB
}

// NewSQLProofStore builds a store over the handle bound to the table's
// database.
func NewSQLProofStore(db *sql.DB) *SQLProofStore {
	return &SQLProofStore{db: db}
}

const consumeSelect = `SELECT outcome FROM stepup_proof_log WHERE proof_id = $1`
const consumeInsert = `INSERT INTO stepup_proof_log (proof_id, outcome, consumed_at) VALUES ($1, $2, $3)`

// Consume implements [ProofStore] with a check-then-insert, and one
// disambiguating re-read: a first miss that turns into a unique-key collision
// means another consumer committed between the two steps, so the answer
// converges to "already consumed". A collision that does not materialize on
// the re-read is reported as [ErrAmbiguous], and a gate seeing that must
// treat the proof as consumed and execute nothing.
func (s *SQLProofStore) Consume(ctx context.Context, proofID, outcome string) (bool, error) {
	var prev string
	err := s.db.QueryRowContext(ctx, consumeSelect, proofID).Scan(&prev)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	_, err = s.db.ExecContext(ctx, consumeInsert, proofID, outcome, time.Now().UTC())
	if err == nil {
		return false, nil
	}

	// The insert failed: re-read once to tell a concurrent commit from
	// anything else.
	err = s.db.QueryRowContext(ctx, consumeSelect, proofID).Scan(&prev)
	if err == nil {
		return true, nil
	}
	return false, ErrAmbiguous
}
