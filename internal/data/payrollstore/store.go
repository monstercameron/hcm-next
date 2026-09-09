// Package payrollstore persists the payroll lifecycle revisions defined by
// internal/domains/payroll. It owns no domain decisions: it validates the
// kernel values, scopes each transaction with tenancy.WithTenant, and maps
// database uniqueness/CAS refusals to the domain's typed refusal codes.
package payrollstore

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
	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DB is the minimal capability needed by [Store]. A connection or pool that
// can begin a transaction satisfies it.
type DB interface{ dbport.Beginner }

// Store implements [payroll.Store] over migrations/00046_payroll_run.sql.
// Each public operation owns one transaction so the RLS scope and the write
// are established and committed together.
type Store struct{ db DB }

var _ payroll.Store = (*Store)(nil)

// New returns a PostgreSQL payroll store over db.
func New(db DB) *Store { return &Store{db: db} }

func (s *Store) withTenant(ctx context.Context, tenantID payroll.TenantID, fn func(dbport.Tx, uuid.UUID) error) error {
	if ctx == nil {
		return refuse("PAYROLL_INVALID_CONTEXT", "context", "context is required", payroll.ErrStoreRefused)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenantID == nil || tenantID.String() == "" {
		return refuse("PAYROLL_INVALID_TENANT", "tenant_id", "tenant id is required", payroll.ErrStoreRefused)
	}
	scopedTenant, err := uuid.Parse(tenantID.String())
	if err != nil || scopedTenant == uuid.Nil {
		return refuse("PAYROLL_INVALID_TENANT", "tenant_id", "tenant id must be a non-nil UUID", payroll.ErrStoreRefused)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("payrollstore: begin transaction: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, scopedTenant); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx, scopedTenant); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("payrollstore: commit transaction: %w", err)
	}
	return nil
}

// SaveRun implements [payroll.Store].
func (s *Store) SaveRun(ctx context.Context, tenantID payroll.TenantID, run payroll.PayrollRun) error {
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx, scopedTenant uuid.UUID) error {
		return s.SaveRunTx(ctx, tx, scopedTenant, run)
	})
}

// SaveRunTx stores one immutable run revision in the caller's transaction.
func (s *Store) SaveRunTx(ctx context.Context, ex dbport.Tx, tenantID uuid.UUID, run payroll.PayrollRun) error {
	run, period, binding, err := encodeRun(run)
	if err != nil {
		return err
	}
	if err := checkNextRun(ctx, ex, tenantID, run); err != nil {
		return err
	}
	_, err = ex.Exec(ctx, `
		INSERT INTO payroll_run (
			tenant_id, row_id, run_id, revision, state, pay_group_ref, period,
			population_binding_ref, calculation_input_digest, calculation_digest,
			release_digest, reversal_digest, supersedes_revision, canonical_digest)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11, $12, $13, $14)`,
		tenantID, uuid.New(), run.RunID, int64(run.Revision), run.State.String(), run.PayGroupRef,
		period, binding, nullableString(storageDigest(run.CalculationInputDigest)), nullableString(storageDigest(run.CalculationDigest)),
		nullableString(storageDigest(run.ReleaseDigest)), nullableString(storageDigest(run.ReversalDigest)), nullableRevision(run.SupersedesRevision),
		storageDigest(run.CanonicalDigest))
	if err != nil {
		return mapWriteError("save payroll run", run.RunID, err)
	}
	return nil
}

// LoadRun implements [payroll.Store].
func (s *Store) LoadRun(ctx context.Context, tenantID payroll.TenantID, runID string, revision uint64) (payroll.PayrollRun, error) {
	var out payroll.PayrollRun
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx, scopedTenant uuid.UUID) error {
		var err error
		out, err = s.LoadRunTx(ctx, tx, scopedTenant, runID, revision)
		return err
	})
	return out, err
}

