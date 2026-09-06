package workforce

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// Executor is the minimal database capability this package needs. A
// [dbport.Tx] and a [dbport.Conn] both satisfy it.
//
// Every method takes it explicitly rather than holding a handle, the same way
// internal/humanwork/workitem's own store does: journey_worker is
// row-level-security protected, so the caller has to have scoped its
// transaction to a tenant (internal/data/tenancy.WithTenant) before any
// statement here runs, and a store that opened its own connection would make
// that impossible to guarantee.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// columns is the full journey_worker projection, in the order [scanWorker]
// reads it. The three numeric columns and the two date columns are cast on the
// way out so the exact decimal text and the exact calendar date are what comes
// back, never a float or a timezone-shifted instant.
const columns = `tenant_id, worker_id, worker_key,
	legal_name, preferred_name, worker_number,
	worker_type, lifecycle_status,
	employment_id, assignment_id,
	job_code, grade, org_unit, position_id, location, pay_zone,
	fte::text, manager_relationship_ref,
	hire_date, effective_from,
	base_pay::text, currency, pay_basis, bonus_target::text,
	revision_stream, revision_sequence, known_at, recorded_at,
	created_by, source`

// Store implements the durable workforce over
// migrations/00023_journey_workforce.sql. It holds no state; every method
// takes its [Executor] explicitly.
type Store struct{}

// Create inserts one worker and returns the row as stored.
//
// The insert is `ON CONFLICT DO NOTHING ... RETURNING`, so a worker whose key
// or id the tenant already carries produces no row and is reported as
// [ErrDuplicate]. That is one atomic statement rather than a read followed by
// a write: a pre-check would leave a window in which two concurrent creations
// both saw "not present" and one of them then failed with a raw constraint
// violation the caller could not classify.
func (s Store) Create(ctx context.Context, ex Executor, in WorkerRow) (WorkerRow, error) {
	if in.Source == "" {
		in.Source = SourceCreated
	}
	if in.RecordedAt.IsZero() {
		in.RecordedAt = in.KnownAt
	}
	if err := in.Validate(); err != nil {
		return WorkerRow{}, err
	}

	row := ex.QueryRow(ctx, `
		INSERT INTO journey_worker (
			tenant_id, worker_id, worker_key,
			legal_name, preferred_name, worker_number,
			worker_type, lifecycle_status,
			employment_id, assignment_id,
			job_code, grade, org_unit, position_id, location, pay_zone,
			fte, manager_relationship_ref,
			hire_date, effective_from,
			base_pay, currency, pay_basis, bonus_target,
			revision_stream, revision_sequence, known_at, recorded_at,
			created_by, source)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
			$17::text::numeric, $18, $19::text::date, $20::text::date,
			$21::text::numeric, $22, $23, $24::text::numeric,
			$25, $26, $27, $28, $29, $30)
		ON CONFLICT DO NOTHING
		RETURNING `+columns,
		in.TenantID, in.WorkerID, in.WorkerKey,
		in.LegalName, in.PreferredName, in.WorkerNumber,
		in.WorkerType, in.LifecycleStatus,
		in.EmploymentID, in.AssignmentID,
		in.JobCode, in.Grade, in.OrgUnit, in.PositionID, in.Location, in.PayZone,
		in.FTE, in.ManagerRelationshipRef,
		in.HireDate, in.EffectiveFrom,
		in.BasePay, in.Currency, in.PayBasis, in.BonusTarget,
		in.RevisionStream, int64(in.RevisionSequence), in.KnownAt.UTC(), in.RecordedAt.UTC(),
		in.CreatedBy, in.Source)

	stored, err := scanWorker(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return WorkerRow{}, fmt.Errorf("%w: %s", ErrDuplicate, in.WorkerKey)
		}
		return WorkerRow{}, fmt.Errorf("workforce: insert worker %s: %w", in.WorkerKey, err)
	}
	return stored, nil
}

// List returns every worker the tenant has created, newest first.
//
// The tie-break on worker_key is not cosmetic: two workers created inside the
// same clock tick would otherwise come back in an order the database chose,
// and a list whose order changes between two identical reads is a list a page
// cannot render stably.
func (s Store) List(ctx context.Context, ex Executor, tenantID uuid.UUID) ([]WorkerRow, error) {
	rows, err := ex.Query(ctx, `
		SELECT `+columns+`
		FROM journey_worker
		WHERE tenant_id = $1
		ORDER BY recorded_at DESC, worker_key ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("workforce: list workers: %w", err)
	}
	defer rows.Close()

	var out []WorkerRow
	for rows.Next() {
		worker, scanErr := scanWorker(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("workforce: scan worker: %w", scanErr)
		}
		out = append(out, worker)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("workforce: list workers: %w", err)
	}
	return out, nil
}

// Get returns one worker by its worker_key or its worker_id text, reporting
// whether it exists.
//
// Both spellings resolve here because both are how a worker is named on the
// way in: the journey lists a worker by its key and the governed worker read
// asks for it by its entity id, and a lookup that answered only one of those
// would push the caller into deciding which kind of string it is holding.
// Absence is a false, not an error: a worker that is not in this population
// may still be in the corpus, and the layered reader has to be able to ask.
func (s Store) Get(ctx context.Context, ex Executor, tenantID uuid.UUID, workerKeyOrID string) (WorkerRow, bool, error) {
	if workerKeyOrID == "" {
		return WorkerRow{}, false, nil
	}
	row := ex.QueryRow(ctx, `
		SELECT `+columns+`
		FROM journey_worker
		WHERE tenant_id = $1 AND (worker_key = $2 OR worker_id::text = $2)
		LIMIT 1`, tenantID, workerKeyOrID)
	worker, err := scanWorker(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return WorkerRow{}, false, nil
		}
		return WorkerRow{}, false, fmt.Errorf("workforce: read worker %s: %w", workerKeyOrID, err)
	}
	return worker, true, nil
}

// scanner is the one shape [scanWorker] needs, so a single-row read and a
// cursor row are scanned by the same code.
type scanner interface {
	Scan(dest ...any) error
}

// scanWorker reads one row in [columns] order.
func scanWorker(src scanner) (WorkerRow, error) {
	var (
		w         WorkerRow
		sequence  int64
		hire      time.Time
		effective time.Time
	)
	if err := src.Scan(
		&w.TenantID, &w.WorkerID, &w.WorkerKey,
		&w.LegalName, &w.PreferredName, &w.WorkerNumber,
		&w.WorkerType, &w.LifecycleStatus,
		&w.EmploymentID, &w.AssignmentID,
		&w.JobCode, &w.Grade, &w.OrgUnit, &w.PositionID, &w.Location, &w.PayZone,
		&w.FTE, &w.ManagerRelationshipRef,
		&hire, &effective,
		&w.BasePay, &w.Currency, &w.PayBasis, &w.BonusTarget,
		&w.RevisionStream, &sequence, &w.KnownAt, &w.RecordedAt,
		&w.CreatedBy, &w.Source,
	); err != nil {
		return WorkerRow{}, err
	}
	w.RevisionSequence = uint64(sequence)
	w.HireDate = hire.UTC().Format(DateLayout)
	w.EffectiveFrom = effective.UTC().Format(DateLayout)
	w.KnownAt = w.KnownAt.UTC()
	w.RecordedAt = w.RecordedAt.UTC()
	return w, nil
}
