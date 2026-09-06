package intentcontrol

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// Executor is the minimal database capability this package needs. A
// [dbport.Tx] and a [dbport.Conn] both satisfy it.
//
// Every method takes it explicitly rather than holding a handle: migration
// 00024's tables are row-level-security protected, so the caller has to have
// scoped its transaction to a tenant (internal/data/tenancy.WithTenant) before
// any statement here runs, and a store that opened its own connection would
// make that impossible to guarantee.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// digestPattern is migration 00002's content_digest domain, restated in Go so
// that a malformed digest is rejected before it reaches the database and comes
// back as an unclassifiable constraint violation.
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// requireDigest rejects an absent or malformed content digest.
func requireDigest(field, value string) error {
	if value == "" {
		return invalid(field, "digest is absent")
	}
	if !digestPattern.MatchString(value) {
		return invalid(field, "digest is not 64 lowercase hex characters")
	}
	return nil
}

// requireText rejects an empty required string.
func requireText(field, value string) error {
	if value == "" {
		return invalid(field, "value is absent")
	}
	return nil
}

// requireInstant rejects a zero timestamp. A row stamped with the zero time is
// a row whose clock was never read.
func requireInstant(field string, value time.Time) error {
	if value.IsZero() {
		return invalid(field, "timestamp is unset")
	}
	return nil
}

// requireID rejects the nil UUID.
func requireID(field string, value uuid.UUID) error {
	if value == uuid.Nil {
		return invalid(field, "identifier is nil")
	}
	return nil
}

// InstanceContext is one intent_instance_context row: the origin, causation,
// correlation, family, mode and feature coverage an intent was created under.
// It is written once, in the same transaction as the intent_instance row it
// extends, and never changes.
type InstanceContext struct {
	TenantID uuid.UUID
	IntentID uuid.UUID

	IntentFamily  string
	ExecutionMode string

	FeatureRef            string
	FeatureCoverageDigest string

	// OriginTrust is TRUSTED, ASSERTED or UNVERIFIED. TRUSTED requires
	// SourceAuthoritySnapshotDigest; the other two forbid it. That is the
	// anti-spoofing rule, enforced here and again by the schema.
	OriginTrust                   string
	OriginKind                    string
	OriginEventRef                string
	SourceAuthoritySnapshotDigest string

	CorrelationID string
	// CausationID is empty exactly for a root intent.
	CausationID string
	TraceID     string

	InitiatorKind        string
	InitiatorPrincipalID string
	IdentityAssuranceRef string
	Purpose              string
	Classification       string
	RetentionClass       string

	ControlDigest     string
	RiskContextDigest string

	RecordedAt time.Time
}

// Origin trust levels.
const (
	OriginTrusted    = "TRUSTED"
	OriginAsserted   = "ASSERTED"
	OriginUnverified = "UNVERIFIED"
)

