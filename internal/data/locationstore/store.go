// Package locationstore persists the immutable location revision set declared
// by migrations/00049_location.sql. Tenant-scoped operations take the caller's
// already-scoped transaction; this keeps RLS context explicit and prevents a
// store from accidentally reusing one tenant's session for another.
package locationstore

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/location"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Executor is the driver-free database capability used by this adapter. A
// dbport.Tx is the intended argument for writes and for reads under RLS.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// ErrorCode identifies a refusal without requiring callers to parse a driver
// error string.
type ErrorCode string

const (
	CodeInvalid            ErrorCode = "LOCATION_INVALID"
	CodeDuplicateRevision  ErrorCode = "LOCATION_DUPLICATE_REVISION"
	CodeNotFound           ErrorCode = "LOCATION_NOT_FOUND"
	CodeVersionConflict    ErrorCode = "LOCATION_VERSION_CONFLICT"
	CodeReferenceNotFound  ErrorCode = "LOCATION_REFERENCE_NOT_FOUND"
	CodeIntegrityViolation ErrorCode = "LOCATION_INTEGRITY_VIOLATION"
)

var (
	ErrInvalid           = errors.New("locationstore: invalid row")
	ErrDuplicate         = errors.New("locationstore: duplicate revision")
	ErrNotFound          = errors.New("locationstore: not found")
	ErrVersionConflict   = errors.New("locationstore: version conflict")
	ErrReferenceNotFound = errors.New("locationstore: work location reference not found")
	ErrIntegrity         = errors.New("locationstore: integrity violation")
)

// Compatibility names make the fault contract explicit to callers that name
// the failure by the revision operation rather than by its generic category.
var (
	ErrDuplicateRevision = ErrDuplicate
	ErrStaleRevision     = ErrVersionConflict
)

// Store implements the migration-00049 persistence port. It has no mutable
// state; the caller owns transaction lifetime and tenant scoping.
type Store struct{}

// New returns a stateless location store. It is provided for composition roots
// that construct all data adapters uniformly; database handles remain method
// arguments so tenant scoping cannot be hidden.
func New() Store { return Store{} }

type revisionPayload struct {
	Address            location.Address `json:"address"`
	LocalityCandidates []string         `json:"locality_candidates"`
	TimezoneCandidates []string         `json:"timezone_candidates"`
	EffectiveCanonical string           `json:"effective_canonical"`
}

type worksitePayload struct {
	Address            location.Address    `json:"address"`
	LocalityCandidates []string            `json:"locality_candidates"`
	TimezoneCandidates []string            `json:"timezone_candidates"`
	EffectiveCanonical string              `json:"effective_canonical"`
	KnownAt            string              `json:"known_at"`
	SourceAuthority    string              `json:"source_authority"`
	Confidence         location.Confidence `json:"confidence"`
	ParentDigest       string              `json:"parent_digest"`
}