// LoadRunTx loads one run revision in the caller's transaction.
func (s *Store) LoadRunTx(ctx context.Context, ex dbport.Querier, tenantID uuid.UUID, runID string, revision uint64) (payroll.PayrollRun, error) {
	var (
		out                           payroll.PayrollRun
		storedRevision                int64
		state                         string
		period                        []byte
		populationBinding             *string
		calculationInput, calculation *string
		release, reversal             *string
		supersedes                    *int64
	)
	err := ex.QueryRow(ctx, `
		SELECT run_id, revision, state, pay_group_ref, period::text,
			population_binding_ref, calculation_input_digest, calculation_digest,
			release_digest, reversal_digest, supersedes_revision, canonical_digest
		FROM payroll_run
		WHERE tenant_id = $1 AND run_id = $2 AND revision = $3`, tenantID, runID, int64(revision)).Scan(
		&out.RunID, &storedRevision, &state, &out.PayGroupRef, &period,
		&populationBinding, &calculationInput, &calculation, &release, &reversal, &supersedes, &out.CanonicalDigest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return payroll.PayrollRun{}, refusalNotFound("run_id", "payroll run revision was not found")
		}
		return payroll.PayrollRun{}, fmt.Errorf("payrollstore: load payroll run %s/%d: %w", runID, revision, err)
	}
	out.Revision = uint64(storedRevision)
	out.State = payroll.PayrollRunState(state)
	if err := json.Unmarshal(period, &out.Period); err != nil {
		return payroll.PayrollRun{}, fmt.Errorf("payrollstore: decode run period: %w", err)
	}
	if populationBinding == nil {
		return payroll.PayrollRun{}, fmt.Errorf("payrollstore: run %s/%d has no population binding", runID, revision)
	}
	if err := json.Unmarshal([]byte(*populationBinding), &out.Population); err != nil {
		return payroll.PayrollRun{}, fmt.Errorf("payrollstore: decode population binding: %w", err)
	}
	out.CalculationInputDigest = derefString(calculationInput)
	out.CalculationDigest = derefString(calculation)
	out.ReleaseDigest = derefString(release)
	out.ReversalDigest = derefString(reversal)
	out.CanonicalDigest = domainDigest(out.CanonicalDigest)
	if supersedes != nil {
		out.SupersedesRevision = uint64(*supersedes)
	}
	if err := out.Validate(); err != nil {
		return payroll.PayrollRun{}, fmt.Errorf("payrollstore: stored payroll run is invalid: %w", err)
	}
	return out, nil
}

// SavePopulation implements [payroll.Store].
func (s *Store) SavePopulation(ctx context.Context, tenantID payroll.TenantID, population payroll.FrozenPopulation) error {
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx, scopedTenant uuid.UUID) error {
		return s.SavePopulationTx(ctx, tx, scopedTenant, population)
	})
}

// SavePopulationTx stores one immutable frozen-population revision.
func (s *Store) SavePopulationTx(ctx context.Context, ex dbport.Tx, tenantID uuid.UUID, population payroll.FrozenPopulation) error {
	population, binding, members, err := encodePopulation(population)
	if err != nil {
		return err
	}
	if err := checkNextPopulation(ctx, ex, tenantID, population); err != nil {
		return err
	}
	_, err = ex.Exec(ctx, `
		INSERT INTO payroll_frozen_population (
			tenant_id, row_id, run_id, run_revision, pay_group_ref, binding, as_of,
			members, revision, state, supersedes_digest, amendment_digest, digest)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8::jsonb, $9, $10, $11, $12, $13)`,
		tenantID, uuid.New(), population.RunID, int64(population.RunRevision), nullableString(population.PayGroupRef),
		binding, population.AsOf.Time(), members, int64(population.Revision), string(population.State),
		nullableString(storageDigest(population.SupersedesDigest)), nullableString(storageDigest(population.AmendmentDigest)), storageDigest(population.Digest))
	if err != nil {
		return mapWriteError("save frozen population", population.RunID, err)
	}
	return nil
}

// LoadPopulation implements [payroll.Store].
func (s *Store) LoadPopulation(ctx context.Context, tenantID payroll.TenantID, runID string, revision uint64) (payroll.FrozenPopulation, error) {
	var out payroll.FrozenPopulation
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx, scopedTenant uuid.UUID) error {
		var err error
		out, err = s.LoadPopulationTx(ctx, tx, scopedTenant, runID, revision)
		return err
	})
	return out, err
}

