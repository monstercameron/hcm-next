// Package cbastore persists the immutable CBA revision families from
// migrations/00076_cba.sql. Each operation owns a short transaction and
// establishes the tenant context before touching a tenant-scoped table.
package cbastore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/cba"
)

// DB is the transaction-opening capability this adapter needs.
type DB interface{ dbport.Beginner }

// Store implements cba.Store over migration 00076.
type Store struct{ db DB }

var _ cba.Store = (*Store)(nil)

// ErrorCode and Error are aliases for the domain port's stable refusal
// vocabulary, so callers of the adapter need not parse PostgreSQL errors.
type ErrorCode = cba.StoreErrorCode
type Error = cba.StoreError

const (
	CodeInvalid   = cba.StoreInvalidCode
	CodeNotFound  = cba.StoreNotFoundCode
	CodeDuplicate = cba.StoreDuplicateCode
	CodeStaleCAS  = cba.StoreStaleCASCode
)

var (
	ErrInvalid   = cba.ErrStoreInvalid
	ErrNotFound  = cba.ErrStoreNotFound
	ErrDuplicate = cba.ErrStoreDuplicate
	ErrStaleCAS  = cba.ErrStoreStaleCAS
)

// New returns a PostgreSQL-backed CBA store.
func New(db DB) *Store { return &Store{db: db} }

func (s *Store) withTenant(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if s == nil || s.db == nil {
		return invalid("database capability is required")
	}
	if tenantID == uuid.Nil {
		return invalid("tenant id is required")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("cbastore: begin transaction: %w", err)
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
		return fmt.Errorf("cbastore: commit transaction: %w", err)
	}
	return nil
}

func parseTenant(tenantID string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(tenantID))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, invalid(fmt.Sprintf("tenant id %q is not a non-nil UUID", tenantID))
	}
	return id, nil
}

func parseRevision(revision string) (int64, error) {
	value := strings.TrimSpace(revision)
	if value == "" {
		return 0, invalid("revision is required")
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number < 1 {
		return 0, invalid(fmt.Sprintf("revision %q must be a positive decimal integer", revision))
	}
	return number, nil
}

func invalid(detail string) error {
	return &cba.StoreError{Code: cba.StoreInvalidCode, Detail: detail}
}

func notFound(detail string) error {
	return &cba.StoreError{Code: cba.StoreNotFoundCode, Detail: detail}
}

func duplicate(detail string) error {
	return &cba.StoreError{Code: cba.StoreDuplicateCode, Detail: detail}
}

func stale(expected, actual, detail string) error {
	return &cba.StoreError{Code: cba.StoreStaleCASCode, Expected: expected, Actual: actual, Detail: detail}
}

// checkRevisionHead serializes writers for one tenant/entity pair and checks
// the caller's expected current revision before the immutable insert.
func checkRevisionHead(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, table, idColumn, id string, revision int64, expected string) error {
	lockKey := tenantID.String() + ":" + table + ":" + id
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return fmt.Errorf("cbastore: lock %s %s: %w", table, id, err)
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+table+` WHERE tenant_id=$1 AND `+idColumn+`=$2 AND revision=$3)`, tenantID, id, revision).Scan(&exists); err != nil {
		return fmt.Errorf("cbastore: check duplicate %s %s/%d: %w", table, id, revision, err)
	}
	if exists {
		return duplicate(fmt.Sprintf("%s %s revision %d already exists", table, id, revision))
	}

	var actual int64
	err := tx.QueryRow(ctx, `SELECT revision FROM `+table+` WHERE tenant_id=$1 AND `+idColumn+`=$2 ORDER BY revision DESC LIMIT 1`, tenantID, id).Scan(&actual)
	hasCurrent := err == nil
	if err != nil && !errors.Is(err, dbport.ErrNoRows) {
		return fmt.Errorf("cbastore: read %s %s head: %w", table, id, err)
	}
	actualText := ""
	if hasCurrent {
		actualText = strconv.FormatInt(actual, 10)
	}
	if expected == "" {
		if hasCurrent {
			return stale(expected, actualText, fmt.Sprintf("%s already has current revision %s", id, actualText))
		}
		return nil
	}
	if !hasCurrent || expected != actualText || revision <= actual {
		return stale(expected, actualText, fmt.Sprintf("%s current revision is %s", id, actualText))
	}
	return nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func storageTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

func storageString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func storedRevision(value int64) string { return strconv.FormatInt(value, 10) }

// SaveAgreement appends an immutable agreement revision.
func (s *Store) SaveAgreement(ctx context.Context, tenantID string, value cba.AgreementRevision, expectedRevision string) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(value.Version) == "" || strings.TrimSpace(value.Title) == "" {
		return invalid("agreement version and title are required")
	}
	revision, err := parseRevision(value.Revision)
	if err != nil {
		return err
	}
	if err := cba.ValidateAgreement(value); err != nil {
		return invalid(err.Error())
	}
	knownFrom := value.KnownFromOrAt()
	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		if err := checkRevisionHead(ctx, tx, tid, "cba_agreement_revision", "agreement_id", value.AgreementID, revision, expectedRevision); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO cba_agreement_revision (
				row_id, tenant_id, agreement_id, revision, version, title,
				representative, source, effective_from, effective_to,
				known_from, known_to, precedence, retired)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			uuid.New(), tid, value.AgreementID, revision, value.Version, value.Title,
			nullableString(value.Representative), nullableString(value.Source), nullableTime(value.EffectiveFrom), nullableTime(value.EffectiveTo),
			nullableTime(knownFrom), nullableTime(value.KnownTo), value.Precedence, value.Retired)
		if err != nil {
			return fmt.Errorf("cbastore: insert agreement %s/%d: %w", value.AgreementID, revision, err)
		}
		return nil
	})
}