// PutWorkLocation appends one work-location revision. For revisions after the
// first, parent_revision and parent_digest are an optimistic compare-and-set
// against the current head. A duplicate identity is never overwritten.
func (s Store) PutWorkLocation(ctx context.Context, ex Executor, tenantID uuid.UUID, in location.WorkLocationRevision) (location.WorkLocationRevision, error) {
	if tenantID == uuid.Nil {
		return location.WorkLocationRevision{}, invalid("tenant_id", "tenant is required")
	}
	id := workLocationID(in)
	if err := in.Validate(); err != nil {
		return location.WorkLocationRevision{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	payload, err := marshalLocationPayload(in)
	if err != nil {
		return location.WorkLocationRevision{}, err
	}
	if err := s.checkRevisionHead(ctx, ex, "work_location_revision", tenantID, id, in.Revision, in.ParentRevision, in.ParentDigest); err != nil {
		return location.WorkLocationRevision{}, err
	}
	from, to := effectiveProjection(in.Effective)
	affected, err := ex.Exec(ctx, `
		INSERT INTO work_location_revision (
			tenant_id, row_id, location_id, revision, parent_revision, parent_digest,
			address, source_authority, confidence, effective_from, effective_to,
			known_at, canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11,$12,$13)
		ON CONFLICT DO NOTHING`,
		tenantID, uuid.New(), id, int64(in.Revision), nullableRevision(in.ParentRevision), nullableDigest(in.ParentDigest), string(payload), in.SourceAuthority, string(in.Confidence), from, to, in.KnownAt.Instant().Time(), storageDigest(in.CanonicalDigest))
	if err != nil {
		return location.WorkLocationRevision{}, fmt.Errorf("locationstore: insert work location %s/%d: %w", id, in.Revision, err)
	}
	if affected == 0 {
		return location.WorkLocationRevision{}, refusal(CodeDuplicateRevision, ErrDuplicate, "work location revision already exists")
	}
	return in, nil
}

// LoadWorkLocation reads one tenant-scoped work-location revision.
func (s Store) LoadWorkLocation(ctx context.Context, ex Executor, tenantID uuid.UUID, id string, revision uint64) (location.WorkLocationRevision, error) {
	if tenantID == uuid.Nil || id == "" || revision == 0 {
		return location.WorkLocationRevision{}, invalid("work_location", "tenant, id and positive revision are required")
	}
	var (
		rowID                                         uuid.UUID
		storedID, digest                              string
		parentRevision                                *int64
		rev                                           int64
		parentDigest, addressJSON, source, confidence *string
		from, to, knownAt                             *time.Time
	)
	err := ex.QueryRow(ctx, `
		SELECT row_id, location_id, revision, parent_revision, parent_digest,
			address::text, source_authority, confidence, effective_from, effective_to,
			known_at, canonical_digest
		FROM work_location_revision
		WHERE tenant_id=$1 AND location_id=$2 AND revision=$3`, tenantID, id, int64(revision)).Scan(
		&rowID, &storedID, &rev, &parentRevision, &parentDigest, &addressJSON, &source, &confidence, &from, &to, &knownAt, &digest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return location.WorkLocationRevision{}, refusal(CodeNotFound, ErrNotFound, "work location revision is absent")
		}
		return location.WorkLocationRevision{}, fmt.Errorf("locationstore: load work location %s/%d: %w", id, revision, err)
	}
	_ = rowID
	if addressJSON == nil || knownAt == nil {
		return location.WorkLocationRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "work location payload or known_at is null")
	}
	payload := revisionPayload{}
	if err := json.Unmarshal([]byte(*addressJSON), &payload); err != nil {
		return location.WorkLocationRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "work location payload is not valid JSON")
	}
	effective, err := decodeInterval(payload.EffectiveCanonical)
	if err != nil {
		return location.WorkLocationRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "work location effective interval cannot be decoded")
	}
	known, err := values.NewKnownAt(values.NewInstant((*knownAt).UTC()))
	if err != nil {
		return location.WorkLocationRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "work location known_at is invalid")
	}
	in := location.WorkLocationRevision{
		LocationID: storedID, Revision: uint64(rev), ParentRevision: uint64(int64Value(parentRevision)),
		ParentDigest: domainDigest(stringValue(parentDigest)), Address: payload.Address,
		LocalityCandidates: payload.LocalityCandidates, TimezoneCandidates: payload.TimezoneCandidates,
		SourceAuthority: stringValue(source), Confidence: location.Confidence(stringValue(confidence)), Effective: effective,
		KnownAt: known, CanonicalDigest: domainDigest(digest),
	}
	if in.ParentRevision == 0 {
		in.ParentDigest = ""
	}
	stored, err := location.NewWorkLocationRevision(in)
	if err != nil || stored.CanonicalDigest != domainDigest(digest) {
		return location.WorkLocationRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "work location digest does not match payload")
	}
	return stored, nil
}

// SaveWorkLocation is an alias for PutWorkLocation.
func (s Store) SaveWorkLocation(ctx context.Context, ex Executor, tenantID uuid.UUID, in location.WorkLocationRevision) (location.WorkLocationRevision, error) {
	return s.PutWorkLocation(ctx, ex, tenantID, in)
}

