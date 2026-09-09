// DB-010: CompensationPackage, CompensationComponent, CompensationBand,
// WorkforceBudget and BudgetReservation.
package aggregates

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Entity kinds for DB-010's canonical_id encoding.
const (
	KindCompensationPackage   values.Kind = "compensation_package"
	KindCompensationComponent values.Kind = "compensation_component"
	KindCompensationBand      values.Kind = "compensation_band"
	KindWorkforceBudget       values.Kind = "workforce_budget"
	KindBudgetReservation     values.Kind = "budget_reservation"
)

// CompensationPort is DB-010's small read/append surface.
type CompensationPort interface {
	PutCompensationPackage(ctx context.Context, ex Executor, p CompensationPackage) (uuid.UUID, error)
	PutCompensationComponent(ctx context.Context, ex Executor, c CompensationComponent) (uuid.UUID, error)
	PutCompensationBand(ctx context.Context, ex Executor, b CompensationBand) (uuid.UUID, error)
	PutWorkforceBudget(ctx context.Context, ex Executor, b WorkforceBudget) (uuid.UUID, error)
	PutBudgetReservation(ctx context.Context, ex Executor, r BudgetReservation) (uuid.UUID, error)

	CurrentWorkforceBudget(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt time.Time) (WorkforceBudget, error)
}

// CompensationStore implements CompensationPort over the tables
// migrations/00013_compensation_aggregates.sql declares.
type CompensationStore struct{}

var _ CompensationPort = CompensationStore{}

// ---------------------------------------------------------------------------
// CompensationPackage
// ---------------------------------------------------------------------------

// CompensationPackage is one bitemporal version of a worker's compensation
// package summary.
type CompensationPackage struct {
	Envelope
	WorkerRef     uuid.UUID
	EmploymentRef *uuid.UUID
	AssignmentRef *uuid.UUID
	Currency      string
}

const compensationPackageColumns = "row_id, tenant_id, entity_id, canonical_id, worker_ref, employment_ref, " +
	"assignment_ref, currency, effective_from, effective_to, recorded_at, superseded_at, digest_algorithm, digest"

// NewCompensationPackage builds a CompensationPackage ready for
// PutCompensationPackage.
func NewCompensationPackage(tenant, entityID, workerRef uuid.UUID, employmentRef, assignmentRef *uuid.UUID,
	effectiveFrom time.Time, effectiveTo *time.Time, recordedAt time.Time, currency string) (CompensationPackage, error) {
	env, err := newEnvelope(KindCompensationPackage, tenant, entityID, effectiveFrom, effectiveTo, recordedAt,
		workerRef.String(), optionalUUIDString(employmentRef), optionalUUIDString(assignmentRef), currency)
	if err != nil {
		return CompensationPackage{}, err
	}
	return CompensationPackage{
		Envelope: env, WorkerRef: workerRef, EmploymentRef: employmentRef, AssignmentRef: assignmentRef, Currency: currency,
	}, nil
}

func (CompensationStore) PutCompensationPackage(ctx context.Context, ex Executor, p CompensationPackage) (uuid.UUID, error) {
	return put(ctx, ex, "compensation_package", p.Envelope,
		[]string{"worker_ref", "employment_ref", "assignment_ref", "currency"},
		[]any{p.WorkerRef, nullableUUID(p.EmploymentRef), nullableUUID(p.AssignmentRef), p.Currency})
}

func scanCompensationPackage(row dbport.Row) (CompensationPackage, error) {
	var p CompensationPackage
	var employmentRef, assignmentRef *uuid.UUID
	err := row.Scan(&p.RowID, &p.Tenant, &p.EntityID, &p.CanonicalID, &p.WorkerRef, &employmentRef, &assignmentRef,
		&p.Currency, &p.EffectiveFrom, &p.EffectiveTo, &p.RecordedAt, &p.SupersededAt, &p.DigestAlgorithm, &p.Digest)
	p.EmploymentRef, p.AssignmentRef = employmentRef, assignmentRef
	return p, err
}