// LoadAgreement loads one immutable agreement revision.
func (s *Store) LoadAgreement(ctx context.Context, tenantID, agreementID, revisionText string) (cba.AgreementRevision, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return cba.AgreementRevision{}, err
	}
	revision, err := parseRevision(revisionText)
	if err != nil {
		return cba.AgreementRevision{}, err
	}
	var out cba.AgreementRevision
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var (
			rowID, storedTenant                            uuid.UUID
			storedID                                       string
			storedRevision                                 int64
			version, title                                 string
			representative, source                         *string
			effectiveFrom, effectiveTo, knownFrom, knownTo *time.Time
			precedence                                     int
			retired                                        bool
		)
		err := tx.QueryRow(ctx, `
			SELECT row_id, tenant_id, agreement_id, revision, version, title,
				representative, source, effective_from, effective_to,
				known_from, known_to, precedence, retired
			FROM cba_agreement_revision
			WHERE tenant_id=$1 AND agreement_id=$2 AND revision=$3`, tid, agreementID, revision).Scan(
			&rowID, &storedTenant, &storedID, &storedRevision, &version, &title,
			&representative, &source, &effectiveFrom, &effectiveTo, &knownFrom, &knownTo, &precedence, &retired)
		if err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return notFound(fmt.Sprintf("agreement %s revision %s", agreementID, revisionText))
			}
			return fmt.Errorf("cbastore: load agreement %s/%d: %w", agreementID, revision, err)
		}
		if effectiveFrom == nil || knownFrom == nil {
			return invalid("stored agreement is missing an effective or known start")
		}
		out = cba.AgreementRevision{
			ID: rowID.String(), AgreementID: storedID, Revision: storedRevisionText(storedRevision), Version: version, Title: title,
			Representative: storageString(representative), Source: storageString(source), EffectiveFrom: storageTime(effectiveFrom), EffectiveTo: storageTime(effectiveTo),
			KnownFrom: storageTime(knownFrom), KnownTo: storageTime(knownTo), Precedence: precedence, Retired: retired,
		}
		// The tenant UUID is selected only to make the RLS boundary explicit;
		// the domain object intentionally has no tenant field.
		_ = storedTenant
		return nil
	})
	return out, err
}

