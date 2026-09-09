// Package benefitsstore persists the tenant-scoped benefit plan-year revision
// catalogue from migrations/00072_benefits.sql.
package benefitsstore

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
	"github.com/monstercameron/human-capital-management-suite/internal/domains/benefits"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DB is the driver-free database capability this adapter needs.
type DB interface{ dbport.Beginner }

// Store implements benefits.Store over PostgreSQL. It owns transactions so
// tenant context is established before any row is read or written.
type Store struct{ db DB }

var _ benefits.Store = (*Store)(nil)

// New returns a PostgreSQL benefits store over db.
func New(db DB) *Store { return &Store{db: db} }

// ErrorCode aliases the domain's stable store classification for callers that
// import the adapter package.
type ErrorCode = benefits.StoreErrorCode

const (
	CodeInvalid           = benefits.StoreInvalidCode
	CodeNotFound          = benefits.StoreNotFoundCode
	CodeDuplicateRevision = benefits.StoreDuplicateCode
	CodeStaleCAS          = benefits.StoreStaleCASCode
	CodeDatabase          = benefits.StoreDatabaseCode
)

// CodeOf returns the stable adapter error code.
func CodeOf(err error) ErrorCode { return benefits.CodeOf(err) }

type intervalEnvelope struct {
	Kind            string `json:"kind"`
	Start           string `json:"start"`
	End             string `json:"end,omitempty"`
	CalendarRef     string `json:"calendar_ref,omitempty"`
	CalendarVersion string `json:"calendar_version,omitempty"`
}

// optionsEnvelope keeps the richer kernel value semantics that cannot be
// represented by the migration's deliberately compact uuid/timestamptz
// projections: reference kinds, revision stream and both effective intervals.
// The options themselves remain the values in Options.Values.
type optionsEnvelope struct {
	Values            []string          `json:"values"`
	RevisionStream    string            `json:"revision_stream"`
	RevisionEffective intervalEnvelope  `json:"revision_effective"`
	PlanYearEffective intervalEnvelope  `json:"plan_year_effective"`
	Refs              map[string]string `json:"refs"`
}

func invalid(detail string) error {
	return &benefits.StoreError{Code: benefits.StoreInvalidCode, Detail: detail}
}

func notFound(detail string) error {
	return &benefits.StoreError{Code: benefits.StoreNotFoundCode, Detail: detail}
}

func duplicate(detail string) error {
	return &benefits.StoreError{Code: benefits.StoreDuplicateCode, Detail: detail}
}

func stale(expected, actual, detail string) error {
	return &benefits.StoreError{Code: benefits.StoreStaleCASCode, Expected: expected, Actual: actual, Detail: detail}
}

func database(detail string) error {
	return &benefits.StoreError{Code: benefits.StoreDatabaseCode, Detail: detail}
}

func (s *Store) withTenant(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if s == nil || s.db == nil {
		return invalid("database capability is required")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return database(fmt.Sprintf("begin transaction: %v", err))
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
		return database(fmt.Sprintf("commit transaction: %v", err))
	}
	return nil
}

func parseTenant(tenantID string) (uuid.UUID, error) {
	tid, err := uuid.Parse(tenantID)
	if err != nil || tid == uuid.Nil {
		return uuid.Nil, invalid(fmt.Sprintf("tenant id %q is not a valid uuid", tenantID))
	}
	return tid, nil
}

func parseRevision(text string) (values.RevisionToken, uint64, error) {
	if text == "" {
		return values.RevisionToken{}, 0, invalid("revision is required")
	}
	var token values.RevisionToken
	if err := token.UnmarshalText([]byte(text)); err != nil {
		return values.RevisionToken{}, 0, invalid(fmt.Sprintf("revision %q: %v", text, err))
	}
	sequence, ok := token.Sequence()
	if !ok || sequence == 0 {
		return values.RevisionToken{}, 0, invalid("benefit plan revisions require a positive sequence token")
	}
	return token, sequence, nil
}

