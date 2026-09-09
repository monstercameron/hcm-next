// Package equitystore persists the tenant-scoped equity revision and
// acceptance-evidence tables from migrations/00090_equity.sql.
package equitystore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/equity"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DB is the transaction-opening capability supplied by a composition root.
// Tenant context is established inside every transaction before any table is
// touched, so the adapter cannot accidentally reuse one tenant's session.
type DB interface{ dbport.Beginner }

// Store implements equity.Store over PostgreSQL.
type Store struct{ db DB }

var _ equity.Store = (*Store)(nil)

// New constructs a store over db. The database handle is not tenant-bound;
// each operation receives its tenant identity through the domain port.
func New(db DB) *Store { return &Store{db: db} }

// NewStore is the descriptive constructor spelling used by data packages.
func NewStore(db DB) *Store { return New(db) }

func (s *Store) withTenant(ctx context.Context, tenantText string, fn func(dbport.Tx, uuid.UUID) error) error {
	if s == nil || s.db == nil {
		return coded(equity.StoreInvalidCode, "database capability is required")
	}
	tenantID, err := parseTenant(tenantText)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("equitystore: begin transaction: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("equitystore: commit transaction: %w", err)
	}
	return nil
}

func parseTenant(text string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(text))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, coded(equity.StoreInvalidCode, "tenant id must be a non-nil UUID")
	}
	return id, nil
}