// CurrentAgreement loads the highest stored revision for an agreement.
func (s *Store) CurrentAgreement(ctx context.Context, tenantID, agreementID string) (cba.AgreementRevision, error) {
	values, err := s.ListAgreementRevisions(ctx, tenantID, agreementID)
	if err != nil {
		return cba.AgreementRevision{}, err
	}
	return values[len(values)-1], nil
}

// ListAgreementRevisions returns an agreement's immutable history ascending
// by revision.
func (s *Store) ListAgreementRevisions(ctx context.Context, tenantID, agreementID string) ([]cba.AgreementRevision, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return nil, err
	}
	var revisions []int64
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT revision FROM cba_agreement_revision WHERE tenant_id=$1 AND agreement_id=$2 ORDER BY revision`, tid, agreementID)
		if err != nil {
			return fmt.Errorf("cbastore: list agreements %s: %w", agreementID, err)
		}
		defer rows.Close()
		for rows.Next() {
			var revision int64
			if err := rows.Scan(&revision); err != nil {
				return fmt.Errorf("cbastore: scan agreement revision: %w", err)
			}
			revisions = append(revisions, revision)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(revisions) == 0 {
		return nil, notFound(fmt.Sprintf("agreement %s", agreementID))
	}
	out := make([]cba.AgreementRevision, 0, len(revisions))
	for _, revision := range revisions {
		value, err := s.LoadAgreement(ctx, tenantID, agreementID, storedRevisionText(revision))
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision < out[j].Revision })
	return out, nil
}

// SaveBargainingUnit appends an immutable bargaining-unit revision.
func (s *Store) SaveBargainingUnit(ctx context.Context, tenantID string, value cba.BargainingUnitRevision, expectedRevision string) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	revision, err := parseRevision(value.Revision)
	if err != nil {
		return err
	}
	if err := cba.ValidateUnit(value); err != nil {
		return invalid(err.Error())
	}
	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		if err := checkRevisionHead(ctx, tx, tid, "cba_bargaining_unit_revision", "unit_id", value.UnitID, revision, expectedRevision); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO cba_bargaining_unit_revision (
				row_id, tenant_id, unit_id, revision, agreement_id, name,
				representative, source, effective_from, effective_to,
				known_from, known_to, retired)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			uuid.New(), tid, value.UnitID, revision, value.AgreementID, value.Name,
			nullableString(value.Representative), nullableString(value.Source), nullableTime(value.EffectiveFrom), nullableTime(value.EffectiveTo),
			nullableTime(value.KnownFrom), nullableTime(value.KnownTo), value.Retired)
		if err != nil {
			return fmt.Errorf("cbastore: insert bargaining unit %s/%d: %w", value.UnitID, revision, err)
		}
		return nil
	})
}

// LoadBargainingUnit loads one immutable bargaining-unit revision.
func (s *Store) LoadBargainingUnit(ctx context.Context, tenantID, unitID, revisionText string) (cba.BargainingUnitRevision, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return cba.BargainingUnitRevision{}, err
	}
	revision, err := parseRevision(revisionText)
	if err != nil {
		return cba.BargainingUnitRevision{}, err
	}
	var out cba.BargainingUnitRevision
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var (
			rowID, storedTenant                            uuid.UUID
			storedID, agreementID                          string
			storedRevision                                 int64
			name                                           string
			representative, source                         *string
			effectiveFrom, effectiveTo, knownFrom, knownTo *time.Time
			retired                                        bool
		)
		err := tx.QueryRow(ctx, `
			SELECT row_id, tenant_id, unit_id, revision, agreement_id, name,
				representative, source, effective_from, effective_to, known_from, known_to, retired
			FROM cba_bargaining_unit_revision
			WHERE tenant_id=$1 AND unit_id=$2 AND revision=$3`, tid, unitID, revision).Scan(
			&rowID, &storedTenant, &storedID, &storedRevision, &agreementID, &name,
			&representative, &source, &effectiveFrom, &effectiveTo, &knownFrom, &knownTo, &retired)
		if err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return notFound(fmt.Sprintf("unit %s revision %s", unitID, revisionText))
			}
			return fmt.Errorf("cbastore: load unit %s/%d: %w", unitID, revision, err)
		}
		if effectiveFrom == nil || knownFrom == nil {
			return invalid("stored unit is missing an effective or known start")
		}
		out = cba.BargainingUnitRevision{ID: rowID.String(), UnitID: storedID, Revision: storedRevisionText(storedRevision), AgreementID: agreementID, Name: name, Representative: storageString(representative), Source: storageString(source), EffectiveFrom: storageTime(effectiveFrom), EffectiveTo: storageTime(effectiveTo), KnownFrom: storageTime(knownFrom), KnownTo: storageTime(knownTo), Retired: retired}
		_ = storedTenant
		return nil
	})
	return out, err
}

// CurrentBargainingUnit loads the highest stored unit revision.
func (s *Store) CurrentBargainingUnit(ctx context.Context, tenantID, unitID string) (cba.BargainingUnitRevision, error) {
	values, err := s.ListBargainingUnitRevisions(ctx, tenantID, unitID)
	if err != nil {
		return cba.BargainingUnitRevision{}, err
	}
	return values[len(values)-1], nil
}

// ListBargainingUnitRevisions returns a unit's immutable history ascending by
// revision.
func (s *Store) ListBargainingUnitRevisions(ctx context.Context, tenantID, unitID string) ([]cba.BargainingUnitRevision, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return nil, err
	}
	var revisions []int64
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT revision FROM cba_bargaining_unit_revision WHERE tenant_id=$1 AND unit_id=$2 ORDER BY revision`, tid, unitID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var revision int64
			if err := rows.Scan(&revision); err != nil {
				return err
			}
			revisions = append(revisions, revision)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(revisions) == 0 {
		return nil, notFound(fmt.Sprintf("unit %s", unitID))
	}
	out := make([]cba.BargainingUnitRevision, 0, len(revisions))
	for _, revision := range revisions {
		value, err := s.LoadBargainingUnit(ctx, tenantID, unitID, storedRevisionText(revision))
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

// SaveMembership appends an immutable membership revision.
func (s *Store) SaveMembership(ctx context.Context, tenantID string, value cba.MembershipRevision, expectedRevision string) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	revision, err := parseRevision(value.Revision)
	if err != nil {
		return err
	}
	if err := cba.ValidateMembership(value); err != nil {
		return invalid(err.Error())
	}
	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		if err := checkRevisionHead(ctx, tx, tid, "cba_membership_revision", "membership_id", value.MembershipID, revision, expectedRevision); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO cba_membership_revision (
				row_id, tenant_id, membership_id, revision, worker_id, unit_id,
				source, effective_from, effective_to, known_from, known_to, retired)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			uuid.New(), tid, value.MembershipID, revision, value.WorkerID, value.UnitID,
			nullableString(value.Source), nullableTime(value.EffectiveFrom), nullableTime(value.EffectiveTo), nullableTime(value.KnownFrom), nullableTime(value.KnownTo), value.Retired)
		if err != nil {
			return fmt.Errorf("cbastore: insert membership %s/%d: %w", value.MembershipID, revision, err)
		}
		return nil
	})
}