// Save appends one immutable revision. expectedRevision is empty for the
// first row and otherwise must equal the current ordered revision token.
func (s *Store) Save(ctx context.Context, tenantID string, revision benefits.PlanRevision, expectedRevision string) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	if revision.PlanID.Tenant.String() != tenantID {
		return invalid("plan tenant does not match store tenant")
	}
	if revision.CanonicalDigest == "" {
		revision, err = benefits.NewPlanRevision(revision)
		if err != nil {
			return invalid(err.Error())
		}
	} else if err := benefits.ValidatePlanRevision(revision); err != nil {
		return invalid(err.Error())
	}
	_, sequence, err := parseRevision(revision.Revision.String())
	if err != nil {
		return err
	}
	planID, err := parseUUID(revision.PlanID.Id, "plan id")
	if err != nil {
		return err
	}
	if _, err := parseUUID(revision.RevisionID.Id, "revision id"); err != nil {
		return err
	}
	envelope, err := encodeEnvelope(revision)
	if err != nil {
		return invalid(err.Error())
	}
	coverage, err := json.Marshal(revision.CoverageTiers)
	if err != nil {
		return invalid(fmt.Sprintf("coverage tiers: %v", err))
	}
	options, err := json.Marshal(envelope)
	if err != nil {
		return invalid(fmt.Sprintf("options: %v", err))
	}
	from, to, err := effectiveProjection(revision.Effective)
	if err != nil {
		return invalid(fmt.Sprintf("effective interval: %v", err))
	}
	rowID, err := parseUUID(revision.RevisionID.Id, "revision id")
	if err != nil {
		return err
	}
	refs, err := referenceColumns(revision)
	if err != nil {
		return err
	}

	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		lockKey := tenantID + ":" + planID.String()
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
			return database(fmt.Sprintf("lock plan %s: %v", planID, err))
		}
		var alreadyExists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM benefit_plan_revision WHERE tenant_id=$1 AND plan_id=$2 AND revision=$3)`, tid, planID, int64(sequence)).Scan(&alreadyExists); err != nil {
			return database(fmt.Sprintf("check plan %s revision %d: %v", planID, sequence, err))
		}
		if alreadyExists {
			return duplicate(fmt.Sprintf("plan %s revision %s", planID, revision.Revision))
		}
		head, found, err := currentHead(ctx, tx, tid, planID)
		if err != nil {
			return err
		}
		if expectedRevision == "" {
			if found {
				return stale(expectedRevision, head.token, "plan already has a current revision")
			}
			if revision.Supersedes.Id != "" {
				return invalid("initial plan revision cannot supersede another revision")
			}
		} else {
			expected, _, parseErr := parseRevision(expectedRevision)
			if parseErr != nil {
				return parseErr
			}
			if !found || head.token != expectedRevision || revision.Supersedes.Id == "" || revision.Supersedes.Id != head.rowID.String() {
				actual := ""
				if found {
					actual = head.token
				}
				return stale(expectedRevision, actual, "plan current revision changed")
			}
			if expected.Stream() != revision.Revision.Stream() {
				return stale(expectedRevision, head.token, "revision stream changed")
			}
			if sequence <= head.revision {
				return stale(expectedRevision, head.token, "successor revision is not newer than current revision")
			}
		}

		var supersedes any
		if revision.Supersedes.Id != "" {
			supersedesID, parseErr := parseUUID(revision.Supersedes.Id, "supersedes")
			if parseErr != nil {
				return parseErr
			}
			supersedes = supersedesID
		}
		affected, err := tx.Exec(ctx, `
			INSERT INTO benefit_plan_revision (
				tenant_id, row_id, plan_id, revision, supersedes, plan_year, name,
				carrier_ref, provider_ref, sponsor_ref, jurisdiction, currency,
				coverage_tiers, options, rate_schedule_ref, eligibility_rules_ref,
				enrollment_rules_ref, contribution_rules_ref, effective_from,
				effective_to, authority_ref, canonical_digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$14::jsonb,
				$15,$16,$17,$18,$19,$20,$21,$22)
			ON CONFLICT DO NOTHING`,
			tid, rowID, planID, int64(sequence), supersedes, revision.PlanYear.Year, revision.Name,
			refs["carrier"], refs["provider"], refs["sponsor"], revision.Jurisdiction, revision.Currency,
			string(coverage), string(options), refs["rate_schedule"], refs["eligibility_rules"],
			refs["enrollment_rules"], refs["contribution_rules"], from, to, refs["authority"], storageDigest(revision.CanonicalDigest))
		if err != nil {
			if isForeignKeyViolation(err) {
				return invalid(fmt.Sprintf("revision lineage: %v", err))
			}
			return database(fmt.Sprintf("insert plan %s revision %s: %v", planID, revision.Revision, err))
		}
		if affected == 0 {
			return duplicate(fmt.Sprintf("plan %s revision %s", planID, revision.Revision))
		}
		return nil
	})
}

// Put is an alias for Save.
func (s *Store) Put(ctx context.Context, tenantID string, revision benefits.PlanRevision, expectedRevision string) error {
	return s.Save(ctx, tenantID, revision, expectedRevision)
}

// Load reads one immutable revision under tenant RLS.
func (s *Store) Load(ctx context.Context, tenantID, planID, revision string) (benefits.PlanRevision, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return benefits.PlanRevision{}, err
	}
	_, sequence, err := parseRevision(revision)
	if err != nil {
		return benefits.PlanRevision{}, err
	}
	pid, err := parseUUID(planID, "plan id")
	if err != nil {
		return benefits.PlanRevision{}, err
	}
	var out benefits.PlanRevision
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var loadErr error
		out, loadErr = loadSequence(ctx, tx, tid, pid, sequence)
		return loadErr
	})
	return out, err
}

// Get is an alias for Load.
func (s *Store) Get(ctx context.Context, tenantID, planID, revision string) (benefits.PlanRevision, error) {
	return s.Load(ctx, tenantID, planID, revision)
}

// Current returns the highest revision in a plan's supersession chain.
func (s *Store) Current(ctx context.Context, tenantID, planID string) (benefits.PlanRevision, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return benefits.PlanRevision{}, err
	}
	pid, err := parseUUID(planID, "plan id")
	if err != nil {
		return benefits.PlanRevision{}, err
	}
	var out benefits.PlanRevision
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		head, found, headErr := currentHead(ctx, tx, tid, pid)
		if headErr != nil {
			return headErr
		}
		if !found {
			return notFound(fmt.Sprintf("plan %s", planID))
		}
		var loadErr error
		out, loadErr = loadSequence(ctx, tx, tid, pid, head.revision)
		return loadErr
	})
	return out, err
}

// List returns a plan's immutable history oldest first.
func (s *Store) List(ctx context.Context, tenantID, planID string) ([]benefits.PlanRevision, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return nil, err
	}
	pid, err := parseUUID(planID, "plan id")
	if err != nil {
		return nil, err
	}
	var out []benefits.PlanRevision
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		rows, queryErr := tx.Query(ctx, `
			SELECT revision FROM benefit_plan_revision
			WHERE tenant_id=$1 AND plan_id=$2 ORDER BY revision`, tid, pid)
		if queryErr != nil {
			return database(fmt.Sprintf("list plan %s: %v", planID, queryErr))
		}
		var revisions []uint64
		for rows.Next() {
			var revision int64
			if scanErr := rows.Scan(&revision); scanErr != nil {
				rows.Close()
				return database(fmt.Sprintf("scan plan %s revision: %v", planID, scanErr))
			}
			revisions = append(revisions, uint64(revision))
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			rows.Close()
			return database(fmt.Sprintf("list plan %s: %v", planID, rowsErr))
		}
		rows.Close()
		if len(revisions) == 0 {
			return notFound(fmt.Sprintf("plan %s", planID))
		}
		for _, revision := range revisions {
			item, loadErr := loadSequence(ctx, tx, tid, pid, revision)
			if loadErr != nil {
				return loadErr
			}
			out = append(out, item)
		}
		return nil
	})
	return out, err
}

type head struct {
	rowID    uuid.UUID
	revision uint64
	token    string
}

func currentHead(ctx context.Context, q dbport.Querier, tenantID, planID uuid.UUID) (head, bool, error) {
	var rowID uuid.UUID
	var revision int64
	var options string
	err := q.QueryRow(ctx, `
		SELECT row_id, revision, options::text
		FROM benefit_plan_revision
		WHERE tenant_id=$1 AND plan_id=$2
		ORDER BY revision DESC LIMIT 1`, tenantID, planID).Scan(&rowID, &revision, &options)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return head{}, false, nil
		}
		return head{}, false, database(fmt.Sprintf("read plan head: %v", err))
	}
	var envelope optionsEnvelope
	if err := json.Unmarshal([]byte(options), &envelope); err != nil || envelope.RevisionStream == "" {
		return head{}, false, database("stored plan head has invalid revision metadata")
	}
	token, err := values.NewSequenceRevision(envelope.RevisionStream, uint64(revision))
	if err != nil {
		return head{}, false, database(fmt.Sprintf("stored plan head revision: %v", err))
	}
	return head{rowID: rowID, revision: uint64(revision), token: token.String()}, true, nil
}

func loadSequence(ctx context.Context, q dbport.Querier, tenantID, planID uuid.UUID, sequence uint64) (benefits.PlanRevision, error) {
	var (
		rowID, storedTenant, storedPlanID  uuid.UUID
		revision                           int64
		supersedes                         *uuid.UUID
		planYear                           int32
		name, jurisdiction, currency       string
		carrier, provider, sponsor         *uuid.UUID
		coverageJSON, optionsJSON          []byte
		rateSchedule, eligibilityRules     *uuid.UUID
		enrollmentRules, contributionRules *uuid.UUID
		from, to                           *time.Time
		authority                          *uuid.UUID
		digest                             string
	)
	err := q.QueryRow(ctx, `
		SELECT row_id, tenant_id, plan_id, revision, supersedes, plan_year, name,
			carrier_ref, provider_ref, sponsor_ref, jurisdiction, currency,
			coverage_tiers, options, rate_schedule_ref, eligibility_rules_ref,
			enrollment_rules_ref, contribution_rules_ref, effective_from,
			effective_to, authority_ref, canonical_digest
		FROM benefit_plan_revision
		WHERE tenant_id=$1 AND plan_id=$2 AND revision=$3`, tenantID, planID, int64(sequence)).Scan(
		&rowID, &storedTenant, &storedPlanID, &revision, &supersedes, &planYear, &name,
		&carrier, &provider, &sponsor, &jurisdiction, &currency, &coverageJSON, &optionsJSON,
		&rateSchedule, &eligibilityRules, &enrollmentRules, &contributionRules, &from, &to,
		&authority, &digest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return benefits.PlanRevision{}, notFound(fmt.Sprintf("plan %s revision %d", planID, sequence))
		}
		return benefits.PlanRevision{}, database(fmt.Sprintf("load plan %s revision %d: %v", planID, sequence, err))
	}
	if storedTenant != tenantID || storedPlanID != planID || from == nil {
		return benefits.PlanRevision{}, database("stored plan revision has inconsistent identity or effective projection")
	}
	var envelope optionsEnvelope
	if err := json.Unmarshal(optionsJSON, &envelope); err != nil || envelope.RevisionStream == "" {
		return benefits.PlanRevision{}, database("stored plan revision has invalid options metadata")
	}
	var coverage []string
	if len(coverageJSON) > 0 && string(coverageJSON) != "null" {
		if err := json.Unmarshal(coverageJSON, &coverage); err != nil {
			return benefits.PlanRevision{}, database(fmt.Sprintf("coverage tiers: %v", err))
		}
	}
	planRef, err := metadataRef(envelope.Refs, "plan", planID, tenantID, "benefit_plan")
	if err != nil {
		return benefits.PlanRevision{}, err
	}
	revisionRef, err := metadataRef(envelope.Refs, "revision_id", rowID, tenantID, "benefit_plan_revision")
	if err != nil {
		return benefits.PlanRevision{}, err
	}
	planYearRef, err := metadataRef(envelope.Refs, "plan_year", planID, tenantID, "benefit_plan")
	if err != nil {
		return benefits.PlanRevision{}, err
	}
	refs := make(map[string]values.EntityRef)
	for key, fallback := range map[string]struct {
		id   *uuid.UUID
		kind values.Kind
	}{
		"carrier": {carrier, "carrier"}, "provider": {provider, "provider"}, "sponsor": {sponsor, "sponsor"},
		"rate_schedule": {rateSchedule, "rate_schedule"}, "eligibility_rules": {eligibilityRules, "eligibility_rules"},
		"enrollment_rules": {enrollmentRules, "enrollment_rules"}, "contribution_rules": {contributionRules, "contribution_rules"},
		"authority": {authority, "authority"},
	} {
		if fallback.id == nil {
			continue
		}
		ref, refErr := metadataRef(envelope.Refs, key, *fallback.id, tenantID, fallback.kind)
		if refErr != nil {
			return benefits.PlanRevision{}, refErr
		}
		refs[key] = ref
	}
	var supersedesRef values.EntityRef
	if supersedes != nil {
		supersedesRef, err = metadataRef(envelope.Refs, "supersedes", *supersedes, tenantID, "benefit_plan_revision")
		if err != nil {
			return benefits.PlanRevision{}, err
		}
	}
	revisionToken, err := values.NewSequenceRevision(envelope.RevisionStream, uint64(revision))
	if err != nil {
		return benefits.PlanRevision{}, database(fmt.Sprintf("revision token: %v", err))
	}
	revisionEffective, err := decodeInterval(envelope.RevisionEffective)
	if err != nil {
		return benefits.PlanRevision{}, database(fmt.Sprintf("revision effective interval: %v", err))
	}
	planYearEffective, err := decodeInterval(envelope.PlanYearEffective)
	if err != nil {
		return benefits.PlanRevision{}, database(fmt.Sprintf("plan-year effective interval: %v", err))
	}
	out := benefits.PlanRevision{
		RevisionID: revisionRef, PlanID: planRef, Revision: revisionToken, Supersedes: supersedesRef,
		PlanYear: benefits.PlanYear{PlanID: planYearRef, Year: planYear, Effective: planYearEffective},
		Name:     name, Carrier: refs["carrier"], Provider: refs["provider"], Sponsor: refs["sponsor"],
		Jurisdiction: jurisdiction, Currency: currency, CoverageTiers: coverage, Options: envelope.Values,
		RateScheduleRef: refs["rate_schedule"], EligibilityRulesRef: refs["eligibility_rules"],
		EnrollmentRulesRef: refs["enrollment_rules"], ContributionRulesRef: refs["contribution_rules"],
		Effective: revisionEffective, Authority: refs["authority"], CanonicalDigest: domainDigest(digest),
	}
	if err := benefits.ValidatePlanRevision(out); err != nil {
		return benefits.PlanRevision{}, database(fmt.Sprintf("stored revision failed validation: %v", err))
	}
	computed, err := out.Digest()
	if err != nil || computed != out.CanonicalDigest {
		return benefits.PlanRevision{}, database("stored plan revision digest does not match its content")
	}
	return out, nil
}

func metadataRef(metadata map[string]string, key string, id uuid.UUID, tenantID uuid.UUID, fallbackKind values.Kind) (values.EntityRef, error) {
	if encoded := metadata[key]; encoded != "" {
		var ref values.EntityRef
		if err := ref.UnmarshalText([]byte(encoded)); err != nil {
			return values.EntityRef{}, database(fmt.Sprintf("reference %s is invalid: %v", key, err))
		}
		if ref.Id != id.String() || ref.Tenant.String() != tenantID.String() {
			return values.EntityRef{}, database(fmt.Sprintf("reference %s does not match row", key))
		}
		return ref, nil
	}
	return values.EntityRef{Tenant: values.TenantId(tenantID.String()), Kind: fallbackKind, Id: id.String()}, nil
}

func encodeEnvelope(revision benefits.PlanRevision) (optionsEnvelope, error) {
	revisionEffective, err := encodeInterval(revision.Effective)
	if err != nil {
		return optionsEnvelope{}, err
	}
	planYearEffective, err := encodeInterval(revision.PlanYear.Effective)
	if err != nil {
		return optionsEnvelope{}, err
	}
	refs := map[string]string{
		"plan": revision.PlanID.String(), "revision_id": revision.RevisionID.String(),
		"plan_year": revision.PlanYear.PlanID.String(),
	}
	for key, ref := range map[string]values.EntityRef{
		"supersedes": revision.Supersedes, "carrier": revision.Carrier, "provider": revision.Provider,
		"sponsor": revision.Sponsor, "rate_schedule": revision.RateScheduleRef,
		"eligibility_rules": revision.EligibilityRulesRef, "enrollment_rules": revision.EnrollmentRulesRef,
		"contribution_rules": revision.ContributionRulesRef, "authority": revision.Authority,
	} {
		if ref.Id != "" {
			refs[key] = ref.String()
		}
	}
	return optionsEnvelope{Values: append([]string(nil), revision.Options...), RevisionStream: revision.Revision.Stream(), RevisionEffective: revisionEffective, PlanYearEffective: planYearEffective, Refs: refs}, nil
}

func encodeInterval(iv values.EffectiveInterval) (intervalEnvelope, error) {
	if err := iv.Validate(); err != nil {
		return intervalEnvelope{}, err
	}
	out := intervalEnvelope{Kind: iv.Kind().String()}
	if iv.Kind() == values.IntervalKindLocalDate {
		start, _ := iv.StartDate()
		out.Start = start.String()
		if end, ok := iv.EndDate(); ok {
			out.End = end.String()
		}
		out.CalendarRef, out.CalendarVersion = iv.Calendar().Ref, iv.Calendar().Version
		return out, nil
	}
	start, _ := iv.StartInstant()
	out.Start = start.String()
	if end, ok := iv.EndInstant(); ok {
		out.End = end.String()
	}
	return out, nil
}

func decodeInterval(encoded intervalEnvelope) (values.EffectiveInterval, error) {
	switch encoded.Kind {
	case "LOCAL_DATE":
		start, err := values.ParseLocalDate(encoded.Start)
		if err != nil {
			return values.EffectiveInterval{}, err
		}
		calendar := values.CalendarRef{Ref: encoded.CalendarRef, Version: encoded.CalendarVersion}
		if encoded.End == "" {
			return values.NewOpenLocalDateInterval(start, calendar)
		}
		end, err := values.ParseLocalDate(encoded.End)
		if err != nil {
			return values.EffectiveInterval{}, err
		}
		return values.NewLocalDateInterval(start, end, calendar)
	case "INSTANT":
		startTime, err := time.Parse(time.RFC3339Nano, encoded.Start)
		if err != nil {
			return values.EffectiveInterval{}, err
		}
		start := values.NewInstant(startTime)
		if encoded.End == "" {
			return values.NewOpenInstantInterval(start)
		}
		endTime, err := time.Parse(time.RFC3339Nano, encoded.End)
		if err != nil {
			return values.EffectiveInterval{}, err
		}
		return values.NewInstantInterval(start, values.NewInstant(endTime))
	default:
		return values.EffectiveInterval{}, fmt.Errorf("unknown interval kind %q", encoded.Kind)
	}
}

func effectiveProjection(iv values.EffectiveInterval) (*time.Time, *time.Time, error) {
	if err := iv.Validate(); err != nil {
		return nil, nil, err
	}
	if iv.Kind() == values.IntervalKindInstant {
		start, _ := iv.StartInstant()
		var end *time.Time
		if value, ok := iv.EndInstant(); ok {
			instant := value.Time()
			end = &instant
		}
		from := start.Time()
		return &from, end, nil
	}
	start, _ := iv.StartDate()
	from := time.Date(int(start.Year()), start.Month(), int(start.Day()), 0, 0, 0, 0, time.UTC)
	var end *time.Time
	if value, ok := iv.EndDate(); ok {
		instant := time.Date(int(value.Year()), value.Month(), int(value.Day()), 0, 0, 0, 0, time.UTC)
		end = &instant
	}
	return &from, end, nil
}

func referenceColumns(revision benefits.PlanRevision) (map[string]any, error) {
	refs := make(map[string]any)
	for key, ref := range map[string]values.EntityRef{
		"carrier": revision.Carrier, "provider": revision.Provider, "sponsor": revision.Sponsor,
		"rate_schedule": revision.RateScheduleRef, "eligibility_rules": revision.EligibilityRulesRef,
		"enrollment_rules": revision.EnrollmentRulesRef, "contribution_rules": revision.ContributionRulesRef,
		"authority": revision.Authority,
	} {
		if ref.Id == "" {
			refs[key] = nil
			continue
		}
		id, err := parseUUID(ref.Id, key)
		if err != nil {
			return nil, err
		}
		refs[key] = id
	}
	return refs, nil
}

func parseUUID(id, field string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil {
		return uuid.Nil, invalid(fmt.Sprintf("%s %q is not a valid uuid", field, id))
	}
	return parsed, nil
}

func storageDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }

func domainDigest(value string) string {
	if value == "" || strings.Contains(value, ":") || len(value) != 64 {
		return value
	}
	return "sha256:" + value
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