// GetWorkLocation is an alias for LoadWorkLocation.
func (s Store) GetWorkLocation(ctx context.Context, ex Executor, tenantID uuid.UUID, id string, revision uint64) (location.WorkLocationRevision, error) {
	return s.LoadWorkLocation(ctx, ex, tenantID, id, revision)
}

// ListWorkLocationRevisions returns a location's immutable history oldest first.
func (s Store) ListWorkLocationRevisions(ctx context.Context, ex Executor, tenantID uuid.UUID, id string) ([]location.WorkLocationRevision, error) {
	if tenantID == uuid.Nil || id == "" {
		return nil, invalid("work_location", "tenant and id are required")
	}
	rows, err := ex.Query(ctx, `SELECT revision FROM work_location_revision WHERE tenant_id=$1 AND location_id=$2 ORDER BY revision`, tenantID, id)
	if err != nil {
		return nil, fmt.Errorf("locationstore: list work location %s: %w", id, err)
	}
	defer rows.Close()
	var out []location.WorkLocationRevision
	for rows.Next() {
		var revision int64
		if err := rows.Scan(&revision); err != nil {
			return nil, fmt.Errorf("locationstore: scan work location revision: %w", err)
		}
		item, err := s.LoadWorkLocation(ctx, ex, tenantID, id, uint64(revision))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("locationstore: list work location %s: %w", id, err)
	}
	return out, nil
}

// PutWorksite appends one worksite revision after proving its referenced
// location exists in the same tenant and its parent is the current head.
func (s Store) PutWorksite(ctx context.Context, ex Executor, tenantID uuid.UUID, in location.WorksiteRevision) (location.WorksiteRevision, error) {
	if tenantID == uuid.Nil {
		return location.WorksiteRevision{}, invalid("tenant_id", "tenant is required")
	}
	id := worksiteID(in)
	if err := in.Validate(); err != nil {
		return location.WorksiteRevision{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	var exists bool
	if err := ex.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM work_location_revision WHERE tenant_id=$1 AND location_id=$2)`, tenantID, in.WorkLocationRef).Scan(&exists); err != nil {
		return location.WorksiteRevision{}, fmt.Errorf("locationstore: check work location reference %s: %w", in.WorkLocationRef, err)
	}
	if !exists {
		return location.WorksiteRevision{}, refusal(CodeReferenceNotFound, ErrReferenceNotFound, "worksite references no location in this tenant")
	}
	if err := s.checkRevisionHead(ctx, ex, "worksite_revision", tenantID, id, in.Revision, in.ParentRevision, in.ParentDigest); err != nil {
		return location.WorksiteRevision{}, err
	}
	payload, err := marshalWorksitePayload(in)
	if err != nil {
		return location.WorksiteRevision{}, err
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO worksite_revision (
			tenant_id, row_id, worksite_id, revision, parent_revision,
			work_location_ref, name, address, canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9)
		ON CONFLICT DO NOTHING`,
		tenantID, uuid.New(), id, int64(in.Revision), nullableRevision(in.ParentRevision), in.WorkLocationRef, in.Name, string(payload), storageDigest(in.CanonicalDigest))
	if err != nil {
		return location.WorksiteRevision{}, fmt.Errorf("locationstore: insert worksite %s/%d: %w", id, in.Revision, err)
	}
	if affected == 0 {
		return location.WorksiteRevision{}, refusal(CodeDuplicateRevision, ErrDuplicate, "worksite revision already exists")
	}
	return in, nil
}

