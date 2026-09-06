// Package opsmeta is the tenant-scoped store for the operational metadata
// migration 00032 creates (DB-015): incidents, backup runs and recovery runs.
//
// Two rules it holds, both also schema constraints:
//
//   - A recovery run cannot close on an unvalidated claim.
//     [CompleteRecoveryRun] requires the actual RPO and RTO it achieved and a
//     validation result of PASS or PARTIAL, and a RESTORE run must name the
//     backup it restored from.
//   - A completed backup is immutable for a stated window. A backup with no
//     immutability horizon is not a recovery source, and
//     [CompleteBackupRun] refuses to record one.
//
// DB-015's REFACTOR keeps operational telemetry out of business and assurance
// evidence: nothing here stores a metric series, a log line or a span. An
// incident carries the digest of its evidence, not the evidence.
package opsmeta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
)

var (
	// ErrNilTenant is returned when a row omits its tenant or its own id.
	ErrNilTenant = errors.New("opsmeta: tenant or row id is nil")
	// ErrMissingLineage is returned when a recovery run omits the backup it
	// restored from, or an incident omits its evidence digest (DB-015 RED:
	// recovery evidence loses lineage).
	ErrMissingLineage = errors.New("opsmeta: recovery or incident lineage missing")
	// ErrUnvalidatedCompletion is returned when a run claims completion
	// without the actuals and validation that make the claim checkable.
	ErrUnvalidatedCompletion = errors.New("opsmeta: run completed without validated actuals")
	// ErrMutableBackup is returned when a completed backup declares no
	// immutability horizon.
	ErrMutableBackup = errors.New("opsmeta: a completed backup must be immutable for a stated window")
	// ErrMissingScope is returned when a row omits its scope.
	ErrMissingScope = errors.New("opsmeta: scope missing")
	// ErrInvalidEnum is returned for a value outside its declared set.
	ErrInvalidEnum = errors.New("opsmeta: value outside its declared set")
	// ErrInvalidInterval is returned for a reversed or empty interval.
	ErrInvalidInterval = errors.New("opsmeta: time interval invalid")
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

// OperationsTables is the exact set of base tables migration 00032 creates for
// the operations family, sorted.
var OperationsTables = []string{
	"backup_run",
	"operational_incident",
	"recovery_run",
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
// operational_incident
// ---------------------------------------------------------------------------

// OperationalIncident is one declared operational failure and its lifecycle.
type OperationalIncident struct {
	TenantID       uuid.UUID       `json:"tenant_id"`
	IncidentID     uuid.UUID       `json:"incident_id"`
	IncidentKey    string          `json:"incident_key"`
	Severity       string          `json:"severity"`
	ImpactRevision int64           `json:"impact_revision"`
	Scope          json.RawMessage `json:"scope"`
	CorrelationKey string          `json:"correlation_key"`
	EvidenceDigest string          `json:"evidence_digest"`
	DeclaredAt     time.Time       `json:"declared_at"`
	ContainedAt    *time.Time      `json:"contained_at"`
	ResolvedAt     *time.Time      `json:"resolved_at"`
	ResidualRisk   string          `json:"residual_risk"`
	PostmortemRef  string          `json:"postmortem_ref"`
	Status         string          `json:"status"`
}

func (i OperationalIncident) Validate() error {
	if i.TenantID == uuid.Nil || i.IncidentID == uuid.Nil {
		return ErrNilTenant
	}
	if i.IncidentKey == "" || i.CorrelationKey == "" {
		return detail(ErrMissingScope, "operational_incident incident_key/correlation_key")
	}
	if i.EvidenceDigest == "" {
		return detail(ErrMissingLineage, "operational_incident.evidence_digest")
	}
	if i.ImpactRevision < 1 {
		return detail(ErrInvalidEnum, "operational_incident.impact_revision=%d", i.ImpactRevision)
	}
	if !oneOf(i.Severity, "SEV1", "SEV2", "SEV3", "SEV4", "SEV5") {
		return detail(ErrInvalidEnum, "operational_incident.severity=%q", i.Severity)
	}
	if !oneOf(i.Status, "OPEN", "CONTAINED", "RESOLVED", "CLOSED") {
		return detail(ErrInvalidEnum, "operational_incident.status=%q", i.Status)
	}
	if i.ContainedAt != nil && i.ContainedAt.Before(i.DeclaredAt) {
		return detail(ErrInvalidInterval, "operational_incident.contained_at precedes declared_at")
	}
	if i.ResolvedAt != nil && (i.ContainedAt == nil || i.ResolvedAt.Before(*i.ContainedAt)) {
		return detail(ErrInvalidInterval, "operational_incident resolved before containment")
	}
	if oneOf(i.Status, "RESOLVED", "CLOSED") && i.ResolvedAt == nil {
		return detail(ErrInvalidInterval, "operational_incident %s without a resolved_at", i.Status)
	}
	if i.Status == "CLOSED" && i.PostmortemRef == "" {
		return detail(ErrMissingLineage, "operational_incident closed without a postmortem reference")
	}
	return nil
}

func InsertOperationalIncident(ctx context.Context, tx dbport.Tx, i OperationalIncident) error {
	if err := i.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, i.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO operational_incident (tenant_id, incident_id, incident_key, severity, impact_revision, scope, correlation_key, evidence_digest, declared_at, contained_at, resolved_at, residual_risk, postmortem_ref, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		i.TenantID, i.IncidentID, i.IncidentKey, i.Severity, i.ImpactRevision, object(i.Scope), i.CorrelationKey, i.EvidenceDigest, i.DeclaredAt, i.ContainedAt, i.ResolvedAt, i.ResidualRisk, i.PostmortemRef, i.Status)
	return err
}

func LoadOperationalIncident(ctx context.Context, q dbport.Querier, tenantID, incidentID uuid.UUID) (OperationalIncident, error) {
	var i OperationalIncident
	var scope []byte
	var contained, resolved *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, incident_id, incident_key, severity, impact_revision, scope, correlation_key, evidence_digest, declared_at, contained_at, resolved_at, residual_risk, postmortem_ref, status FROM operational_incident WHERE tenant_id=$1 AND incident_id=$2`, tenantID, incidentID).
		Scan(&i.TenantID, &i.IncidentID, &i.IncidentKey, &i.Severity, &i.ImpactRevision, &scope, &i.CorrelationKey, &i.EvidenceDigest, &i.DeclaredAt, &contained, &resolved, &i.ResidualRisk, &i.PostmortemRef, &i.Status)
	if err != nil {
		return OperationalIncident{}, err
	}
	i.Scope = scope
	i.ContainedAt = contained
	i.ResolvedAt = resolved
	return i, nil
}

// ---------------------------------------------------------------------------
// backup_run
// ---------------------------------------------------------------------------

// BackupRun is one backup of one store at one point in time.
type BackupRun struct {
	TenantID           uuid.UUID  `json:"tenant_id"`
	RunID              uuid.UUID  `json:"run_id"`
	PolicyKey          string     `json:"policy_key"`
	Plane              string     `json:"plane"`
	StoreRef           string     `json:"store_ref"`
	BackupMode         string     `json:"backup_mode"`
	PointInTime        time.Time  `json:"point_in_time"`
	Watermark          string     `json:"watermark"`
	LocationRef        string     `json:"location_ref"`
	KeyVersion         int64      `json:"key_version"`
	ManifestDigest     string     `json:"manifest_digest"`
	StartedAt          time.Time  `json:"started_at"`
	CompletedAt        *time.Time `json:"completed_at"`
	ImmutableUntil     *time.Time `json:"immutable_until"`
	VerificationResult string     `json:"verification_result"`
	Status             string     `json:"status"`
}

func (b BackupRun) Validate() error {
	if b.TenantID == uuid.Nil || b.RunID == uuid.Nil {
		return ErrNilTenant
	}
	if b.PolicyKey == "" || b.StoreRef == "" || b.Watermark == "" {
		return detail(ErrMissingScope, "backup_run policy_key/store_ref/watermark")
	}
	if b.ManifestDigest == "" {
		return detail(ErrMissingLineage, "backup_run.manifest_digest")
	}
	if b.KeyVersion < 1 {
		return detail(ErrInvalidEnum, "backup_run.key_version=%d", b.KeyVersion)
	}
	if !oneOf(b.Plane, "DATA", "CONTROL", "OBJECT_STORE", "SEARCH") {
		return detail(ErrInvalidEnum, "backup_run.plane=%q", b.Plane)
	}
	if !oneOf(b.BackupMode, "FULL", "INCREMENTAL", "SNAPSHOT") {
		return detail(ErrInvalidEnum, "backup_run.backup_mode=%q", b.BackupMode)
	}
	if !oneOf(b.Status, "RUNNING", "COMPLETED", "FAILED", "EXPIRED") {
		return detail(ErrInvalidEnum, "backup_run.status=%q", b.Status)
	}
	if b.CompletedAt != nil && b.CompletedAt.Before(b.StartedAt) {
		return detail(ErrInvalidInterval, "backup_run.completed_at precedes started_at")
	}
	if b.Status != "RUNNING" && b.CompletedAt == nil {
		return detail(ErrInvalidInterval, "backup_run in terminal status %s without completed_at", b.Status)
	}
	if b.Status == "COMPLETED" && (b.ImmutableUntil == nil || !b.ImmutableUntil.After(*b.CompletedAt)) {
		return ErrMutableBackup
	}
	return nil
}

func InsertBackupRun(ctx context.Context, tx dbport.Tx, b BackupRun) error {
	if err := b.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, b.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO backup_run (tenant_id, run_id, policy_key, plane, store_ref, backup_mode, point_in_time, watermark, location_ref, key_version, manifest_digest, started_at, completed_at, immutable_until, verification_result, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		b.TenantID, b.RunID, b.PolicyKey, b.Plane, b.StoreRef, b.BackupMode, b.PointInTime, b.Watermark, b.LocationRef, b.KeyVersion, b.ManifestDigest, b.StartedAt, b.CompletedAt, b.ImmutableUntil, b.VerificationResult, b.Status)
	return err
}

func LoadBackupRun(ctx context.Context, q dbport.Querier, tenantID, runID uuid.UUID) (BackupRun, error) {
	var b BackupRun
	var completed, immutable *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, run_id, policy_key, plane, store_ref, backup_mode, point_in_time, watermark, location_ref, key_version, manifest_digest, started_at, completed_at, immutable_until, verification_result, status FROM backup_run WHERE tenant_id=$1 AND run_id=$2`, tenantID, runID).
		Scan(&b.TenantID, &b.RunID, &b.PolicyKey, &b.Plane, &b.StoreRef, &b.BackupMode, &b.PointInTime, &b.Watermark, &b.LocationRef, &b.KeyVersion, &b.ManifestDigest, &b.StartedAt, &completed, &immutable, &b.VerificationResult, &b.Status)
	if err != nil {
		return BackupRun{}, err
	}
	b.CompletedAt = completed
	b.ImmutableUntil = immutable
	return b, nil
}

// CompleteBackupRun closes a running backup. It refuses a completion with no
// immutability horizon: a mutable backup is not a recovery source.
func CompleteBackupRun(ctx context.Context, tx dbport.Tx, tenantID, runID uuid.UUID, completedAt, immutableUntil time.Time, verification string) error {
	if tenantID == uuid.Nil || runID == uuid.Nil {
		return ErrNilTenant
	}
	if !immutableUntil.After(completedAt) {
		return ErrMutableBackup
	}
	if !oneOf(verification, "PENDING", "PASS", "FAIL", "PARTIAL", "UNKNOWN") {
		return detail(ErrInvalidEnum, "backup verification=%q", verification)
	}
	if err := ensureTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	affected, err := tx.Exec(ctx, `UPDATE backup_run SET status='COMPLETED', completed_at=$3, immutable_until=$4, verification_result=$5 WHERE tenant_id=$1 AND run_id=$2 AND status='RUNNING'`, tenantID, runID, completedAt, immutableUntil, verification)
	if err != nil {
		return err
	}
	if affected == 0 {
		return dbport.ErrNoRows
	}
	return nil
}

// ---------------------------------------------------------------------------
// recovery_run
// ---------------------------------------------------------------------------

// RecoveryRun is one exercise or execution of a recovery plan.
type RecoveryRun struct {
	TenantID            uuid.UUID  `json:"tenant_id"`
	RunID               uuid.UUID  `json:"run_id"`
	ScenarioKey         string     `json:"scenario_key"`
	PlanRef             string     `json:"plan_ref"`
	BackupRunID         *uuid.UUID `json:"backup_run_id"`
	IncidentID          *uuid.UUID `json:"incident_id"`
	RecoveryMode        string     `json:"recovery_mode"`
	IsolatedEnvironment string     `json:"isolated_environment"`
	TargetRPOSeconds    int64      `json:"target_rpo_seconds"`
	TargetRTOSeconds    int64      `json:"target_rto_seconds"`
	ActualRPOSeconds    *int64     `json:"actual_rpo_seconds"`
	ActualRTOSeconds    *int64     `json:"actual_rto_seconds"`
	ValidationResult    string     `json:"validation_result"`
	StartedAt           time.Time  `json:"started_at"`
	CompletedAt         *time.Time `json:"completed_at"`
	Status              string     `json:"status"`
}

func (r RecoveryRun) Validate() error {
	if r.TenantID == uuid.Nil || r.RunID == uuid.Nil {
		return ErrNilTenant
	}
	if r.ScenarioKey == "" || r.PlanRef == "" {
		return detail(ErrMissingScope, "recovery_run scenario_key/plan_ref")
	}
	if !oneOf(r.RecoveryMode, "RESTORE", "REBUILD", "REPLAY") {
		return detail(ErrInvalidEnum, "recovery_run.recovery_mode=%q", r.RecoveryMode)
	}
	if !oneOf(r.Status, "RUNNING", "COMPLETED", "FAILED", "ABORTED") {
		return detail(ErrInvalidEnum, "recovery_run.status=%q", r.Status)
	}
	if !oneOf(r.ValidationResult, "PENDING", "PASS", "FAIL", "PARTIAL", "UNKNOWN") {
		return detail(ErrInvalidEnum, "recovery_run.validation_result=%q", r.ValidationResult)
	}
	if r.TargetRPOSeconds < 0 || r.TargetRTOSeconds < 0 {
		return detail(ErrInvalidEnum, "recovery_run objectives must be non-negative")
	}
	if r.RecoveryMode == "RESTORE" && r.BackupRunID == nil {
		return detail(ErrMissingLineage, "recovery_run RESTORE without a backup_run_id")
	}
	if r.CompletedAt != nil && r.CompletedAt.Before(r.StartedAt) {
		return detail(ErrInvalidInterval, "recovery_run.completed_at precedes started_at")
	}
	if r.Status == "COMPLETED" {
		if r.CompletedAt == nil || r.ActualRPOSeconds == nil || r.ActualRTOSeconds == nil {
			return ErrUnvalidatedCompletion
		}
		if !oneOf(r.ValidationResult, "PASS", "PARTIAL") {
			return ErrUnvalidatedCompletion
		}
	}
	return nil
}

func InsertRecoveryRun(ctx context.Context, tx dbport.Tx, r RecoveryRun) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, r.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO recovery_run (tenant_id, run_id, scenario_key, plan_ref, backup_run_id, incident_id, recovery_mode, isolated_environment, target_rpo_seconds, target_rto_seconds, actual_rpo_seconds, actual_rto_seconds, validation_result, started_at, completed_at, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		r.TenantID, r.RunID, r.ScenarioKey, r.PlanRef, r.BackupRunID, r.IncidentID, r.RecoveryMode, r.IsolatedEnvironment, r.TargetRPOSeconds, r.TargetRTOSeconds, r.ActualRPOSeconds, r.ActualRTOSeconds, r.ValidationResult, r.StartedAt, r.CompletedAt, r.Status)
	return err
}