// LoadMembership loads one immutable membership revision.
func (s *Store) LoadMembership(ctx context.Context, tenantID, membershipID, revisionText string) (cba.MembershipRevision, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return cba.MembershipRevision{}, err
	}
	revision, err := parseRevision(revisionText)
	if err != nil {
		return cba.MembershipRevision{}, err
	}
	var out cba.MembershipRevision
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var (
			rowID, storedTenant                            uuid.UUID
			storedID, workerID, unitID                     string
			storedRevision                                 int64
			source                                         *string
			effectiveFrom, effectiveTo, knownFrom, knownTo *time.Time
			retired                                        bool
		)
		err := tx.QueryRow(ctx, `
			SELECT row_id, tenant_id, membership_id, revision, worker_id, unit_id,
				source, effective_from, effective_to, known_from, known_to, retired
			FROM cba_membership_revision
			WHERE tenant_id=$1 AND membership_id=$2 AND revision=$3`, tid, membershipID, revision).Scan(
			&rowID, &storedTenant, &storedID, &storedRevision, &workerID, &unitID,
			&source, &effectiveFrom, &effectiveTo, &knownFrom, &knownTo, &retired)
		if err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return notFound(fmt.Sprintf("membership %s revision %s", membershipID, revisionText))
			}
			return fmt.Errorf("cbastore: load membership %s/%d: %w", membershipID, revision, err)
		}
		if effectiveFrom == nil || knownFrom == nil {
			return invalid("stored membership is missing an effective or known start")
		}
		out = cba.MembershipRevision{ID: rowID.String(), MembershipID: storedID, Revision: storedRevisionText(storedRevision), WorkerID: workerID, UnitID: unitID, Source: storageString(source), EffectiveFrom: storageTime(effectiveFrom), EffectiveTo: storageTime(effectiveTo), KnownFrom: storageTime(knownFrom), KnownTo: storageTime(knownTo), Retired: retired}
		_ = storedTenant
		return nil
	})
	return out, err
}