// Validate rejects a context row that could not be stored, including the two
// rules the schema also enforces: only an attested origin may be TRUSTED, and a
// non-trusted origin may not carry an attestation it did not earn.
func (c InstanceContext) Validate() error {
	for _, req := range []struct{ field, value string }{
		{"intent_family", c.IntentFamily},
		{"execution_mode", c.ExecutionMode},
		{"feature_ref", c.FeatureRef},
		{"origin_trust", c.OriginTrust},
		{"origin_kind", c.OriginKind},
		{"correlation_id", c.CorrelationID},
		{"trace_id", c.TraceID},
		{"initiator_kind", c.InitiatorKind},
		{"initiator_principal_id", c.InitiatorPrincipalID},
		{"identity_assurance_ref", c.IdentityAssuranceRef},
		{"purpose", c.Purpose},
		{"classification", c.Classification},
		{"retention_class", c.RetentionClass},
	} {
		if err := requireText(req.field, req.value); err != nil {
			return err
		}
	}
	if err := requireID("tenant_id", c.TenantID); err != nil {
		return err
	}
	if err := requireID("intent_id", c.IntentID); err != nil {
		return err
	}
	for _, req := range []struct{ field, value string }{
		{"feature_coverage_digest", c.FeatureCoverageDigest},
		{"control_digest", c.ControlDigest},
		{"risk_context_digest", c.RiskContextDigest},
	} {
		if err := requireDigest(req.field, req.value); err != nil {
			return err
		}
	}
	switch c.OriginTrust {
	case OriginTrusted:
		if err := requireDigest("source_authority_snapshot_digest", c.SourceAuthoritySnapshotDigest); err != nil {
			return fmt.Errorf("%w (a TRUSTED origin must name the authority that attests it)", err)
		}
	case OriginAsserted, OriginUnverified:
		if c.SourceAuthoritySnapshotDigest != "" {
			return invalid("source_authority_snapshot_digest",
				"only a TRUSTED origin carries a source authority snapshot")
		}
	default:
		return invalid("origin_trust", "value is not TRUSTED, ASSERTED or UNVERIFIED")
	}
	if c.CausationID == c.IntentID.String() && c.CausationID != "" {
		return invalid("causation_id", "an intent may not be its own cause")
	}
	return nil
}

// ContextStore reads and writes intent_instance_context. It holds no state.
type ContextStore struct{}

// Record inserts the context row for one intent.
//
// The insert is ON CONFLICT DO NOTHING ... RETURNING, so a second context row
// for the same intent produces no row and is reported as [ErrDuplicate] rather
// than as a raw constraint violation. That matters because the table is
// append-only: "already recorded" is the only other outcome an insert can have,
// and a caller must be able to tell it apart from a genuine failure.
func (s ContextStore) Record(ctx context.Context, ex Executor, in InstanceContext) error {
	if err := in.Validate(); err != nil {
		return err
	}
	row := ex.QueryRow(ctx, `
		INSERT INTO intent_instance_context (
			tenant_id, intent_id, intent_family, execution_mode,
			feature_ref, feature_coverage_digest,
			origin_trust, origin_kind, origin_event_ref, source_authority_snapshot_digest,
			correlation_id, causation_id, trace_id,
			initiator_kind, initiator_principal_id, identity_assurance_ref,
			purpose, classification, retention_class,
			control_digest, risk_context_digest)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''), NULLIF($10, ''),
			$11, NULLIF($12, ''), $13, $14, $15, $16, $17, $18, $19, $20, $21)
		ON CONFLICT DO NOTHING
		RETURNING intent_id`,
		in.TenantID, in.IntentID, in.IntentFamily, in.ExecutionMode,
		in.FeatureRef, in.FeatureCoverageDigest,
		in.OriginTrust, in.OriginKind, in.OriginEventRef, in.SourceAuthoritySnapshotDigest,
		in.CorrelationID, in.CausationID, in.TraceID,
		in.InitiatorKind, in.InitiatorPrincipalID, in.IdentityAssuranceRef,
		in.Purpose, in.Classification, in.RetentionClass,
		in.ControlDigest, in.RiskContextDigest)

	var stored uuid.UUID
	if err := row.Scan(&stored); err != nil {
		if isNoRows(err) {
			return fmt.Errorf("%w: intent_instance_context %s", ErrDuplicate, in.IntentID)
		}
		return fmt.Errorf("intentcontrol: record intent context %s: %w", in.IntentID, err)
	}
	return nil
}