// LoadPopulationTx loads one frozen-population revision.
func (s *Store) LoadPopulationTx(ctx context.Context, ex dbport.Querier, tenantID uuid.UUID, runID string, revision uint64) (payroll.FrozenPopulation, error) {
	var (
		out               payroll.FrozenPopulation
		state             string
		binding, members  []byte
		supersedes, amend *string
		asOf              time.Time
		storedRevision    int64
		storedRunRevision int64
	)
	err := ex.QueryRow(ctx, `
		SELECT run_id, run_revision, pay_group_ref, binding::text, as_of, members::text,
			revision, state, supersedes_digest, amendment_digest, digest
		FROM payroll_frozen_population
		WHERE tenant_id = $1 AND run_id = $2 AND revision = $3`, tenantID, runID, int64(revision)).Scan(
		&out.RunID, &storedRunRevision, &out.PayGroupRef, &binding, &asOf, &members,
		&storedRevision, &state, &supersedes, &amend, &out.Digest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return payroll.FrozenPopulation{}, refusalNotFound("run_id", "frozen population revision was not found")
		}
		return payroll.FrozenPopulation{}, fmt.Errorf("payrollstore: load frozen population %s/%d: %w", runID, revision, err)
	}
	out.RunRevision = uint64(storedRunRevision)
	out.Revision = uint64(storedRevision)
	out.State = payroll.PopulationState(state)
	out.SupersedesDigest = domainDigest(derefString(supersedes))
	out.AmendmentDigest = domainDigest(derefString(amend))
	out.Digest = domainDigest(out.Digest)
	var envelope populationBindingEnvelope
	if err := json.Unmarshal(binding, &envelope); err != nil {
		return payroll.FrozenPopulation{}, fmt.Errorf("payrollstore: decode population envelope: %w", err)
	}
	out.Binding = envelope.Population
	out.LateEntryPolicy = payroll.LateEntryPolicy(envelope.LateEntryPolicy)
	if err := json.Unmarshal(members, &out.Members); err != nil {
		return payroll.FrozenPopulation{}, fmt.Errorf("payrollstore: decode population members: %w", err)
	}
	instant := values.NewInstant(asOf.UTC())
	out.AsOf = instant
	if err := out.Validate(); err != nil {
		return payroll.FrozenPopulation{}, fmt.Errorf("payrollstore: stored frozen population is invalid: %w", err)
	}
	return out, nil
}

// AppendAmendment implements [payroll.Store].
func (s *Store) AppendAmendment(ctx context.Context, tenantID payroll.TenantID, runID string, eventSequence uint64, amendment payroll.PopulationAmendment) error {
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx, scopedTenant uuid.UUID) error {
		return s.AppendAmendmentTx(ctx, tx, scopedTenant, runID, eventSequence, amendment)
	})
}

// AppendAmendmentTx appends one immutable population amendment event.
func (s *Store) AppendAmendmentTx(ctx context.Context, ex dbport.Tx, tenantID uuid.UUID, runID string, eventSequence uint64, amendment payroll.PopulationAmendment) error {
	if runID == "" {
		return refuse("PAYROLL_INVALID_RUN_ID", "run_id", "run id is required", payroll.ErrStoreRefused)
	}
	if eventSequence == 0 {
		return refuse("PAYROLL_INVALID_EVENT_SEQUENCE", "event_sequence", "event sequence must be positive", payroll.ErrStoreRefused)
	}
	amendment, member, err := encodeAmendment(amendment)
	if err != nil {
		return err
	}
	_, err = ex.Exec(ctx, `
		INSERT INTO payroll_population_amendment (
			tenant_id, row_id, run_id, kind, member, reason, effective_as_of,
			digest, event_sequence)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9)`,
		tenantID, uuid.New(), runID, string(amendment.Kind), member, nullableString(amendment.Reason),
		amendment.EffectiveAsOf.Time(), storageDigest(amendment.Digest), int64(eventSequence))
	if err != nil {
		return mapWriteError("append population amendment", runID, err)
	}
	return nil
}

// ListAmendments implements [payroll.Store].
func (s *Store) ListAmendments(ctx context.Context, tenantID payroll.TenantID, runID string) ([]payroll.PopulationAmendment, error) {
	var out []payroll.PopulationAmendment
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx, scopedTenant uuid.UUID) error {
		var err error
		out, err = s.ListAmendmentsTx(ctx, tx, scopedTenant, runID)
		return err
	})
	return out, err
}

