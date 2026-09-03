// DB-009: LegalEntity, OrganizationUnit, Job, JobPosition and
// PositionOccupancy.
package aggregates

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Entity kinds for DB-009's canonical_id encoding. JobPosition uses kind
// "job_position" to match the table name migrations/00012 uses (a bare
// "position" table name risks colliding with the SQL POSITION(... IN ...)
// function-like keyword, so the table -- and this kind -- are named
// job_position instead).
const (
	KindLegalEntity       values.Kind = "legal_entity"
	KindOrganizationUnit  values.Kind = "organization_unit"
	KindJob               values.Kind = "job"
	KindJobPosition       values.Kind = "job_position"
	KindPositionOccupancy values.Kind = "position_occupancy"
)

// OrganizationPort is DB-009's small read/append surface.
type OrganizationPort interface {
	PutLegalEntity(ctx context.Context, ex Executor, e LegalEntity) (uuid.UUID, error)
	PutOrganizationUnit(ctx context.Context, ex Executor, o OrganizationUnit) (uuid.UUID, error)
	PutJob(ctx context.Context, ex Executor, j Job) (uuid.UUID, error)
	PutJobPosition(ctx context.Context, ex Executor, p JobPosition) (uuid.UUID, error)
	PutPositionOccupancy(ctx context.Context, ex Executor, o PositionOccupancy) (uuid.UUID, error)

	CurrentJobPosition(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt time.Time) (JobPosition, error)
}

// OrganizationStore implements OrganizationPort over the tables
// migrations/00012_organization_aggregates.sql declares.
type OrganizationStore struct{}

var _ OrganizationPort = OrganizationStore{}

// ---------------------------------------------------------------------------
// LegalEntity
// ---------------------------------------------------------------------------

// LegalEntity is one bitemporal version of a legal entity.
type LegalEntity struct {
	Envelope
	RegisteredName string
	LifecycleState string
}

const legalEntityColumns = "row_id, tenant_id, entity_id, canonical_id, registered_name, lifecycle_state, " +
	"effective_from, effective_to, recorded_at, superseded_at, digest_algorithm, digest"

// NewLegalEntity builds a LegalEntity ready for PutLegalEntity.
func NewLegalEntity(tenant, entityID uuid.UUID, effectiveFrom time.Time, effectiveTo *time.Time, recordedAt time.Time,
	registeredName, lifecycleState string) (LegalEntity, error) {
	env, err := newEnvelope(KindLegalEntity, tenant, entityID, effectiveFrom, effectiveTo, recordedAt,
		registeredName, lifecycleState)
	if err != nil {
		return LegalEntity{}, err
	}
	return LegalEntity{Envelope: env, RegisteredName: registeredName, LifecycleState: lifecycleState}, nil
}

func (OrganizationStore) PutLegalEntity(ctx context.Context, ex Executor, e LegalEntity) (uuid.UUID, error) {
	return put(ctx, ex, "legal_entity", e.Envelope,
		[]string{"registered_name", "lifecycle_state"}, []any{e.RegisteredName, e.LifecycleState})
}

func scanLegalEntity(row pgx.Row) (LegalEntity, error) {
	var e LegalEntity
	err := row.Scan(&e.RowID, &e.Tenant, &e.EntityID, &e.CanonicalID, &e.RegisteredName, &e.LifecycleState,
		&e.EffectiveFrom, &e.EffectiveTo, &e.RecordedAt, &e.SupersededAt, &e.DigestAlgorithm, &e.Digest)
	return e, err
}

// CurrentLegalEntity returns the live LegalEntity as of businessAt.
func (OrganizationStore) CurrentLegalEntity(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt time.Time) (LegalEntity, error) {
	row := ex.QueryRow(ctx, currentAsOfSQL("legal_entity", legalEntityColumns), tenant, entityID, businessAt)
	e, err := scanLegalEntity(row)
	if err != nil {
		return LegalEntity{}, wrapNotFound(err, "legal_entity", entityID)
	}
	return e, nil
}

// ---------------------------------------------------------------------------
// OrganizationUnit
// ---------------------------------------------------------------------------

// OrganizationUnit is one bitemporal version of an org unit. The database
// trigger organization_unit_forbid_cycle refuses an insert whose parent chain
// would loop back to ParentOrganizationRef's own entity_id.
type OrganizationUnit struct {
	Envelope
	OrgType               string
	Code                  string
	Name                  string
	LegalEntityRef        *uuid.UUID
	ParentOrganizationRef *uuid.UUID
	LifecycleState        string
}

