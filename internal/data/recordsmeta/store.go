// Package recordsmeta is the tenant-scoped store for the records-management
// metadata migration 00032 creates (DB-015): record declarations, legal
// holds, the hold/record intersections that make a hold enforceable, and
// retention dispositions.
//
// The rule this package exists to hold, and which the schema enforces
// independently: a hold blocks disposition. [ExecuteDisposition] refuses to
// record an execution while any ACTIVE hold intersects the declaration, and
// the schema's retention_disposition_hold_blocks_execution constraint refuses
// the same row if a raw SQL path ever tried it. A correction never
// overwrites: it is a new row naming the one it corrects.
package recordsmeta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

var (
	// ErrNilTenant is returned when a row omits its tenant or its own id.
	ErrNilTenant = errors.New("recordsmeta: tenant or row id is nil")
	// ErrHoldBlocksDisposition is returned when a disposition would execute
	// while a hold is still active over its record (DB-015 RED).
	ErrHoldBlocksDisposition = errors.New("recordsmeta: an active legal hold blocks this disposition")
	// ErrMissingRetention is returned when a row omits the retention
	// schedule key or version its lifecycle is derived from.
	ErrMissingRetention = errors.New("recordsmeta: retention schedule key or version missing")
	// ErrMissingScope is returned when a hold omits its scope or scope
	// digest, or a declaration omits its subject.
	ErrMissingScope = errors.New("recordsmeta: scope missing")
	// ErrSelfCorrection is returned when a row claims to correct itself.
	ErrSelfCorrection = errors.New("recordsmeta: a row cannot correct itself")
	// ErrUnattributedRelease is returned when a hold releases without
	// naming who released it and why.
	ErrUnattributedRelease = errors.New("recordsmeta: hold release is not attributed")
	// ErrInvalidEnum is returned for a value outside its declared set.
	ErrInvalidEnum = errors.New("recordsmeta: value outside its declared set")
	// ErrInvalidInterval is returned for a reversed or empty interval.
	ErrInvalidInterval = errors.New("recordsmeta: time interval invalid")
)

// ErrDetail names the field behind one of the sentinels above.
type ErrDetail struct {
	Sentinel error
	Detail   string
}

func (e ErrDetail) Error() string { return fmt.Sprintf("%v: %s", e.Sentinel, e.Detail) }

func (e ErrDetail) Unwrap() error { return e.Sentinel }

func detail(sentinel error, format string, args ...any) error {
	return ErrDetail{Sentinel: sentinel, Detail: fmt.Sprintf(format, args...)}
}

// RecordsTables is the exact set of base tables migration 00032 creates for
// the records family, sorted.
var RecordsTables = []string{
	"hold_intersection",
	"legal_hold",
	"record_declaration",
	"retention_disposition",
}

func ensureTenant(ctx context.Context, tx dbport.Execer, tenantID uuid.UUID) error {
	if tenantID == uuid.Nil {
		return ErrNilTenant
	}
	return tenancy.WithTenant(ctx, tx, tenantID)
}