func (s *Store) SavePlan(ctx context.Context, tenantID string, plan equity.EquityPlanRevision) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	if plan.CanonicalDigest == "" {
		plan, err = equity.NewEquityPlanRevision(plan)
	} else {
		err = plan.Validate()
	}
	if err != nil {
		return coded(equity.StoreInvalidCode, err.Error())
	}
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx, tid uuid.UUID) error {
		if err := checkHead(ctx, tx, "equity_plan_revision", "plan_id", tid, plan.PlanID, plan.Revision, plan.SupersedesRevision, plan.ParentDigest); err != nil {
			return err
		}
		quantity, err := fixedDecimal(plan.AuthorizedQuantity)
		if err != nil {
			return coded(equity.StoreInvalidCode, "authorized_quantity: "+err.Error())
		}
		instruments, err := json.Marshal(plan.InstrumentKinds)
		if err != nil {
			return fmt.Errorf("equitystore: encode instrument kinds: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO equity_plan_revision (
				row_id, tenant_id, plan_id, revision, name, pool_ref,
				authorized_quantity, currency, instrument_kinds, approval_ref,
				parent_digest, supersedes_revision, canonical_digest, digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7::text::numeric,$8,$9::jsonb,$10,$11,$12,$13,$13)`,
			uuid.New(), tid, plan.PlanID, int64(plan.Revision), plan.Name, plan.PoolRef,
			quantity, plan.Currency, string(instruments), plan.ApprovalRef,
			nullableDigest(plan.ParentDigest), nullableRevision(plan.SupersedesRevision),
			storageDigest(plan.CanonicalDigest))
		if err != nil {
			return classify(equity.StoreDuplicateCode, "plan revision already exists", err)
		}
		return nil
	})
}

// PutPlan is an alias for SavePlan.
func (s *Store) PutPlan(ctx context.Context, tenantID string, plan equity.EquityPlanRevision) error {
	return s.SavePlan(ctx, tenantID, plan)
}

func (s *Store) LoadPlan(ctx context.Context, tenantID, planID string, revision uint64) (equity.EquityPlanRevision, error) {
	var out equity.EquityPlanRevision
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx, tid uuid.UUID) error {
		var (
			storedID, name, poolRef, currency, approvalRef, quantityText, instrumentsJSON, canonicalDigest, digest string
			parentDigest                                                                                           *string
			supersedes, storedRevision                                                                             *int64
		)
		err := tx.QueryRow(ctx, `
			SELECT plan_id, revision, name, pool_ref, authorized_quantity::text,
				currency, instrument_kinds::text, approval_ref, parent_digest,
				supersedes_revision, canonical_digest, digest
			FROM equity_plan_revision
			WHERE tenant_id=$1 AND plan_id=$2 AND revision=$3`, tid, planID, int64(revision)).Scan(
			&storedID, &storedRevision, &name, &poolRef, &quantityText, &currency,
			&instrumentsJSON, &approvalRef, &parentDigest, &supersedes, &canonicalDigest, &digest)
		if err != nil {
			return noRows(equity.StoreNotFoundCode, fmt.Sprintf("plan %s revision %d", planID, revision), err)
		}
		if storedRevision == nil || strings.TrimSpace(quantityText) == "" || strings.TrimSpace(instrumentsJSON) == "" {
			return coded(equity.StoreIntegrityCode, "plan revision has a null required payload")
		}
		var instruments []equity.InstrumentKind
		if err := json.Unmarshal([]byte(instrumentsJSON), &instruments); err != nil {
			return coded(equity.StoreIntegrityCode, "plan instrument kinds are not valid JSON")
		}
		for _, quantity := range decimalCandidates(quantityText) {
			candidate := equity.EquityPlanRevision{
				PlanID: storedID, Revision: uint64(*storedRevision), Name: name, PoolRef: poolRef,
				AuthorizedQuantity: quantity, Currency: currency, InstrumentKinds: instruments,
				ApprovalRef: approvalRef, ParentDigest: domainDigest(stringValue(parentDigest)),
				SupersedesRevision: uint64(int64Value(supersedes)), CanonicalDigest: domainDigest(canonicalDigest), Digest: domainDigest(digest),
			}
			stored, buildErr := equity.NewEquityPlanRevision(candidate)
			if buildErr == nil && stored.CanonicalDigest == domainDigest(canonicalDigest) && stored.Digest == domainDigest(digest) {
				out = stored
				return nil
			}
		}
		return coded(equity.StoreIntegrityCode, "plan revision digest does not match stored payload")
	})
	return out, err
}

// GetPlan is an alias for LoadPlan.
func (s *Store) GetPlan(ctx context.Context, tenantID, planID string, revision uint64) (equity.EquityPlanRevision, error) {
	return s.LoadPlan(ctx, tenantID, planID, revision)
}

func (s *Store) ListPlans(ctx context.Context, tenantID, planID string) ([]equity.EquityPlanRevision, error) {
	var out []equity.EquityPlanRevision
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx, tid uuid.UUID) error {
		rows, err := tx.Query(ctx, `SELECT revision FROM equity_plan_revision WHERE tenant_id=$1 AND plan_id=$2 ORDER BY revision`, tid, planID)
		if err != nil {
			return fmt.Errorf("equitystore: list plans: %w", err)
		}
		defer rows.Close()
		var revisions []uint64
		for rows.Next() {
			var revision int64
			if err := rows.Scan(&revision); err != nil {
				return fmt.Errorf("equitystore: scan plan revision: %w", err)
			}
			revisions = append(revisions, uint64(revision))
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("equitystore: list plans: %w", err)
		}
		if len(revisions) == 0 {
			return coded(equity.StoreNotFoundCode, fmt.Sprintf("plan %s", planID))
		}
		for _, revision := range revisions {
			plan, err := s.loadPlanTx(ctx, tx, tid, planID, revision)
			if err != nil {
				return err
			}
			out = append(out, plan)
		}
		return nil
	})
	return out, err
}

func (s *Store) loadPlanTx(ctx context.Context, tx dbport.Tx, tid uuid.UUID, planID string, revision uint64) (equity.EquityPlanRevision, error) {
	// LoadPlan's transaction wrapper is intentionally kept separate from this
	// helper; ListPlans already has the tenant setting in place.
	var (
		storedID, name, poolRef, currency, approvalRef, quantityText, instrumentsJSON, canonicalDigest, digest string
		parentDigest                                                                                           *string
		supersedes, storedRevision                                                                             *int64
	)
	err := tx.QueryRow(ctx, `SELECT plan_id, revision, name, pool_ref, authorized_quantity::text, currency, instrument_kinds::text, approval_ref, parent_digest, supersedes_revision, canonical_digest, digest FROM equity_plan_revision WHERE tenant_id=$1 AND plan_id=$2 AND revision=$3`, tid, planID, int64(revision)).Scan(&storedID, &storedRevision, &name, &poolRef, &quantityText, &currency, &instrumentsJSON, &approvalRef, &parentDigest, &supersedes, &canonicalDigest, &digest)
	if err != nil {
		return equity.EquityPlanRevision{}, noRows(equity.StoreNotFoundCode, "plan revision is absent", err)
	}
	var instruments []equity.InstrumentKind
	if json.Unmarshal([]byte(instrumentsJSON), &instruments) != nil || storedRevision == nil {
		return equity.EquityPlanRevision{}, coded(equity.StoreIntegrityCode, "plan payload is invalid")
	}
	for _, quantity := range decimalCandidates(quantityText) {
		candidate := equity.EquityPlanRevision{PlanID: storedID, Revision: uint64(*storedRevision), Name: name, PoolRef: poolRef, AuthorizedQuantity: quantity, Currency: currency, InstrumentKinds: instruments, ApprovalRef: approvalRef, ParentDigest: domainDigest(stringValue(parentDigest)), SupersedesRevision: uint64(int64Value(supersedes)), CanonicalDigest: domainDigest(canonicalDigest), Digest: domainDigest(digest)}
		if stored, err := equity.NewEquityPlanRevision(candidate); err == nil && stored.CanonicalDigest == domainDigest(canonicalDigest) {
			return stored, nil
		}
	}
	return equity.EquityPlanRevision{}, coded(equity.StoreIntegrityCode, "plan digest does not match payload")
}

func (s *Store) SaveGrant(ctx context.Context, tenantID string, grant equity.EquityGrant) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	if grant.CanonicalDigest == "" {
		grant, err = equity.NewEquityGrant(grant)
	} else {
		err = grant.Validate()
	}
	if err != nil {
		return coded(equity.StoreInvalidCode, err.Error())
	}
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx, tid uuid.UUID) error {
		var planID string
		if err := tx.QueryRow(ctx, `SELECT plan_id FROM equity_plan_revision WHERE tenant_id=$1 AND revision=$2 AND canonical_digest=$3 AND digest=$3`, tid, int64(grant.PlanRevision), storageDigest(grant.PlanDigest)).Scan(&planID); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return coded(equity.StorePlanMismatchCode, "grant plan digest does not name a stored plan revision")
			}
			return fmt.Errorf("equitystore: resolve grant plan: %w", err)
		}
		plan, err := loadPlanTx(ctx, tx, tid, planID, grant.PlanRevision)
		if err != nil {
			return err
		}
		if err := plan.ValidateGrant(grant); err != nil {
			return coded(equity.StorePlanMismatchCode, err.Error())
		}
		if err := checkHead(ctx, tx, "equity_grant", "grant_id", tid, grant.GrantID, grant.Revision, grant.SupersedesRevision, grant.ParentDigest); err != nil {
			return err
		}
		quantity, err := fixedDecimal(grant.Quantity)
		if err != nil {
			return coded(equity.StoreInvalidCode, "quantity: "+err.Error())
		}
		strike, err := fixedDecimal(grant.StrikePrice)
		if err != nil {
			return coded(equity.StoreInvalidCode, "strike_price: "+err.Error())
		}
		vesting, err := json.Marshal(grant.Vesting)
		if err != nil {
			return fmt.Errorf("equitystore: encode vesting: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO equity_grant (
				row_id, tenant_id, grant_id, revision, plan_id, plan_digest,
				plan_revision, pool_ref, worker_ref, instrument_kind, quantity,
				grant_date, strike_price, currency, vesting, state,
				acceptance_digest, approval_ref, evidence_ref, parent_digest,
				supersedes_revision, canonical_digest, digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::text::numeric,$12::text::date,$13::text::numeric,$14,$15::jsonb,$16,$17,$18,$19,$20,$21,$22,$22)`,
			uuid.New(), tid, grant.GrantID, int64(grant.Revision), planID, storageDigest(grant.PlanDigest), int64(grant.PlanRevision),
			grant.PoolRef, grant.WorkerRef, string(grant.Instrument), quantity, grant.GrantDate.String(), strike, grant.Currency,
			string(vesting), string(grant.State), nullableDigest(grant.AcceptanceDigest), grant.ApprovalRef, grant.EvidenceRef,
			nullableDigest(grant.ParentDigest), nullableRevision(grant.SupersedesRevision), storageDigest(grant.CanonicalDigest))
		if err != nil {
			return classify(equity.StoreDuplicateCode, "grant revision already exists", err)
		}
		return nil
	})
}