const organizationUnitColumns = "row_id, tenant_id, entity_id, canonical_id, org_type, code, name, legal_entity_ref, " +
	"parent_organization_ref, lifecycle_state, effective_from, effective_to, recorded_at, superseded_at, digest_algorithm, digest"

// NewOrganizationUnit builds an OrganizationUnit ready for PutOrganizationUnit.
func NewOrganizationUnit(tenant, entityID uuid.UUID, effectiveFrom time.Time, effectiveTo *time.Time, recordedAt time.Time,
	orgType, code, name string, legalEntityRef, parentOrganizationRef *uuid.UUID, lifecycleState string) (OrganizationUnit, error) {
	env, err := newEnvelope(KindOrganizationUnit, tenant, entityID, effectiveFrom, effectiveTo, recordedAt,
		orgType, code, name, optionalUUIDString(legalEntityRef), optionalUUIDString(parentOrganizationRef), lifecycleState)
	if err != nil {
		return OrganizationUnit{}, err
	}
	return OrganizationUnit{
		Envelope: env, OrgType: orgType, Code: code, Name: name,
		LegalEntityRef: legalEntityRef, ParentOrganizationRef: parentOrganizationRef, LifecycleState: lifecycleState,
	}, nil
}

func (OrganizationStore) PutOrganizationUnit(ctx context.Context, ex Executor, o OrganizationUnit) (uuid.UUID, error) {
	return put(ctx, ex, "organization_unit", o.Envelope,
		[]string{"org_type", "code", "name", "legal_entity_ref", "parent_organization_ref", "lifecycle_state"},
		[]any{o.OrgType, o.Code, o.Name, nullableUUID(o.LegalEntityRef), nullableUUID(o.ParentOrganizationRef), o.LifecycleState})
}

func scanOrganizationUnit(row pgx.Row) (OrganizationUnit, error) {
	var o OrganizationUnit
	var legalEntityRef, parentRef *uuid.UUID
	err := row.Scan(&o.RowID, &o.Tenant, &o.EntityID, &o.CanonicalID, &o.OrgType, &o.Code, &o.Name, &legalEntityRef,
		&parentRef, &o.LifecycleState, &o.EffectiveFrom, &o.EffectiveTo, &o.RecordedAt, &o.SupersededAt,
		&o.DigestAlgorithm, &o.Digest)
	o.LegalEntityRef, o.ParentOrganizationRef = legalEntityRef, parentRef
	return o, err
}

// CurrentOrganizationUnit returns the live OrganizationUnit as of businessAt.
func (OrganizationStore) CurrentOrganizationUnit(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt time.Time) (OrganizationUnit, error) {
	row := ex.QueryRow(ctx, currentAsOfSQL("organization_unit", organizationUnitColumns), tenant, entityID, businessAt)
	o, err := scanOrganizationUnit(row)
	if err != nil {
		return OrganizationUnit{}, wrapNotFound(err, "organization_unit", entityID)
	}
	return o, nil
}

// ---------------------------------------------------------------------------
// Job
// ---------------------------------------------------------------------------

// Job is one bitemporal version of a job.
type Job struct {
	Envelope
	Code         string
	Title        string
	JobFamily    string
	Grade        string
	ExemptStatus string
}

const jobColumns = "row_id, tenant_id, entity_id, canonical_id, code, title, job_family, grade, exempt_status, " +
	"effective_from, effective_to, recorded_at, superseded_at, digest_algorithm, digest"

// NewJob builds a Job ready for PutJob.
func NewJob(tenant, entityID uuid.UUID, effectiveFrom time.Time, effectiveTo *time.Time, recordedAt time.Time,
	code, title, jobFamily, grade, exemptStatus string) (Job, error) {
	env, err := newEnvelope(KindJob, tenant, entityID, effectiveFrom, effectiveTo, recordedAt,
		code, title, jobFamily, grade, exemptStatus)
	if err != nil {
		return Job{}, err
	}
	return Job{Envelope: env, Code: code, Title: title, JobFamily: jobFamily, Grade: grade, ExemptStatus: exemptStatus}, nil
}