// Load returns the context row for one intent.
func (s ContextStore) Load(ctx context.Context, ex Executor, tenantID, intentID uuid.UUID) (InstanceContext, error) {
	var (
		out            InstanceContext
		originEvent    *string
		sourceAuthKey  *string
		causation      *string
		recordedAtTime time.Time
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, intent_id, intent_family, execution_mode,
			feature_ref, feature_coverage_digest,
			origin_trust, origin_kind, origin_event_ref, source_authority_snapshot_digest,
			correlation_id, causation_id, trace_id,
			initiator_kind, initiator_principal_id, identity_assurance_ref,
			purpose, classification, retention_class,
			control_digest, risk_context_digest, recorded_at
		FROM intent_instance_context
		WHERE tenant_id = $1 AND intent_id = $2`, tenantID, intentID).Scan(
		&out.TenantID, &out.IntentID, &out.IntentFamily, &out.ExecutionMode,
		&out.FeatureRef, &out.FeatureCoverageDigest,
		&out.OriginTrust, &out.OriginKind, &originEvent, &sourceAuthKey,
		&out.CorrelationID, &causation, &out.TraceID,
		&out.InitiatorKind, &out.InitiatorPrincipalID, &out.IdentityAssuranceRef,
		&out.Purpose, &out.Classification, &out.RetentionClass,
		&out.ControlDigest, &out.RiskContextDigest, &recordedAtTime)
	if err != nil {
		if isNoRows(err) {
			return InstanceContext{}, fmt.Errorf("%w: intent_instance_context %s", ErrNotFound, intentID)
		}
		return InstanceContext{}, fmt.Errorf("intentcontrol: load intent context %s: %w", intentID, err)
	}
	out.OriginEventRef = derefString(originEvent)
	out.SourceAuthoritySnapshotDigest = derefString(sourceAuthKey)
	out.CausationID = derefString(causation)
	out.RecordedAt = recordedAtTime.UTC()
	return out, nil
}

// derefString reads a nullable text column as the empty string. The Go side
// uses "" for absent throughout this package, so a *string never escapes it.
func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ChangeRequest is one hcm_change_request row: the governed envelope an intent
// of family CHANGE_REQUEST is requested through. Unlike most of this package's
// rows it is live state, so it carries a compare-and-swap RequestVersion.
type ChangeRequest struct {
	TenantID        uuid.UUID
	ChangeRequestID uuid.UUID
	IntentID        uuid.UUID

	RequestKind string
	SubjectRef  string
	RequestedBy string
	RequestedAt time.Time

	EffectiveFrom time.Time
	// EffectiveTo is the zero time for an open-ended interval.
	EffectiveTo time.Time

	RequestStatus  string
	RequestVersion uint64

	IntentDigest  string
	ControlDigest string

	RecordedAt       time.Time
	LastTransitionAt time.Time
}

// Change request statuses, matching the schema's closed vocabulary.
const (
	RequestDraft       = "DRAFT"
	RequestPreflighted = "PREFLIGHTED"
	RequestSubmitted   = "SUBMITTED"
	RequestApproved    = "APPROVED"
	RequestRejected    = "REJECTED"
	RequestWithdrawn   = "WITHDRAWN"
	RequestCommitted   = "COMMITTED"
	RequestClosed      = "CLOSED"
)

// requestTransitions is the change request lifecycle as an explicit graph. It
// lives in Go rather than in a CHECK constraint because a CHECK sees only the
// new row and never the old one, so the schema can constrain which statuses
// exist but not which succession between two of them is legal.
var requestTransitions = map[string][]string{
	RequestDraft:       {RequestPreflighted, RequestWithdrawn},
	RequestPreflighted: {RequestSubmitted, RequestWithdrawn, RequestRejected},
	RequestSubmitted:   {RequestApproved, RequestRejected, RequestWithdrawn},
	RequestApproved:    {RequestCommitted, RequestWithdrawn},
	RequestCommitted:   {RequestClosed},
	RequestRejected:    {RequestClosed},
	RequestWithdrawn:   {RequestClosed},
	RequestClosed:      nil,
}

// Validate rejects a change request that could not be stored.
func (r ChangeRequest) Validate() error {
	for _, req := range []struct {
		field string
		value uuid.UUID
	}{
		{"tenant_id", r.TenantID},
		{"change_request_id", r.ChangeRequestID},
		{"intent_id", r.IntentID},
	} {
		if err := requireID(req.field, req.value); err != nil {
			return err
		}
	}
	for _, req := range []struct{ field, value string }{
		{"request_kind", r.RequestKind},
		{"subject_ref", r.SubjectRef},
		{"requested_by", r.RequestedBy},
	} {
		if err := requireText(req.field, req.value); err != nil {
			return err
		}
	}
	if _, known := requestTransitions[r.RequestStatus]; !known {
		return invalid("request_status", "status is not part of the change request lifecycle")
	}
	for _, req := range []struct{ field, value string }{
		{"intent_digest", r.IntentDigest},
		{"control_digest", r.ControlDigest},
	} {
		if err := requireDigest(req.field, req.value); err != nil {
			return err
		}
	}
	if err := requireInstant("requested_at", r.RequestedAt); err != nil {
		return err
	}
	if err := requireInstant("effective_from", r.EffectiveFrom); err != nil {
		return err
	}
	if !r.EffectiveTo.IsZero() && !r.EffectiveTo.After(r.EffectiveFrom) {
		return invalid("effective_to", "business validity must be a half-open interval [from, to)")
	}
	return nil
}

// ChangeRequestStore reads and writes hcm_change_request.
type ChangeRequestStore struct{}

// Open inserts a change request at version 1.
func (s ChangeRequestStore) Open(ctx context.Context, ex Executor, in ChangeRequest) (ChangeRequest, error) {
	if in.RequestStatus == "" {
		in.RequestStatus = RequestDraft
	}
	if in.LastTransitionAt.IsZero() {
		in.LastTransitionAt = in.RequestedAt
	}
	if err := in.Validate(); err != nil {
		return ChangeRequest{}, err
	}
	if in.LastTransitionAt.Before(in.RequestedAt) {
		return ChangeRequest{}, invalid("last_transition_at", "a transition may not precede the request")
	}
	row := ex.QueryRow(ctx, `
		INSERT INTO hcm_change_request (
			tenant_id, change_request_id, intent_id,
			request_kind, subject_ref, requested_by, requested_at,
			effective_from, effective_to,
			request_status, request_version,
			intent_digest, control_digest, last_transition_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 1, $11, $12, $13)
		ON CONFLICT DO NOTHING
		RETURNING `+changeRequestColumns,
		in.TenantID, in.ChangeRequestID, in.IntentID,
		in.RequestKind, in.SubjectRef, in.RequestedBy, in.RequestedAt.UTC(),
		in.EffectiveFrom.UTC(), nullableInstant(in.EffectiveTo),
		in.RequestStatus, in.IntentDigest, in.ControlDigest, in.LastTransitionAt.UTC())

	stored, err := scanChangeRequest(row)
	if err != nil {
		if isNoRows(err) {
			return ChangeRequest{}, fmt.Errorf("%w: hcm_change_request for intent %s", ErrDuplicate, in.IntentID)
		}
		return ChangeRequest{}, fmt.Errorf("intentcontrol: open change request %s: %w", in.ChangeRequestID, err)
	}
	return stored, nil
}

// Transition advances a change request's status under compare-and-swap.
//
// It refuses two different things for two different reasons. An illegal
// succession ([ErrIllegalTransition]) is refused before any statement runs,
// because the request that asked for it is wrong no matter what the database
// holds. A stale expectedVersion ([ErrVersionConflict]) is refused by the
// UPDATE's own WHERE clause, because only the database knows whether the caller
// lost a race -- and because the check and the write are then one statement,
// there is no window in which a second writer could slip between them.
func (s ChangeRequestStore) Transition(ctx context.Context, ex Executor, tenantID, changeRequestID uuid.UUID,
	expectedVersion uint64, next string, at time.Time,
) (ChangeRequest, error) {
	if err := requireInstant("last_transition_at", at); err != nil {
		return ChangeRequest{}, err
	}
	if expectedVersion == 0 {
		return ChangeRequest{}, invalid("expected_version", "a compare-and-swap needs the version it expects")
	}
	current, err := s.Load(ctx, ex, tenantID, changeRequestID)
	if err != nil {
		return ChangeRequest{}, err
	}
	if !allows(requestTransitions, current.RequestStatus, next) {
		return ChangeRequest{}, fmt.Errorf("%w: hcm_change_request %s: %s -> %s",
			ErrIllegalTransition, changeRequestID, current.RequestStatus, next)
	}

	row := ex.QueryRow(ctx, `
		UPDATE hcm_change_request
		SET request_status = $4,
			request_version = request_version + 1,
			last_transition_at = $5
		WHERE tenant_id = $1 AND change_request_id = $2 AND request_version = $3
		RETURNING `+changeRequestColumns,
		tenantID, changeRequestID, int64(expectedVersion), next, at.UTC())

	stored, err := scanChangeRequest(row)
	if err != nil {
		if isNoRows(err) {
			return ChangeRequest{}, fmt.Errorf("%w: hcm_change_request %s expected version %d",
				ErrVersionConflict, changeRequestID, expectedVersion)
		}
		return ChangeRequest{}, fmt.Errorf("intentcontrol: transition change request %s: %w", changeRequestID, err)
	}
	return stored, nil
}

// Load returns one change request.
func (s ChangeRequestStore) Load(ctx context.Context, ex Executor, tenantID, changeRequestID uuid.UUID) (ChangeRequest, error) {
	row := ex.QueryRow(ctx, `
		SELECT `+changeRequestColumns+`
		FROM hcm_change_request
		WHERE tenant_id = $1 AND change_request_id = $2`, tenantID, changeRequestID)
	stored, err := scanChangeRequest(row)
	if err != nil {
		if isNoRows(err) {
			return ChangeRequest{}, fmt.Errorf("%w: hcm_change_request %s", ErrNotFound, changeRequestID)
		}
		return ChangeRequest{}, fmt.Errorf("intentcontrol: load change request %s: %w", changeRequestID, err)
	}
	return stored, nil
}

const changeRequestColumns = `tenant_id, change_request_id, intent_id,
	request_kind, subject_ref, requested_by, requested_at,
	effective_from, effective_to,
	request_status, request_version,
	intent_digest, control_digest, recorded_at, last_transition_at`

func scanChangeRequest(src scanner) (ChangeRequest, error) {
	var (
		out         ChangeRequest
		effectiveTo *time.Time
		version     int64
	)
	if err := src.Scan(
		&out.TenantID, &out.ChangeRequestID, &out.IntentID,
		&out.RequestKind, &out.SubjectRef, &out.RequestedBy, &out.RequestedAt,
		&out.EffectiveFrom, &effectiveTo,
		&out.RequestStatus, &version,
		&out.IntentDigest, &out.ControlDigest, &out.RecordedAt, &out.LastTransitionAt,
	); err != nil {
		return ChangeRequest{}, err
	}
	out.RequestVersion = uint64(version)
	if effectiveTo != nil {
		out.EffectiveTo = effectiveTo.UTC()
	}
	out.RequestedAt = out.RequestedAt.UTC()
	out.EffectiveFrom = out.EffectiveFrom.UTC()
	out.RecordedAt = out.RecordedAt.UTC()
	out.LastTransitionAt = out.LastTransitionAt.UTC()
	return out, nil
}

// scanner is the one shape a row scanner needs, so a single-row read and a
// cursor row are scanned by the same code.
type scanner interface {
	Scan(dest ...any) error
}

// nullableInstant renders the zero time as SQL NULL, which is how this package
// spells "no upper bound" for a half-open interval.
func nullableInstant(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

// allows reports whether graph permits from -> to.
func allows(graph map[string][]string, from, to string) bool {
	for _, next := range graph[from] {
		if next == to {
			return true
		}
	}
	return false
}
