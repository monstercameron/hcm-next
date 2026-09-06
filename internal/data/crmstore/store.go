// Package crmstore persists the CRM domain's immutable talent-pool and
// pool-membership revisions declared by migrations/00084_crm.sql.
package crmstore

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/crm"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// DB is the transaction capability required by the adapter.
type DB interface{ dbport.Beginner }

// Store implements crm.Store over PostgreSQL.
type Store struct{ db DB }

var _ crm.Store = (*Store)(nil)

// New returns a PostgreSQL-backed CRM store.
func New(db DB) *Store { return &Store{db: db} }

func (s *Store) PutPool(ctx context.Context, tenant values.TenantId, pool crm.TalentPoolRevision, expectedVersion ...uint64) error {
	if err := pool.Validate(); err != nil {
		return invalid("talent_pool_revision", pool.PoolID.Id, err)
	}
	if len(expectedVersion) > 1 {
		return invalid("talent_pool_revision", pool.PoolID.Id, errors.New("at most one expected version is allowed"))
	}
	if pool.PoolID.Tenant != tenant {
		return invalid("talent_pool_revision", pool.PoolID.Id, errors.New("pool tenant does not match store tenant"))
	}
	poolID, err := uuid.Parse(pool.PoolID.Id)
	if err != nil {
		return invalid("talent_pool_revision", pool.PoolID.Id, fmt.Errorf("pool id must be a UUID: %w", err))
	}
	owner, err := uuid.Parse(pool.Owner.Id)
	if err != nil {
		return invalid("talent_pool_revision", pool.PoolID.Id, fmt.Errorf("owner must be a UUID: %w", err))
	}
	revision, err := sequence(pool.Revision)
	if err != nil {
		return invalid("talent_pool_revision", pool.PoolID.Id, err)
	}
	from, to, err := effectiveProjection(pool.Effective)
	if err != nil {
		return invalid("talent_pool_revision", pool.PoolID.Id, err)
	}
	criteria, err := json.Marshal(criteriaPayload{Text: pool.Criteria, Effective: encodeEffective(pool.Effective)})
	if err != nil {
		return database("talent_pool_revision", pool.PoolID.Id, err)
	}
	source, err := json.Marshal(pool.Source)
	if err != nil {
		return invalid("talent_pool_revision", pool.PoolID.Id, err)
	}
	consent, err := json.Marshal(pool.Consent)
	if err != nil {
		return invalid("talent_pool_revision", pool.PoolID.Id, err)
	}
	scope, err := marshalScope(pool.Scope)
	if err != nil {
		return invalid("talent_pool_revision", pool.PoolID.Id, err)
	}
	removal, err := json.Marshal(removalPayload{Policy: pool.RemovalPolicy})
	if err != nil {
		return invalid("talent_pool_revision", pool.PoolID.Id, err)
	}
	expected := optionalExpected(expectedVersion)
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		latest, err := latestVersion(ctx, tx, "talent_pool_revision", "pool_id", poolID, tenantID)
		if err != nil {
			return err
		}
		if revision <= latest {
			return refusal(crm.StoreDuplicateCode, fmt.Sprintf("pool %s revision %d already exists", pool.PoolID.Id, revision))
		}
		if expected != latest || revision != latest+1 {
			return stale(expected, latest, fmt.Sprintf("pool %s", pool.PoolID.Id))
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO talent_pool_revision
				(row_id, tenant_id, pool_id, revision, purpose, criteria, source,
				 consent, scope, effective_from, effective_to, owner_ref, removal_policy)
			VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8::jsonb,$9::jsonb,$10,$11,$12,$13::jsonb)`,
			uuid.New(), tenantID, poolID, int64(revision), pool.Purpose, string(criteria), string(source),
			string(consent), string(scope), from, to, owner, string(removal))
		if err != nil {
			return mapWriteError("talent_pool_revision", pool.PoolID.Id, err)
		}
		return nil
	})
}

func (s *Store) GetPool(ctx context.Context, tenant values.TenantId, id string, revision uint64) (crm.TalentPoolRevision, error) {
	var out crm.TalentPoolRevision
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var (
			poolID, owner                             uuid.UUID
			storedRevision                            int64
			criteria, source, consent, scope, removal string
			from, to                                  *time.Time
		)
		err := tx.QueryRow(ctx, `
			SELECT pool_id, revision, purpose, criteria::text, source::text,
				consent::text, scope::text, effective_from, effective_to,
				owner_ref, removal_policy::text
			FROM talent_pool_revision
			WHERE tenant_id=$1 AND pool_id=$2 AND revision=$3`, tenantID, parseUUIDOrNil(id), int64(revision)).Scan(
			&poolID, &storedRevision, &out.Purpose, &criteria, &source, &consent, &scope,
			&from, &to, &owner, &removal)
		if errors.Is(err, dbport.ErrNoRows) {
			return notFound("talent_pool_revision", id, crm.ErrPoolNotFound)
		}
		if err != nil {
			return database("talent_pool_revision", id, err)
		}
		var criteriaData criteriaPayload
		if err := json.Unmarshal([]byte(criteria), &criteriaData); err != nil {
			return database("talent_pool_revision", id, fmt.Errorf("decode criteria: %w", err))
		}
		interval, err := decodeEffective(criteriaData.Effective)
		if err != nil {
			return database("talent_pool_revision", id, fmt.Errorf("decode effective interval: %w", err))
		}
		if err := json.Unmarshal([]byte(source), &out.Source); err != nil {
			return database("talent_pool_revision", id, fmt.Errorf("decode source: %w", err))
		}
		if err := json.Unmarshal([]byte(consent), &out.Consent); err != nil {
			return database("talent_pool_revision", id, fmt.Errorf("decode consent: %w", err))
		}
		if err := unmarshalScope([]byte(scope), &out.Scope); err != nil {
			return database("talent_pool_revision", id, fmt.Errorf("decode scope: %w", err))
		}
		var removalData removalPayload
		if err := json.Unmarshal([]byte(removal), &removalData); err != nil {
			return database("talent_pool_revision", id, fmt.Errorf("decode removal policy: %w", err))
		}
		out.PoolID = values.EntityRef{Tenant: tenant, Kind: "talent_pool", Id: poolID.String()}
		out.Revision, err = values.NewSequenceRevision("crm", uint64(storedRevision))
		if err != nil {
			return database("talent_pool_revision", id, err)
		}
		out.Criteria = criteriaData.Text
		out.Effective = interval
		out.Owner = values.EntityRef{Tenant: tenant, Kind: "owner", Id: owner.String()}
		out.RemovalPolicy = removalData.Policy
		_ = from
		_ = to
		return nil
	})
	return out, err
}

func (s *Store) ListPoolVersions(ctx context.Context, tenant values.TenantId, id string) ([]crm.TalentPoolRevision, error) {
	versions := make([]uint64, 0)
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		poolID, err := parseUUID(id)
		if err != nil {
			return invalid("talent_pool_revision", id, err)
		}
		rows, err := tx.Query(ctx, `SELECT revision FROM talent_pool_revision WHERE tenant_id=$1 AND pool_id=$2 ORDER BY revision`, tenantID, poolID)
		if err != nil {
			return database("talent_pool_revision", id, err)
		}
		defer rows.Close()
		for rows.Next() {
			var revision int64
			if err := rows.Scan(&revision); err != nil {
				return database("talent_pool_revision", id, err)
			}
			versions = append(versions, uint64(revision))
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, notFound("talent_pool_revision", id, crm.ErrPoolNotFound)
	}
	out := make([]crm.TalentPoolRevision, 0, len(versions))
	for _, revision := range versions {
		pool, err := s.GetPool(ctx, tenant, id, revision)
		if err != nil {
			return nil, err
		}
		out = append(out, pool)
	}
	return out, nil
}

func (s *Store) PutMembership(ctx context.Context, tenant values.TenantId, membership crm.TalentPoolMembershipRevision, expectedVersion ...uint64) error {
	if err := membership.Validate(); err != nil {
		return invalid("talent_pool_membership_revision", membership.MembershipID.Id, err)
	}
	if len(expectedVersion) > 1 {
		return invalid("talent_pool_membership_revision", membership.MembershipID.Id, errors.New("at most one expected version is allowed"))
	}
	if membership.MembershipID.Tenant != tenant || membership.Pool.Tenant != tenant {
		return invalid("talent_pool_membership_revision", membership.MembershipID.Id, errors.New("membership references do not match store tenant"))
	}
	membershipID, err := uuid.Parse(membership.MembershipID.Id)
	if err != nil {
		return invalid("talent_pool_membership_revision", membership.MembershipID.Id, fmt.Errorf("membership id must be a UUID: %w", err))
	}
	poolID, err := uuid.Parse(membership.Pool.Id)
	if err != nil {
		return invalid("talent_pool_membership_revision", membership.MembershipID.Id, fmt.Errorf("pool reference must be a UUID: %w", err))
	}
	subject, err := uuid.Parse(membership.Subject.Id)
	if err != nil {
		return invalid("talent_pool_membership_revision", membership.MembershipID.Id, fmt.Errorf("subject reference must be a UUID: %w", err))
	}
	owner, err := uuid.Parse(membership.Owner.Id)
	if err != nil {
		return invalid("talent_pool_membership_revision", membership.MembershipID.Id, fmt.Errorf("owner must be a UUID: %w", err))
	}
	revision, err := sequence(membership.Revision)
	if err != nil {
		return invalid("talent_pool_membership_revision", membership.MembershipID.Id, err)
	}
	from, to, err := effectiveProjection(membership.Effective)
	if err != nil {
		return invalid("talent_pool_membership_revision", membership.MembershipID.Id, err)
	}
	source, err := json.Marshal(membershipSourcePayload{Source: membership.Source, SubjectKind: string(membership.Subject.Kind), Effective: encodeEffective(membership.Effective)})
	if err != nil {
		return invalid("talent_pool_membership_revision", membership.MembershipID.Id, err)
	}
	consent, err := json.Marshal(membership.Consent)
	if err != nil {
		return invalid("talent_pool_membership_revision", membership.MembershipID.Id, err)
	}
	scope, err := marshalScope(membership.Scope)
	if err != nil {
		return invalid("talent_pool_membership_revision", membership.MembershipID.Id, err)
	}
	removal, err := json.Marshal(removalPayload{Policy: membership.RemovalPolicy})
	if err != nil {
		return invalid("talent_pool_membership_revision", membership.MembershipID.Id, err)
	}
	expected := optionalExpected(expectedVersion)
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var poolRow uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT row_id FROM talent_pool_revision WHERE tenant_id=$1 AND pool_id=$2 ORDER BY revision DESC LIMIT 1`, tenantID, poolID).Scan(&poolRow); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return refusal(crm.StoreReferenceNotFoundCode, fmt.Sprintf("pool %s is not present for tenant", membership.Pool.Id))
			}
			return database("talent_pool_membership_revision", membership.MembershipID.Id, err)
		}
		latest, err := latestVersion(ctx, tx, "talent_pool_membership_revision", "membership_id", membershipID, tenantID)
		if err != nil {
			return err
		}
		if revision <= latest {
			return refusal(crm.StoreDuplicateCode, fmt.Sprintf("membership %s revision %d already exists", membership.MembershipID.Id, revision))
		}
		if expected != latest || revision != latest+1 {
			return stale(expected, latest, fmt.Sprintf("membership %s", membership.MembershipID.Id))
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO talent_pool_membership_revision
				(row_id, tenant_id, membership_id, revision, pool_ref, subject_ref,
				 role, purpose, source, consent, scope, effective_from, effective_to,
				 owner_ref, removal_policy)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb,$11::jsonb,$12,$13,$14,$15::jsonb)`,
			uuid.New(), tenantID, membershipID, int64(revision), poolID, subject, string(membership.Role),
			membership.Purpose, string(source), string(consent), string(scope), from, to, owner, string(removal))
		if err != nil {
			return mapWriteError("talent_pool_membership_revision", membership.MembershipID.Id, err)
		}
		return nil
	})
}

func (s *Store) GetMembership(ctx context.Context, tenant values.TenantId, id string, revision uint64) (crm.TalentPoolMembershipRevision, error) {
	var out crm.TalentPoolMembershipRevision
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var (
			membershipID, poolID, subject, owner uuid.UUID
			storedRevision                       int64
			source, consent, scope, removal      *string
			from, to                             *time.Time
			purpose, role                        string
		)
		rowID, err := parseUUID(id)
		if err != nil {
			return invalid("talent_pool_membership_revision", id, err)
		}
		err = tx.QueryRow(ctx, `
			SELECT membership_id, revision, pool_ref, subject_ref, role, purpose,
				source::text, consent::text, scope::text, effective_from, effective_to,
				owner_ref, removal_policy::text
			FROM talent_pool_membership_revision
			WHERE tenant_id=$1 AND membership_id=$2 AND revision=$3`, tenantID, rowID, int64(revision)).Scan(
			&membershipID, &storedRevision, &poolID, &subject, &role, &purpose, &source, &consent,
			&scope, &from, &to, &owner, &removal)
		if errors.Is(err, dbport.ErrNoRows) {
			return notFound("talent_pool_membership_revision", id, crm.ErrMembershipNotFound)
		}
		if err != nil {
			return database("talent_pool_membership_revision", id, err)
		}
		if source == nil || consent == nil || removal == nil {
			return database("talent_pool_membership_revision", id, errors.New("required JSON field is null"))
		}
		var sourceData membershipSourcePayload
		if err := json.Unmarshal([]byte(*source), &sourceData); err != nil {
			return database("talent_pool_membership_revision", id, fmt.Errorf("decode source: %w", err))
		}
		if err := json.Unmarshal([]byte(*consent), &out.Consent); err != nil {
			return database("talent_pool_membership_revision", id, fmt.Errorf("decode consent: %w", err))
		}
		if scope != nil {
			if err := unmarshalScope([]byte(*scope), &out.Scope); err != nil {
				return database("talent_pool_membership_revision", id, fmt.Errorf("decode scope: %w", err))
			}
		}
		var removalData removalPayload
		if err := json.Unmarshal([]byte(*removal), &removalData); err != nil {
			return database("talent_pool_membership_revision", id, fmt.Errorf("decode removal policy: %w", err))
		}
		interval, err := decodeEffective(sourceData.Effective)
		if err != nil {
			return database("talent_pool_membership_revision", id, fmt.Errorf("decode effective interval: %w", err))
		}
		out.MembershipID = values.EntityRef{Tenant: tenant, Kind: "talent_pool_membership", Id: membershipID.String()}
		out.Revision, err = values.NewSequenceRevision("crm", uint64(storedRevision))
		if err != nil {
			return database("talent_pool_membership_revision", id, err)
		}
		out.Pool = values.EntityRef{Tenant: tenant, Kind: "talent_pool", Id: poolID.String()}
		out.Subject = values.EntityRef{Tenant: tenant, Kind: values.Kind(sourceData.SubjectKind), Id: subject.String()}
		out.Role = crm.MembershipRole(role)
		out.Purpose = purpose
		out.Source = sourceData.Source
		out.Effective = interval
		out.Owner = values.EntityRef{Tenant: tenant, Kind: "owner", Id: owner.String()}
		out.RemovalPolicy = removalData.Policy
		_ = from
		_ = to
		return nil
	})
	return out, err
}

func (s *Store) ListMembershipVersions(ctx context.Context, tenant values.TenantId, id string) ([]crm.TalentPoolMembershipRevision, error) {
	versions := make([]uint64, 0)
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		membershipID, err := parseUUID(id)
		if err != nil {
			return invalid("talent_pool_membership_revision", id, err)
		}
		rows, err := tx.Query(ctx, `SELECT revision FROM talent_pool_membership_revision WHERE tenant_id=$1 AND membership_id=$2 ORDER BY revision`, tenantID, membershipID)
		if err != nil {
			return database("talent_pool_membership_revision", id, err)
		}
		defer rows.Close()
		for rows.Next() {
			var revision int64
			if err := rows.Scan(&revision); err != nil {
				return database("talent_pool_membership_revision", id, err)
			}
			versions = append(versions, uint64(revision))
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, notFound("talent_pool_membership_revision", id, crm.ErrMembershipNotFound)
	}
	out := make([]crm.TalentPoolMembershipRevision, 0, len(versions))
	for _, revision := range versions {
		membership, err := s.GetMembership(ctx, tenant, id, revision)
		if err != nil {
			return nil, err
		}
		out = append(out, membership)
	}
	return out, nil
}

func (s *Store) withTenant(ctx context.Context, tenant values.TenantId, fn func(dbport.Tx, uuid.UUID) error) error {
	if s == nil || s.db == nil {
		return database("transaction", "", errors.New("database is nil"))
	}
	tenantID, err := parseUUID(tenant.String())
	if err != nil || tenantID == uuid.Nil {
		return invalid("tenant", tenant.String(), errors.New("tenant must be a non-nil UUID"))
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return database("transaction", "", fmt.Errorf("begin: %w", err))
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return database("tenant", tenant.String(), err)
	}
	if err := fn(tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return database("transaction", "", fmt.Errorf("commit: %w", err))
	}
	return nil
}

type criteriaPayload struct {
	Text      string `json:"text"`
	Effective string `json:"effective"`
}

type membershipSourcePayload struct {
	Source      crm.SourceAttribution `json:"source"`
	SubjectKind string                `json:"subject_kind"`
	Effective   string                `json:"effective"`
}

type removalPayload struct {
	Policy crm.RemovalPolicy `json:"policy"`
}

// scopePayload avoids invoking values.EntityRef's strict text marshaler for
// Scope.Population, which is deliberately optional in the CRM domain model.
type scopePayload struct {
	Organization string `json:"organization"`
	Population   string `json:"population,omitempty"`
}

func marshalScope(scope crm.Scope) ([]byte, error) {
	organization, err := scope.Organization.MarshalText()
	if err != nil {
		return nil, err
	}
	population := ""
	if scope.Population.Validate() == nil {
		encoded, err := scope.Population.MarshalText()
		if err != nil {
			return nil, err
		}
		population = string(encoded)
	}
	return json.Marshal(scopePayload{Organization: string(organization), Population: population})
}

func unmarshalScope(raw []byte, scope *crm.Scope) error {
	var payload scopePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	var organization values.EntityRef
	if err := organization.UnmarshalText([]byte(payload.Organization)); err != nil {
		return err
	}
	scope.Organization = organization
	scope.Population = values.EntityRef{}
	if payload.Population != "" {
		if err := scope.Population.UnmarshalText([]byte(payload.Population)); err != nil {
			return err
		}
	}
	return nil
}

func latestVersion(ctx context.Context, ex dbport.Querier, table, key string, id, tenantID uuid.UUID) (uint64, error) {
	var latest *int64
	query := `SELECT max(revision) FROM ` + table + ` WHERE tenant_id=$1 AND ` + key + `=$2`
	if err := ex.QueryRow(ctx, query, tenantID, id).Scan(&latest); err != nil {
		return 0, database(table, id.String(), err)
	}
	if latest == nil {
		return 0, nil
	}
	return uint64(*latest), nil
}

func sequence(token values.RevisionToken) (uint64, error) {
	if !token.IsSpecified() {
		return 0, errors.New("revision is required")
	}
	value, ok := token.Sequence()
	if !ok || value == 0 || value > uint64(^uint64(0)>>1) {
		return 0, errors.New("revision must be a positive sequence")
	}
	return value, nil
}

func effectiveProjection(interval values.EffectiveInterval) (time.Time, *time.Time, error) {
	if err := interval.Validate(); err != nil {
		return time.Time{}, nil, err
	}
	if start, ok := interval.StartDate(); ok {
		from := time.Date(int(start.Year()), start.Month(), int(start.Day()), 0, 0, 0, 0, time.UTC)
		if end, hasEnd := interval.EndDate(); hasEnd {
			value := time.Date(int(end.Year()), end.Month(), int(end.Day()), 0, 0, 0, 0, time.UTC)
			return from, &value, nil
		}
		return from, nil, nil
	}
	start, ok := interval.StartInstant()
	if !ok {
		return time.Time{}, nil, errors.New("effective interval has no start")
	}
	from := start.Time()
	if end, hasEnd := interval.EndInstant(); hasEnd {
		value := end.Time()
		return from, &value, nil
	}
	return from, nil, nil
}

func encodeEffective(interval values.EffectiveInterval) string {
	return base64.StdEncoding.EncodeToString(interval.Canonical())
}

func decodeEffective(encoded string) (values.EffectiveInterval, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	if len(raw) < 3 || raw[0] != 0x05 {
		return values.EffectiveInterval{}, errors.New("invalid effective interval encoding")
	}
	kind := values.IntervalKind(raw[1])
	hasEnd := raw[2] == 1
	pos := 3
	readDate := func() (values.LocalDate, error) {
		if len(raw)-pos < 7 || raw[pos] != 0x02 {
			return values.LocalDate{}, errors.New("invalid local date encoding")
		}
		year := int32(binary.BigEndian.Uint32(raw[pos+1 : pos+5]))
		month, day := time.Month(raw[pos+5]), int(raw[pos+6])
		pos += 7
		return values.NewLocalDate(int(year), month, day)
	}
	readInstant := func() (values.Instant, error) {
		if len(raw)-pos < 13 || raw[pos] != 0x01 {
			return values.Instant{}, errors.New("invalid instant encoding")
		}
		sec := int64(binary.BigEndian.Uint64(raw[pos+1 : pos+9]))
		nsec := int32(binary.BigEndian.Uint32(raw[pos+9 : pos+13]))
		pos += 13
		return values.NewInstantFromUnix(sec, nsec)
	}
	readString := func() (string, error) {
		if len(raw)-pos < 4 {
			return "", errors.New("invalid interval string length")
		}
		n := int(binary.BigEndian.Uint32(raw[pos : pos+4]))
		pos += 4
		if n < 0 || len(raw)-pos < n {
			return "", errors.New("invalid interval string")
		}
		out := string(raw[pos : pos+n])
		pos += n
		return out, nil
	}
	var (
		localStart, localEnd     values.LocalDate
		instantStart, instantEnd values.Instant
	)
	switch kind {
	case values.IntervalKindLocalDate:
		localStart, err = readDate()
		if err != nil {
			return values.EffectiveInterval{}, err
		}
		if hasEnd {
			localEnd, err = readDate()
			if err != nil {
				return values.EffectiveInterval{}, err
			}
		}
	case values.IntervalKindInstant:
		instantStart, err = readInstant()
		if err != nil {
			return values.EffectiveInterval{}, err
		}
		if hasEnd {
			instantEnd, err = readInstant()
			if err != nil {
				return values.EffectiveInterval{}, err
			}
		}
	default:
		return values.EffectiveInterval{}, errors.New("invalid interval kind")
	}
	calendarRef, err := readString()
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	calendarVersion, err := readString()
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	zoneID, err := readString()
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	zoneVersion, err := readString()
	if err != nil || pos >= len(raw) {
		return values.EffectiveInterval{}, errors.New("invalid interval zone")
	}
	disambiguation := values.Disambiguation(raw[pos])
	var interval values.EffectiveInterval
	if kind == values.IntervalKindLocalDate {
		if hasEnd {
			interval, err = values.NewLocalDateInterval(localStart, localEnd, values.CalendarRef{Ref: calendarRef, Version: calendarVersion})
		} else {
			interval, err = values.NewOpenLocalDateInterval(localStart, values.CalendarRef{Ref: calendarRef, Version: calendarVersion})
		}
	} else if hasEnd {
		interval, err = values.NewInstantInterval(instantStart, instantEnd)
	} else {
		interval, err = values.NewOpenInstantInterval(instantStart)
	}
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	if zoneID != "" || zoneVersion != "" {
		interval, err = interval.WithZone(values.ZoneRef{ID: zoneID, TzdbVersion: zoneVersion}, disambiguation)
	}
	return interval, err
}

func parseUUID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New("identifier must be a non-nil UUID")
	}
	return id, nil
}

func parseUUIDOrNil(value string) uuid.UUID {
	id, _ := uuid.Parse(value)
	return id
}

func optionalExpected(values []uint64) uint64 {
	if len(values) == 1 {
		return values[0]
	}
	return 0
}

func (s *Store) SavePool(ctx context.Context, tenant values.TenantId, pool crm.TalentPoolRevision, expectedVersion ...uint64) error {
	return s.PutPool(ctx, tenant, pool, expectedVersion...)
}

func (s *Store) LoadPool(ctx context.Context, tenant values.TenantId, id string, revision uint64) (crm.TalentPoolRevision, error) {
	return s.GetPool(ctx, tenant, id, revision)
}

func (s *Store) SaveMembership(ctx context.Context, tenant values.TenantId, membership crm.TalentPoolMembershipRevision, expectedVersion ...uint64) error {
	return s.PutMembership(ctx, tenant, membership, expectedVersion...)
}

func (s *Store) LoadMembership(ctx context.Context, tenant values.TenantId, id string, revision uint64) (crm.TalentPoolMembershipRevision, error) {
	return s.GetMembership(ctx, tenant, id, revision)
}

func refusal(code crm.StoreErrorCode, detail string) error {
	return &crm.StoreError{Code: code, Detail: detail}
}

func invalid(table, id string, err error) error {
	return &crm.StoreError{Code: crm.StoreInvalidCode, Detail: fmt.Sprintf("%s %s: %v", table, id, err)}
}

func notFound(table, id string, cause error) error {
	return &crm.StoreError{Code: crm.StoreNotFoundCode, Detail: fmt.Sprintf("%s %s: %v", table, id, cause)}
}

func database(table, id string, err error) error {
	return &crm.StoreError{Code: crm.StoreDatabaseCode, Detail: fmt.Sprintf("%s %s: %v", table, id, err)}
}

func stale(expected, actual uint64, subject string) error {
	return &crm.StoreError{Code: crm.StoreStaleCASCode, Detail: fmt.Sprintf("%s expected %d, actual %d", subject, expected, actual), Expected: expected, Actual: actual}
}

func mapWriteError(table, id string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return refusal(crm.StoreDuplicateCode, fmt.Sprintf("%s %s already exists", table, id))
	}
	return database(table, id, err)
}