// PutGrant is an alias for SaveGrant.
func (s *Store) PutGrant(ctx context.Context, tenantID string, grant equity.EquityGrant) error {
	return s.SaveGrant(ctx, tenantID, grant)
}

func (s *Store) LoadGrant(ctx context.Context, tenantID, grantID string, revision uint64) (equity.EquityGrant, error) {
	var out equity.EquityGrant
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx, tid uuid.UUID) error {
		var err error
		out, err = loadGrantTx(ctx, tx, tid, grantID, revision)
		return err
	})
	return out, err
}

// GetGrant is an alias for LoadGrant.
func (s *Store) GetGrant(ctx context.Context, tenantID, grantID string, revision uint64) (equity.EquityGrant, error) {
	return s.LoadGrant(ctx, tenantID, grantID, revision)
}

func (s *Store) ListGrants(ctx context.Context, tenantID, grantID string) ([]equity.EquityGrant, error) {
	var out []equity.EquityGrant
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx, tid uuid.UUID) error {
		rows, err := tx.Query(ctx, `SELECT revision FROM equity_grant WHERE tenant_id=$1 AND grant_id=$2 ORDER BY revision`, tid, grantID)
		if err != nil {
			return fmt.Errorf("equitystore: list grants: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var revision int64
			if err := rows.Scan(&revision); err != nil {
				return fmt.Errorf("equitystore: scan grant revision: %w", err)
			}
			grant, err := loadGrantTx(ctx, tx, tid, grantID, uint64(revision))
			if err != nil {
				return err
			}
			out = append(out, grant)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("equitystore: list grants: %w", err)
		}
		if len(out) == 0 {
			return coded(equity.StoreNotFoundCode, fmt.Sprintf("grant %s", grantID))
		}
		return nil
	})
	return out, err
}