func object(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func oneOf(value string, allowed ...string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// record_declaration
// ---------------------------------------------------------------------------

// RecordDeclaration binds an entity or artifact to a record series and the
// exact retention schedule version that governs it.
type RecordDeclaration struct {
	TenantID                 uuid.UUID  `json:"tenant_id"`
	DeclarationID            uuid.UUID  `json:"declaration_id"`
	SubjectEntityRef         string     `json:"subject_entity_ref"`
	ArtifactRef              string     `json:"artifact_ref"`
	RecordSeries             string     `json:"record_series"`
	RecordClass              string     `json:"record_class"`
	OwnerRef                 string     `json:"owner_ref"`
	CustodianRef             string     `json:"custodian_ref"`
	CutoffTrigger            string     `json:"cutoff_trigger"`
	CutoffAt                 *time.Time `json:"cutoff_at"`
	RetentionScheduleKey     string     `json:"retention_schedule_key"`
	RetentionScheduleVersion int64      `json:"retention_schedule_version"`
	DispositionEligibleAt    *time.Time `json:"disposition_eligible_at"`
	CorrectsDeclarationID    *uuid.UUID `json:"corrects_declaration_id"`
	Status                   string     `json:"status"`
	CreatedAt                time.Time  `json:"created_at"`
}

func (d RecordDeclaration) Validate() error {
	if d.TenantID == uuid.Nil || d.DeclarationID == uuid.Nil {
		return ErrNilTenant
	}
	if d.SubjectEntityRef == "" || d.RecordSeries == "" {
		return detail(ErrMissingScope, "record_declaration subject_entity_ref/record_series")
	}
	if d.RetentionScheduleKey == "" || d.RetentionScheduleVersion < 1 {
		return detail(ErrMissingRetention, "record_declaration retention_schedule_key/version")
	}
	if !oneOf(d.CutoffTrigger, "CREATION", "TERMINATION", "EVENT", "FISCAL_YEAR_END", "SUPERSESSION") {
		return detail(ErrInvalidEnum, "record_declaration.cutoff_trigger=%q", d.CutoffTrigger)
	}
	if !oneOf(d.Status, "DECLARED", "ELIGIBLE", "HELD", "DISPOSED", "SUPERSEDED") {
		return detail(ErrInvalidEnum, "record_declaration.status=%q", d.Status)
	}
	if d.CorrectsDeclarationID != nil && *d.CorrectsDeclarationID == d.DeclarationID {
		return ErrSelfCorrection
	}
	if d.DispositionEligibleAt != nil {
		if d.CutoffAt == nil {
			return detail(ErrInvalidInterval, "record_declaration eligible without a cutoff")
		}
		if d.DispositionEligibleAt.Before(*d.CutoffAt) {
			return detail(ErrInvalidInterval, "record_declaration eligible before its cutoff")
		}
	}
	if d.Status == "ELIGIBLE" && d.DispositionEligibleAt == nil {
		return detail(ErrInvalidInterval, "record_declaration is ELIGIBLE with no eligibility date")
	}
	return nil
}

func InsertRecordDeclaration(ctx context.Context, tx dbport.Tx, d RecordDeclaration) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, d.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO record_declaration (tenant_id, declaration_id, subject_entity_ref, artifact_ref, record_series, record_class, owner_ref, custodian_ref, cutoff_trigger, cutoff_at, retention_schedule_key, retention_schedule_version, disposition_eligible_at, corrects_declaration_id, status, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		d.TenantID, d.DeclarationID, d.SubjectEntityRef, d.ArtifactRef, d.RecordSeries, d.RecordClass, d.OwnerRef, d.CustodianRef, d.CutoffTrigger, d.CutoffAt, d.RetentionScheduleKey, d.RetentionScheduleVersion, d.DispositionEligibleAt, d.CorrectsDeclarationID, d.Status, d.CreatedAt)
	return err
}

func LoadRecordDeclaration(ctx context.Context, q dbport.Querier, tenantID, declarationID uuid.UUID) (RecordDeclaration, error) {
	var d RecordDeclaration
	var cutoff, eligible *time.Time
	var corrects *uuid.UUID
	err := q.QueryRow(ctx, `SELECT tenant_id, declaration_id, subject_entity_ref, artifact_ref, record_series, record_class, owner_ref, custodian_ref, cutoff_trigger, cutoff_at, retention_schedule_key, retention_schedule_version, disposition_eligible_at, corrects_declaration_id, status, created_at FROM record_declaration WHERE tenant_id=$1 AND declaration_id=$2`, tenantID, declarationID).
		Scan(&d.TenantID, &d.DeclarationID, &d.SubjectEntityRef, &d.ArtifactRef, &d.RecordSeries, &d.RecordClass, &d.OwnerRef, &d.CustodianRef, &d.CutoffTrigger, &cutoff, &d.RetentionScheduleKey, &d.RetentionScheduleVersion, &eligible, &corrects, &d.Status, &d.CreatedAt)
	if err != nil {
		return RecordDeclaration{}, err
	}
	d.CutoffAt = cutoff
	d.DispositionEligibleAt = eligible
	d.CorrectsDeclarationID = corrects
	return d, nil
}

// ---------------------------------------------------------------------------
// legal_hold and hold_intersection
// ---------------------------------------------------------------------------

// LegalHold is one matter's preservation order over a scope of records.
type LegalHold struct {
	TenantID       uuid.UUID       `json:"tenant_id"`
	HoldID         uuid.UUID       `json:"hold_id"`
	MatterRef      string          `json:"matter_ref"`
	AuthorityRef   string          `json:"authority_ref"`
	HoldVersion    int64           `json:"hold_version"`
	Reason         string          `json:"reason"`
	ScopePredicate json.RawMessage `json:"scope_predicate"`
	ScopeDigest    string          `json:"scope_digest"`
	PlacedBy       string          `json:"placed_by"`
	PlacedAt       time.Time       `json:"placed_at"`
	ReleasedBy     *string         `json:"released_by"`
	ReleasedAt     *time.Time      `json:"released_at"`
	ReleaseReason  string          `json:"release_reason"`
	Status         string          `json:"status"`
}

func (h LegalHold) Validate() error {
	if h.TenantID == uuid.Nil || h.HoldID == uuid.Nil {
		return ErrNilTenant
	}
	if h.MatterRef == "" || h.ScopeDigest == "" {
		return detail(ErrMissingScope, "legal_hold matter_ref/scope_digest")
	}
	if h.HoldVersion < 1 {
		return detail(ErrInvalidEnum, "legal_hold.hold_version=%d", h.HoldVersion)
	}
	if h.Reason == "" {
		return detail(ErrMissingScope, "legal_hold.reason")
	}
	if !oneOf(h.Status, "ACTIVE", "PARTIALLY_RELEASED", "RELEASED") {
		return detail(ErrInvalidEnum, "legal_hold.status=%q", h.Status)
	}
	if h.Status == "RELEASED" && (h.ReleasedAt == nil || h.ReleasedBy == nil || h.ReleaseReason == "") {
		return ErrUnattributedRelease
	}
	if h.ReleasedAt != nil && h.ReleasedAt.Before(h.PlacedAt) {
		return detail(ErrInvalidInterval, "legal_hold.released_at precedes placed_at")
	}
	return nil
}

func InsertLegalHold(ctx context.Context, tx dbport.Tx, h LegalHold) error {
	if err := h.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, h.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO legal_hold (tenant_id, hold_id, matter_ref, authority_ref, hold_version, reason, scope_predicate, scope_digest, placed_by, placed_at, released_by, released_at, release_reason, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		h.TenantID, h.HoldID, h.MatterRef, h.AuthorityRef, h.HoldVersion, h.Reason, object(h.ScopePredicate), h.ScopeDigest, h.PlacedBy, h.PlacedAt, h.ReleasedBy, h.ReleasedAt, h.ReleaseReason, h.Status)
	return err
}

func LoadLegalHold(ctx context.Context, q dbport.Querier, tenantID, holdID uuid.UUID) (LegalHold, error) {
	var h LegalHold
	var predicate []byte
	var by *string
	var at *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, hold_id, matter_ref, authority_ref, hold_version, reason, scope_predicate, scope_digest, placed_by, placed_at, released_by, released_at, release_reason, status FROM legal_hold WHERE tenant_id=$1 AND hold_id=$2`, tenantID, holdID).
		Scan(&h.TenantID, &h.HoldID, &h.MatterRef, &h.AuthorityRef, &h.HoldVersion, &h.Reason, &predicate, &h.ScopeDigest, &h.PlacedBy, &h.PlacedAt, &by, &at, &h.ReleaseReason, &h.Status)
	if err != nil {
		return LegalHold{}, err
	}
	h.ScopePredicate = predicate
	h.ReleasedBy = by
	h.ReleasedAt = at
	return h, nil
}

// HoldIntersection is one hold's grip on one declared record, or on one
// specific material copy of it. It is what makes a hold enforceable against
// a specific disposition rather than a statement of intent.
//
// CopyID references privacymeta's discovery-inventory data_copy row; LinkID
// references recordsmeta's own lifecycle-tracked record_copy_link row (what
// [PropagateHold] grips, one per tracked copy). A row may name neither (the
// original DB-015 shape: one declaration-level grip), or exactly one of the
// two; migration 00284's partial unique indexes enforce that at most one
// ACTIVE intersection exists per hold/declaration for each of those three
// shapes.
type HoldIntersection struct {
	TenantID       uuid.UUID  `json:"tenant_id"`
	IntersectionID uuid.UUID  `json:"intersection_id"`
	HoldID         uuid.UUID  `json:"hold_id"`
	DeclarationID  uuid.UUID  `json:"declaration_id"`
	CopyID         *uuid.UUID `json:"copy_id"`
	LinkID         *uuid.UUID `json:"link_id"`
	MatchedReason  string     `json:"matched_reason"`
	MatchedAt      time.Time  `json:"matched_at"`
	ReleasedAt     *time.Time `json:"released_at"`
	State          string     `json:"state"`
}

func (i HoldIntersection) Validate() error {
	if i.TenantID == uuid.Nil || i.IntersectionID == uuid.Nil || i.HoldID == uuid.Nil || i.DeclarationID == uuid.Nil {
		return ErrNilTenant
	}
	if i.MatchedReason == "" {
		return detail(ErrMissingScope, "hold_intersection.matched_reason")
	}
	if !oneOf(i.State, "ACTIVE", "RELEASED") {
		return detail(ErrInvalidEnum, "hold_intersection.state=%q", i.State)
	}
	if (i.State == "RELEASED") != (i.ReleasedAt != nil) {
		return detail(ErrInvalidInterval, "hold_intersection state %s does not match its released_at", i.State)
	}
	if i.ReleasedAt != nil && i.ReleasedAt.Before(i.MatchedAt) {
		return detail(ErrInvalidInterval, "hold_intersection.released_at precedes matched_at")
	}
	return nil
}

func InsertHoldIntersection(ctx context.Context, tx dbport.Tx, i HoldIntersection) error {
	if err := i.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, i.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO hold_intersection (tenant_id, intersection_id, hold_id, declaration_id, copy_id, link_id, matched_reason, matched_at, released_at, state) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		i.TenantID, i.IntersectionID, i.HoldID, i.DeclarationID, i.CopyID, i.LinkID, i.MatchedReason, i.MatchedAt, i.ReleasedAt, i.State)
	return err
}

func LoadHoldIntersection(ctx context.Context, q dbport.Querier, tenantID, intersectionID uuid.UUID) (HoldIntersection, error) {
	var i HoldIntersection
	var copyID, linkID *uuid.UUID
	var released *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, intersection_id, hold_id, declaration_id, copy_id, link_id, matched_reason, matched_at, released_at, state FROM hold_intersection WHERE tenant_id=$1 AND intersection_id=$2`, tenantID, intersectionID).
		Scan(&i.TenantID, &i.IntersectionID, &i.HoldID, &i.DeclarationID, &copyID, &linkID, &i.MatchedReason, &i.MatchedAt, &released, &i.State)
	if err != nil {
		return HoldIntersection{}, err
	}
	i.CopyID = copyID
	i.LinkID = linkID
	i.ReleasedAt = released
	return i, nil
}

