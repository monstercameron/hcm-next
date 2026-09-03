// DB-008: Person, IdentityClaim, Worker, Employment and Assignment.
package aggregates

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// KindPerson etc. name the entity kind each table's canonical_id is built
// from. Kept exported so a caller building an EntityRef into this data can
// use the same kind constant this package used to write the row.
const (
	KindPerson        values.Kind = "person"
	KindIdentityClaim values.Kind = "identity_claim"
	KindWorker        values.Kind = "worker"
	KindEmployment    values.Kind = "employment"
	KindAssignment    values.Kind = "assignment"
)

// PeoplePort is the small read/append surface DB-008 exposes: put a new
// version of one of the five entities, or read the version that held at a
// given business time as of a given system time. A domain package consuming
// this adapter depends on PeoplePort rather than on pgx or on this package's
// table layout directly.
type PeoplePort interface {
	PutPerson(ctx context.Context, ex Executor, p Person) (uuid.UUID, error)
	PutIdentityClaim(ctx context.Context, ex Executor, c IdentityClaim) (uuid.UUID, error)
	PutWorker(ctx context.Context, ex Executor, w Worker) (uuid.UUID, error)
	PutEmployment(ctx context.Context, ex Executor, e Employment) (uuid.UUID, error)
	PutAssignment(ctx context.Context, ex Executor, a Assignment) (uuid.UUID, error)

	CurrentPerson(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt time.Time) (Person, error)
	KnownAsOfPerson(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt, knownAt time.Time) (Person, error)
}

// PeopleStore implements PeoplePort (and the four other tables' equivalent
// methods defined below) over the tables migrations/00011_people_aggregates.sql
// declares. It holds no state; every method takes its Executor explicitly.
type PeopleStore struct{}

var _ PeoplePort = PeopleStore{}

// ---------------------------------------------------------------------------
// Person
// ---------------------------------------------------------------------------

// Person is one bitemporal version of a party identity.
type Person struct {
	Envelope
	LifecycleState string
	LegalName      string
	PreferredName  string // "" means unset (column is nullable)
}

const personColumns = "row_id, tenant_id, entity_id, canonical_id, lifecycle_state, legal_name, preferred_name, " +
	"effective_from, effective_to, recorded_at, superseded_at, digest_algorithm, digest"

// NewPerson builds a Person ready for PutPerson, computing its canonical id
// and content digest.
func NewPerson(tenant, personID uuid.UUID, effectiveFrom time.Time, effectiveTo *time.Time, recordedAt time.Time,
	lifecycleState, legalName, preferredName string) (Person, error) {
	env, err := newEnvelope(KindPerson, tenant, personID, effectiveFrom, effectiveTo, recordedAt,
		lifecycleState, legalName, preferredName)
	if err != nil {
		return Person{}, err
	}
	return Person{Envelope: env, LifecycleState: lifecycleState, LegalName: legalName, PreferredName: preferredName}, nil
}

// PutPerson appends p, superseding whichever live Person row for the same
// entity would otherwise overlap its business-time range.
func (PeopleStore) PutPerson(ctx context.Context, ex Executor, p Person) (uuid.UUID, error) {
	return put(ctx, ex, "person", p.Envelope,
		[]string{"lifecycle_state", "legal_name", "preferred_name"},
		[]any{p.LifecycleState, p.LegalName, nullableText(p.PreferredName)})
}

func scanPerson(row pgx.Row) (Person, error) {
	var p Person
	var preferredName *string
	err := row.Scan(&p.RowID, &p.Tenant, &p.EntityID, &p.CanonicalID, &p.LifecycleState, &p.LegalName, &preferredName,
		&p.EffectiveFrom, &p.EffectiveTo, &p.RecordedAt, &p.SupersededAt, &p.DigestAlgorithm, &p.Digest)
	if preferredName != nil {
		p.PreferredName = *preferredName
	}
	return p, err
}

