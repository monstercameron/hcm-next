// Package meritstore persists the tenant-scoped merit revisions declared by
// migrations/00100_merit.sql. It owns no merit decisions: the domain validates
// the cycle, while this adapter supplies tenant isolation and durable CAS.
package meritstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/merit"
	"github.com/monstercameron/hcm-next/internal/domains/performance"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// DB is the transaction capability needed by Store.
type DB interface{ dbport.Beginner }

// Store implements merit.Store over PostgreSQL.
type Store struct{ db DB }

var _ merit.Store = (*Store)(nil)

// New returns a PostgreSQL merit store over db.
func New(db DB) *Store { return &Store{db: db} }

func invalid(detail string) error {
	return &merit.StoreError{Code: merit.StoreInvalidCode, Detail: detail}
}

func notFound(detail string) error {
	return &merit.StoreError{Code: merit.StoreNotFoundCode, Detail: detail}
}

func duplicate(detail string) error {
	return &merit.StoreError{Code: merit.StoreDuplicateCode, Detail: detail}
}

func stale(expected, actual uint64, detail string) error {
	return &merit.StoreError{Code: merit.StoreStaleCASCode, Expected: expected, Actual: actual, Detail: detail}
}

func parseTenant(tenantID string) (uuid.UUID, error) {
	tid, err := uuid.Parse(tenantID)
	if err != nil || tid == uuid.Nil {
		return uuid.Nil, invalid("tenant id must be a non-nil UUID")
	}
	return tid, nil
}

func (s *Store) withTenant(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if s == nil || s.db == nil {
		return invalid("database capability is required")
	}
	if ctx == nil {
		return invalid("context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("meritstore: begin transaction: %w", err)
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
		_ = tx.Rollback(ctx)
		return fmt.Errorf("meritstore: commit transaction: %w", err)
	}
	return nil
}

// Save appends one immutable cycle revision and its recommendation rows.
func (s *Store) Save(ctx context.Context, tenantID string, cycle merit.MeritCycle) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	cycle, err = normalizeCycle(cycle)
	if err != nil {
		return err
	}
	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		return s.saveTx(ctx, tx, tid, cycle)
	})
}

