// Package assurancemeta is the tenant-scoped store for the assurance
// metadata migration 00032 creates (DB-015): SLO definitions and their
// windowed observations, control evidence, and audit-package metadata.
//
// DB-015's REFACTOR draws the line this package sits on: operational
// telemetry stays separate from assurance evidence. An SLO observation
// records the aggregated good/total counts a decision is actually made from,
// not the underlying series; control evidence records the digest of what was
// collected, not the artifact. The bytes live in the governed object store.
//
// Two rules it holds beyond the schema's own constraints: an audit package
// that claims COMPLETE must actually carry evidence, and its signature is a
// reference into the governed store rather than a value.
package assurancemeta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

var (
	// ErrNilTenant is returned when a row omits its tenant or its own id.
	ErrNilTenant = errors.New("assurancemeta: tenant or row id is nil")
	// ErrMissingVersion is returned when a versioned row omits its version,
	// or an observation omits the SLO version it was measured against.
	ErrMissingVersion = errors.New("assurancemeta: version missing or not positive")
	// ErrMissingWindow is returned for a reversed, empty or absent
	// measurement window.
	ErrMissingWindow = errors.New("assurancemeta: measurement window invalid")
	// ErrMissingDigest is returned when a required digest is absent.
	ErrMissingDigest = errors.New("assurancemeta: content digest missing")
	// ErrMissingFreshness is returned when control evidence omits the
	// freshness horizon that says when it stops counting.
	ErrMissingFreshness = errors.New("assurancemeta: freshness deadline missing")
	// ErrEmptyPackage is returned when an audit package claims completeness
	// while carrying no evidence.
	ErrEmptyPackage = errors.New("assurancemeta: a COMPLETE audit package must carry evidence")
	// ErrRawSignature is returned when a package carries a signature value
	// instead of a governed reference.
	ErrRawSignature = errors.New("assurancemeta: raw signature rejected, expected a governed reference")
	// ErrImpossibleRatio is returned when an observation reports more good
	// events than total events, or an out-of-range error budget.
	ErrImpossibleRatio = errors.New("assurancemeta: observation ratio is impossible")
	// ErrInvalidEnum is returned for a value outside its declared set.
	ErrInvalidEnum = errors.New("assurancemeta: value outside its declared set")
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

// AssuranceTables is the exact set of base tables migration 00032 creates for
// the assurance family, sorted.
var AssuranceTables = []string{
	"audit_package",
	"control_evidence",
	"slo_definition",
	"slo_observation",
}

// signatureRefPattern mirrors the schema's
// audit_package_signature_is_reference constraint.
var signatureRefPattern = regexp.MustCompile(`^(artifact|kms|provider)://\S+$`)

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
// slo_definition and slo_observation
// ---------------------------------------------------------------------------

// SLODefinition is one versioned service-level objective.
type SLODefinition struct {
	TenantID      uuid.UUID  `json:"tenant_id"`
	SLOID         uuid.UUID  `json:"slo_id"`
	SLOKey        string     `json:"slo_key"`
	SLOVersion    int64      `json:"slo_version"`
	Service       string     `json:"service"`
	Capability    string     `json:"capability"`
	ObjectiveKind string     `json:"objective_kind"`
	TargetRatio   float64    `json:"target_ratio"`
	WindowSeconds int64      `json:"window_seconds"`
	OwnerRef      string     `json:"owner_ref"`
	EffectiveFrom time.Time  `json:"effective_from"`
	EffectiveTo   *time.Time `json:"effective_to"`
}

func (d SLODefinition) Validate() error {
	if d.TenantID == uuid.Nil || d.SLOID == uuid.Nil {
		return ErrNilTenant
	}
	if d.SLOVersion < 1 {
		return detail(ErrMissingVersion, "slo_definition.slo_version")
	}
	if !oneOf(d.ObjectiveKind, "AVAILABILITY", "LATENCY", "FRESHNESS", "RECONCILIATION", "RPO", "RTO") {
		return detail(ErrInvalidEnum, "slo_definition.objective_kind=%q", d.ObjectiveKind)
	}
	if d.TargetRatio <= 0 || d.TargetRatio > 1 {
		return detail(ErrImpossibleRatio, "slo_definition.target_ratio=%v", d.TargetRatio)
	}
	if d.WindowSeconds <= 0 {
		return detail(ErrMissingWindow, "slo_definition.window_seconds=%d", d.WindowSeconds)
	}
	if d.EffectiveTo != nil && !d.EffectiveTo.After(d.EffectiveFrom) {
		return detail(ErrMissingWindow, "slo_definition effective interval is not half-open")
	}
	return nil
}

func InsertSLODefinition(ctx context.Context, tx dbport.Tx, d SLODefinition) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, d.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO slo_definition (tenant_id, slo_id, slo_key, slo_version, service, capability, objective_kind, target_ratio, window_seconds, owner_ref, effective_from, effective_to) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		d.TenantID, d.SLOID, d.SLOKey, d.SLOVersion, d.Service, d.Capability, d.ObjectiveKind, d.TargetRatio, d.WindowSeconds, d.OwnerRef, d.EffectiveFrom, d.EffectiveTo)
	return err
}