// CurrentMembership loads the highest stored membership revision.
func (s *Store) CurrentMembership(ctx context.Context, tenantID, membershipID string) (cba.MembershipRevision, error) {
	values, err := s.ListMembershipRevisions(ctx, tenantID, membershipID)
	if err != nil {
		return cba.MembershipRevision{}, err
	}
	return values[len(values)-1], nil
}

// ListMembershipRevisions returns a membership's immutable history ascending
// by revision.
func (s *Store) ListMembershipRevisions(ctx context.Context, tenantID, membershipID string) ([]cba.MembershipRevision, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return nil, err
	}
	var revisions []int64
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT revision FROM cba_membership_revision WHERE tenant_id=$1 AND membership_id=$2 ORDER BY revision`, tid, membershipID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var revision int64
			if err := rows.Scan(&revision); err != nil {
				return err
			}
			revisions = append(revisions, revision)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(revisions) == 0 {
		return nil, notFound(fmt.Sprintf("membership %s", membershipID))
	}
	out := make([]cba.MembershipRevision, 0, len(revisions))
	for _, revision := range revisions {
		value, err := s.LoadMembership(ctx, tenantID, membershipID, storedRevisionText(revision))
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

func marshalStrings(values []string) any {
	if values == nil {
		return nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil
	}
	return string(encoded)
}

func decodeStrings(raw *string) ([]string, error) {
	if raw == nil || *raw == "null" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(*raw), &values); err != nil {
		return nil, fmt.Errorf("cbastore: decode string array: %w", err)
	}
	return values, nil
}

// SaveAgreementClause appends an immutable clause revision.
func (s *Store) SaveAgreementClause(ctx context.Context, tenantID string, value cba.AgreementClauseRevision, expectedRevision string) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	revision, err := parseRevision(value.Revision)
	if err != nil {
		return err
	}
	agreementRevision, err := parseRevision(value.AgreementRevision)
	if err != nil {
		return invalid("agreement_revision: " + err.Error())
	}
	if err := value.Validate(); err != nil {
		return invalid(err.Error())
	}
	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		if err := checkRevisionHead(ctx, tx, tid, "cba_agreement_clause_revision", "clause_id", value.ID, revision, expectedRevision); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO cba_agreement_clause_revision (
				row_id, tenant_id, clause_id, agreement_id, agreement_revision,
				revision, kind, job_codes, location_ids, effective_from,
				effective_to, known_from, known_to, retired)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10,$11,$12,$13,$14)`,
			uuid.New(), tid, value.ID, value.AgreementID, agreementRevision, revision, value.Kind,
			marshalStrings(value.JobCodes), marshalStrings(value.LocationIDs), nullableTime(value.EffectiveFrom), nullableTime(value.EffectiveTo), nullableTime(value.KnownFrom), nullableTime(value.KnownTo), value.Retired)
		if err != nil {
			return fmt.Errorf("cbastore: insert clause %s/%d: %w", value.ID, revision, err)
		}
		return nil
	})
}

