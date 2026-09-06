// Package incentivestore persists the tenant-scoped incentive metadata
// projections from migration 00098.
package incentivestore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/incentive"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// DB is the transaction capability required by Store.
type DB interface{ dbport.Beginner }

// Store implements incentive.Store over PostgreSQL. Each operation owns a
// short transaction and establishes the tenant setting before touching data.
type Store struct{ db DB }

var _ incentive.Store = (*Store)(nil)

// New returns a PostgreSQL incentive store over db.
func New(db DB) *Store { return &Store{db: db} }

// NewStore is the descriptive constructor spelling used by data packages.
func NewStore(db DB) *Store { return New(db) }

// CodeOf returns the stable domain store code carried by err.
func CodeOf(err error) incentive.StoreCode {
	var coded *incentive.StoreError
	if errors.As(err, &coded) {
		return coded.Code
	}
	return ""
}

func storeError(code incentive.StoreCode, detail string) error {
	return &incentive.StoreError{Code: code, Detail: detail}
}

func invalid(detail string) error   { return storeError(incentive.StoreInvalidCode, detail) }
func notFound(detail string) error  { return storeError(incentive.StoreNotFoundCode, detail) }
func duplicate(detail string) error { return storeError(incentive.StoreDuplicateCode, detail) }
func sequenceConflict(detail string) error {
	return storeError(incentive.StoreSequenceCode, detail)
}
func planMismatch(detail string) error {
	return storeError(incentive.StorePlanMismatchCode, detail)
}
func storage(detail string) error { return storeError(incentive.StoreStorageCode, detail) }
func stale(expected, actual, detail string) error {
	return &incentive.StoreError{Code: incentive.StoreStaleCASCode, Expected: expected, Actual: actual, Detail: detail}
}

func parseTenant(tenantID string) (uuid.UUID, error) {
	tid, err := uuid.Parse(strings.TrimSpace(tenantID))
	if err != nil || tid == uuid.Nil {
		return uuid.Nil, invalid(fmt.Sprintf("tenant id %q is not a valid UUID", tenantID))
	}
	return tid, nil
}

func withTenant(ctx context.Context, db DB, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if ctx == nil {
		return invalid("context is required")
	}
	if db == nil {
		return storage("database capability is required")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return storage(fmt.Sprintf("begin transaction: %v", err))
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return storage(err.Error())
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return storage(fmt.Sprintf("commit transaction: %v", err))
	}
	return nil
}

func normalizePlan(plan incentive.IncentivePlanRevision) (incentive.IncentivePlanRevision, error) {
	if plan.CanonicalDigest == "" {
		plan, err := incentive.NewIncentivePlanRevision(plan)
		if err != nil {
			return incentive.IncentivePlanRevision{}, invalid(err.Error())
		}
		return plan, nil
	}
	if err := plan.Validate(); err != nil {
		return incentive.IncentivePlanRevision{}, invalid(err.Error())
	}
	return plan, nil
}

func normalizeObservation(observation incentive.AttainmentObservation) (incentive.AttainmentObservation, error) {
	observation, err := incentive.NewAttainmentObservation(observation)
	if err != nil {
		return incentive.AttainmentObservation{}, invalid(err.Error())
	}
	if _, err := uuid.Parse(observation.WorkerRef); err != nil {
		return incentive.AttainmentObservation{}, invalid("worker_ref must be a UUID")
	}
	return observation, nil
}

func normalizeAward(award incentive.AwardCalculation) (incentive.AwardCalculation, error) {
	if award.CanonicalDigest == "" {
		award, err := incentive.NewAwardCalculation(award)
		if err != nil {
			return incentive.AwardCalculation{}, invalid(err.Error())
		}
		if _, err := uuid.Parse(award.WorkerRef); err != nil {
			return incentive.AwardCalculation{}, invalid("worker_ref must be a UUID")
		}
		return award, nil
	}
	if err := award.Validate(); err != nil {
		return incentive.AwardCalculation{}, invalid(err.Error())
	}
	if _, err := uuid.Parse(award.WorkerRef); err != nil {
		return incentive.AwardCalculation{}, invalid("worker_ref must be a UUID")
	}
	return award, nil
}

func storageDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }

func domainDigest(value string) string {
	if value == "" || strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}

func uint64Value(value int64, field string) (uint64, error) {
	if value < 0 {
		return 0, invalid(fmt.Sprintf("%s is negative", field))
	}
	return uint64(value), nil
}

type revisionState struct {
	revision int64
	digest   string
}

func currentPlan(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, planID string) (revisionState, bool, error) {
	var state revisionState
	err := tx.QueryRow(ctx, `
		SELECT revision, digest::text
		FROM incentive_plan_revision
		WHERE tenant_id=$1 AND plan_id=$2
		ORDER BY revision DESC LIMIT 1`, tenantID, planID).Scan(&state.revision, &state.digest)
	if errors.Is(err, dbport.ErrNoRows) {
		return revisionState{}, false, nil
	}
	if err != nil {
		return revisionState{}, false, storage(fmt.Sprintf("read current plan %s: %v", planID, err))
	}
	return state, true, nil
}

func currentAward(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, calculationID string) (revisionState, bool, error) {
	var state revisionState
	err := tx.QueryRow(ctx, `
		SELECT revision, digest::text
		FROM award_calculation
		WHERE tenant_id=$1 AND calculation_id=$2
		ORDER BY revision DESC LIMIT 1`, tenantID, calculationID).Scan(&state.revision, &state.digest)
	if errors.Is(err, dbport.ErrNoRows) {
		return revisionState{}, false, nil
	}
	if err != nil {
		return revisionState{}, false, storage(fmt.Sprintf("read current award %s: %v", calculationID, err))
	}
	return state, true, nil
}

func lock(ctx context.Context, tx dbport.Tx, key string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
		return storage(fmt.Sprintf("lock %s: %v", key, err))
	}
	return nil
}

func (s *Store) SavePlan(ctx context.Context, tenantID string, plan incentive.IncentivePlanRevision) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	plan, err = normalizePlan(plan)
	if err != nil {
		return err
	}
	return withTenant(ctx, s.db, tid, func(tx dbport.Tx) error {
		if err := lock(ctx, tx, tid.String()+":plan:"+plan.PlanID); err != nil {
			return err
		}
		current, exists, err := currentPlan(ctx, tx, tid, plan.PlanID)
		if err != nil {
			return err
		}
		if exists {
			if int64(plan.Revision) == current.revision {
				return duplicate(fmt.Sprintf("plan %s revision %d", plan.PlanID, plan.Revision))
			}
			if plan.Revision != uint64(current.revision)+1 || plan.SupersedesRevision != uint64(current.revision) {
				return stale(fmt.Sprintf("%d", current.revision), fmt.Sprintf("%d", current.revision), "plan revision does not extend the current revision")
			}
			if storageDigest(plan.ParentDigest) != current.digest {
				return stale(fmt.Sprintf("%d", current.revision), fmt.Sprintf("%d", current.revision), "plan parent digest does not match the current revision")
			}
		} else if plan.Revision != 1 || plan.SupersedesRevision != 0 || plan.ParentDigest != "" {
			return stale("", "", "the first plan revision must be revision 1 without lineage")
		}
		result, err := tx.Exec(ctx, `
			INSERT INTO incentive_plan_revision
				(row_id, tenant_id, plan_id, revision, supersedes_revision, parent_digest, canonical_digest, digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (tenant_id, plan_id, revision) DO NOTHING`,
			uuid.New(), tid, plan.PlanID, int64(plan.Revision), nullableRevision(plan.SupersedesRevision),
			nullableDigest(plan.ParentDigest), storageDigest(plan.CanonicalDigest), storageDigest(plan.Digest))
		if err != nil {
			return classifyPG(err, "insert plan revision")
		}
		if result == 0 {
			return duplicate(fmt.Sprintf("plan %s revision %d", plan.PlanID, plan.Revision))
		}
		return nil
	})
}