func (OrganizationStore) PutJob(ctx context.Context, ex Executor, j Job) (uuid.UUID, error) {
	return put(ctx, ex, "job", j.Envelope,
		[]string{"code", "title", "job_family", "grade", "exempt_status"},
		[]any{j.Code, j.Title, nullableText(j.JobFamily), nullableText(j.Grade), j.ExemptStatus})
}

func scanJob(row pgx.Row) (Job, error) {
	var j Job
	var jobFamily, grade *string
	err := row.Scan(&j.RowID, &j.Tenant, &j.EntityID, &j.CanonicalID, &j.Code, &j.Title, &jobFamily, &grade,
		&j.ExemptStatus, &j.EffectiveFrom, &j.EffectiveTo, &j.RecordedAt, &j.SupersededAt, &j.DigestAlgorithm, &j.Digest)
	if jobFamily != nil {
		j.JobFamily = *jobFamily
	}
	if grade != nil {
		j.Grade = *grade
	}
	return j, err
}

// CurrentJob returns the live Job as of businessAt.
func (OrganizationStore) CurrentJob(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt time.Time) (Job, error) {
	row := ex.QueryRow(ctx, currentAsOfSQL("job", jobColumns), tenant, entityID, businessAt)
	j, err := scanJob(row)
	if err != nil {
		return Job{}, wrapNotFound(err, "job", entityID)
	}
	return j, nil
}

// ---------------------------------------------------------------------------
// JobPosition
// ---------------------------------------------------------------------------

// JobPosition is one bitemporal version of a position. CapacityFTE is exact
// decimal text.
type JobPosition struct {
	Envelope
	PositionCode    string
	JobRef          uuid.UUID
	OrganizationRef uuid.UUID
	LegalEntityRef  *uuid.UUID
	Location        string
	CapacityFTE     string
	LifecycleState  string
}

const jobPositionColumns = "row_id, tenant_id, entity_id, canonical_id, position_code, job_ref, organization_ref, " +
	"legal_entity_ref, location, capacity_fte::text, lifecycle_state, " +
	"effective_from, effective_to, recorded_at, superseded_at, digest_algorithm, digest"

// NewJobPosition builds a JobPosition ready for PutJobPosition. capacityFTE
// is exact decimal text, never a float.
func NewJobPosition(tenant, entityID, jobRef, organizationRef uuid.UUID, legalEntityRef *uuid.UUID,
	effectiveFrom time.Time, effectiveTo *time.Time, recordedAt time.Time,
	positionCode, location, capacityFTE, lifecycleState string) (JobPosition, error) {
	capacityFTE, err := normalizeDecimal(capacityFTE)
	if err != nil {
		return JobPosition{}, fmt.Errorf("aggregates: job_position capacity_fte: %w", err)
	}
	env, err := newEnvelope(KindJobPosition, tenant, entityID, effectiveFrom, effectiveTo, recordedAt,
		positionCode, jobRef.String(), organizationRef.String(), optionalUUIDString(legalEntityRef),
		location, capacityFTE, lifecycleState)
	if err != nil {
		return JobPosition{}, err
	}
	return JobPosition{
		Envelope: env, PositionCode: positionCode, JobRef: jobRef, OrganizationRef: organizationRef,
		LegalEntityRef: legalEntityRef, Location: location, CapacityFTE: capacityFTE, LifecycleState: lifecycleState,
	}, nil
}

func (OrganizationStore) PutJobPosition(ctx context.Context, ex Executor, p JobPosition) (uuid.UUID, error) {
	return put(ctx, ex, "job_position", p.Envelope,
		[]string{"position_code", "job_ref", "organization_ref", "legal_entity_ref", "location", "capacity_fte", "lifecycle_state"},
		[]any{p.PositionCode, p.JobRef, p.OrganizationRef, nullableUUID(p.LegalEntityRef), nullableText(p.Location),
			decimalParam(p.CapacityFTE), p.LifecycleState})
}

func scanJobPosition(row pgx.Row) (JobPosition, error) {
	var p JobPosition
	var legalEntityRef *uuid.UUID
	var location *string
	err := row.Scan(&p.RowID, &p.Tenant, &p.EntityID, &p.CanonicalID, &p.PositionCode, &p.JobRef, &p.OrganizationRef,
		&legalEntityRef, &location, &p.CapacityFTE, &p.LifecycleState,
		&p.EffectiveFrom, &p.EffectiveTo, &p.RecordedAt, &p.SupersededAt, &p.DigestAlgorithm, &p.Digest)
	p.LegalEntityRef = legalEntityRef
	if location != nil {
		p.Location = *location
	}
	return p, err
}