// CurrentPerson returns the Person live (not yet superseded) as of businessAt.
func (PeopleStore) CurrentPerson(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt time.Time) (Person, error) {
	row := ex.QueryRow(ctx, currentAsOfSQL("person", personColumns), tenant, entityID, businessAt)
	p, err := scanPerson(row)
	if err != nil {
		return Person{}, wrapNotFound(err, "person", entityID)
	}
	return p, nil
}

// KnownAsOfPerson returns the Person that held at businessAt, using only what
// had been recorded by knownAt -- a later Put's supersession is invisible to
// a query whose knownAt precedes it.
func (PeopleStore) KnownAsOfPerson(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt, knownAt time.Time) (Person, error) {
	row := ex.QueryRow(ctx, knownAsOfSQL("person", personColumns), tenant, entityID, businessAt, knownAt)
	p, err := scanPerson(row)
	if err != nil {
		return Person{}, wrapNotFound(err, "person", entityID)
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// IdentityClaim
// ---------------------------------------------------------------------------

// IdentityClaim is one bitemporal version of a typed, effective-dated claim
// linking an already-hashed identifier to a person.
type IdentityClaim struct {
	Envelope
	PersonRef           *uuid.UUID
	ClaimType           string
	Namespace           string
	NormalizedValueHash string
	Issuer              string
	Assurance           string
}

const identityClaimColumns = "row_id, tenant_id, entity_id, canonical_id, person_ref, claim_type, namespace, " +
	"normalized_value_hash, issuer, assurance, effective_from, effective_to, recorded_at, superseded_at, digest_algorithm, digest"

// NewIdentityClaim builds an IdentityClaim ready for PutIdentityClaim.
func NewIdentityClaim(tenant, claimID uuid.UUID, personRef *uuid.UUID, effectiveFrom time.Time, effectiveTo *time.Time, recordedAt time.Time,
	claimType, namespace, normalizedValueHash, issuer, assurance string) (IdentityClaim, error) {
	env, err := newEnvelope(KindIdentityClaim, tenant, claimID, effectiveFrom, effectiveTo, recordedAt,
		optionalUUIDString(personRef), claimType, namespace, normalizedValueHash, issuer, assurance)
	if err != nil {
		return IdentityClaim{}, err
	}
	return IdentityClaim{
		Envelope: env, PersonRef: personRef, ClaimType: claimType, Namespace: namespace,
		NormalizedValueHash: normalizedValueHash, Issuer: issuer, Assurance: assurance,
	}, nil
}

func (PeopleStore) PutIdentityClaim(ctx context.Context, ex Executor, c IdentityClaim) (uuid.UUID, error) {
	return put(ctx, ex, "identity_claim", c.Envelope,
		[]string{"person_ref", "claim_type", "namespace", "normalized_value_hash", "issuer", "assurance"},
		[]any{nullableUUID(c.PersonRef), c.ClaimType, c.Namespace, c.NormalizedValueHash, c.Issuer, c.Assurance})
}

func scanIdentityClaim(row pgx.Row) (IdentityClaim, error) {
	var c IdentityClaim
	var personRef *uuid.UUID
	err := row.Scan(&c.RowID, &c.Tenant, &c.EntityID, &c.CanonicalID, &personRef, &c.ClaimType, &c.Namespace,
		&c.NormalizedValueHash, &c.Issuer, &c.Assurance,
		&c.EffectiveFrom, &c.EffectiveTo, &c.RecordedAt, &c.SupersededAt, &c.DigestAlgorithm, &c.Digest)
	c.PersonRef = personRef
	return c, err
}

// CurrentIdentityClaim returns the live claim as of businessAt.
func (PeopleStore) CurrentIdentityClaim(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt time.Time) (IdentityClaim, error) {
	row := ex.QueryRow(ctx, currentAsOfSQL("identity_claim", identityClaimColumns), tenant, entityID, businessAt)
	c, err := scanIdentityClaim(row)
	if err != nil {
		return IdentityClaim{}, wrapNotFound(err, "identity_claim", entityID)
	}
	return c, nil
}

// ---------------------------------------------------------------------------
// Worker
// ---------------------------------------------------------------------------

// Worker is one bitemporal version of the employment-side role over a Person.
type Worker struct {
	Envelope
	PersonRef       uuid.UUID
	WorkerNumber    string
	WorkerType      string
	LifecycleStatus string
}

const workerColumns = "row_id, tenant_id, entity_id, canonical_id, person_ref, worker_number, worker_type, " +
	"lifecycle_status, effective_from, effective_to, recorded_at, superseded_at, digest_algorithm, digest"

// NewWorker builds a Worker ready for PutWorker.
func NewWorker(tenant, workerID, personRef uuid.UUID, effectiveFrom time.Time, effectiveTo *time.Time, recordedAt time.Time,
	workerNumber, workerType, lifecycleStatus string) (Worker, error) {
	env, err := newEnvelope(KindWorker, tenant, workerID, effectiveFrom, effectiveTo, recordedAt,
		personRef.String(), workerNumber, workerType, lifecycleStatus)
	if err != nil {
		return Worker{}, err
	}
	return Worker{
		Envelope: env, PersonRef: personRef, WorkerNumber: workerNumber,
		WorkerType: workerType, LifecycleStatus: lifecycleStatus,
	}, nil
}

func (PeopleStore) PutWorker(ctx context.Context, ex Executor, w Worker) (uuid.UUID, error) {
	return put(ctx, ex, "worker", w.Envelope,
		[]string{"person_ref", "worker_number", "worker_type", "lifecycle_status"},
		[]any{w.PersonRef, nullableText(w.WorkerNumber), w.WorkerType, w.LifecycleStatus})
}

func scanWorker(row pgx.Row) (Worker, error) {
	var w Worker
	var workerNumber *string
	err := row.Scan(&w.RowID, &w.Tenant, &w.EntityID, &w.CanonicalID, &w.PersonRef, &workerNumber, &w.WorkerType,
		&w.LifecycleStatus, &w.EffectiveFrom, &w.EffectiveTo, &w.RecordedAt, &w.SupersededAt, &w.DigestAlgorithm, &w.Digest)
	if workerNumber != nil {
		w.WorkerNumber = *workerNumber
	}
	return w, err
}

// CurrentWorker returns the live Worker as of businessAt.
func (PeopleStore) CurrentWorker(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt time.Time) (Worker, error) {
	row := ex.QueryRow(ctx, currentAsOfSQL("worker", workerColumns), tenant, entityID, businessAt)
	w, err := scanWorker(row)
	if err != nil {
		return Worker{}, wrapNotFound(err, "worker", entityID)
	}
	return w, nil
}

// KnownAsOfWorker returns the Worker that held at businessAt, using only what
// had been recorded by knownAt.
func (PeopleStore) KnownAsOfWorker(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt, knownAt time.Time) (Worker, error) {
	row := ex.QueryRow(ctx, knownAsOfSQL("worker", workerColumns), tenant, entityID, businessAt, knownAt)
	w, err := scanWorker(row)
	if err != nil {
		return Worker{}, wrapNotFound(err, "worker", entityID)
	}
	return w, nil
}

// ---------------------------------------------------------------------------
// Employment
// ---------------------------------------------------------------------------

// Employment is one bitemporal version of a worker's employment with a legal
// entity, folding EmploymentStatusRevision into the same row.
type Employment struct {
	Envelope
	WorkerRef        uuid.UUID
	LegalEntityRef   uuid.UUID
	EmploymentType   string
	EmploymentStatus string
	HireDate         *time.Time
}

const employmentColumns = "row_id, tenant_id, entity_id, canonical_id, worker_ref, legal_entity_ref, employment_type, " +
	"employment_status, hire_date, effective_from, effective_to, recorded_at, superseded_at, digest_algorithm, digest"

// NewEmployment builds an Employment ready for PutEmployment.
func NewEmployment(tenant, employmentID, workerRef, legalEntityRef uuid.UUID, effectiveFrom time.Time, effectiveTo *time.Time,
	recordedAt time.Time, employmentType, employmentStatus string, hireDate *time.Time) (Employment, error) {
	env, err := newEnvelope(KindEmployment, tenant, employmentID, effectiveFrom, effectiveTo, recordedAt,
		workerRef.String(), legalEntityRef.String(), employmentType, employmentStatus, formatOptionalTime(hireDate))
	if err != nil {
		return Employment{}, err
	}
	return Employment{
		Envelope: env, WorkerRef: workerRef, LegalEntityRef: legalEntityRef,
		EmploymentType: employmentType, EmploymentStatus: employmentStatus, HireDate: normalizeOptionalTime(hireDate),
	}, nil
}

func (PeopleStore) PutEmployment(ctx context.Context, ex Executor, e Employment) (uuid.UUID, error) {
	return put(ctx, ex, "employment", e.Envelope,
		[]string{"worker_ref", "legal_entity_ref", "employment_type", "employment_status", "hire_date"},
		[]any{e.WorkerRef, e.LegalEntityRef, e.EmploymentType, e.EmploymentStatus, e.HireDate})
}

func scanEmployment(row pgx.Row) (Employment, error) {
	var e Employment
	err := row.Scan(&e.RowID, &e.Tenant, &e.EntityID, &e.CanonicalID, &e.WorkerRef, &e.LegalEntityRef, &e.EmploymentType,
		&e.EmploymentStatus, &e.HireDate, &e.EffectiveFrom, &e.EffectiveTo, &e.RecordedAt, &e.SupersededAt,
		&e.DigestAlgorithm, &e.Digest)
	return e, err
}

// CurrentEmployment returns the live Employment as of businessAt.
func (PeopleStore) CurrentEmployment(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt time.Time) (Employment, error) {
	row := ex.QueryRow(ctx, currentAsOfSQL("employment", employmentColumns), tenant, entityID, businessAt)
	e, err := scanEmployment(row)
	if err != nil {
		return Employment{}, wrapNotFound(err, "employment", entityID)
	}
	return e, nil
}

// ---------------------------------------------------------------------------
// Assignment
// ---------------------------------------------------------------------------

// Assignment is one bitemporal version of a job/position/org placement,
// folding AssignmentRevision into the same row.
type Assignment struct {
	Envelope
	EmploymentRef          uuid.UUID
	PrimaryFlag            bool
	JobCode                string
	Grade                  string
	OrganizationRef        *uuid.UUID
	PositionRef            *uuid.UUID
	Location               string
	PayZone                string
	FTE                    string // exact decimal text, e.g. "1.0000"
	ManagerRelationshipRef string
}

const assignmentColumns = "row_id, tenant_id, entity_id, canonical_id, employment_ref, primary_flag, job_code, grade, " +
	"organization_ref, position_ref, location, pay_zone, fte::text, manager_relationship_ref, " +
	"effective_from, effective_to, recorded_at, superseded_at, digest_algorithm, digest"

// NewAssignment builds an Assignment ready for PutAssignment. fte is exact
// decimal text (never a float) matching migrations/00011's numeric(9,4)
// column.
func NewAssignment(tenant, assignmentID, employmentRef uuid.UUID, primaryFlag bool, effectiveFrom time.Time,
	effectiveTo *time.Time, recordedAt time.Time,
	jobCode, grade string, organizationRef, positionRef *uuid.UUID, location, payZone, fte, managerRelationshipRef string,
) (Assignment, error) {
	fte, err := normalizeDecimal(fte)
	if err != nil {
		return Assignment{}, fmt.Errorf("aggregates: assignment fte: %w", err)
	}
	env, err := newEnvelope(KindAssignment, tenant, assignmentID, effectiveFrom, effectiveTo, recordedAt,
		employmentRef.String(), fmt.Sprint(primaryFlag), jobCode, grade,
		optionalUUIDString(organizationRef), optionalUUIDString(positionRef), location, payZone, fte, managerRelationshipRef)
	if err != nil {
		return Assignment{}, err
	}
	return Assignment{
		Envelope: env, EmploymentRef: employmentRef, PrimaryFlag: primaryFlag, JobCode: jobCode, Grade: grade,
		OrganizationRef: organizationRef, PositionRef: positionRef, Location: location, PayZone: payZone, FTE: fte,
		ManagerRelationshipRef: managerRelationshipRef,
	}, nil
}

func (PeopleStore) PutAssignment(ctx context.Context, ex Executor, a Assignment) (uuid.UUID, error) {
	return put(ctx, ex, "assignment", a.Envelope,
		[]string{"employment_ref", "primary_flag", "job_code", "grade", "organization_ref", "position_ref",
			"location", "pay_zone", "fte", "manager_relationship_ref"},
		[]any{a.EmploymentRef, a.PrimaryFlag, a.JobCode, nullableText(a.Grade), nullableUUID(a.OrganizationRef),
			nullableUUID(a.PositionRef), nullableText(a.Location), nullableText(a.PayZone), decimalParam(a.FTE), nullableText(a.ManagerRelationshipRef)})
}

func scanAssignment(row pgx.Row) (Assignment, error) {
	var a Assignment
	var grade, location, payZone, managerRef *string
	var orgRef, posRef *uuid.UUID
	err := row.Scan(&a.RowID, &a.Tenant, &a.EntityID, &a.CanonicalID, &a.EmploymentRef, &a.PrimaryFlag, &a.JobCode, &grade,
		&orgRef, &posRef, &location, &payZone, &a.FTE, &managerRef,
		&a.EffectiveFrom, &a.EffectiveTo, &a.RecordedAt, &a.SupersededAt, &a.DigestAlgorithm, &a.Digest)
	a.OrganizationRef, a.PositionRef = orgRef, posRef
	if grade != nil {
		a.Grade = *grade
	}
	if location != nil {
		a.Location = *location
	}
	if payZone != nil {
		a.PayZone = *payZone
	}
	if managerRef != nil {
		a.ManagerRelationshipRef = *managerRef
	}
	return a, err
}

// CurrentAssignment returns the live Assignment as of businessAt.
func (PeopleStore) CurrentAssignment(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt time.Time) (Assignment, error) {
	row := ex.QueryRow(ctx, currentAsOfSQL("assignment", assignmentColumns), tenant, entityID, businessAt)
	a, err := scanAssignment(row)
	if err != nil {
		return Assignment{}, wrapNotFound(err, "assignment", entityID)
	}
	return a, nil
}

// KnownAsOfAssignment returns the Assignment that held at businessAt, using
// only what had been recorded by knownAt.
func (PeopleStore) KnownAsOfAssignment(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt, knownAt time.Time) (Assignment, error) {
	row := ex.QueryRow(ctx, knownAsOfSQL("assignment", assignmentColumns), tenant, entityID, businessAt, knownAt)
	a, err := scanAssignment(row)
	if err != nil {
		return Assignment{}, wrapNotFound(err, "assignment", entityID)
	}
	return a, nil
}

// ---------------------------------------------------------------------------
// shared scalar-nullability helpers
// ---------------------------------------------------------------------------

func nullableText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullableUUID(id *uuid.UUID) *uuid.UUID {
	if id != nil && *id == uuid.Nil {
		return nil
	}
	return id
}

func optionalUUIDString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// ErrNotFound is returned by a Current*/KnownAsOf* read that matches no row.
var ErrNotFound = errors.New("aggregates: no row for the given tenant/entity/time bounds")

func wrapNotFound(err error, table string, entityID uuid.UUID) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s %s", ErrNotFound, table, entityID)
	}
	return fmt.Errorf("aggregates: read %s %s: %w", table, entityID, err)
}