// CurrentCompensationPackage returns the live CompensationPackage as of businessAt.
func (CompensationStore) CurrentCompensationPackage(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt time.Time) (CompensationPackage, error) {
	row := ex.QueryRow(ctx, currentAsOfSQL("compensation_package", compensationPackageColumns), tenant, entityID, businessAt)
	p, err := scanCompensationPackage(row)
	if err != nil {
		return CompensationPackage{}, wrapNotFound(err, "compensation_package", entityID)
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// CompensationComponent
// ---------------------------------------------------------------------------

// CompensationComponent is one bitemporal version of a package's pay
// component. Amount is exact decimal text via values.Money, never a float;
// the database additionally refuses (via
// compensation_component_forbid_currency_mismatch) a currency that does not
// match the owning package's.
type CompensationComponent struct {
	Envelope
	PackageRef    uuid.UUID
	ComponentType string
	Amount        string
	Currency      string
	Frequency     string
}

const compensationComponentColumns = "row_id, tenant_id, entity_id, canonical_id, package_ref, component_type, " +
	"amount::text, currency, frequency, effective_from, effective_to, recorded_at, superseded_at, digest_algorithm, digest"

// NewCompensationComponent builds a CompensationComponent ready for
// PutCompensationComponent, from an already-validated values.Money so the
// amount can never be a bare, unchecked string.
func NewCompensationComponent(tenant, entityID, packageRef uuid.UUID, effectiveFrom time.Time, effectiveTo *time.Time,
	recordedAt time.Time, componentType string, amount values.Money, frequency string) (CompensationComponent, error) {
	if err := amount.Validate(); err != nil {
		return CompensationComponent{}, fmt.Errorf("aggregates: compensation_component amount: %w", err)
	}
	amountText, err := moneyDecimalText(amount)
	if err != nil {
		return CompensationComponent{}, err
	}
	env, err := newEnvelope(KindCompensationComponent, tenant, entityID, effectiveFrom, effectiveTo, recordedAt,
		packageRef.String(), componentType, amountText, amount.Currency(), frequency)
	if err != nil {
		return CompensationComponent{}, err
	}
	return CompensationComponent{
		Envelope: env, PackageRef: packageRef, ComponentType: componentType,
		Amount: amountText, Currency: amount.Currency(), Frequency: frequency,
	}, nil
}

func (CompensationStore) PutCompensationComponent(ctx context.Context, ex Executor, c CompensationComponent) (uuid.UUID, error) {
	return put(ctx, ex, "compensation_component", c.Envelope,
		[]string{"package_ref", "component_type", "amount", "currency", "frequency"},
		[]any{c.PackageRef, c.ComponentType, decimalParam(c.Amount), c.Currency, c.Frequency})
}

func scanCompensationComponent(row dbport.Row) (CompensationComponent, error) {
	var c CompensationComponent
	err := row.Scan(&c.RowID, &c.Tenant, &c.EntityID, &c.CanonicalID, &c.PackageRef, &c.ComponentType, &c.Amount,
		&c.Currency, &c.Frequency, &c.EffectiveFrom, &c.EffectiveTo, &c.RecordedAt, &c.SupersededAt,
		&c.DigestAlgorithm, &c.Digest)
	return c, err
}

// CurrentCompensationComponent returns the live component as of businessAt.
func (CompensationStore) CurrentCompensationComponent(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt time.Time) (CompensationComponent, error) {
	row := ex.QueryRow(ctx, currentAsOfSQL("compensation_component", compensationComponentColumns), tenant, entityID, businessAt)
	c, err := scanCompensationComponent(row)
	if err != nil {
		return CompensationComponent{}, wrapNotFound(err, "compensation_component", entityID)
	}
	return c, nil
}

// ---------------------------------------------------------------------------
// CompensationBand
// ---------------------------------------------------------------------------

// CompensationBand is one bitemporal version of a pay band. The database
// enforces minimum <= midpoint <= maximum (RED: "invalid band range").
type CompensationBand struct {
	Envelope
	JobCode  string
	Grade    string
	PayZone  string
	Currency string
	Minimum  string
	Midpoint string
	Maximum  string
}

const compensationBandColumns = "row_id, tenant_id, entity_id, canonical_id, job_code, grade, pay_zone, currency, " +
	"minimum::text, midpoint::text, maximum::text, effective_from, effective_to, recorded_at, superseded_at, digest_algorithm, digest"

// NewCompensationBand builds a CompensationBand ready for
// PutCompensationBand, from already-validated values.Money bounds sharing one
// currency.
func NewCompensationBand(tenant, entityID uuid.UUID, effectiveFrom time.Time, effectiveTo *time.Time, recordedAt time.Time,
	jobCode, grade, payZone string, minimum, midpoint, maximum values.Money) (CompensationBand, error) {
	minVsMid, err := minimum.Cmp(midpoint)
	if err != nil {
		return CompensationBand{}, fmt.Errorf("aggregates: compensation_band minimum/midpoint: %w", err)
	}
	midVsMax, err := midpoint.Cmp(maximum)
	if err != nil {
		return CompensationBand{}, fmt.Errorf("aggregates: compensation_band midpoint/maximum: %w", err)
	}
	if minVsMid > 0 || midVsMax > 0 {
		return CompensationBand{}, fmt.Errorf("aggregates: compensation_band range must satisfy minimum <= midpoint <= maximum")
	}
	minText, err := moneyDecimalText(minimum)
	if err != nil {
		return CompensationBand{}, err
	}
	midText, err := moneyDecimalText(midpoint)
	if err != nil {
		return CompensationBand{}, err
	}
	maxText, err := moneyDecimalText(maximum)
	if err != nil {
		return CompensationBand{}, err
	}
	env, err := newEnvelope(KindCompensationBand, tenant, entityID, effectiveFrom, effectiveTo, recordedAt,
		jobCode, grade, payZone, minimum.Currency(), minText, midText, maxText)
	if err != nil {
		return CompensationBand{}, err
	}
	return CompensationBand{
		Envelope: env, JobCode: jobCode, Grade: grade, PayZone: payZone, Currency: minimum.Currency(),
		Minimum: minText, Midpoint: midText, Maximum: maxText,
	}, nil
}

func (CompensationStore) PutCompensationBand(ctx context.Context, ex Executor, b CompensationBand) (uuid.UUID, error) {
	return put(ctx, ex, "compensation_band", b.Envelope,
		[]string{"job_code", "grade", "pay_zone", "currency", "minimum", "midpoint", "maximum"},
		[]any{b.JobCode, b.Grade, b.PayZone, b.Currency, decimalParam(b.Minimum), decimalParam(b.Midpoint), decimalParam(b.Maximum)})
}

func scanCompensationBand(row dbport.Row) (CompensationBand, error) {
	var b CompensationBand
	err := row.Scan(&b.RowID, &b.Tenant, &b.EntityID, &b.CanonicalID, &b.JobCode, &b.Grade, &b.PayZone, &b.Currency,
		&b.Minimum, &b.Midpoint, &b.Maximum, &b.EffectiveFrom, &b.EffectiveTo, &b.RecordedAt, &b.SupersededAt,
		&b.DigestAlgorithm, &b.Digest)
	return b, err
}

// CurrentCompensationBand returns the live band as of businessAt.
func (CompensationStore) CurrentCompensationBand(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt time.Time) (CompensationBand, error) {
	row := ex.QueryRow(ctx, currentAsOfSQL("compensation_band", compensationBandColumns), tenant, entityID, businessAt)
	b, err := scanCompensationBand(row)
	if err != nil {
		return CompensationBand{}, wrapNotFound(err, "compensation_band", entityID)
	}
	return b, nil
}

// ---------------------------------------------------------------------------
// WorkforceBudget
// ---------------------------------------------------------------------------

// WorkforceBudget is one bitemporal version of a workforce budget
// (planning/specs/workforce-budget-authority.md). Currency is required when
// Unit is MONEY and forbidden-or-optional otherwise; the database enforces
// the MONEY case (workforce_budget_currency_required_for_money).
type WorkforceBudget struct {
	Envelope
	BudgetType        string
	OwnerSystem       string
	Scope             string
	Period            string
	Currency          string // "" when Unit is not MONEY
	Unit              string
	AvailableQuantity string
	BaselineVersion   string
}

const workforceBudgetColumns = "row_id, tenant_id, entity_id, canonical_id, budget_type, owner_system, scope, period, " +
	"currency, unit, available_quantity::text, baseline_version, " +
	"effective_from, effective_to, recorded_at, superseded_at, digest_algorithm, digest"

// NewWorkforceBudget builds a WorkforceBudget ready for PutWorkforceBudget.
// availableQuantity is exact decimal text, never a float.
func NewWorkforceBudget(tenant, entityID uuid.UUID, effectiveFrom time.Time, effectiveTo *time.Time, recordedAt time.Time,
	budgetType, ownerSystem, scope, period, currency, unit, availableQuantity, baselineVersion string) (WorkforceBudget, error) {
	availableQuantity, err := normalizeDecimal(availableQuantity)
	if err != nil {
		return WorkforceBudget{}, fmt.Errorf("aggregates: workforce_budget available_quantity: %w", err)
	}
	env, err := newEnvelope(KindWorkforceBudget, tenant, entityID, effectiveFrom, effectiveTo, recordedAt,
		budgetType, ownerSystem, scope, period, currency, unit, availableQuantity, baselineVersion)
	if err != nil {
		return WorkforceBudget{}, err
	}
	return WorkforceBudget{
		Envelope: env, BudgetType: budgetType, OwnerSystem: ownerSystem, Scope: scope, Period: period,
		Currency: currency, Unit: unit, AvailableQuantity: availableQuantity, BaselineVersion: baselineVersion,
	}, nil
}

func (CompensationStore) PutWorkforceBudget(ctx context.Context, ex Executor, b WorkforceBudget) (uuid.UUID, error) {
	return put(ctx, ex, "workforce_budget", b.Envelope,
		[]string{"budget_type", "owner_system", "scope", "period", "currency", "unit", "available_quantity", "baseline_version"},
		[]any{b.BudgetType, b.OwnerSystem, b.Scope, b.Period, nullableText(b.Currency), b.Unit,
			decimalParam(b.AvailableQuantity), nullableText(b.BaselineVersion)})
}

func scanWorkforceBudget(row dbport.Row) (WorkforceBudget, error) {
	var b WorkforceBudget
	var currency, baselineVersion *string
	err := row.Scan(&b.RowID, &b.Tenant, &b.EntityID, &b.CanonicalID, &b.BudgetType, &b.OwnerSystem, &b.Scope, &b.Period,
		&currency, &b.Unit, &b.AvailableQuantity, &baselineVersion,
		&b.EffectiveFrom, &b.EffectiveTo, &b.RecordedAt, &b.SupersededAt, &b.DigestAlgorithm, &b.Digest)
	if currency != nil {
		b.Currency = *currency
	}
	if baselineVersion != nil {
		b.BaselineVersion = *baselineVersion
	}
	return b, err
}

// CurrentWorkforceBudget returns the live WorkforceBudget as of businessAt.
func (CompensationStore) CurrentWorkforceBudget(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt time.Time) (WorkforceBudget, error) {
	row := ex.QueryRow(ctx, currentAsOfSQL("workforce_budget", workforceBudgetColumns), tenant, entityID, businessAt)
	b, err := scanWorkforceBudget(row)
	if err != nil {
		return WorkforceBudget{}, wrapNotFound(err, "workforce_budget", entityID)
	}
	return b, nil
}

// ---------------------------------------------------------------------------
// BudgetReservation
// ---------------------------------------------------------------------------

// BudgetReservation is one bitemporal version of a reservation against a
// WorkforceBudget. The database enforces (via
// budget_reservation_forbid_overcommit) that live HELD/COMMITTED
// reservations against one budget never exceed that budget's own live
// AvailableQuantity, and that Currency matches the budget's.
type BudgetReservation struct {
	Envelope
	BudgetRef   uuid.UUID
	ProposalRef *uuid.UUID
	Amount      string
	Currency    string // "" when the budget itself carries no currency
	Status      string
	Expiry      *time.Time
}

const budgetReservationColumns = "row_id, tenant_id, entity_id, canonical_id, budget_ref, proposal_ref, " +
	"amount::text, currency, status, expiry, effective_from, effective_to, recorded_at, superseded_at, digest_algorithm, digest"

// NewBudgetReservation builds a BudgetReservation ready for
// PutBudgetReservation. amount is exact decimal text, never a float.
func NewBudgetReservation(tenant, entityID, budgetRef uuid.UUID, proposalRef *uuid.UUID, effectiveFrom time.Time,
	effectiveTo *time.Time, recordedAt time.Time, amount, currency, status string, expiry *time.Time) (BudgetReservation, error) {
	amount, err := normalizeDecimal(amount)
	if err != nil {
		return BudgetReservation{}, fmt.Errorf("aggregates: budget_reservation amount: %w", err)
	}
	env, err := newEnvelope(KindBudgetReservation, tenant, entityID, effectiveFrom, effectiveTo, recordedAt,
		budgetRef.String(), optionalUUIDString(proposalRef), amount, currency, status, formatOptionalTime(expiry))
	if err != nil {
		return BudgetReservation{}, err
	}
	return BudgetReservation{
		Envelope: env, BudgetRef: budgetRef, ProposalRef: proposalRef, Amount: amount, Currency: currency,
		Status: status, Expiry: normalizeOptionalTime(expiry),
	}, nil
}

func (CompensationStore) PutBudgetReservation(ctx context.Context, ex Executor, r BudgetReservation) (uuid.UUID, error) {
	return put(ctx, ex, "budget_reservation", r.Envelope,
		[]string{"budget_ref", "proposal_ref", "amount", "currency", "status", "expiry"},
		[]any{r.BudgetRef, nullableUUID(r.ProposalRef), decimalParam(r.Amount), nullableText(r.Currency), r.Status, r.Expiry})
}

func scanBudgetReservation(row dbport.Row) (BudgetReservation, error) {
	var r BudgetReservation
	var proposalRef *uuid.UUID
	var currency *string
	err := row.Scan(&r.RowID, &r.Tenant, &r.EntityID, &r.CanonicalID, &r.BudgetRef, &proposalRef, &r.Amount,
		&currency, &r.Status, &r.Expiry, &r.EffectiveFrom, &r.EffectiveTo, &r.RecordedAt, &r.SupersededAt,
		&r.DigestAlgorithm, &r.Digest)
	r.ProposalRef = proposalRef
	if currency != nil {
		r.Currency = *currency
	}
	return r, err
}

// CurrentBudgetReservation returns the live reservation as of businessAt.
func (CompensationStore) CurrentBudgetReservation(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt time.Time) (BudgetReservation, error) {
	row := ex.QueryRow(ctx, currentAsOfSQL("budget_reservation", budgetReservationColumns), tenant, entityID, businessAt)
	r, err := scanBudgetReservation(row)
	if err != nil {
		return BudgetReservation{}, wrapNotFound(err, "budget_reservation", entityID)
	}
	return r, nil
}