// CurrentJobPosition returns the live JobPosition as of businessAt.
func (OrganizationStore) CurrentJobPosition(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt time.Time) (JobPosition, error) {
	row := ex.QueryRow(ctx, currentAsOfSQL("job_position", jobPositionColumns), tenant, entityID, businessAt)
	p, err := scanJobPosition(row)
	if err != nil {
		return JobPosition{}, wrapNotFound(err, "job_position", entityID)
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// PositionOccupancy
// ---------------------------------------------------------------------------

// PositionOccupancy is one bitemporal version of an allocation against a
// position. The database trigger position_occupancy_forbid_overcommit
// refuses an insert that would push the position's live committed allocation
// past its live capacity_fte.
type PositionOccupancy struct {
	Envelope
	PositionRef   uuid.UUID
	AssignmentRef *uuid.UUID
	WorkerRef     *uuid.UUID
	AllocationFTE string
	PrimaryFlag   bool
}

const positionOccupancyColumns = "row_id, tenant_id, entity_id, canonical_id, position_ref, assignment_ref, worker_ref, " +
	"allocation_fte::text, primary_flag, effective_from, effective_to, recorded_at, superseded_at, digest_algorithm, digest"

// NewPositionOccupancy builds a PositionOccupancy ready for
// PutPositionOccupancy. allocationFTE is exact decimal text, never a float.
func NewPositionOccupancy(tenant, entityID, positionRef uuid.UUID, assignmentRef, workerRef *uuid.UUID,
	effectiveFrom time.Time, effectiveTo *time.Time, recordedAt time.Time, allocationFTE string, primaryFlag bool) (PositionOccupancy, error) {
	allocationFTE, err := normalizeDecimal(allocationFTE)
	if err != nil {
		return PositionOccupancy{}, fmt.Errorf("aggregates: position_occupancy allocation_fte: %w", err)
	}
	env, err := newEnvelope(KindPositionOccupancy, tenant, entityID, effectiveFrom, effectiveTo, recordedAt,
		positionRef.String(), optionalUUIDString(assignmentRef), optionalUUIDString(workerRef), allocationFTE, boolText(primaryFlag))
	if err != nil {
		return PositionOccupancy{}, err
	}
	return PositionOccupancy{
		Envelope: env, PositionRef: positionRef, AssignmentRef: assignmentRef, WorkerRef: workerRef,
		AllocationFTE: allocationFTE, PrimaryFlag: primaryFlag,
	}, nil
}

func (OrganizationStore) PutPositionOccupancy(ctx context.Context, ex Executor, o PositionOccupancy) (uuid.UUID, error) {
	return put(ctx, ex, "position_occupancy", o.Envelope,
		[]string{"position_ref", "assignment_ref", "worker_ref", "allocation_fte", "primary_flag"},
		[]any{o.PositionRef, nullableUUID(o.AssignmentRef), nullableUUID(o.WorkerRef), decimalParam(o.AllocationFTE), o.PrimaryFlag})
}

func scanPositionOccupancy(row pgx.Row) (PositionOccupancy, error) {
	var o PositionOccupancy
	var assignmentRef, workerRef *uuid.UUID
	err := row.Scan(&o.RowID, &o.Tenant, &o.EntityID, &o.CanonicalID, &o.PositionRef, &assignmentRef, &workerRef,
		&o.AllocationFTE, &o.PrimaryFlag, &o.EffectiveFrom, &o.EffectiveTo, &o.RecordedAt, &o.SupersededAt,
		&o.DigestAlgorithm, &o.Digest)
	o.AssignmentRef, o.WorkerRef = assignmentRef, workerRef
	return o, err
}

// CurrentPositionOccupancy returns the live PositionOccupancy as of businessAt.
func (OrganizationStore) CurrentPositionOccupancy(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt time.Time) (PositionOccupancy, error) {
	row := ex.QueryRow(ctx, currentAsOfSQL("position_occupancy", positionOccupancyColumns), tenant, entityID, businessAt)
	o, err := scanPositionOccupancy(row)
	if err != nil {
		return PositionOccupancy{}, wrapNotFound(err, "position_occupancy", entityID)
	}
	return o, nil
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