// LoadAgreementClause loads one immutable clause revision.
func (s *Store) LoadAgreementClause(ctx context.Context, tenantID, clauseID, revisionText string) (cba.AgreementClauseRevision, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return cba.AgreementClauseRevision{}, err
	}
	revision, err := parseRevision(revisionText)
	if err != nil {
		return cba.AgreementClauseRevision{}, err
	}
	var out cba.AgreementClauseRevision
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var (
			rowID, storedTenant                            uuid.UUID
			storedID, agreementID, kind                    string
			agreementRevision, storedRevision              int64
			jobCodes, locationIDs                          *string
			effectiveFrom, effectiveTo, knownFrom, knownTo *time.Time
			retired                                        bool
		)
		err := tx.QueryRow(ctx, `
			SELECT row_id, tenant_id, clause_id, agreement_id, agreement_revision,
				revision, kind, job_codes::text, location_ids::text,
				effective_from, effective_to, known_from, known_to, retired
			FROM cba_agreement_clause_revision
			WHERE tenant_id=$1 AND clause_id=$2 AND revision=$3`, tid, clauseID, revision).Scan(
			&rowID, &storedTenant, &storedID, &agreementID, &agreementRevision, &storedRevision, &kind,
			&jobCodes, &locationIDs, &effectiveFrom, &effectiveTo, &knownFrom, &knownTo, &retired)
		if err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return notFound(fmt.Sprintf("clause %s revision %s", clauseID, revisionText))
			}
			return fmt.Errorf("cbastore: load clause %s/%d: %w", clauseID, revision, err)
		}
		if effectiveFrom == nil || knownFrom == nil {
			return invalid("stored clause is missing an effective or known start")
		}
		jobs, err := decodeStrings(jobCodes)
		if err != nil {
			return invalid(err.Error())
		}
		locations, err := decodeStrings(locationIDs)
		if err != nil {
			return invalid(err.Error())
		}
		// The migration intentionally stores clause scope only. Source is a
		// kernel-required explanation reference but is not a clause body; use a
		// stable adapter reference when materializing the schema's scope row.
		out = cba.AgreementClauseRevision{ID: storedID, AgreementID: agreementID, AgreementRevision: storedRevisionText(agreementRevision), Revision: storedRevisionText(storedRevision), Kind: kind, Source: "cba_agreement_clause_revision", JobCodes: jobs, LocationIDs: locations, EffectiveFrom: storageTime(effectiveFrom), EffectiveTo: storageTime(effectiveTo), KnownFrom: storageTime(knownFrom), KnownTo: storageTime(knownTo), Retired: retired}
		_ = rowID
		_ = storedTenant
		return nil
	})
	return out, err
}

// CurrentAgreementClause loads the highest stored clause revision.
func (s *Store) CurrentAgreementClause(ctx context.Context, tenantID, clauseID string) (cba.AgreementClauseRevision, error) {
	values, err := s.ListAgreementClauseRevisions(ctx, tenantID, clauseID)
	if err != nil {
		return cba.AgreementClauseRevision{}, err
	}
	return values[len(values)-1], nil
}