func (s *Store) saveTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, cycle merit.MeritCycle) error {
	lockKey := tenantID.String() + ":merit-cycle:" + cycle.CycleID
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return fmt.Errorf("meritstore: lock cycle %s: %w", cycle.CycleID, err)
	}
	if err := checkNextRevision(ctx, tx, tenantID, cycle); err != nil {
		return err
	}
	populationRef, err := s.ensurePopulation(ctx, tx, tenantID, cycle.Population)
	if err != nil {
		return err
	}
	guidelines, err := marshalCycleGuidelines(cycle)
	if err != nil {
		return err
	}
	budget := cycle.Budget.String()
	_, err = tx.Exec(ctx, `
		INSERT INTO merit_cycle_revision (
			tenant_id, row_id, cycle_id, revision, parent_revision, parent_digest,
			population_ref, guidelines, budget, state, effective_at, known_at,
			canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12,$13)`,
		tenantID, uuid.New(), cycle.CycleID, int64(cycle.Revision), nullableRevision(cycle.ParentRevision),
		nullableDigest(cycle.ParentDigest), populationRef, string(guidelines), budget,
		cycle.State, cycle.EffectiveAt.Time(), cycle.KnownAt.Time(), storageDigest(cycle.CanonicalDigest))
	if err != nil {
		return mapWriteError("save cycle", cycle.CycleID, err)
	}
	for _, recommendation := range cycle.Recommendations {
		payload, err := marshalRecommendation(recommendation)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO merit_recommendation (
				tenant_id, row_id, cycle_id, cycle_revision, participant_id,
				base_pay, state, adjustments, canonical_digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9)
			ON CONFLICT DO NOTHING`,
			tenantID, uuid.New(), recommendation.CycleID, int64(cycle.Revision),
			recommendation.ParticipantID, recommendation.BasePay.Amount().String(), recommendation.State,
			string(payload), storageDigest(recommendation.CanonicalDigest))
		if err != nil {
			return mapWriteError("save recommendation", recommendation.ParticipantID, err)
		}
	}
	return nil
}

// Load returns one immutable cycle revision.
func (s *Store) Load(ctx context.Context, tenantID, cycleID string, revision uint64) (merit.MeritCycle, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return merit.MeritCycle{}, err
	}
	if strings.TrimSpace(cycleID) == "" || revision == 0 {
		return merit.MeritCycle{}, invalid("cycle id and positive revision are required")
	}
	var out merit.MeritCycle
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var err error
		out, err = s.loadTx(ctx, tx, tid, cycleID, revision)
		return err
	})
	return out, err
}

// Current returns the highest stored revision for a cycle.
func (s *Store) Current(ctx context.Context, tenantID, cycleID string) (merit.MeritCycle, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return merit.MeritCycle{}, err
	}
	if strings.TrimSpace(cycleID) == "" {
		return merit.MeritCycle{}, invalid("cycle id is required")
	}
	var out merit.MeritCycle
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var revision int64
		if err := tx.QueryRow(ctx, `
			SELECT revision FROM merit_cycle_revision
			WHERE tenant_id=$1 AND cycle_id=$2 ORDER BY revision DESC LIMIT 1`, tenantIDUUID(tid), cycleID).Scan(&revision); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return notFound("merit cycle is absent")
			}
			return fmt.Errorf("meritstore: find current cycle %s: %w", cycleID, err)
		}
		out, err = s.loadTx(ctx, tx, tid, cycleID, uint64(revision))
		return err
	})
	return out, err
}

// tenantIDUUID keeps SQL call sites visually aligned with the tenant-scoping
// argument used by the other store methods.
func tenantIDUUID(id uuid.UUID) uuid.UUID { return id }

func checkNextRevision(ctx context.Context, tx dbport.Querier, tenantID uuid.UUID, cycle merit.MeritCycle) error {
	var latestRevision int64
	var latestDigest string
	err := tx.QueryRow(ctx, `
		SELECT revision, canonical_digest FROM merit_cycle_revision
		WHERE tenant_id=$1 AND cycle_id=$2 ORDER BY revision DESC LIMIT 1`, tenantID, cycle.CycleID).Scan(&latestRevision, &latestDigest)
	if errors.Is(err, dbport.ErrNoRows) {
		if cycle.Revision != 1 || cycle.ParentRevision != 0 || cycle.ParentDigest != "" {
			return stale(0, cycle.ParentRevision, "initial cycle must be revision 1 without a parent")
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("meritstore: inspect cycle revision chain: %w", err)
	}
	if uint64(latestRevision) >= cycle.Revision {
		if uint64(latestRevision) == cycle.Revision {
			return duplicate(fmt.Sprintf("cycle %s revision %d", cycle.CycleID, cycle.Revision))
		}
		return stale(uint64(latestRevision), cycle.ParentRevision, "cycle revision is behind the current head")
	}
	if cycle.Revision != uint64(latestRevision)+1 || cycle.ParentRevision != uint64(latestRevision) || storageDigest(cycle.ParentDigest) != latestDigest {
		return stale(uint64(latestRevision), cycle.ParentRevision, "cycle successor does not extend the current head")
	}
	return nil
}

func (s *Store) ensurePopulation(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, population merit.PopulationSnapshot) (uuid.UUID, error) {
	members, err := marshalMembers(population.Members)
	if err != nil {
		return uuid.Nil, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO merit_population_snapshot (
			tenant_id, row_id, snapshot_id, revision, members, watermark, frozen,
			frozen_at, canonical_digest)
		VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9)
		ON CONFLICT (tenant_id, snapshot_id, revision) DO NOTHING`,
		tenantID, uuid.New(), population.SnapshotID, int64(population.Revision), string(members),
		population.Watermark.String(), population.Frozen, population.FrozenAt.Time(), storageDigest(population.CanonicalDigest))
	if err != nil {
		return uuid.Nil, mapWriteError("save population snapshot", population.SnapshotID, err)
	}
	var rowID uuid.UUID
	var digest string
	if err := tx.QueryRow(ctx, `
		SELECT row_id, canonical_digest FROM merit_population_snapshot
		WHERE tenant_id=$1 AND snapshot_id=$2 AND revision=$3`, tenantID, population.SnapshotID, int64(population.Revision)).Scan(&rowID, &digest); err != nil {
		return uuid.Nil, fmt.Errorf("meritstore: find population snapshot %s/%d: %w", population.SnapshotID, population.Revision, err)
	}
	if digest != storageDigest(population.CanonicalDigest) {
		return uuid.Nil, invalid("population snapshot digest conflicts with the stored revision")
	}
	return rowID, nil
}