func LoadRecoveryRun(ctx context.Context, q dbport.Querier, tenantID, runID uuid.UUID) (RecoveryRun, error) {
	var r RecoveryRun
	var backup, incident *uuid.UUID
	var rpo, rto *int64
	var completed *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, run_id, scenario_key, plan_ref, backup_run_id, incident_id, recovery_mode, isolated_environment, target_rpo_seconds, target_rto_seconds, actual_rpo_seconds, actual_rto_seconds, validation_result, started_at, completed_at, status FROM recovery_run WHERE tenant_id=$1 AND run_id=$2`, tenantID, runID).
		Scan(&r.TenantID, &r.RunID, &r.ScenarioKey, &r.PlanRef, &backup, &incident, &r.RecoveryMode, &r.IsolatedEnvironment, &r.TargetRPOSeconds, &r.TargetRTOSeconds, &rpo, &rto, &r.ValidationResult, &r.StartedAt, &completed, &r.Status)
	if err != nil {
		return RecoveryRun{}, err
	}
	r.BackupRunID = backup
	r.IncidentID = incident
	r.ActualRPOSeconds = rpo
	r.ActualRTOSeconds = rto
	r.CompletedAt = completed
	return r, nil
}

// CompleteRecoveryRun closes a running recovery with the actuals it achieved
// and the validation that checked them. It refuses an unvalidated completion.
func CompleteRecoveryRun(ctx context.Context, tx dbport.Tx, tenantID, runID uuid.UUID, completedAt time.Time, actualRPO, actualRTO int64, validation string) error {
	if tenantID == uuid.Nil || runID == uuid.Nil {
		return ErrNilTenant
	}
	if actualRPO < 0 || actualRTO < 0 {
		return detail(ErrInvalidEnum, "recovery actuals must be non-negative")
	}
	if !oneOf(validation, "PASS", "PARTIAL") {
		return detail(ErrUnvalidatedCompletion, "validation=%q", validation)
	}
	if err := ensureTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	affected, err := tx.Exec(ctx, `UPDATE recovery_run SET status='COMPLETED', completed_at=$3, actual_rpo_seconds=$4, actual_rto_seconds=$5, validation_result=$6 WHERE tenant_id=$1 AND run_id=$2 AND status='RUNNING'`, tenantID, runID, completedAt, actualRPO, actualRTO, validation)
	if err != nil {
		return err
	}
	if affected == 0 {
		return dbport.ErrNoRows
	}
	return nil
}