func LoadSLODefinition(ctx context.Context, q dbport.Querier, tenantID, sloID uuid.UUID) (SLODefinition, error) {
	var d SLODefinition
	var to *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, slo_id, slo_key, slo_version, service, capability, objective_kind, target_ratio, window_seconds, owner_ref, effective_from, effective_to FROM slo_definition WHERE tenant_id=$1 AND slo_id=$2`, tenantID, sloID).
		Scan(&d.TenantID, &d.SLOID, &d.SLOKey, &d.SLOVersion, &d.Service, &d.Capability, &d.ObjectiveKind, &d.TargetRatio, &d.WindowSeconds, &d.OwnerRef, &d.EffectiveFrom, &to)
	if err != nil {
		return SLODefinition{}, err
	}
	d.EffectiveTo = to
	return d, nil
}

// SLOObservation is one window's aggregated measurement against an SLO
// version. It carries the counts a decision is made from, never the series.
type SLOObservation struct {
	TenantID             uuid.UUID `json:"tenant_id"`
	ObservationID        uuid.UUID `json:"observation_id"`
	SLOID                uuid.UUID `json:"slo_id"`
	SLOVersion           int64     `json:"slo_version"`
	WindowStart          time.Time `json:"window_start"`
	WindowEnd            time.Time `json:"window_end"`
	GoodEvents           int64     `json:"good_events"`
	TotalEvents          int64     `json:"total_events"`
	ErrorBudgetRemaining float64   `json:"error_budget_remaining"`
	SourceQuality        string    `json:"source_quality"`
	ObservedAt           time.Time `json:"observed_at"`
}

func (o SLOObservation) Validate() error {
	if o.TenantID == uuid.Nil || o.ObservationID == uuid.Nil || o.SLOID == uuid.Nil {
		return ErrNilTenant
	}
	if o.SLOVersion < 1 {
		return detail(ErrMissingVersion, "slo_observation.slo_version")
	}
	if !o.WindowEnd.After(o.WindowStart) {
		return detail(ErrMissingWindow, "slo_observation window is empty or reversed")
	}
	if o.GoodEvents < 0 || o.TotalEvents < 0 {
		return detail(ErrImpossibleRatio, "slo_observation counts must be non-negative")
	}
	if o.GoodEvents > o.TotalEvents {
		return detail(ErrImpossibleRatio, "%d good of %d total", o.GoodEvents, o.TotalEvents)
	}
	if o.ErrorBudgetRemaining < -1 || o.ErrorBudgetRemaining > 1 {
		return detail(ErrImpossibleRatio, "slo_observation.error_budget_remaining=%v", o.ErrorBudgetRemaining)
	}
	if !oneOf(o.SourceQuality, "COMPLETE", "PARTIAL", "DEGRADED", "UNKNOWN") {
		return detail(ErrInvalidEnum, "slo_observation.source_quality=%q", o.SourceQuality)
	}
	return nil
}

// InsertSLOObservation refuses an observation whose SLO version is not the
// version the named definition actually carries: an observation measured
// against a version that never existed is not evidence.
func InsertSLOObservation(ctx context.Context, tx dbport.Tx, o SLOObservation) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, o.TenantID); err != nil {
		return err
	}
	var version int64
	if err := tx.QueryRow(ctx, `SELECT slo_version FROM slo_definition WHERE tenant_id=$1 AND slo_id=$2`, o.TenantID, o.SLOID).Scan(&version); err != nil {
		return err
	}
	if version != o.SLOVersion {
		return detail(ErrMissingVersion, "observation names version %d, definition is version %d", o.SLOVersion, version)
	}
	_, err := tx.Exec(ctx, `INSERT INTO slo_observation (tenant_id, observation_id, slo_id, slo_version, window_start, window_end, good_events, total_events, error_budget_remaining, source_quality, observed_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		o.TenantID, o.ObservationID, o.SLOID, o.SLOVersion, o.WindowStart, o.WindowEnd, o.GoodEvents, o.TotalEvents, o.ErrorBudgetRemaining, o.SourceQuality, o.ObservedAt)
	return err
}