func loadGrantTx(ctx context.Context, tx dbport.Tx, tid uuid.UUID, grantID string, revision uint64) (equity.EquityGrant, error) {
	var (
		storedID, planDigest, poolRef, workerRef, instrument, currency, state, quantityText, strikeText, vestingJSON, canonicalDigest, digest string
		grantDate                                                                                                                             *time.Time
		planRevision, storedRevision                                                                                                          int64
		acceptanceDigest, approvalRef, evidenceRef, parentDigest                                                                              *string
		supersedes                                                                                                                            *int64
	)
	err := tx.QueryRow(ctx, `
		SELECT grant_id, revision, plan_digest, plan_revision, pool_ref, worker_ref,
			instrument_kind, quantity::text, grant_date, strike_price::text, currency,
			vesting::text, state, acceptance_digest, approval_ref, evidence_ref,
			parent_digest, supersedes_revision, canonical_digest, digest
		FROM equity_grant WHERE tenant_id=$1 AND grant_id=$2 AND revision=$3`, tid, grantID, int64(revision)).Scan(
		&storedID, &storedRevision, &planDigest, &planRevision, &poolRef, &workerRef, &instrument,
		&quantityText, &grantDate, &strikeText, &currency, &vestingJSON, &state, &acceptanceDigest,
		&approvalRef, &evidenceRef, &parentDigest, &supersedes, &canonicalDigest, &digest)
	if err != nil {
		return equity.EquityGrant{}, noRows(equity.StoreNotFoundCode, fmt.Sprintf("grant %s revision %d", grantID, revision), err)
	}
	if grantDate == nil || strings.TrimSpace(vestingJSON) == "" {
		return equity.EquityGrant{}, coded(equity.StoreIntegrityCode, "grant has a null required payload")
	}
	date, err := values.NewLocalDate(grantDate.Year(), grantDate.Month(), grantDate.Day())
	if err != nil {
		return equity.EquityGrant{}, coded(equity.StoreIntegrityCode, "grant date is invalid")
	}
	var vesting equity.VestingSchedule
	if err := json.Unmarshal([]byte(vestingJSON), &vesting); err != nil {
		return equity.EquityGrant{}, coded(equity.StoreIntegrityCode, "grant vesting is not valid JSON")
	}
	for _, quantity := range decimalCandidates(quantityText) {
		for _, strike := range decimalCandidates(strikeText) {
			candidate := equity.EquityGrant{
				GrantID: storedID, Revision: uint64(storedRevision), PlanDigest: domainDigest(planDigest), PlanRevision: uint64(planRevision),
				PoolRef: poolRef, WorkerRef: workerRef, Instrument: equity.InstrumentKind(instrument), Quantity: quantity, GrantDate: date,
				StrikePrice: strike, Currency: currency, Vesting: vesting, State: equity.GrantState(state), AcceptanceDigest: domainDigest(stringValue(acceptanceDigest)),
				ApprovalRef: stringValue(approvalRef), EvidenceRef: stringValue(evidenceRef), ParentDigest: domainDigest(stringValue(parentDigest)),
				SupersedesRevision: uint64(int64Value(supersedes)), CanonicalDigest: domainDigest(canonicalDigest), Digest: domainDigest(digest),
			}
			stored, buildErr := equity.NewEquityGrant(candidate)
			if buildErr == nil && stored.CanonicalDigest == domainDigest(canonicalDigest) && stored.Digest == domainDigest(digest) {
				return stored, nil
			}
		}
	}
	return equity.EquityGrant{}, coded(equity.StoreIntegrityCode, "grant digest does not match stored payload")
}

