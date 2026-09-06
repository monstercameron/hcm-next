package workforce

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Refusals this package reports. Both are matchable with errors.Is.
var (
	// ErrDuplicate is returned by [Store.Create] when the tenant already
	// carries a worker with that worker_key or worker_id. It is a typed
	// refusal rather than a raw constraint violation because "this employee
	// already exists" is an answer the caller has to render, not a storage
	// fault it can only log.
	ErrDuplicate = errors.New("workforce: a worker with that key already exists in this tenant")
	// ErrInvalidRow is returned when a row is not complete enough to insert.
	// journey_worker is append-only, so a row that is wrong on the way in
	// cannot be corrected afterward -- it is refused here instead.
	ErrInvalidRow = errors.New("workforce: the worker row is incomplete")
)

// SourceCreated is the only journey_worker.source token that exists. A worker
// imported from an incumbent or observed through the connectivity plane would
// carry its own token beside this one rather than overloading it.
const SourceCreated = "CREATED"

// The declared source authority every created worker's facts are disclosed
// under. It is this cell's own People authority: a worker a user typed into
// this cell is asserted locally, under the same authority-by-field policy the
// corpus population is read under, so an explanation cannot tell the two apart
// by their authority alone -- which is correct, because their authority really
// is the same.
const (
	// SourceSystem is the asserting system identifier.
	SourceSystem = "hcmnext.people"
	// AuthorityPolicy is the authority-by-field decision that granted it.
	AuthorityPolicy = "people.source_authority/2026.1"
)

// DateLayout is the ISO-8601 calendar-date layout every date-valued field on
// [WorkerRow] is carried in. Dates are strings and not time.Time because a
// business date is not an instant: rendering one through a time.Time forces a
// timezone choice nothing in the record made.
const DateLayout = "2006-01-02"

// WorkerRow is one journey_worker row: an employee as a durable fact.
//
// Every decimal-valued field (FTE, BasePay, BonusTarget) is decimal text
// rather than a float, and travels to and from PostgreSQL through an explicit
// ::text::numeric / ::text cast, so the exact digits written are the exact
// digits read back. internal/data/aggregates documents the same rule for the
// same reason.
type WorkerRow struct {
	TenantID  uuid.UUID
	WorkerID  uuid.UUID
	WorkerKey string

	LegalName     string
	PreferredName string
	WorkerNumber  string

	// WorkerType and LifecycleStatus are the vocabulary tokens
	// people.FieldWorkerType and people.FieldLifecycleStatus are projected
	// from. LifecycleStatus is also what people.FieldEmploymentStatus is
	// projected from: journey_worker carries one lifecycle token, and a
	// created worker whose employment had ended would not be a worker this
	// surface can create.
	WorkerType      string
	LifecycleStatus string

	EmploymentID string
	AssignmentID string

	JobCode                string
	JobTitle               string
	Grade                  string
	OrgUnit                string
	PositionID             string
	Location               string
	PayZone                string
	FTE                    string
	ManagerRelationshipRef string

	// ProfilePhotoOriginalRef is the private retained upload reference;
	// ProfilePhotoProxyRef is the same-origin, display-safe derivative. Both
	// are optional, but a worker can never carry only one half of the pair.
	ProfilePhotoOriginalRef string
	ProfilePhotoProxyRef    string

	// HireDate and EffectiveFrom are ISO-8601 calendar dates ([DateLayout]).
	HireDate      string
	EffectiveFrom string

	// BasePay, Currency, PayBasis and BonusTarget are the declared
	// compensation baseline the promotion simulation reads for this worker.
	BasePay     string
	Currency    string
	PayBasis    string
	BonusTarget string

	RevisionStream   string
	RevisionSequence uint64
	KnownAt          time.Time
	RecordedAt       time.Time

	CreatedBy string
	Source    string
}

// Validate reports whether the row can be inserted at all.
//
// It checks presence and shape only. Whether the placement lands in a pay band
// the simulation knows is a business question the journey engine answers
// before it ever gets here; this package declines to re-decide it, because a
// storage package that enforced a domain rule would be a second, quieter place
// that rule lives.
func (w WorkerRow) Validate() error {
	if w.TenantID == uuid.Nil {
		return fmt.Errorf("%w: tenant id is the nil UUID", ErrInvalidRow)
	}
	if w.WorkerID == uuid.Nil {
		return fmt.Errorf("%w: worker id is the nil UUID", ErrInvalidRow)
	}
	for _, f := range []struct{ name, value string }{
		{"worker_key", w.WorkerKey},
		{"legal_name", w.LegalName},
		{"preferred_name", w.PreferredName},
		{"worker_number", w.WorkerNumber},
		{"worker_type", w.WorkerType},
		{"lifecycle_status", w.LifecycleStatus},
		{"employment_id", w.EmploymentID},
		{"assignment_id", w.AssignmentID},
		{"job_code", w.JobCode},
		{"grade", w.Grade},
		{"org_unit", w.OrgUnit},
		{"position_id", w.PositionID},
		{"location", w.Location},
		{"pay_zone", w.PayZone},
		{"fte", w.FTE},
		{"manager_relationship_ref", w.ManagerRelationshipRef},
		{"base_pay", w.BasePay},
		{"pay_basis", w.PayBasis},
		{"bonus_target", w.BonusTarget},
		{"revision_stream", w.RevisionStream},
		{"created_by", w.CreatedBy},
		{"source", w.Source},
	} {
		if strings.TrimSpace(f.value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidRow, f.name)
		}
	}
	if len(w.Currency) != 3 || w.Currency != strings.ToUpper(w.Currency) {
		return fmt.Errorf("%w: currency %q is not a three-letter ISO-4217 code", ErrInvalidRow, w.Currency)
	}
	for _, f := range []struct{ name, value string }{
		{"hire_date", w.HireDate},
		{"effective_from", w.EffectiveFrom},
	} {
		if _, err := time.Parse(DateLayout, f.value); err != nil {
			return fmt.Errorf("%w: %s %q is not an ISO-8601 date", ErrInvalidRow, f.name, f.value)
		}
	}
	if w.RevisionSequence < 1 {
		return fmt.Errorf("%w: revision sequence must be at least 1", ErrInvalidRow)
	}
	if w.KnownAt.IsZero() {
		return fmt.Errorf("%w: known_at is unset", ErrInvalidRow)
	}
	if w.RecordedAt.IsZero() {
		return fmt.Errorf("%w: recorded_at is unset", ErrInvalidRow)
	}
	if (w.ProfilePhotoOriginalRef == "") != (w.ProfilePhotoProxyRef == "") {
		return fmt.Errorf("%w: profile photo original and proxy references must be set together", ErrInvalidRow)
	}
	if w.KnownAt.After(w.RecordedAt) {
		return fmt.Errorf("%w: known_at %s is after recorded_at %s",
			ErrInvalidRow, w.KnownAt.UTC().Format(time.RFC3339), w.RecordedAt.UTC().Format(time.RFC3339))
	}
	if w.Source != SourceCreated {
		return fmt.Errorf("%w: source %q is not %s", ErrInvalidRow, w.Source, SourceCreated)
	}
	return nil
}

// DisplayName is the name a surface addresses this worker by: the preferred
// name when there is one, otherwise the legal name. It is here rather than in
// each caller so the two never disagree about which name a worker is shown
// under.
func (w WorkerRow) DisplayName() string {
	if strings.TrimSpace(w.PreferredName) != "" {
		return w.PreferredName
	}
	return w.LegalName
}