func LoadSLOObservation(ctx context.Context, q dbport.Querier, tenantID, observationID uuid.UUID) (SLOObservation, error) {
	var o SLOObservation
	err := q.QueryRow(ctx, `SELECT tenant_id, observation_id, slo_id, slo_version, window_start, window_end, good_events, total_events, error_budget_remaining, source_quality, observed_at FROM slo_observation WHERE tenant_id=$1 AND observation_id=$2`, tenantID, observationID).
		Scan(&o.TenantID, &o.ObservationID, &o.SLOID, &o.SLOVersion, &o.WindowStart, &o.WindowEnd, &o.GoodEvents, &o.TotalEvents, &o.ErrorBudgetRemaining, &o.SourceQuality, &o.ObservedAt)
	if err != nil {
		return SLOObservation{}, err
	}
	return o, nil
}

// ---------------------------------------------------------------------------
// control_evidence
// ---------------------------------------------------------------------------

// ControlEvidence is one collected demonstration that a control operated over
// a window.
type ControlEvidence struct {
	TenantID          uuid.UUID       `json:"tenant_id"`
	EvidenceID        uuid.UUID       `json:"evidence_id"`
	ControlKey        string          `json:"control_key"`
	ControlVersion    int64           `json:"control_version"`
	ImplementationRef string          `json:"implementation_ref"`
	Scope             json.RawMessage `json:"scope"`
	WindowStart       time.Time       `json:"window_start"`
	WindowEnd         time.Time       `json:"window_end"`
	SourceRef         string          `json:"source_ref"`
	ArtifactDigest    string          `json:"artifact_digest"`
	Collector         string          `json:"collector"`
	Completeness      string          `json:"completeness"`
	Result            string          `json:"result"`
	DeficiencyRef     string          `json:"deficiency_ref"`
	FreshnessDeadline time.Time       `json:"freshness_deadline"`
	RetentionClass    string          `json:"retention_class"`
	CollectedAt       time.Time       `json:"collected_at"`
}

func (e ControlEvidence) Validate() error {
	if e.TenantID == uuid.Nil || e.EvidenceID == uuid.Nil {
		return ErrNilTenant
	}
	if e.ControlVersion < 1 {
		return detail(ErrMissingVersion, "control_evidence.control_version")
	}
	if e.ArtifactDigest == "" {
		return detail(ErrMissingDigest, "control_evidence.artifact_digest")
	}
	if !e.WindowEnd.After(e.WindowStart) {
		return detail(ErrMissingWindow, "control_evidence window is empty or reversed")
	}
	if e.FreshnessDeadline.IsZero() || !e.FreshnessDeadline.After(e.WindowEnd) {
		return detail(ErrMissingFreshness, "control_evidence.freshness_deadline")
	}
	if !oneOf(e.Completeness, "COMPLETE", "PARTIAL", "UNKNOWN") {
		return detail(ErrInvalidEnum, "control_evidence.completeness=%q", e.Completeness)
	}
	if !oneOf(e.Result, "PASS", "FAIL", "PARTIAL", "UNKNOWN") {
		return detail(ErrInvalidEnum, "control_evidence.result=%q", e.Result)
	}
	if !oneOf(e.RetentionClass, "PERMANENT", "OPERATIONAL", "REBUILDABLE") {
		return detail(ErrInvalidEnum, "control_evidence.retention_class=%q", e.RetentionClass)
	}
	if oneOf(e.Result, "FAIL", "PARTIAL") && e.DeficiencyRef == "" {
		return detail(ErrInvalidEnum, "control_evidence result %s without a deficiency reference", e.Result)
	}
	return nil
}

func InsertControlEvidence(ctx context.Context, tx dbport.Tx, e ControlEvidence) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, e.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO control_evidence (tenant_id, evidence_id, control_key, control_version, implementation_ref, scope, window_start, window_end, source_ref, artifact_digest, collector, completeness, result, deficiency_ref, freshness_deadline, retention_class, collected_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		e.TenantID, e.EvidenceID, e.ControlKey, e.ControlVersion, e.ImplementationRef, object(e.Scope), e.WindowStart, e.WindowEnd, e.SourceRef, e.ArtifactDigest, e.Collector, e.Completeness, e.Result, e.DeficiencyRef, e.FreshnessDeadline, e.RetentionClass, e.CollectedAt)
	return err
}