func (s *Store) loadTx(ctx context.Context, tx dbport.Querier, tenantID uuid.UUID, cycleID string, revision uint64) (merit.MeritCycle, error) {
	var (
		rowID, populationRef uuid.UUID
		storedRevision       int64
		parentRevision       *int64
		parentDigest         *string
		guidelinesJSON       []byte
		budget               string
		state                string
		canonicalDigest      string
		effectiveAt, knownAt *time.Time
	)
	err := tx.QueryRow(ctx, `
		SELECT row_id, revision, parent_revision, parent_digest, population_ref,
			guidelines::text, budget, state, effective_at, known_at, canonical_digest
		FROM merit_cycle_revision
		WHERE tenant_id=$1 AND cycle_id=$2 AND revision=$3`, tenantID, cycleID, int64(revision)).Scan(
		&rowID, &storedRevision, &parentRevision, &parentDigest, &populationRef,
		&guidelinesJSON, &budget, &state, &effectiveAt, &knownAt, &canonicalDigest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return merit.MeritCycle{}, notFound("merit cycle revision is absent")
		}
		return merit.MeritCycle{}, fmt.Errorf("meritstore: load cycle %s/%d: %w", cycleID, revision, err)
	}
	if effectiveAt == nil || knownAt == nil {
		return merit.MeritCycle{}, invalid("stored cycle timestamps are incomplete")
	}
	population, err := s.loadPopulationTx(ctx, tx, tenantID, populationRef)
	if err != nil {
		return merit.MeritCycle{}, err
	}
	guidelines, currency, storedBudget, err := unmarshalCycleGuidelines(guidelinesJSON)
	if err != nil {
		return merit.MeritCycle{}, err
	}
	if storedBudget.String() != budget && !sameNumeric(storedBudget.String(), budget) {
		return merit.MeritCycle{}, invalid("stored cycle budget does not match its JSON contract")
	}
	recommendations, err := s.loadRecommendationsTx(ctx, tx, tenantID, cycleID, uint64(storedRevision))
	if err != nil {
		return merit.MeritCycle{}, err
	}
	cycle := merit.MeritCycle{
		CycleID: cycleID, Revision: uint64(storedRevision), ParentRevision: int64Value(parentRevision),
		ParentDigest: domainDigest(stringValue(parentDigest)), Population: population, Guidelines: guidelines,
		Budget: storedBudget, Currency: currency, State: merit.MeritCycleState(state), Recommendations: recommendations,
		EffectiveAt: values.NewInstant(effectiveAt.UTC()), KnownAt: values.NewInstant(knownAt.UTC()),
		CanonicalDigest: domainDigest(canonicalDigest),
	}
	if err := cycle.Validate(); err != nil {
		return merit.MeritCycle{}, invalid("stored cycle is invalid: " + err.Error())
	}
	if cycle.CanonicalDigest != domainDigest(canonicalDigest) {
		return merit.MeritCycle{}, invalid("stored cycle digest does not match its content")
	}
	return cycle, nil
}

func (s *Store) loadPopulationTx(ctx context.Context, tx dbport.Querier, tenantID, rowID uuid.UUID) (merit.PopulationSnapshot, error) {
	var (
		storedID, watermark, digest string
		revision                    int64
		membersJSON                 []byte
		frozen                      bool
		frozenAt                    *time.Time
	)
	err := tx.QueryRow(ctx, `
		SELECT snapshot_id, revision, members::text, watermark, frozen, frozen_at, canonical_digest
		FROM merit_population_snapshot WHERE tenant_id=$1 AND row_id=$2`, tenantID, rowID).Scan(
		&storedID, &revision, &membersJSON, &watermark, &frozen, &frozenAt, &digest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return merit.PopulationSnapshot{}, notFound("merit population snapshot is absent")
		}
		return merit.PopulationSnapshot{}, fmt.Errorf("meritstore: load population snapshot: %w", err)
	}
	if frozenAt == nil || watermark == "" {
		return merit.PopulationSnapshot{}, invalid("stored population snapshot is incomplete")
	}
	members, err := unmarshalMembers(membersJSON)
	if err != nil {
		return merit.PopulationSnapshot{}, err
	}
	var token values.RevisionToken
	if err := token.UnmarshalText([]byte(watermark)); err != nil {
		return merit.PopulationSnapshot{}, invalid("stored population watermark is invalid")
	}
	population, err := merit.NewPopulationSnapshot(merit.PopulationSnapshot{
		SnapshotID: storedID, Revision: uint64(revision), Members: members, Watermark: token,
		Frozen: frozen, FrozenAt: values.NewInstant(frozenAt.UTC()), CanonicalDigest: domainDigest(digest),
	})
	if err != nil {
		return merit.PopulationSnapshot{}, invalid("stored population snapshot is invalid: " + err.Error())
	}
	return population, nil
}