func (s *Store) LoadPlan(ctx context.Context, tenantID, planID string, revision uint64) (record incentive.PlanRevisionRecord, err error) {
	tid, parseErr := parseTenant(tenantID)
	if parseErr != nil {
		return record, parseErr
	}
	if strings.TrimSpace(planID) == "" || revision == 0 || revision > math.MaxInt64 {
		return record, invalid("plan id and positive revision are required")
	}
	err = withTenant(ctx, s.db, tid, func(tx dbport.Tx) error {
		var rowID, storedTenant uuid.UUID
		var storedPlanID string
		var storedRevision int64
		var storedSupersedes *int64
		var parent, canonical, digest *string
		scanErr := tx.QueryRow(ctx, `
			SELECT row_id, tenant_id, plan_id, revision, supersedes_revision,
				parent_digest::text, canonical_digest::text, digest::text
			FROM incentive_plan_revision
			WHERE tenant_id=$1 AND plan_id=$2 AND revision=$3`, tid, planID, int64(revision)).
			Scan(&rowID, &storedTenant, &storedPlanID, &storedRevision, &storedSupersedes, &parent, &canonical, &digest)
		if errors.Is(scanErr, dbport.ErrNoRows) {
			return notFound(fmt.Sprintf("plan %s revision %d", planID, revision))
		}
		if scanErr != nil {
			return storage(fmt.Sprintf("load plan %s revision %d: %v", planID, revision, scanErr))
		}
		rev, convErr := uint64Value(storedRevision, "revision")
		if convErr != nil {
			return convErr
		}
		var supersedes uint64
		if storedSupersedes != nil {
			supersedes, convErr = uint64Value(*storedSupersedes, "supersedes_revision")
			if convErr != nil {
				return convErr
			}
		}
		record = incentive.PlanRevisionRecord{RowID: rowID.String(), TenantID: storedTenant.String(), PlanID: storedPlanID,
			Revision: rev, SupersedesRevision: supersedes, ParentDigest: optionalDomainDigest(parent),
			CanonicalDigest: optionalDomainDigest(canonical), Digest: optionalDomainDigest(digest)}
		return nil
	})
	return record, err
}

func (s *Store) AppendObservation(ctx context.Context, tenantID string, observation incentive.AttainmentObservation, eventSequence uint64) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	if eventSequence == 0 || eventSequence > math.MaxInt64 {
		return invalid("event sequence must be positive")
	}
	observation, err = normalizeObservation(observation)
	if err != nil {
		return err
	}
	workerID, _ := uuid.Parse(observation.WorkerRef)
	return withTenant(ctx, s.db, tid, func(tx dbport.Tx) error {
		if err := lock(ctx, tx, tid.String()+":observation:"+observation.WorkerRef+":"+observation.PlanID); err != nil {
			return err
		}
		result, err := tx.Exec(ctx, `
			INSERT INTO attainment_observation
				(row_id, tenant_id, observation_id, plan_id, plan_revision, worker_ref,
				 value, as_of, known_at, source_ref, digest, event_sequence)
			VALUES ($1,$2,$3,$4,$5,$6,$7::numeric,$8::date,$9,$10,$11,$12)
			ON CONFLICT (tenant_id, worker_ref, plan_id, event_sequence) DO NOTHING`,
			uuid.New(), tid, observation.ObservationID, observation.PlanID, int64(observation.PlanRevision), workerID,
			observation.Value.String(), observation.AsOfEffective.String(), observation.AsKnownAt.Instant().Time(),
			observation.SourceRef, storageDigest(observation.Digest), int64(eventSequence))
		if err != nil {
			return classifyPG(err, "insert attainment observation")
		}
		if result == 0 {
			return sequenceConflict(fmt.Sprintf("worker %s plan %s event sequence %d", observation.WorkerRef, observation.PlanID, eventSequence))
		}
		return nil
	})
}