func LoadControlEvidence(ctx context.Context, q dbport.Querier, tenantID, evidenceID uuid.UUID) (ControlEvidence, error) {
	var e ControlEvidence
	var scope []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, evidence_id, control_key, control_version, implementation_ref, scope, window_start, window_end, source_ref, artifact_digest, collector, completeness, result, deficiency_ref, freshness_deadline, retention_class, collected_at FROM control_evidence WHERE tenant_id=$1 AND evidence_id=$2`, tenantID, evidenceID).
		Scan(&e.TenantID, &e.EvidenceID, &e.ControlKey, &e.ControlVersion, &e.ImplementationRef, &scope, &e.WindowStart, &e.WindowEnd, &e.SourceRef, &e.ArtifactDigest, &e.Collector, &e.Completeness, &e.Result, &e.DeficiencyRef, &e.FreshnessDeadline, &e.RetentionClass, &e.CollectedAt)
	if err != nil {
		return ControlEvidence{}, err
	}
	e.Scope = scope
	return e, nil
}

// ---------------------------------------------------------------------------
// audit_package
// ---------------------------------------------------------------------------

// AuditPackage is one signed, expiring bundle of control evidence assembled
// for an auditor.
type AuditPackage struct {
	TenantID       uuid.UUID       `json:"tenant_id"`
	PackageID      uuid.UUID       `json:"package_id"`
	Purpose        string          `json:"purpose"`
	Scope          json.RawMessage `json:"scope"`
	EvidenceIDs    []uuid.UUID     `json:"evidence_ids"`
	ManifestDigest string          `json:"manifest_digest"`
	SignatureRef   string          `json:"signature_ref"`
	Completeness   string          `json:"completeness"`
	GeneratedAt    time.Time       `json:"generated_at"`
	DeliveredAt    *time.Time      `json:"delivered_at"`
	ExpiresAt      time.Time       `json:"expires_at"`
	Status         string          `json:"status"`
}

func (p AuditPackage) Validate() error {
	if p.TenantID == uuid.Nil || p.PackageID == uuid.Nil {
		return ErrNilTenant
	}
	if p.ManifestDigest == "" {
		return detail(ErrMissingDigest, "audit_package.manifest_digest")
	}
	if !signatureRefPattern.MatchString(p.SignatureRef) {
		return detail(ErrRawSignature, "audit_package.signature_ref=%q is not an artifact://, kms:// or provider:// reference", p.SignatureRef)
	}
	if !oneOf(p.Completeness, "COMPLETE", "PARTIAL", "UNKNOWN") {
		return detail(ErrInvalidEnum, "audit_package.completeness=%q", p.Completeness)
	}
	if !oneOf(p.Status, "GENERATED", "DELIVERED", "EXPIRED", "WITHDRAWN") {
		return detail(ErrInvalidEnum, "audit_package.status=%q", p.Status)
	}
	if p.Completeness == "COMPLETE" && len(p.EvidenceIDs) == 0 {
		return ErrEmptyPackage
	}
	if !p.ExpiresAt.After(p.GeneratedAt) {
		return detail(ErrMissingWindow, "audit_package expires at or before it was generated")
	}
	if p.DeliveredAt != nil && p.DeliveredAt.Before(p.GeneratedAt) {
		return detail(ErrMissingWindow, "audit_package.delivered_at precedes generated_at")
	}
	if p.Status == "DELIVERED" && p.DeliveredAt == nil {
		return detail(ErrMissingWindow, "audit_package DELIVERED without a delivered_at")
	}
	return nil
}

func InsertAuditPackage(ctx context.Context, tx dbport.Tx, p AuditPackage) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, p.TenantID); err != nil {
		return err
	}
	ids := p.EvidenceIDs
	if ids == nil {
		ids = []uuid.UUID{}
	}
	_, err := tx.Exec(ctx, `INSERT INTO audit_package (tenant_id, package_id, purpose, scope, evidence_ids, manifest_digest, signature_ref, completeness, generated_at, delivered_at, expires_at, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		p.TenantID, p.PackageID, p.Purpose, object(p.Scope), ids, p.ManifestDigest, p.SignatureRef, p.Completeness, p.GeneratedAt, p.DeliveredAt, p.ExpiresAt, p.Status)
	return err
}

func LoadAuditPackage(ctx context.Context, q dbport.Querier, tenantID, packageID uuid.UUID) (AuditPackage, error) {
	var p AuditPackage
	var scope []byte
	var ids []uuid.UUID
	var delivered *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, package_id, purpose, scope, evidence_ids, manifest_digest, signature_ref, completeness, generated_at, delivered_at, expires_at, status FROM audit_package WHERE tenant_id=$1 AND package_id=$2`, tenantID, packageID).
		Scan(&p.TenantID, &p.PackageID, &p.Purpose, &scope, &ids, &p.ManifestDigest, &p.SignatureRef, &p.Completeness, &p.GeneratedAt, &delivered, &p.ExpiresAt, &p.Status)
	if err != nil {
		return AuditPackage{}, err
	}
	p.Scope = scope
	p.EvidenceIDs = ids
	p.DeliveredAt = delivered
	return p, nil
}