// LoadWorksite reads one tenant-scoped worksite revision.
func (s Store) LoadWorksite(ctx context.Context, ex Executor, tenantID uuid.UUID, id string, revision uint64) (location.WorksiteRevision, error) {
	if tenantID == uuid.Nil || id == "" || revision == 0 {
		return location.WorksiteRevision{}, invalid("worksite", "tenant, id and positive revision are required")
	}
	var (
		rowID                                   uuid.UUID
		storedID, workLocationRef, name, digest string
		parentRevision                          *int64
		rev                                     int64
		addressJSON                             *string
	)
	err := ex.QueryRow(ctx, `
		SELECT row_id, worksite_id, revision, parent_revision, work_location_ref,
			name, address::text, canonical_digest
		FROM worksite_revision
		WHERE tenant_id=$1 AND worksite_id=$2 AND revision=$3`, tenantID, id, int64(revision)).Scan(
		&rowID, &storedID, &rev, &parentRevision, &workLocationRef, &name, &addressJSON, &digest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return location.WorksiteRevision{}, refusal(CodeNotFound, ErrNotFound, "worksite revision is absent")
		}
		return location.WorksiteRevision{}, fmt.Errorf("locationstore: load worksite %s/%d: %w", id, revision, err)
	}
	_ = rowID
	if addressJSON == nil {
		return location.WorksiteRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "worksite payload is null")
	}
	payload := worksitePayload{}
	if err := json.Unmarshal([]byte(*addressJSON), &payload); err != nil {
		return location.WorksiteRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "worksite payload is not valid JSON")
	}
	effective, err := decodeInterval(payload.EffectiveCanonical)
	if err != nil {
		return location.WorksiteRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "worksite effective interval cannot be decoded")
	}
	knownInstant, err := time.Parse(time.RFC3339Nano, payload.KnownAt)
	if err != nil {
		return location.WorksiteRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "worksite known_at is invalid")
	}
	known, err := values.NewKnownAt(values.NewInstant(knownInstant))
	if err != nil {
		return location.WorksiteRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "worksite known_at is invalid")
	}
	in := location.WorksiteRevision{
		WorksiteID: storedID, Revision: uint64(rev), ParentRevision: uint64(int64Value(parentRevision)), ParentDigest: payload.ParentDigest,
		WorkLocationRef: workLocationRef, Name: name, Address: payload.Address,
		LocalityCandidates: payload.LocalityCandidates, TimezoneCandidates: payload.TimezoneCandidates,
		SourceAuthority: payload.SourceAuthority, Confidence: payload.Confidence, Effective: effective,
		KnownAt: known, CanonicalDigest: domainDigest(digest),
	}
	if in.ParentRevision == 0 {
		in.ParentDigest = ""
	}
	stored, err := location.NewWorksiteRevision(in)
	if err != nil || stored.CanonicalDigest != domainDigest(digest) {
		return location.WorksiteRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "worksite digest does not match payload")
	}
	return stored, nil
}

// SaveWorksite is an alias for PutWorksite.
func (s Store) SaveWorksite(ctx context.Context, ex Executor, tenantID uuid.UUID, in location.WorksiteRevision) (location.WorksiteRevision, error) {
	return s.PutWorksite(ctx, ex, tenantID, in)
}

// GetWorksite is an alias for LoadWorksite.
func (s Store) GetWorksite(ctx context.Context, ex Executor, tenantID uuid.UUID, id string, revision uint64) (location.WorksiteRevision, error) {
	return s.LoadWorksite(ctx, ex, tenantID, id, revision)
}