func (s *Store) ListObservations(ctx context.Context, tenantID, workerRef, planID string) (records []incentive.AttainmentObservationRecord, err error) {
	tid, parseErr := parseTenant(tenantID)
	if parseErr != nil {
		return nil, parseErr
	}
	workerID, uuidErr := uuid.Parse(workerRef)
	if uuidErr != nil || strings.TrimSpace(planID) == "" {
		return nil, invalid("worker ref and plan id are required")
	}
	err = withTenant(ctx, s.db, tid, func(tx dbport.Tx) error {
		rows, queryErr := tx.Query(ctx, `
			SELECT row_id, tenant_id, observation_id, plan_id, plan_revision, worker_ref,
				value::text, as_of::text, known_at, source_ref, digest::text, event_sequence
			FROM attainment_observation
			WHERE tenant_id=$1 AND worker_ref=$2 AND plan_id=$3
			ORDER BY event_sequence`, tid, workerID, planID)
		if queryErr != nil {
			return storage(fmt.Sprintf("list observations: %v", queryErr))
		}
		defer rows.Close()
		for rows.Next() {
			var rowID, storedTenant, storedWorker uuid.UUID
			var observationID, storedPlanID, valueText, asOfText, sourceRef, digest string
			var planRevision, eventSequence int64
			var knownAt time.Time
			if scanErr := rows.Scan(&rowID, &storedTenant, &observationID, &storedPlanID, &planRevision, &storedWorker,
				&valueText, &asOfText, &knownAt, &sourceRef, &digest, &eventSequence); scanErr != nil {
				return storage(fmt.Sprintf("scan observation: %v", scanErr))
			}
			if planRevision < 0 || eventSequence < 0 {
				return invalid("stored observation counter is negative")
			}
			if _, decimalErr := values.NewDecimal(valueText, 4, values.RoundingExactRequired); decimalErr != nil {
				return invalid(fmt.Sprintf("stored observation value: %v", decimalErr))
			}
			records = append(records, incentive.AttainmentObservationRecord{RowID: rowID.String(), TenantID: storedTenant.String(),
				ObservationID: observationID, PlanID: storedPlanID, PlanRevision: uint64(planRevision), WorkerRef: storedWorker.String(),
				Value: valueText, AsOf: asOfText, KnownAt: knownAt.UTC().Format(time.RFC3339Nano), SourceRef: sourceRef,
				Digest: domainDigest(digest), EventSequence: uint64(eventSequence)})
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			return storage(fmt.Sprintf("list observations: %v", rowsErr))
		}
		return nil
	})
	return records, err
}