// ListAmendmentsTx returns amendment facts in event-sequence order.
func (s *Store) ListAmendmentsTx(ctx context.Context, ex dbport.Querier, tenantID uuid.UUID, runID string) ([]payroll.PopulationAmendment, error) {
	rows, err := ex.Query(ctx, `
		SELECT kind, member::text, reason, effective_as_of, digest
		FROM payroll_population_amendment
		WHERE tenant_id = $1 AND run_id = $2
		ORDER BY event_sequence`, tenantID, runID)
	if err != nil {
		return nil, fmt.Errorf("payrollstore: list population amendments %s: %w", runID, err)
	}
	defer rows.Close()
	var out []payroll.PopulationAmendment
	for rows.Next() {
		var (
			kind, memberJSON string
			reason, digest   *string
			effectiveAsOf    *time.Time
			member           payroll.PopulationMember
		)
		if err := rows.Scan(&kind, &memberJSON, &reason, &effectiveAsOf, &digest); err != nil {
			return nil, fmt.Errorf("payrollstore: scan population amendment: %w", err)
		}
		if err := json.Unmarshal([]byte(memberJSON), &member); err != nil {
			return nil, fmt.Errorf("payrollstore: decode population amendment member: %w", err)
		}
		if effectiveAsOf == nil {
			return nil, fmt.Errorf("payrollstore: population amendment has no effective_as_of")
		}
		amendment := payroll.PopulationAmendment{
			Kind: payroll.PopulationAmendmentKind(kind), Member: member, Reason: derefString(reason),
			EffectiveAsOf: values.NewInstant(effectiveAsOf.UTC()), Digest: domainDigest(derefString(digest)),
		}
		if err := amendment.Validate(member.PayGroupRef); err != nil {
			return nil, fmt.Errorf("payrollstore: stored population amendment is invalid: %w", err)
		}
		out = append(out, amendment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("payrollstore: list population amendments %s: %w", runID, err)
	}
	return out, nil
}

type populationBindingEnvelope struct {
	Population      payroll.PopulationBindingRef `json:"population"`
	LateEntryPolicy string                       `json:"late_entry_policy,omitempty"`
}

func encodeRun(run payroll.PayrollRun) (payroll.PayrollRun, string, string, error) {
	if err := run.Validate(); err != nil {
		return payroll.PayrollRun{}, "", "", err
	}
	if run.CanonicalDigest == "" {
		digest, err := run.Digest()
		if err != nil {
			return payroll.PayrollRun{}, "", "", err
		}
		run.CanonicalDigest = digest
	}
	period, err := json.Marshal(run.Period)
	if err != nil {
		return payroll.PayrollRun{}, "", "", fmt.Errorf("payrollstore: encode run period: %w", err)
	}
	binding, err := json.Marshal(run.Population)
	if err != nil {
		return payroll.PayrollRun{}, "", "", fmt.Errorf("payrollstore: encode population binding: %w", err)
	}
	return run, string(period), string(binding), nil
}

func encodePopulation(population payroll.FrozenPopulation) (payroll.FrozenPopulation, string, string, error) {
	if err := population.Validate(); err != nil {
		return payroll.FrozenPopulation{}, "", "", err
	}
	binding, err := json.Marshal(populationBindingEnvelope{Population: population.Binding, LateEntryPolicy: string(population.LateEntryPolicy)})
	if err != nil {
		return payroll.FrozenPopulation{}, "", "", fmt.Errorf("payrollstore: encode population binding: %w", err)
	}
	members, err := json.Marshal(population.Members)
	if err != nil {
		return payroll.FrozenPopulation{}, "", "", fmt.Errorf("payrollstore: encode population members: %w", err)
	}
	return population, string(binding), string(members), nil
}

func encodeAmendment(amendment payroll.PopulationAmendment) (payroll.PopulationAmendment, string, error) {
	canonical, err := payroll.NewPopulationAmendment(amendment.Kind, amendment.Member, amendment.Reason, amendment.EffectiveAsOf)
	if err != nil {
		return payroll.PopulationAmendment{}, "", err
	}
	if amendment.Digest != "" && amendment.Digest != canonical.Digest {
		return payroll.PopulationAmendment{}, "", refuse("PAYROLL_DIGEST_MISMATCH", "digest", "amendment digest does not match its content", payroll.ErrStoreRefused)
	}
	member, err := json.Marshal(canonical.Member)
	if err != nil {
		return payroll.PopulationAmendment{}, "", fmt.Errorf("payrollstore: encode population amendment member: %w", err)
	}
	return canonical, string(member), nil
}

func checkNextRun(ctx context.Context, ex dbport.Querier, tenantID uuid.UUID, run payroll.PayrollRun) error {
	var latest int64
	err := ex.QueryRow(ctx, `
		SELECT revision FROM payroll_run
		WHERE tenant_id = $1 AND run_id = $2
		ORDER BY revision DESC LIMIT 1`, tenantID, run.RunID).Scan(&latest)
	if errors.Is(err, dbport.ErrNoRows) {
		if run.Revision != 1 || run.SupersedesRevision != 0 {
			return refuse(payroll.ErrStaleRevision.Error(), "revision", "the first revision must be revision 1", payroll.ErrStaleRevision)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("payrollstore: inspect payroll run chain: %w", err)
	}
	if uint64(latest) >= run.Revision {
		if uint64(latest) == run.Revision {
			return refuse(payroll.ErrDuplicateRevision.Error(), "revision", "revision identity is already stored", payroll.ErrDuplicateRevision)
		}
		return refuse(payroll.ErrStaleRevision.Error(), "revision", "revision is behind the current run chain", payroll.ErrStaleRevision)
	}
	if run.Revision != uint64(latest)+1 || run.SupersedesRevision != uint64(latest) {
		return refuse(payroll.ErrStaleRevision.Error(), "supersedes_revision", "revision does not extend the current run chain", payroll.ErrStaleRevision)
	}
	return nil
}

func checkNextPopulation(ctx context.Context, ex dbport.Querier, tenantID uuid.UUID, population payroll.FrozenPopulation) error {
	var latest struct {
		revision int64
		digest   string
	}
	err := ex.QueryRow(ctx, `
		SELECT revision, digest FROM payroll_frozen_population
		WHERE tenant_id = $1 AND run_id = $2
		ORDER BY revision DESC LIMIT 1`, tenantID, population.RunID).Scan(&latest.revision, &latest.digest)
	if errors.Is(err, dbport.ErrNoRows) {
		if population.Revision != 1 || population.SupersedesDigest != "" {
			return refuse(payroll.ErrStaleRevision.Error(), "supersedes_digest", "the first population revision has no predecessor", payroll.ErrStaleRevision)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("payrollstore: inspect frozen population chain: %w", err)
	}
	if uint64(latest.revision) >= population.Revision {
		if uint64(latest.revision) == population.Revision {
			return refuse(payroll.ErrDuplicateRevision.Error(), "revision", "revision identity is already stored", payroll.ErrDuplicateRevision)
		}
		return refuse(payroll.ErrStaleRevision.Error(), "revision", "revision is behind the current population chain", payroll.ErrStaleRevision)
	}
	if population.Revision != uint64(latest.revision)+1 || storageDigest(population.SupersedesDigest) != latest.digest {
		return refuse(payroll.ErrStaleRevision.Error(), "supersedes_digest", "population revision does not extend the current chain", payroll.ErrStaleRevision)
	}
	return nil
}

func mapWriteError(operation, id string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		if pgErr.ConstraintName == "payroll_population_amendment_sequence_unique" {
			return refuse(payroll.ErrDuplicateAmendment.Error(), "event_sequence", "event sequence is already stored", payroll.ErrDuplicateAmendment)
		}
		return refuse(payroll.ErrDuplicateRevision.Error(), "revision", "revision identity is already stored", payroll.ErrDuplicateRevision)
	}
	return fmt.Errorf("payrollstore: %s %s: %w", operation, id, err)
}

func refuse(code, field, reason string, cause error) error {
	return &payroll.RefusalError{Code: code, Field: field, Reason: reason, Cause: cause}
}

func refusalNotFound(field, reason string) error {
	return refuse(payroll.ErrNotFound.Error(), field, reason, payroll.ErrNotFound)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableRevision(value uint64) any {
	if value == 0 {
		return nil
	}
	return int64(value)
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// content_digest is the database's bare SHA-256 hex domain, while the kernel
// canonicalbytes engine carries the same digest with its "sha256:" algorithm
// prefix. The adapter removes/adds only that transport prefix; the content is
// unchanged.
func storageDigest(value string) string {
	return strings.TrimPrefix(value, "sha256:")
}

func domainDigest(value string) string {
	if value == "" || strings.Contains(value, ":") {
		return value
	}
	if len(value) == 64 {
		return "sha256:" + value
	}
	return value
}