// ListAgreementClauseRevisions returns a clause's immutable history ascending
// by revision.
func (s *Store) ListAgreementClauseRevisions(ctx context.Context, tenantID, clauseID string) ([]cba.AgreementClauseRevision, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return nil, err
	}
	var revisions []int64
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT revision FROM cba_agreement_clause_revision WHERE tenant_id=$1 AND clause_id=$2 ORDER BY revision`, tid, clauseID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var revision int64
			if err := rows.Scan(&revision); err != nil {
				return err
			}
			revisions = append(revisions, revision)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(revisions) == 0 {
		return nil, notFound(fmt.Sprintf("clause %s", clauseID))
	}
	out := make([]cba.AgreementClauseRevision, 0, len(revisions))
	for _, revision := range revisions {
		value, err := s.LoadAgreementClause(ctx, tenantID, clauseID, storedRevisionText(revision))
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

func storedRevisionText(value int64) string { return strconv.FormatInt(value, 10) }

// Revision-named aliases keep the adapter vocabulary aligned with the table
// names while the shorter methods remain the domain port.
func (s *Store) SaveAgreementRevision(ctx context.Context, tenant string, value cba.AgreementRevision, expected string) error {
	return s.SaveAgreement(ctx, tenant, value, expected)
}
func (s *Store) LoadAgreementRevision(ctx context.Context, tenant, id, revision string) (cba.AgreementRevision, error) {
	return s.LoadAgreement(ctx, tenant, id, revision)
}
func (s *Store) CurrentAgreementRevision(ctx context.Context, tenant, id string) (cba.AgreementRevision, error) {
	return s.CurrentAgreement(ctx, tenant, id)
}
func (s *Store) ListAgreementRevision(ctx context.Context, tenant, id string) ([]cba.AgreementRevision, error) {
	return s.ListAgreementRevisions(ctx, tenant, id)
}
func (s *Store) SaveBargainingUnitRevision(ctx context.Context, tenant string, value cba.BargainingUnitRevision, expected string) error {
	return s.SaveBargainingUnit(ctx, tenant, value, expected)
}
func (s *Store) LoadBargainingUnitRevision(ctx context.Context, tenant, id, revision string) (cba.BargainingUnitRevision, error) {
	return s.LoadBargainingUnit(ctx, tenant, id, revision)
}
func (s *Store) CurrentBargainingUnitRevision(ctx context.Context, tenant, id string) (cba.BargainingUnitRevision, error) {
	return s.CurrentBargainingUnit(ctx, tenant, id)
}
func (s *Store) ListBargainingUnitRevision(ctx context.Context, tenant, id string) ([]cba.BargainingUnitRevision, error) {
	return s.ListBargainingUnitRevisions(ctx, tenant, id)
}
func (s *Store) SaveMembershipRevision(ctx context.Context, tenant string, value cba.MembershipRevision, expected string) error {
	return s.SaveMembership(ctx, tenant, value, expected)
}
func (s *Store) LoadMembershipRevision(ctx context.Context, tenant, id, revision string) (cba.MembershipRevision, error) {
	return s.LoadMembership(ctx, tenant, id, revision)
}
func (s *Store) CurrentMembershipRevision(ctx context.Context, tenant, id string) (cba.MembershipRevision, error) {
	return s.CurrentMembership(ctx, tenant, id)
}
func (s *Store) ListMembershipRevision(ctx context.Context, tenant, id string) ([]cba.MembershipRevision, error) {
	return s.ListMembershipRevisions(ctx, tenant, id)
}
func (s *Store) SaveAgreementClauseRevision(ctx context.Context, tenant string, value cba.AgreementClauseRevision, expected string) error {
	return s.SaveAgreementClause(ctx, tenant, value, expected)
}
func (s *Store) LoadAgreementClauseRevision(ctx context.Context, tenant, id, revision string) (cba.AgreementClauseRevision, error) {
	return s.LoadAgreementClause(ctx, tenant, id, revision)
}
func (s *Store) CurrentAgreementClauseRevision(ctx context.Context, tenant, id string) (cba.AgreementClauseRevision, error) {
	return s.CurrentAgreementClause(ctx, tenant, id)
}
func (s *Store) ListAgreementClauseRevision(ctx context.Context, tenant, id string) ([]cba.AgreementClauseRevision, error) {
	return s.ListAgreementClauseRevisions(ctx, tenant, id)
}

func expectedRevision(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// Put* aliases provide insertion-oriented names for callers that do not use
// the cba.Store interface's Save* vocabulary.
func (s *Store) PutAgreement(ctx context.Context, tenant string, value cba.AgreementRevision, expected ...string) error {
	return s.SaveAgreement(ctx, tenant, value, expectedRevision(expected))
}
func (s *Store) PutBargainingUnit(ctx context.Context, tenant string, value cba.BargainingUnitRevision, expected ...string) error {
	return s.SaveBargainingUnit(ctx, tenant, value, expectedRevision(expected))
}
func (s *Store) PutMembership(ctx context.Context, tenant string, value cba.MembershipRevision, expected ...string) error {
	return s.SaveMembership(ctx, tenant, value, expectedRevision(expected))
}
func (s *Store) PutAgreementClause(ctx context.Context, tenant string, value cba.AgreementClauseRevision, expected ...string) error {
	return s.SaveAgreementClause(ctx, tenant, value, expectedRevision(expected))
}