// PutJurisdictionTable inserts one immutable platform reference row.
func (s Store) PutJurisdictionTable(ctx context.Context, ex Executor, in location.JurisdictionTable) (location.JurisdictionTable, error) {
	if err := in.Validate(); err != nil {
		return location.JurisdictionTable{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	rules, err := json.Marshal(in.Rules)
	if err != nil {
		return location.JurisdictionTable{}, fmt.Errorf("locationstore: marshal jurisdiction table: %w", err)
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO jurisdiction_table (row_id, table_id, version, rules, canonical_digest)
		VALUES ($1,$2,$3,$4::jsonb,$5)
		ON CONFLICT DO NOTHING`, uuid.New(), in.TableID, in.Version, string(rules), storageDigest(in.CanonicalDigest))
	if err != nil {
		return location.JurisdictionTable{}, fmt.Errorf("locationstore: insert jurisdiction table %s/%s: %w", in.TableID, in.Version, err)
	}
	if affected == 0 {
		return location.JurisdictionTable{}, refusal(CodeDuplicateRevision, ErrDuplicate, "jurisdiction table version already exists")
	}
	return in, nil
}

// LoadJurisdictionTable reads a platform reference row. It intentionally does
// not accept a tenant because jurisdiction_table is platform-scoped.
func (s Store) LoadJurisdictionTable(ctx context.Context, ex Executor, tableID, version string) (location.JurisdictionTable, error) {
	if tableID == "" || version == "" {
		return location.JurisdictionTable{}, invalid("jurisdiction_table", "table id and version are required")
	}
	var rowID uuid.UUID
	var rulesJSON, digest string
	if err := ex.QueryRow(ctx, `SELECT row_id, rules::text, canonical_digest FROM jurisdiction_table WHERE table_id=$1 AND version=$2`, tableID, version).Scan(&rowID, &rulesJSON, &digest); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return location.JurisdictionTable{}, refusal(CodeNotFound, ErrNotFound, "jurisdiction table version is absent")
		}
		return location.JurisdictionTable{}, fmt.Errorf("locationstore: load jurisdiction table %s/%s: %w", tableID, version, err)
	}
	_ = rowID
	var rules []location.JurisdictionRule
	if err := json.Unmarshal([]byte(rulesJSON), &rules); err != nil {
		return location.JurisdictionTable{}, refusal(CodeIntegrityViolation, ErrIntegrity, "jurisdiction rules are not valid JSON")
	}
	stored, err := location.NewJurisdictionTable(location.JurisdictionTable{TableID: tableID, Version: version, Rules: rules, CanonicalDigest: domainDigest(digest)})
	if err != nil || stored.CanonicalDigest != domainDigest(digest) {
		return location.JurisdictionTable{}, refusal(CodeIntegrityViolation, ErrIntegrity, "jurisdiction table digest does not match rules")
	}
	return stored, nil
}

// GetJurisdictionTable is an alias for LoadJurisdictionTable.
func (s Store) GetJurisdictionTable(ctx context.Context, ex Executor, tableID, version string) (location.JurisdictionTable, error) {
	return s.LoadJurisdictionTable(ctx, ex, tableID, version)
}

// WorksiteReader adapts the durable store to location.WorksiteReader when the
// caller has a tenant-scoped executor and a fixed tenant identity.
type WorksiteReader struct {
	Store    Store
	Executor Executor
	TenantID uuid.UUID
}

var _ location.WorksiteReader = WorksiteReader{}

func (r WorksiteReader) Worksite(ctx context.Context, id string, revision uint64) (location.WorksiteRevision, error) {
	return r.Store.LoadWorksite(ctx, r.Executor, r.TenantID, id, revision)
}

func invalid(field, detail string) error {
	return fmt.Errorf("%w: %s: %s", ErrInvalid, field, detail)
}

// Error is a classified store refusal. Code is stable for telemetry and
// Cause remains available to callers through errors.Is.
type Error struct {
	Code   ErrorCode
	Cause  error
	Detail string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s: %s", e.Code, e.Cause, e.Detail) }
func (e *Error) Unwrap() error { return e.Cause }

func refusal(code ErrorCode, cause error, detail string) error {
	return &Error{Code: code, Cause: cause, Detail: detail}
}

func workLocationID(in location.WorkLocationRevision) string {
	if in.WorkLocationID != "" {
		return in.WorkLocationID
	}
	return in.LocationID
}

func worksiteID(in location.WorksiteRevision) string {
	if in.WorksiteID != "" {
		return in.WorksiteID
	}
	return in.ID
}

func nullableRevision(revision uint64) any {
	if revision == 0 {
		return nil
	}
	return int64(revision)
}

func nullableDigest(digest string) any {
	if digest == "" {
		return nil
	}
	return storageDigest(digest)
}

// content_digest is the database's untagged hexadecimal representation of the
// domain's sha256:<hex> digest. Keep this conversion at the adapter boundary;
// callers and domain values retain the algorithm tag.
func storageDigest(digest string) string { return strings.TrimPrefix(digest, "sha256:") }

func domainDigest(digest string) string {
	if digest == "" || strings.HasPrefix(digest, "sha256:") {
		return digest
	}
	return "sha256:" + digest
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

// checkRevisionHead is deliberately a single read in the caller's write
// transaction. The row is locked before the insert, so two successors of one
// head cannot both pass the same parent check.
func (s Store) checkRevisionHead(ctx context.Context, ex Executor, table string, tenantID uuid.UUID, id string, revision, parentRevision uint64, parentDigest string) error {
	lockKey := tenantID.String() + ":" + table + ":" + id
	if _, err := ex.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return fmt.Errorf("locationstore: lock revision head: %w", err)
	}
	if revision == 1 {
		var exists bool
		if err := ex.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+table+` WHERE tenant_id=$1 AND `+idColumn(table)+`=$2 AND revision=$3)`, tenantID, id, int64(revision)).Scan(&exists); err != nil {
			return fmt.Errorf("locationstore: check duplicate revision: %w", err)
		}
		if exists {
			return refusal(CodeDuplicateRevision, ErrDuplicate, "revision identity already exists")
		}
		return nil
	}
	var (
		latestRevision int64
		latestDigest   string
	)
	err := ex.QueryRow(ctx, `SELECT revision, canonical_digest FROM `+table+` WHERE tenant_id=$1 AND `+idColumn(table)+`=$2 ORDER BY revision DESC LIMIT 1`, tenantID, id).Scan(&latestRevision, &latestDigest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return refusal(CodeVersionConflict, ErrVersionConflict, "successor has no current parent")
		}
		return fmt.Errorf("locationstore: read revision head: %w", err)
	}
	if uint64(latestRevision) == revision {
		return refusal(CodeDuplicateRevision, ErrDuplicate, "revision identity already exists")
	}
	if uint64(latestRevision) != parentRevision || latestDigest != storageDigest(parentDigest) {
		return refusal(CodeVersionConflict, ErrVersionConflict, "successor parent is stale")
	}
	return nil
}

func idColumn(table string) string {
	if table == "worksite_revision" {
		return "worksite_id"
	}
	return "location_id"
}

func marshalLocationPayload(in location.WorkLocationRevision) ([]byte, error) {
	b, err := json.Marshal(revisionPayload{Address: in.Address, LocalityCandidates: in.LocalityCandidates, TimezoneCandidates: in.TimezoneCandidates, EffectiveCanonical: base64.StdEncoding.EncodeToString(in.Effective.Canonical())})
	if err != nil {
		return nil, fmt.Errorf("locationstore: marshal work location payload: %w", err)
	}
	return b, nil
}

func marshalWorksitePayload(in location.WorksiteRevision) ([]byte, error) {
	b, err := json.Marshal(worksitePayload{Address: in.Address, LocalityCandidates: in.LocalityCandidates, TimezoneCandidates: in.TimezoneCandidates, EffectiveCanonical: base64.StdEncoding.EncodeToString(in.Effective.Canonical()), KnownAt: in.KnownAt.String(), SourceAuthority: in.SourceAuthority, Confidence: in.Confidence, ParentDigest: in.ParentDigest})
	if err != nil {
		return nil, fmt.Errorf("locationstore: marshal worksite payload: %w", err)
	}
	return b, nil
}

func effectiveProjection(iv values.EffectiveInterval) (time.Time, any) {
	if d, ok := iv.StartDate(); ok {
		from := time.Date(int(d.Year()), d.Month(), int(d.Day()), 0, 0, 0, 0, time.UTC)
		if end, hasEnd := iv.EndDate(); hasEnd {
			return from, time.Date(int(end.Year()), end.Month(), int(end.Day()), 0, 0, 0, 0, time.UTC)
		}
		return from, nil
	}
	if instant, ok := iv.StartInstant(); ok {
		from := instant.Time()
		if end, hasEnd := iv.EndInstant(); hasEnd {
			return from, end.Time()
		}
		return from, nil
	}
	return time.Time{}, nil
}

func decodeInterval(encoded string) (values.EffectiveInterval, error) {
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