func (s *Store) SaveAward(ctx context.Context, tenantID string, award incentive.AwardCalculation) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	award, err = normalizeAward(award)
	if err != nil {
		return err
	}
	workerID, _ := uuid.Parse(award.WorkerRef)
	return withTenant(ctx, s.db, tid, func(tx dbport.Tx) error {
		if err := lock(ctx, tx, tid.String()+":award:"+award.CalculationID); err != nil {
			return err
		}
		var planExists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM incentive_plan_revision
				WHERE tenant_id=$1 AND revision=$2 AND digest=$3)`,
			tid, int64(award.PlanRevision), storageDigest(award.PlanDigest)).Scan(&planExists); err != nil {
			return storage(fmt.Sprintf("check plan digest: %v", err))
		}
		if !planExists {
			return planMismatch(fmt.Sprintf("plan revision %d does not carry digest %q", award.PlanRevision, award.PlanDigest))
		}
		current, exists, err := currentAward(ctx, tx, tid, award.CalculationID)
		if err != nil {
			return err
		}
		if exists {
			if int64(award.Revision) == current.revision {
				return duplicate(fmt.Sprintf("award %s revision %d", award.CalculationID, award.Revision))
			}
			if award.Revision != uint64(current.revision)+1 || award.SupersedesRevision != uint64(current.revision) {
				return stale(fmt.Sprintf("%d", current.revision), fmt.Sprintf("%d", current.revision), "award revision does not extend the current revision")
			}
		} else if award.Revision != 1 || award.SupersedesRevision != 0 {
			return stale("", "", "the first award revision must be revision 1")
		}
		result, err := tx.Exec(ctx, `
			INSERT INTO award_calculation
				(row_id, tenant_id, calculation_id, worker_ref, plan_digest, plan_revision,
				 state, revision, supersedes_revision, canonical_digest, digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (tenant_id, calculation_id, revision) DO NOTHING`,
			uuid.New(), tid, award.CalculationID, workerID, storageDigest(award.PlanDigest), int64(award.PlanRevision),
			string(award.State), int64(award.Revision), nullableRevision(award.SupersedesRevision),
			storageDigest(award.CanonicalDigest), storageDigest(award.Digest))
		if err != nil {
			return classifyPG(err, "insert award calculation")
		}
		if result == 0 {
			return duplicate(fmt.Sprintf("award %s revision %d", award.CalculationID, award.Revision))
		}
		return nil
	})
}

func (s *Store) LoadAward(ctx context.Context, tenantID, calculationID string, revision uint64) (record incentive.AwardCalculationRecord, err error) {
	tid, parseErr := parseTenant(tenantID)
	if parseErr != nil {
		return record, parseErr
	}
	if strings.TrimSpace(calculationID) == "" || revision == 0 || revision > math.MaxInt64 {
		return record, invalid("calculation id and positive revision are required")
	}
	err = withTenant(ctx, s.db, tid, func(tx dbport.Tx) error {
		var rowID, storedTenant, workerID uuid.UUID
		var storedCalculationID, planDigest, state, canonical, digest string
		var planRevision, storedRevision int64
		var storedSupersedes *int64
		scanErr := tx.QueryRow(ctx, `
			SELECT row_id, tenant_id, calculation_id, worker_ref, plan_digest::text, plan_revision,
				state, revision, supersedes_revision, canonical_digest::text, digest::text
			FROM award_calculation
			WHERE tenant_id=$1 AND calculation_id=$2 AND revision=$3`, tid, calculationID, int64(revision)).
			Scan(&rowID, &storedTenant, &storedCalculationID, &workerID, &planDigest, &planRevision,
				&state, &storedRevision, &storedSupersedes, &canonical, &digest)
		if errors.Is(scanErr, dbport.ErrNoRows) {
			return notFound(fmt.Sprintf("award %s revision %d", calculationID, revision))
		}
		if scanErr != nil {
			return storage(fmt.Sprintf("load award %s revision %d: %v", calculationID, revision, scanErr))
		}
		if planRevision < 0 || storedRevision < 0 || (storedSupersedes != nil && *storedSupersedes < 0) {
			return invalid("stored award counter is negative")
		}
		var supersedes uint64
		if storedSupersedes != nil {
			supersedes = uint64(*storedSupersedes)
		}
		record = incentive.AwardCalculationRecord{RowID: rowID.String(), TenantID: storedTenant.String(), CalculationID: storedCalculationID,
			WorkerRef: workerID.String(), PlanDigest: domainDigest(planDigest), PlanRevision: uint64(planRevision), State: incentive.AwardState(state),
			Revision: uint64(storedRevision), SupersedesRevision: supersedes, CanonicalDigest: domainDigest(canonical), Digest: domainDigest(digest)}
		return nil
	})
	return record, err
}

// PutPlan is an alias for SavePlan.
func (s *Store) PutPlan(ctx context.Context, tenantID string, plan incentive.IncentivePlanRevision) error {
	return s.SavePlan(ctx, tenantID, plan)
}

// SaveObservation is an alias for AppendObservation.
func (s *Store) SaveObservation(ctx context.Context, tenantID string, observation incentive.AttainmentObservation, eventSequence uint64) error {
	return s.AppendObservation(ctx, tenantID, observation, eventSequence)
}

// SaveAwardCalculation is an alias for SaveAward.
func (s *Store) SaveAwardCalculation(ctx context.Context, tenantID string, award incentive.AwardCalculation) error {
	return s.SaveAward(ctx, tenantID, award)
}

func nullableRevision(value uint64) any {
	if value == 0 {
		return nil
	}
	return int64(value)
}

func nullableDigest(value string) any {
	if value == "" {
		return nil
	}
	return storageDigest(value)
}

func optionalDomainDigest(value *string) string {
	if value == nil {
		return ""
	}
	return domainDigest(*value)
}

func classifyPG(err error, operation string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return duplicate(operation + ": unique constraint refused the write")
		case "23514", "23503", "42501", "23001":
			return storage(operation + ": " + pgErr.Message)
		}
	}
	return storage(fmt.Sprintf("%s: %v", operation, err))
}