// ActiveHold returns the id of an ACTIVE hold gripping declarationID, if any.
func ActiveHold(ctx context.Context, q dbport.Querier, tenantID, declarationID uuid.UUID) (uuid.UUID, bool, error) {
	var holdID uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT i.hold_id
		FROM hold_intersection i
		JOIN legal_hold h ON h.tenant_id = i.tenant_id AND h.hold_id = i.hold_id
		WHERE i.tenant_id=$1 AND i.declaration_id=$2 AND i.state='ACTIVE' AND h.status <> 'RELEASED'
		ORDER BY i.matched_at
		LIMIT 1`, tenantID, declarationID).Scan(&holdID)
	if errors.Is(err, dbport.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	return holdID, true, nil
}

// ReleaseIntersection lifts one hold's grip on one record.
func ReleaseIntersection(ctx context.Context, tx dbport.Tx, tenantID, intersectionID uuid.UUID, at time.Time) error {
	if tenantID == uuid.Nil || intersectionID == uuid.Nil {
		return ErrNilTenant
	}
	if err := ensureTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	affected, err := tx.Exec(ctx, `UPDATE hold_intersection SET state='RELEASED', released_at=$3 WHERE tenant_id=$1 AND intersection_id=$2 AND state='ACTIVE'`, tenantID, intersectionID, at)
	if err != nil {
		return err
	}
	if affected == 0 {
		return dbport.ErrNoRows
	}
	return nil
}

// ---------------------------------------------------------------------------
// retention_disposition
// ---------------------------------------------------------------------------

// RetentionDisposition is one planned or executed end-of-life action over a
// declared record.
type RetentionDisposition struct {
	TenantID                 uuid.UUID  `json:"tenant_id"`
	DispositionID            uuid.UUID  `json:"disposition_id"`
	DeclarationID            uuid.UUID  `json:"declaration_id"`
	Action                   string     `json:"action"`
	RetentionScheduleKey     string     `json:"retention_schedule_key"`
	RetentionScheduleVersion int64      `json:"retention_schedule_version"`
	DueAt                    time.Time  `json:"due_at"`
	ExecutedAt               *time.Time `json:"executed_at"`
	Method                   string     `json:"method"`
	ReceiptDigest            *string    `json:"receipt_digest"`
	CertificateDigest        *string    `json:"certificate_digest"`
	VerificationResult       string     `json:"verification_result"`
	BlockingHoldID           *uuid.UUID `json:"blocking_hold_id"`
	CorrectsDispositionID    *uuid.UUID `json:"corrects_disposition_id"`
	Status                   string     `json:"status"`
	CreatedAt                time.Time  `json:"created_at"`
}

func (d RetentionDisposition) Validate() error {
	if d.TenantID == uuid.Nil || d.DispositionID == uuid.Nil || d.DeclarationID == uuid.Nil {
		return ErrNilTenant
	}
	if d.RetentionScheduleKey == "" || d.RetentionScheduleVersion < 1 {
		return detail(ErrMissingRetention, "retention_disposition retention_schedule_key/version")
	}
	if !oneOf(d.Action, "DESTROY", "ANONYMIZE", "RESTRICT", "TRANSFER") {
		return detail(ErrInvalidEnum, "retention_disposition.action=%q", d.Action)
	}
	if !oneOf(d.Status, "PLANNED", "BLOCKED", "EXECUTED", "VERIFIED", "CANCELLED") {
		return detail(ErrInvalidEnum, "retention_disposition.status=%q", d.Status)
	}
	if !oneOf(d.VerificationResult, "PENDING", "PASS", "FAIL", "PARTIAL", "UNKNOWN") {
		return detail(ErrInvalidEnum, "retention_disposition.verification_result=%q", d.VerificationResult)
	}
	if d.CorrectsDispositionID != nil && *d.CorrectsDispositionID == d.DispositionID {
		return ErrSelfCorrection
	}
	if d.BlockingHoldID != nil && d.ExecutedAt != nil {
		return ErrHoldBlocksDisposition
	}
	if d.Status == "BLOCKED" && d.BlockingHoldID == nil {
		return detail(ErrInvalidEnum, "retention_disposition BLOCKED without naming the hold")
	}
	if oneOf(d.Status, "EXECUTED", "VERIFIED") && (d.ExecutedAt == nil || d.ReceiptDigest == nil || d.Method == "") {
		return detail(ErrInvalidEnum, "retention_disposition %s without executed_at, receipt and method", d.Status)
	}
	if d.Status == "VERIFIED" && (d.CertificateDigest == nil || d.VerificationResult != "PASS") {
		return detail(ErrInvalidEnum, "retention_disposition VERIFIED without a passing certificate")
	}
	if d.ExecutedAt != nil && d.ExecutedAt.Before(d.DueAt) {
		return detail(ErrInvalidInterval, "retention_disposition executed before it was due")
	}
	return nil
}

func InsertRetentionDisposition(ctx context.Context, tx dbport.Tx, d RetentionDisposition) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, d.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO retention_disposition (tenant_id, disposition_id, declaration_id, action, retention_schedule_key, retention_schedule_version, due_at, executed_at, method, receipt_digest, certificate_digest, verification_result, blocking_hold_id, corrects_disposition_id, status, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		d.TenantID, d.DispositionID, d.DeclarationID, d.Action, d.RetentionScheduleKey, d.RetentionScheduleVersion, d.DueAt, d.ExecutedAt, d.Method, d.ReceiptDigest, d.CertificateDigest, d.VerificationResult, d.BlockingHoldID, d.CorrectsDispositionID, d.Status, d.CreatedAt)
	return err
}

func LoadRetentionDisposition(ctx context.Context, q dbport.Querier, tenantID, dispositionID uuid.UUID) (RetentionDisposition, error) {
	var d RetentionDisposition
	var executed *time.Time
	var receipt, certificate *string
	var hold, corrects *uuid.UUID
	err := q.QueryRow(ctx, `SELECT tenant_id, disposition_id, declaration_id, action, retention_schedule_key, retention_schedule_version, due_at, executed_at, method, receipt_digest, certificate_digest, verification_result, blocking_hold_id, corrects_disposition_id, status, created_at FROM retention_disposition WHERE tenant_id=$1 AND disposition_id=$2`, tenantID, dispositionID).
		Scan(&d.TenantID, &d.DispositionID, &d.DeclarationID, &d.Action, &d.RetentionScheduleKey, &d.RetentionScheduleVersion, &d.DueAt, &executed, &d.Method, &receipt, &certificate, &d.VerificationResult, &hold, &corrects, &d.Status, &d.CreatedAt)
	if err != nil {
		return RetentionDisposition{}, err
	}
	d.ExecutedAt = executed
	d.ReceiptDigest = receipt
	d.CertificateDigest = certificate
	d.BlockingHoldID = hold
	d.CorrectsDispositionID = corrects
	return d, nil
}

// ExecuteDisposition records an execution only when no active hold grips the
// record. When one does it records the block instead, naming the hold, and
// returns ErrHoldBlocksDisposition: the disposition is not lost, it is
// visibly waiting on the hold.
func ExecuteDisposition(ctx context.Context, tx dbport.Tx, tenantID, dispositionID uuid.UUID, method, receiptDigest string, at time.Time) error {
	if tenantID == uuid.Nil || dispositionID == uuid.Nil {
		return ErrNilTenant
	}
	if method == "" || receiptDigest == "" {
		return detail(ErrInvalidEnum, "ExecuteDisposition needs both a method and a receipt digest")
	}
	if err := ensureTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	var declarationID uuid.UUID
	var dueAt time.Time
	if err := tx.QueryRow(ctx, `SELECT declaration_id, due_at FROM retention_disposition WHERE tenant_id=$1 AND disposition_id=$2`, tenantID, dispositionID).Scan(&declarationID, &dueAt); err != nil {
		return err
	}
	if at.Before(dueAt) {
		return detail(ErrInvalidInterval, "disposition is due at %s, execution attempted at %s", dueAt, at)
	}
	holdID, held, err := ActiveHold(ctx, tx, tenantID, declarationID)
	if err != nil {
		return err
	}
	if held {
		if _, err := tx.Exec(ctx, `UPDATE retention_disposition SET status='BLOCKED', blocking_hold_id=$3 WHERE tenant_id=$1 AND disposition_id=$2`, tenantID, dispositionID, holdID); err != nil {
			return err
		}
		return detail(ErrHoldBlocksDisposition, "hold %s over declaration %s", holdID, declarationID)
	}
	affected, err := tx.Exec(ctx, `UPDATE retention_disposition SET status='EXECUTED', executed_at=$3, method=$4, receipt_digest=$5, blocking_hold_id=NULL WHERE tenant_id=$1 AND disposition_id=$2`, tenantID, dispositionID, at, method, receiptDigest)
	if err != nil {
		return err
	}
	if affected == 0 {
		return dbport.ErrNoRows
	}
	return nil
}