func (s *Store) RecordAcceptance(ctx context.Context, tenantID string, event equity.AcceptanceEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	if event.CanonicalDigest == "" {
		event, err = equity.NewAcceptanceEvent(event)
	} else {
		err = event.Validate()
	}
	if err != nil {
		return coded(equity.StoreInvalidCode, err.Error())
	}
	acceptedBy, err := uuid.Parse(event.AcceptedBy)
	if err != nil || acceptedBy == uuid.Nil {
		return coded(equity.StoreInvalidCode, "accepted_by must be a non-nil UUID")
	}
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx, tid uuid.UUID) error {
		lockKey := tid.String() + ":equity-acceptance:" + storageDigest(event.GrantDigest)
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
			return fmt.Errorf("equitystore: lock acceptance sequence: %w", err)
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM equity_grant WHERE tenant_id=$1 AND digest=$2 AND revision=$3)`, tid, storageDigest(event.GrantDigest), int64(event.GrantRevision)).Scan(&exists); err != nil {
			return fmt.Errorf("equitystore: check acceptance grant: %w", err)
		}
		if !exists {
			return coded(equity.StoreIntegrityCode, "acceptance event names no stored grant revision")
		}
		var sequence int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(event_sequence),0)+1 FROM equity_acceptance_event WHERE tenant_id=$1 AND grant_digest=$2`, tid, storageDigest(event.GrantDigest)).Scan(&sequence); err != nil {
			return fmt.Errorf("equitystore: allocate acceptance sequence: %w", err)
		}
		affected, err := tx.Exec(ctx, `
			INSERT INTO equity_acceptance_event (
				row_id, tenant_id, event_id, grant_digest, grant_revision, accepted_by,
				accepted_at, evidence_ref, canonical_digest, digest, event_sequence)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9,$10)
			ON CONFLICT DO NOTHING`, uuid.New(), tid, event.EventID, storageDigest(event.GrantDigest), int64(event.GrantRevision), acceptedBy,
			event.AcceptedAt.Time(), event.EvidenceRef, storageDigest(event.CanonicalDigest), sequence)
		if err != nil {
			return classify(equity.StoreDuplicateCode, "acceptance event conflicts with stored evidence", err)
		}
		if affected == 0 {
			return coded(equity.StoreDuplicateCode, "acceptance event already exists")
		}
		return nil
	})
}

// AppendAcceptance is an alias for RecordAcceptance.
func (s *Store) AppendAcceptance(ctx context.Context, tenantID string, event equity.AcceptanceEvent) error {
	return s.RecordAcceptance(ctx, tenantID, event)
}

func (s *Store) ListAcceptanceEvents(ctx context.Context, tenantID, grantDigest string) ([]equity.AcceptanceEvent, error) {
	var out []equity.AcceptanceEvent
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx, tid uuid.UUID) error {
		rows, err := tx.Query(ctx, `
			SELECT event_id, grant_digest, grant_revision, accepted_by, accepted_at,
				evidence_ref, canonical_digest, digest
			FROM equity_acceptance_event
			WHERE tenant_id=$1 AND grant_digest=$2 ORDER BY event_sequence`, tid, storageDigest(grantDigest))
		if err != nil {
			return fmt.Errorf("equitystore: list acceptance events: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				event                                  equity.AcceptanceEvent
				grantRevision                          int64
				acceptedBy                             uuid.UUID
				acceptedAt                             time.Time
				grantDigestDB, canonicalDigest, digest string
			)
			if err := rows.Scan(&event.EventID, &grantDigestDB, &grantRevision, &acceptedBy, &acceptedAt, &event.EvidenceRef, &canonicalDigest, &digest); err != nil {
				return fmt.Errorf("equitystore: scan acceptance event: %w", err)
			}
			event.GrantDigest, event.GrantRevision, event.AcceptedBy = domainDigest(grantDigestDB), uint64(grantRevision), acceptedBy.String()
			event.AcceptedAt = values.NewInstant(acceptedAt.UTC())
			event.CanonicalDigest, event.Digest = domainDigest(canonicalDigest), domainDigest(digest)
			if err := event.Validate(); err != nil {
				return coded(equity.StoreIntegrityCode, "acceptance event digest does not match payload")
			}
			out = append(out, event)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("equitystore: list acceptance events: %w", err)
		}
		if len(out) == 0 {
			return coded(equity.StoreNotFoundCode, "acceptance events are absent")
		}
		return nil
	})
	return out, err
}