func (s *Store) loadRecommendationsTx(ctx context.Context, tx dbport.Querier, tenantID uuid.UUID, cycleID string, revision uint64) ([]merit.MeritRecommendation, error) {
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT ON (participant_id)
			participant_id, cycle_revision, base_pay, state, adjustments::text, canonical_digest
		FROM merit_recommendation
		WHERE tenant_id=$1 AND cycle_id=$2 AND cycle_revision <= $3
		ORDER BY participant_id, cycle_revision DESC, row_id DESC`, tenantID, cycleID, int64(revision))
	if err != nil {
		return nil, fmt.Errorf("meritstore: list recommendations %s/%d: %w", cycleID, revision, err)
	}
	defer rows.Close()
	var out []merit.MeritRecommendation
	for rows.Next() {
		var participantID, basePay, state, digest string
		var recommendationRevision int64
		var payloadJSON []byte
		if err := rows.Scan(&participantID, &recommendationRevision, &basePay, &state, &payloadJSON, &digest); err != nil {
			return nil, fmt.Errorf("meritstore: scan recommendation: %w", err)
		}
		recommendation, err := unmarshalRecommendation(payloadJSON, participantID, basePay, cycleID, uint64(recommendationRevision), state, digest)
		if err != nil {
			return nil, err
		}
		out = append(out, recommendation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("meritstore: list recommendations %s/%d: %w", cycleID, revision, err)
	}
	return out, nil
}

type memberPayload struct {
	ParticipantID     string `json:"participant_id"`
	ManagerID         string `json:"manager_id"`
	BasePay           string `json:"base_pay"`
	PerformanceRating string `json:"performance_rating"`
	BandPosition      string `json:"band_position"`
	SalaryRevisionRef string `json:"salary_revision_ref"`
	PerformanceRef    string `json:"performance_ref"`
	EffectiveAt       string `json:"effective_at"`
	KnownAt           string `json:"known_at"`
}

type guidelinePayload struct {
	MatrixID        string                 `json:"matrix_id"`
	Version         string                 `json:"version"`
	Rules           []guidelineRulePayload `json:"rules"`
	CanonicalDigest string                 `json:"canonical_digest"`
}

type guidelineRulePayload struct {
	RatingMin       string `json:"rating_min"`
	RatingMax       string `json:"rating_max"`
	BandPositionMin string `json:"band_position_min"`
	BandPositionMax string `json:"band_position_max"`
	MinimumRate     string `json:"minimum_rate"`
	MaximumRate     string `json:"maximum_rate"`
}

type cycleGuidelinesPayload struct {
	Matrix   guidelinePayload `json:"matrix"`
	Currency string           `json:"currency"`
	Budget   string           `json:"budget"`
}

type adjustmentPayload struct {
	ParticipantID   string `json:"participant_id"`
	From            string `json:"from"`
	To              string `json:"to"`
	Reason          string `json:"reason"`
	AdjusterID      string `json:"adjuster_id"`
	CanonicalDigest string `json:"canonical_digest"`
}

type recommendationPayload struct {
	CycleRevision     uint64              `json:"cycle_revision"`
	BasePay           string              `json:"base_pay"`
	PerformanceRating string              `json:"performance_rating"`
	BandPosition      string              `json:"band_position"`
	SalaryRevisionRef string              `json:"salary_revision_ref"`
	PerformanceRef    string              `json:"performance_ref"`
	Rate              string              `json:"rate"`
	Amount            string              `json:"amount"`
	GuidelineDigest   string              `json:"guideline_digest"`
	ProposedBy        string              `json:"proposed_by"`
	ApprovedBy        string              `json:"approved_by"`
	Adjustments       []adjustmentPayload `json:"adjustments"`
}

func marshalMembers(members []merit.PopulationMember) ([]byte, error) {
	out := make([]memberPayload, len(members))
	for i, member := range members {
		basePay, err := textMoney(member.BasePay)
		if err != nil {
			return nil, invalid("population base pay: " + err.Error())
		}
		performanceRating, err := textDecimal(member.PerformanceRating)
		if err != nil {
			return nil, invalid("population performance rating: " + err.Error())
		}
		bandPosition, err := textDecimal(member.BandPosition)
		if err != nil {
			return nil, invalid("population band position: " + err.Error())
		}
		out[i] = memberPayload{member.ParticipantID, member.ManagerID, basePay, performanceRating, bandPosition, member.SalaryRevisionRef, member.PerformanceRef, member.EffectiveAt.String(), member.KnownAt.String()}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("meritstore: marshal population members: %w", err)
	}
	return b, nil
}

func unmarshalMembers(raw []byte) ([]merit.PopulationMember, error) {
	var payload []memberPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, invalid("population members are not valid JSON")
	}
	out := make([]merit.PopulationMember, len(payload))
	for i, member := range payload {
		basePay, err := parseMoney(member.BasePay)
		if err != nil {
			return nil, invalid("stored population base pay is invalid")
		}
		performanceRating, err := parseDecimal(member.PerformanceRating)
		if err != nil {
			return nil, invalid("stored population performance rating is invalid")
		}
		bandPosition, err := parseDecimal(member.BandPosition)
		if err != nil {
			return nil, invalid("stored population band position is invalid")
		}
		effectiveAt, err := parseInstant(member.EffectiveAt)
		if err != nil {
			return nil, invalid("stored population effective_at is invalid")
		}
		knownAt, err := parseInstant(member.KnownAt)
		if err != nil {
			return nil, invalid("stored population known_at is invalid")
		}
		out[i] = merit.PopulationMember{ParticipantID: member.ParticipantID, ManagerID: member.ManagerID, BasePay: basePay, PerformanceRating: performanceRating, BandPosition: bandPosition, SalaryRevisionRef: member.SalaryRevisionRef, PerformanceRef: member.PerformanceRef, EffectiveAt: effectiveAt, KnownAt: knownAt}
	}
	return out, nil
}

func marshalCycleGuidelines(cycle merit.MeritCycle) ([]byte, error) {
	rules := make([]guidelineRulePayload, len(cycle.Guidelines.Rules))
	for i, rule := range cycle.Guidelines.Rules {
		values := []*values.Decimal{&rule.RatingMin, &rule.RatingMax, &rule.BandPositionMin, &rule.BandPositionMax, &rule.MinimumRate, &rule.MaximumRate}
		texts := make([]string, len(values))
		for j, value := range values {
			var err error
			texts[j], err = textDecimal(*value)
			if err != nil {
				return nil, invalid("guideline decimal: " + err.Error())
			}
		}
		rules[i] = guidelineRulePayload{texts[0], texts[1], texts[2], texts[3], texts[4], texts[5]}
	}
	budget, err := textDecimal(cycle.Budget)
	if err != nil {
		return nil, invalid("cycle budget: " + err.Error())
	}
	payload := cycleGuidelinesPayload{Matrix: guidelinePayload{cycle.Guidelines.MatrixID, cycle.Guidelines.Version, rules, cycle.Guidelines.CanonicalDigest}, Currency: cycle.Currency, Budget: budget}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("meritstore: marshal cycle guidelines: %w", err)
	}
	return b, nil
}

func unmarshalCycleGuidelines(raw []byte) (merit.GuidelineMatrix, string, values.Decimal, error) {
	var payload cycleGuidelinesPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return merit.GuidelineMatrix{}, "", values.Decimal{}, invalid("cycle guidelines are not valid JSON")
	}
	rules := make([]merit.GuidelineRule, len(payload.Matrix.Rules))
	for i, rule := range payload.Matrix.Rules {
		texts := []string{rule.RatingMin, rule.RatingMax, rule.BandPositionMin, rule.BandPositionMax, rule.MinimumRate, rule.MaximumRate}
		parsed := make([]values.Decimal, len(texts))
		for j, text := range texts {
			var err error
			parsed[j], err = parseDecimal(text)
			if err != nil {
				return merit.GuidelineMatrix{}, "", values.Decimal{}, invalid("stored guideline decimal is invalid")
			}
		}
		rules[i] = merit.GuidelineRule{RatingMin: parsed[0], RatingMax: parsed[1], BandPositionMin: parsed[2], BandPositionMax: parsed[3], MinimumRate: parsed[4], MaximumRate: parsed[5]}
	}
	guidelines, err := merit.NewGuidelineMatrix(merit.GuidelineMatrix{MatrixID: payload.Matrix.MatrixID, Version: payload.Matrix.Version, Rules: rules, CanonicalDigest: domainDigest(payload.Matrix.CanonicalDigest)})
	if err != nil {
		return merit.GuidelineMatrix{}, "", values.Decimal{}, invalid("stored guidelines are invalid: " + err.Error())
	}
	budget, err := parseDecimal(payload.Budget)
	if err != nil {
		return merit.GuidelineMatrix{}, "", values.Decimal{}, invalid("stored budget is invalid")
	}
	return guidelines, payload.Currency, budget, nil
}

func marshalRecommendation(recommendation merit.MeritRecommendation) ([]byte, error) {
	basePay, err := textMoney(recommendation.BasePay)
	if err != nil {
		return nil, invalid("recommendation base pay: " + err.Error())
	}
	performanceRating, err := textDecimal(recommendation.PerformanceRating)
	if err != nil {
		return nil, invalid("recommendation performance rating: " + err.Error())
	}
	bandPosition, err := textDecimal(recommendation.BandPosition)
	if err != nil {
		return nil, invalid("recommendation band position: " + err.Error())
	}
	rate, err := textDecimal(recommendation.Rate)
	if err != nil {
		return nil, invalid("recommendation rate: " + err.Error())
	}
	amount, err := textDecimal(recommendation.Amount)
	if err != nil {
		return nil, invalid("recommendation amount: " + err.Error())
	}
	adjustments := make([]adjustmentPayload, len(recommendation.Adjustments))
	for i, adjustment := range recommendation.Adjustments {
		from, err := textDecimal(adjustment.From)
		if err != nil {
			return nil, invalid("adjustment from: " + err.Error())
		}
		to, err := textDecimal(adjustment.To)
		if err != nil {
			return nil, invalid("adjustment to: " + err.Error())
		}
		adjustments[i] = adjustmentPayload{adjustment.ParticipantID, from, to, string(adjustment.Reason), adjustment.AdjusterID, adjustment.CanonicalDigest}
	}
	payload := recommendationPayload{
		CycleRevision: recommendation.CycleRevision, BasePay: basePay,
		PerformanceRating: performanceRating, BandPosition: bandPosition,
		SalaryRevisionRef: recommendation.SalaryRevisionRef, PerformanceRef: recommendation.PerformanceRef,
		Rate: rate, Amount: amount, GuidelineDigest: recommendation.GuidelineDigest,
		ProposedBy: recommendation.ProposedBy, ApprovedBy: recommendation.ApprovedBy,
		Adjustments: adjustments,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("meritstore: marshal recommendation: %w", err)
	}
	return b, nil
}

func unmarshalRecommendation(raw []byte, participantID, basePayNumeric, cycleID string, revision uint64, state, digest string) (merit.MeritRecommendation, error) {
	var payload recommendationPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return merit.MeritRecommendation{}, invalid("recommendation payload is not valid JSON")
	}
	basePay, err := parseMoney(payload.BasePay)
	if err != nil {
		return merit.MeritRecommendation{}, invalid("stored recommendation base pay is invalid")
	}
	if !sameNumeric(basePay.Amount().String(), basePayNumeric) {
		return merit.MeritRecommendation{}, invalid("stored recommendation base pay column disagrees with its payload")
	}
	performanceRating, err := parseDecimal(payload.PerformanceRating)
	if err != nil {
		return merit.MeritRecommendation{}, invalid("stored recommendation performance rating is invalid")
	}
	bandPosition, err := parseDecimal(payload.BandPosition)
	if err != nil {
		return merit.MeritRecommendation{}, invalid("stored recommendation band position is invalid")
	}
	rate, err := parseDecimal(payload.Rate)
	if err != nil {
		return merit.MeritRecommendation{}, invalid("stored recommendation rate is invalid")
	}
	amount, err := parseDecimal(payload.Amount)
	if err != nil {
		return merit.MeritRecommendation{}, invalid("stored recommendation amount is invalid")
	}
	adjustments := make([]merit.CalibrationAdjustment, len(payload.Adjustments))
	for i, adjustment := range payload.Adjustments {
		from, err := parseDecimal(adjustment.From)
		if err != nil {
			return merit.MeritRecommendation{}, invalid("stored adjustment from is invalid")
		}
		to, err := parseDecimal(adjustment.To)
		if err != nil {
			return merit.MeritRecommendation{}, invalid("stored adjustment to is invalid")
		}
		adjustments[i] = merit.CalibrationAdjustment{ParticipantID: adjustment.ParticipantID, From: from, To: to, Reason: performance.CalibrationReasonCode(adjustment.Reason), AdjusterID: adjustment.AdjusterID, CanonicalDigest: domainDigest(adjustment.CanonicalDigest)}
	}
	recommendationRevision := payload.CycleRevision
	if recommendationRevision == 0 {
		recommendationRevision = revision
	}
	recommendation, err := merit.NewMeritRecommendation(merit.MeritRecommendation{CycleID: cycleID, CycleRevision: recommendationRevision, ParticipantID: participantID, BasePay: basePay, PerformanceRating: performanceRating, BandPosition: bandPosition, SalaryRevisionRef: payload.SalaryRevisionRef, PerformanceRef: payload.PerformanceRef, Rate: rate, Amount: amount, GuidelineDigest: domainDigest(payload.GuidelineDigest), ProposedBy: payload.ProposedBy, ApprovedBy: payload.ApprovedBy, State: merit.RecommendationState(state), Adjustments: adjustments, CanonicalDigest: domainDigest(digest)})
	if err != nil {
		return merit.MeritRecommendation{}, invalid("stored recommendation is invalid: " + err.Error())
	}
	return recommendation, nil
}

func normalizeCycle(cycle merit.MeritCycle) (merit.MeritCycle, error) {
	population, err := merit.NewPopulationSnapshot(cycle.Population)
	if err != nil {
		return merit.MeritCycle{}, invalid(err.Error())
	}
	guidelines, err := merit.NewGuidelineMatrix(cycle.Guidelines)
	if err != nil {
		return merit.MeritCycle{}, invalid(err.Error())
	}
	recommendations := make([]merit.MeritRecommendation, len(cycle.Recommendations))
	for i, recommendation := range cycle.Recommendations {
		recommendations[i], err = merit.NewMeritRecommendation(recommendation)
		if err != nil {
			return merit.MeritCycle{}, invalid(err.Error())
		}
	}
	cycle.Population, cycle.Guidelines, cycle.Recommendations = population, guidelines, recommendations
	cycle, err = merit.NewMeritCycle(cycle)
	if err != nil {
		return merit.MeritCycle{}, invalid(err.Error())
	}
	return cycle, nil
}

func textDecimal(value values.Decimal) (string, error) {
	b, err := value.MarshalText()
	return string(b), err
}
func textMoney(value values.Money) (string, error) {
	b, err := value.MarshalText()
	return string(b), err
}

func parseDecimal(text string) (values.Decimal, error) {
	var value values.Decimal
	err := value.UnmarshalText([]byte(text))
	return value, err
}

func parseMoney(text string) (values.Money, error) {
	parts := strings.Fields(text)
	if len(parts) != 2 {
		return values.Money{}, fmt.Errorf("money needs amount and currency")
	}
	amount, err := parseDecimal(parts[0])
	if err != nil {
		return values.Money{}, err
	}
	return values.NewMoneyFromDecimal(amount, parts[1])
}

func parseInstant(text string) (values.Instant, error) {
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return values.Instant{}, err
	}
	instant := values.NewInstant(parsed)
	if instant.String() != text {
		return values.Instant{}, fmt.Errorf("instant is not canonical")
	}
	return instant, nil
}

func mapWriteError(operation, key string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return duplicate(fmt.Sprintf("%s %s already exists", operation, key))
	}
	return fmt.Errorf("meritstore: %s %s: %w", operation, key, err)
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
func int64Value(value *int64) uint64 {
	if value == nil {
		return 0
	}
	return uint64(*value)
}
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func storageDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }
func domainDigest(value string) string {
	if value == "" || strings.Contains(value, ":") {
		return value
	}
	return "sha256:" + value
}

func sameNumeric(left, right string) bool {
	left = strings.TrimRight(strings.TrimRight(left, "0"), ".")
	right = strings.TrimRight(strings.TrimRight(right, "0"), ".")
	if left == "" {
		left = "0"
	}
	if right == "" {
		right = "0"
	}
	return left == right
}