func checkHead(ctx context.Context, tx dbport.Tx, table, idColumn string, tenantID uuid.UUID, id string, revision, supersedes uint64, parentDigest string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, tenantID.String()+":"+table+":"+id); err != nil {
		return fmt.Errorf("equitystore: lock %s head: %w", table, err)
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+table+` WHERE tenant_id=$1 AND `+idColumn+`=$2 AND revision=$3)`, tenantID, id, int64(revision)).Scan(&exists); err != nil {
		return fmt.Errorf("equitystore: check duplicate %s: %w", table, err)
	}
	if exists {
		return coded(equity.StoreDuplicateCode, fmt.Sprintf("%s %s revision %d", table, id, revision))
	}
	var currentRevision int64
	var currentDigest string
	err := tx.QueryRow(ctx, `SELECT revision, canonical_digest FROM `+table+` WHERE tenant_id=$1 AND `+idColumn+`=$2 ORDER BY revision DESC LIMIT 1`, tenantID, id).Scan(&currentRevision, &currentDigest)
	if errors.Is(err, dbport.ErrNoRows) {
		if revision != 1 || supersedes != 0 || parentDigest != "" {
			return coded(equity.StoreStaleCASCode, "initial revision must be revision 1 without lineage")
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("equitystore: read %s head: %w", table, err)
	}
	if revision != uint64(currentRevision)+1 || supersedes != uint64(currentRevision) || storageDigest(parentDigest) != currentDigest {
		return coded(equity.StoreStaleCASCode, fmt.Sprintf("%s %s current revision is %d", table, id, currentRevision))
	}
	return nil
}

func loadPlanTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, planID string, revision uint64) (equity.EquityPlanRevision, error) {
	store := &Store{}
	return store.loadPlanTx(ctx, tx, tenantID, planID, revision)
}

func fixedDecimal(value values.Decimal) (string, error) {
	quantized, err := value.Quantize(4, values.RoundingExactRequired)
	if err != nil {
		return "", err
	}
	return quantized.String(), nil
}

func decimalCandidates(text string) []values.Decimal {
	text = strings.TrimSpace(text)
	if point := strings.IndexByte(text, '.'); point >= 0 {
		fraction := strings.TrimRight(text[point+1:], "0")
		text = text[:point]
		if fraction != "" {
			text += "." + fraction
		}
	}
	out := make([]values.Decimal, 0, 5)
	for scale := int32(0); scale <= 4; scale++ {
		if value, err := values.NewDecimal(text, scale, values.RoundingExactRequired); err == nil {
			out = append(out, value)
		}
	}
	return out
}

func storageDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }

func domainDigest(value string) string {
	if value == "" || strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}

func nullableDigest(value string) any {
	if value == "" {
		return nil
	}
	return storageDigest(value)
}

func nullableRevision(value uint64) any {
	if value == 0 {
		return nil
	}
	return int64(value)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func coded(code equity.StoreErrorCode, detail string) error {
	return &equity.StoreError{Code: code, Detail: detail}
}

func noRows(code equity.StoreErrorCode, detail string, err error) error {
	if errors.Is(err, dbport.ErrNoRows) {
		return coded(code, detail)
	}
	return fmt.Errorf("equitystore: %s: %w", detail, err)
}

func classify(code equity.StoreErrorCode, detail string, err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return coded(code, detail)
		case "23503", "23514", "23502", "23522":
			return coded(equity.StoreIntegrityCode, detail)
		}
	}
	return fmt.Errorf("equitystore: %s: %w", detail, err)
}
