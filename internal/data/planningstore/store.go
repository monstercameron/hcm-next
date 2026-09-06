// Package planningstore persists the tenant-scoped planning input tables from
// migration 00110. It is descriptive storage only: no method reserves a
// position, creates an assignment or grants budget authority.
package planningstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/demand"
	"github.com/monstercameron/hcm-next/internal/domains/scenario"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Executor is the driver-free capability used by the stateless helper
// methods. Tenant-scoped callers must pass a transaction already configured
// with tenancy.WithTenant.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

type ErrorCode string

const (
	CodeInvalid            ErrorCode = "PLANNING_INVALID"
	CodeNotFound           ErrorCode = "PLANNING_NOT_FOUND"
	CodeDuplicateRevision  ErrorCode = "PLANNING_DUPLICATE_REVISION"
	CodeStaleCAS           ErrorCode = "PLANNING_STALE_CAS"
	CodeIntegrityViolation ErrorCode = "PLANNING_INTEGRITY_VIOLATION"
	CodeTenant             ErrorCode = "PLANNING_TENANT"
	CodeStorage            ErrorCode = "PLANNING_STORAGE"
)

var (
	ErrInvalid            = errors.New("planningstore: invalid row")
	ErrNotFound           = errors.New("planningstore: not found")
	ErrDuplicateRevision  = errors.New("planningstore: duplicate revision")
	ErrStaleCAS           = errors.New("planningstore: stale compare-and-set")
	ErrIntegrityViolation = errors.New("planningstore: integrity violation")
	ErrTenant             = errors.New("planningstore: tenant mismatch")
)

// Error is a stable storage refusal. Callers can use errors.Is for the
// semantic cause and CodeOf for telemetry without parsing PostgreSQL text.
type Error struct {
	Code     ErrorCode
	Cause    error
	Detail   string
	Expected string
	Actual   string
}

func (e *Error) Error() string {
	if e == nil {
		return "planningstore: <nil error>"
	}
	if e.Detail == "" {
		return string(e.Code) + ": " + e.Cause.Error()
	}
	return string(e.Code) + ": " + e.Detail + ": " + e.Cause.Error()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func CodeOf(err error) ErrorCode {
	var coded *Error
	if errors.As(err, &coded) {
		return coded.Code
	}
	var demandCoded *demand.StoreError
	if errors.As(err, &demandCoded) {
		switch demandCoded.Code {
		case demand.StoreInvalidCode:
			return CodeInvalid
		case demand.StoreNotFoundCode:
			return CodeNotFound
		case demand.StoreDuplicateCode:
			return CodeDuplicateRevision
		case demand.StoreStaleCASCode:
			return CodeStaleCAS
		}
	}
	return ""
}

func refusal(code ErrorCode, cause error, detail string) *Error {
	return &Error{Code: code, Cause: cause, Detail: detail}
}

func stale(expected, actual string) *Error {
	err := refusal(CodeStaleCAS, ErrStaleCAS, fmt.Sprintf("expected %q, actual %q", expected, actual))
	err.Expected, err.Actual = expected, actual
	return err
}

// Store is a tenant-bound adapter. The public domain-port methods retain a
// tenant argument so the adapter can fail closed if a caller supplies a
// different partition than the connection was constructed for.
type Store struct {
	db     dbport.Beginner
	tenant uuid.UUID
}

var _ demand.Store = (*Store)(nil)

func New(db dbport.Beginner, tenant uuid.UUID) *Store      { return &Store{db: db, tenant: tenant} }
func NewStore(db dbport.Beginner, tenant uuid.UUID) *Store { return New(db, tenant) }

func (s *Store) withTx(ctx context.Context, tenantID uuid.UUID, fn func(context.Context, dbport.Tx) error) error {
	if s == nil || s.db == nil || s.tenant == uuid.Nil || tenantID == uuid.Nil || tenantID != s.tenant {
		return refusal(CodeTenant, ErrTenant, "store and operation tenant must be non-nil and equal")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return refusal(CodeStorage, err, "begin planning transaction")
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return refusal(CodeStorage, err, "scope planning transaction")
	}
	if err := fn(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return refusal(CodeStorage, err, "commit planning transaction")
	}
	return nil
}

func parseTenant(tenant string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(tenant))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, refusal(CodeTenant, ErrTenant, "tenant must be a non-nil UUID")
	}
	return id, nil
}

// SaveSignal implements demand.Store.
func (s *Store) SaveSignal(ctx context.Context, tenant string, signal demand.DemandSignal, expectedVersion string) error {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return err
	}
	return s.withTx(ctx, tenantID, func(ctx context.Context, tx dbport.Tx) error {
		return putSignal(ctx, tx, tenantID, signal, expectedVersion)
	})
}

// LoadSignal implements demand.Store.
func (s *Store) LoadSignal(ctx context.Context, tenant, signalID, version string) (demand.DemandSignal, error) {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return demand.DemandSignal{}, err
	}
	var out demand.DemandSignal
	err = s.withTx(ctx, tenantID, func(ctx context.Context, tx dbport.Tx) error {
		var loadErr error
		out, loadErr = loadSignal(ctx, tx, tenantID, signalID, version)
		return loadErr
	})
	return out, err
}

func (s *Store) ListSignalVersions(ctx context.Context, tenant, signalID string) ([]demand.DemandSignal, error) {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return nil, err
	}
	var out []demand.DemandSignal
	err = s.withTx(ctx, tenantID, func(ctx context.Context, tx dbport.Tx) error {
		var listErr error
		out, listErr = listSignals(ctx, tx, tenantID, signalID)
		return listErr
	})
	return out, err
}

// SaveCoverageRequirement implements demand.Store.
func (s *Store) SaveCoverageRequirement(ctx context.Context, tenant string, requirement demand.CoverageRequirement, expectedVersion string) error {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return err
	}
	return s.withTx(ctx, tenantID, func(ctx context.Context, tx dbport.Tx) error {
		return putCoverage(ctx, tx, tenantID, requirement, expectedVersion)
	})
}

func (s *Store) LoadCoverageRequirement(ctx context.Context, tenant, requirementID, version string) (demand.CoverageRequirement, error) {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return demand.CoverageRequirement{}, err
	}
	var out demand.CoverageRequirement
	err = s.withTx(ctx, tenantID, func(ctx context.Context, tx dbport.Tx) error {
		var loadErr error
		out, loadErr = loadCoverage(ctx, tx, tenantID, requirementID, version)
		return loadErr
	})
	return out, err
}

func (s *Store) ListCoverageVersions(ctx context.Context, tenant, requirementID string) ([]demand.CoverageRequirement, error) {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return nil, err
	}
	var out []demand.CoverageRequirement
	err = s.withTx(ctx, tenantID, func(ctx context.Context, tx dbport.Tx) error {
		var listErr error
		out, listErr = listCoverages(ctx, tx, tenantID, requirementID)
		return listErr
	})
	return out, err
}

// SaveScenarioRevision stores one immutable scenario revision. expectedRevision
// is zero for the first revision and otherwise must equal both the current
// database head and the revision's declared parent.
func (s *Store) SaveScenarioRevision(ctx context.Context, tenant string, revision scenario.ScenarioRevision, expectedRevision uint64) error {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return err
	}
	return s.withTx(ctx, tenantID, func(ctx context.Context, tx dbport.Tx) error {
		return putScenario(ctx, tx, tenantID, revision, expectedRevision)
	})
}

func (s *Store) LoadScenarioRevision(ctx context.Context, tenant, scenarioID string, revision uint64) (scenario.ScenarioRevision, error) {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return scenario.ScenarioRevision{}, err
	}
	var out scenario.ScenarioRevision
	err = s.withTx(ctx, tenantID, func(ctx context.Context, tx dbport.Tx) error {
		var loadErr error
		out, loadErr = loadScenario(ctx, tx, tenantID, scenarioID, revision)
		return loadErr
	})
	return out, err
}

func (s *Store) ListScenarioRevisions(ctx context.Context, tenant, scenarioID string) ([]scenario.ScenarioRevision, error) {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return nil, err
	}
	var out []scenario.ScenarioRevision
	err = s.withTx(ctx, tenantID, func(ctx context.Context, tx dbport.Tx) error {
		var listErr error
		out, listErr = listScenarios(ctx, tx, tenantID, scenarioID)
		return listErr
	})
	return out, err
}

// PutSignal is the explicit transaction form used by composition roots that
// already coordinate several planning writes atomically.
func (s Store) PutSignal(ctx context.Context, ex Executor, tenantID uuid.UUID, signal demand.DemandSignal, expectedVersion string) error {
	return putSignal(ctx, ex, tenantID, signal, expectedVersion)
}

func (s Store) LoadSignalRevision(ctx context.Context, ex Executor, tenantID uuid.UUID, signalID, version string) (demand.DemandSignal, error) {
	return loadSignal(ctx, ex, tenantID, signalID, version)
}

func (s Store) PutCoverageRequirement(ctx context.Context, ex Executor, tenantID uuid.UUID, requirement demand.CoverageRequirement, expectedVersion string) error {
	return putCoverage(ctx, ex, tenantID, requirement, expectedVersion)
}

func (s Store) LoadCoverageRevision(ctx context.Context, ex Executor, tenantID uuid.UUID, requirementID, version string) (demand.CoverageRequirement, error) {
	return loadCoverage(ctx, ex, tenantID, requirementID, version)
}

func (s Store) PutScenarioRevision(ctx context.Context, ex Executor, tenantID uuid.UUID, revision scenario.ScenarioRevision, expectedRevision uint64) error {
	return putScenario(ctx, ex, tenantID, revision, expectedRevision)
}

func (s Store) LoadScenario(ctx context.Context, ex Executor, tenantID uuid.UUID, scenarioID string, revision uint64) (scenario.ScenarioRevision, error) {
	return loadScenario(ctx, ex, tenantID, scenarioID, revision)
}

type decimalWire struct {
	Text     string `json:"text"`
	Scale    int32  `json:"scale"`
	Rounding string `json:"rounding"`
}

type intervalWire struct {
	Kind            string `json:"kind"`
	Start           string `json:"start"`
	End             string `json:"end,omitempty"`
	CalendarRef     string `json:"calendar_ref,omitempty"`
	CalendarVersion string `json:"calendar_version,omitempty"`
}

type quantityWire struct {
	Value decimalWire `json:"value"`
	Unit  string      `json:"unit"`
}

type signalWire struct {
	SignalID        string       `json:"signal_id"`
	Work            intervalWire `json:"work"`
	Location        string       `json:"location"`
	OrgUnit         string       `json:"org_unit"`
	Quantity        quantityWire `json:"quantity"`
	Unit            string       `json:"unit"`
	Skill           string       `json:"skill"`
	Role            string       `json:"role"`
	RoleOrSkillRef  string       `json:"role_or_skill_ref"`
	Priority        int          `json:"priority"`
	Source          string       `json:"source"`
	SourceRef       string       `json:"source_ref"`
	Confidence      *decimalWire `json:"confidence,omitempty"`
	ConfidenceClass string       `json:"confidence_class,omitempty"`
	Scenario        string       `json:"scenario"`
	Version         string       `json:"version"`
}

type assumptionWire struct {
	Key            string       `json:"key"`
	Kind           string       `json:"kind"`
	Text           string       `json:"text,omitempty"`
	Number         *decimalWire `json:"number,omitempty"`
	Boolean        bool         `json:"boolean,omitempty"`
	Unit           string       `json:"unit"`
	ProvenanceRefs []string     `json:"provenance_refs"`
}

type scenarioWire struct {
	Horizon     intervalWire     `json:"horizon"`
	Assumptions []assumptionWire `json:"assumptions"`
}

func decimalToWire(value values.Decimal) decimalWire {
	return decimalWire{Text: value.String(), Scale: value.Scale(), Rounding: value.Rounding().String()}
}

func decimalFromWire(value decimalWire) (values.Decimal, error) {
	mode, err := values.ParseRoundingMode(value.Rounding)
	if err != nil {
		return values.Decimal{}, err
	}
	return values.NewDecimal(value.Text, value.Scale, mode)
}

func quantityToWire(value values.Quantity) quantityWire {
	return quantityWire{Value: decimalToWire(value.Value()), Unit: value.Unit()}
}

func quantityFromWire(value quantityWire) (values.Quantity, error) {
	decimal, err := decimalFromWire(value.Value)
	if err != nil {
		return values.Quantity{}, err
	}
	return values.NewQuantity(decimal.String(), value.Unit, decimal.Scale(), decimal.Rounding())
}

func intervalToWire(value values.EffectiveInterval) (intervalWire, error) {
	if err := value.Validate(); err != nil {
		return intervalWire{}, err
	}
	wire := intervalWire{Kind: value.Kind().String()}
	if start, ok := value.StartInstant(); ok {
		wire.Start = start.String()
		if end, hasEnd := value.EndInstant(); hasEnd {
			wire.End = end.String()
		}
		return wire, nil
	}
	start, _ := value.StartDate()
	wire.Start = start.String()
	wire.CalendarRef = value.Calendar().Ref
	wire.CalendarVersion = value.Calendar().Version
	if end, hasEnd := value.EndDate(); hasEnd {
		wire.End = end.String()
	}
	return wire, nil
}

func intervalFromWire(wire intervalWire) (values.EffectiveInterval, error) {
	switch wire.Kind {
	case values.IntervalKindInstant.String():
		var start values.Instant
		if err := start.UnmarshalText([]byte(wire.Start)); err != nil {
			return values.EffectiveInterval{}, err
		}
		if wire.End == "" {
			return values.NewOpenInstantInterval(start)
		}
		var end values.Instant
		if err := end.UnmarshalText([]byte(wire.End)); err != nil {
			return values.EffectiveInterval{}, err
		}
		return values.NewInstantInterval(start, end)
	case values.IntervalKindLocalDate.String():
		var start values.LocalDate
		if err := start.UnmarshalText([]byte(wire.Start)); err != nil {
			return values.EffectiveInterval{}, err
		}
		calendar := values.CalendarRef{Ref: wire.CalendarRef, Version: wire.CalendarVersion}
		if wire.End == "" {
			return values.NewOpenLocalDateInterval(start, calendar)
		}
		var end values.LocalDate
		if err := end.UnmarshalText([]byte(wire.End)); err != nil {
			return values.EffectiveInterval{}, err
		}
		return values.NewLocalDateInterval(start, end, calendar)
	default:
		return values.EffectiveInterval{}, fmt.Errorf("unknown interval kind %q", wire.Kind)
	}
}

func intervalBounds(value values.EffectiveInterval) (time.Time, *time.Time, error) {
	if err := value.Validate(); err != nil {
		return time.Time{}, nil, err
	}
	if start, ok := value.StartInstant(); ok {
		var end *time.Time
		if finish, hasEnd := value.EndInstant(); hasEnd {
			at := finish.Time()
			end = &at
		}
		return start.Time(), end, nil
	}
	start, _ := value.StartDate()
	from := time.Date(int(start.Year()), start.Month(), int(start.Day()), 0, 0, 0, 0, time.UTC)
	var end *time.Time
	if finish, hasEnd := value.EndDate(); hasEnd {
		at := time.Date(int(finish.Year()), finish.Month(), int(finish.Day()), 0, 0, 0, 0, time.UTC)
		end = &at
	}
	return from, end, nil
}

func signalToWire(value demand.DemandSignal) (signalWire, error) {
	work, err := intervalToWire(value.Work)
	if err != nil {
		return signalWire{}, err
	}
	wire := signalWire{
		SignalID: value.SignalID, Work: work, Location: value.Location, OrgUnit: value.OrgUnit,
		Quantity: quantityToWire(value.Quantity), Unit: value.Unit, Skill: value.Skill, Role: value.Role,
		RoleOrSkillRef: value.RoleOrSkillRef, Priority: value.Priority, Source: value.Source,
		SourceRef: value.SourceRef, ConfidenceClass: string(value.ConfidenceClass), Scenario: value.Scenario,
		Version: value.Version,
	}
	if value.Confidence.Validate() == nil {
		confidence := decimalToWire(value.Confidence)
		wire.Confidence = &confidence
	}
	return wire, nil
}

func signalFromWire(wire signalWire) (demand.DemandSignal, error) {
	work, err := intervalFromWire(wire.Work)
	if err != nil {
		return demand.DemandSignal{}, err
	}
	quantity, err := quantityFromWire(wire.Quantity)
	if err != nil {
		return demand.DemandSignal{}, err
	}
	value := demand.DemandSignal{
		SignalID: wire.SignalID, Work: work, Location: wire.Location, OrgUnit: wire.OrgUnit,
		Quantity: quantity, Unit: wire.Unit, Skill: wire.Skill, Role: wire.Role,
		RoleOrSkillRef: wire.RoleOrSkillRef, Priority: wire.Priority, Source: wire.Source,
		SourceRef: wire.SourceRef, ConfidenceClass: demand.ConfidenceClass(wire.ConfidenceClass),
		Scenario: wire.Scenario, Version: wire.Version,
	}
	if wire.Confidence != nil {
		value.Confidence, err = decimalFromWire(*wire.Confidence)
		if err != nil {
			return demand.DemandSignal{}, err
		}
	}
	return value, nil
}

func assumptionToWire(value scenario.Assumption) assumptionWire {
	wire := assumptionWire{Key: value.Key, Unit: value.Unit, ProvenanceRefs: append([]string(nil), value.ProvenanceRefs...)}
	wire.Kind = string(value.Value.Kind)
	switch value.Value.Kind {
	case scenario.ValueText:
		wire.Text = value.Value.Text
	case scenario.ValueDecimal:
		decimal := decimalToWire(value.Value.Number)
		wire.Number = &decimal
	case scenario.ValueBoolean:
		wire.Boolean = value.Value.Boolean
	}
	return wire
}

func assumptionFromWire(value assumptionWire) (scenario.Assumption, error) {
	assumption := scenario.Assumption{Key: value.Key, Unit: value.Unit, ProvenanceRefs: append([]string(nil), value.ProvenanceRefs...)}
	switch scenario.ValueKind(value.Kind) {
	case scenario.ValueText:
		assumption.Value = scenario.TextValue(value.Text)
	case scenario.ValueDecimal:
		if value.Number == nil {
			return scenario.Assumption{}, errors.New("decimal assumption has no number")
		}
		decimal, err := decimalFromWire(*value.Number)
		if err != nil {
			return scenario.Assumption{}, err
		}
		assumption.Value = scenario.DecimalValue(decimal)
	case scenario.ValueBoolean:
		assumption.Value = scenario.BooleanValue(value.Boolean)
	default:
		return scenario.Assumption{}, fmt.Errorf("unknown assumption kind %q", value.Kind)
	}
	return assumption, nil
}

func signalPayload(value demand.DemandSignal) ([]byte, intervalWire, time.Time, *time.Time, error) {
	wire, err := signalToWire(value)
	if err != nil {
		return nil, intervalWire{}, time.Time{}, nil, err
	}
	from, to, err := intervalBounds(value.Work)
	if err != nil {
		return nil, intervalWire{}, time.Time{}, nil, err
	}
	payload, err := json.Marshal(wire)
	return payload, wire.Work, from, to, err
}

func storageDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }

func domainDigest(value string) string {
	if value == "" || strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}

func putSignal(ctx context.Context, ex Executor, tenantID uuid.UUID, value demand.DemandSignal, expectedVersion string) error {
	if tenantID == uuid.Nil {
		return refusal(CodeTenant, ErrTenant, "tenant is required")
	}
	if value.CanonicalDigest == "" {
		var err error
		value, err = demand.NewDemandSignal(value)
		if err != nil {
			return refusal(CodeInvalid, ErrInvalid, err.Error())
		}
	}
	if err := value.Validate(); err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	payload, _, from, to, err := signalPayload(value)
	if err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	existsRevision, err := revisionExists(ctx, ex, "demand_signal", tenantID, value.SignalID, value.Version)
	if err != nil {
		return err
	}
	if existsRevision {
		return refusal(CodeDuplicateRevision, ErrDuplicateRevision, "demand signal revision already exists")
	}
	actual, exists, err := currentSignalVersion(ctx, ex, tenantID, value.SignalID)
	if err != nil {
		return err
	}
	if expectedVersion != actual && (expectedVersion != "" || exists) {
		return stale(expectedVersion, actual)
	}
	if value.Version == "" {
		return refusal(CodeInvalid, ErrInvalid, "signal version is required")
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO demand_signal (
			tenant_id, row_id, signal_id, work_effective_from, work_effective_to,
			location, org_unit, quantity, unit, skill, role, priority, source,
			source_ref, confidence, confidence_class, scenario_ref, version,
			canonical_digest, work_interval)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::numeric,$9,$10,$11,$12,$13,$14,$15::numeric,$16,$17,$18,$19,$20::jsonb)
		ON CONFLICT DO NOTHING`, tenantID, uuid.New(), value.SignalID, from, to,
		value.Location, value.OrgUnit, value.Quantity.Value().String(), value.Unit, value.Skill,
		value.Role, strconv.Itoa(value.Priority), value.Source, value.SourceRef,
		nullableConfidence(value), nullableConfidenceClass(value), value.Scenario, value.Version,
		storageDigest(value.CanonicalDigest), string(payload))
	if err != nil {
		return classify(err, ErrDuplicateRevision, "insert demand signal")
	}
	if affected == 0 {
		return refusal(CodeDuplicateRevision, ErrDuplicateRevision, "demand signal revision already exists")
	}
	return nil
}

func nullableConfidence(value demand.DemandSignal) any {
	if value.Confidence.Validate() != nil {
		return nil
	}
	return value.Confidence.String()
}

func nullableConfidenceClass(value demand.DemandSignal) any {
	if value.ConfidenceClass == "" {
		return nil
	}
	return string(value.ConfidenceClass)
}

func currentSignalVersion(ctx context.Context, ex Executor, tenantID uuid.UUID, signalID string) (string, bool, error) {
	var version string
	err := ex.QueryRow(ctx, `SELECT version FROM demand_signal WHERE tenant_id=$1 AND signal_id=$2 ORDER BY version DESC LIMIT 1`, tenantID, signalID).Scan(&version)
	if errors.Is(err, dbport.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, refusal(CodeStorage, err, "find demand signal head")
	}
	return version, true, nil
}

func loadSignal(ctx context.Context, ex Executor, tenantID uuid.UUID, signalID, version string) (demand.DemandSignal, error) {
	if tenantID == uuid.Nil || signalID == "" || version == "" {
		return demand.DemandSignal{}, refusal(CodeInvalid, ErrInvalid, "tenant, signal id and version are required")
	}
	var (
		storedDigest string
		payload      []byte
		storedID     string
		storedVer    string
	)
	err := ex.QueryRow(ctx, `SELECT signal_id, version, canonical_digest, work_interval FROM demand_signal WHERE tenant_id=$1 AND signal_id=$2 AND version=$3`, tenantID, signalID, version).Scan(&storedID, &storedVer, &storedDigest, &payload)
	if errors.Is(err, dbport.ErrNoRows) {
		return demand.DemandSignal{}, refusal(CodeNotFound, ErrNotFound, "demand signal revision is absent")
	}
	if err != nil {
		return demand.DemandSignal{}, refusal(CodeStorage, err, "load demand signal")
	}
	var wire signalWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return demand.DemandSignal{}, refusal(CodeIntegrityViolation, ErrIntegrityViolation, "demand signal wire is invalid JSON")
	}
	value, err := signalFromWire(wire)
	if err != nil {
		return demand.DemandSignal{}, refusal(CodeIntegrityViolation, ErrIntegrityViolation, err.Error())
	}
	if value.SignalID != storedID || value.Version != storedVer {
		return demand.DemandSignal{}, refusal(CodeIntegrityViolation, ErrIntegrityViolation, "demand signal wire identity disagrees with row")
	}
	value, err = demand.NewDemandSignal(value)
	if err != nil || storageDigest(value.CanonicalDigest) != storedDigest {
		return demand.DemandSignal{}, refusal(CodeIntegrityViolation, ErrIntegrityViolation, "demand signal digest does not match payload")
	}
	return value, nil
}

func listSignals(ctx context.Context, ex Executor, tenantID uuid.UUID, signalID string) ([]demand.DemandSignal, error) {
	rows, err := ex.Query(ctx, `SELECT version FROM demand_signal WHERE tenant_id=$1 AND signal_id=$2 ORDER BY version`, tenantID, signalID)
	if err != nil {
		return nil, refusal(CodeStorage, err, "list demand signals")
	}
	defer rows.Close()
	var versions []string
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, refusal(CodeStorage, err, "scan demand signal version")
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, refusal(CodeStorage, err, "list demand signals")
	}
	rows.Close()
	var out []demand.DemandSignal
	for _, version := range versions {
		value, err := loadSignal(ctx, ex, tenantID, signalID, version)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil, refusal(CodeNotFound, ErrNotFound, "demand signal has no revisions")
	}
	return out, nil
}

func putCoverage(ctx context.Context, ex Executor, tenantID uuid.UUID, value demand.CoverageRequirement, expectedVersion string) error {
	if tenantID == uuid.Nil {
		return refusal(CodeTenant, ErrTenant, "tenant is required")
	}
	if value.CanonicalDigest == "" {
		return refusal(CodeInvalid, ErrInvalid, "coverage requirement canonical digest is required")
	}
	if err := value.Validate(); err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	var signalRefs []signalWire
	for _, signal := range value.Signals {
		encoded, err := signalToWire(signal)
		if err != nil {
			return refusal(CodeInvalid, ErrInvalid, err.Error())
		}
		signalRefs = append(signalRefs, encoded)
	}
	payload, err := json.Marshal(signalRefs)
	if err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	existsRevision, err := revisionExists(ctx, ex, "coverage_requirement", tenantID, value.RequirementID, value.Version)
	if err != nil {
		return err
	}
	if existsRevision {
		return refusal(CodeDuplicateRevision, ErrDuplicateRevision, "coverage requirement revision already exists")
	}
	actual, exists, err := currentCoverageVersion(ctx, ex, tenantID, value.RequirementID)
	if err != nil {
		return err
	}
	if expectedVersion != actual && (expectedVersion != "" || exists) {
		return stale(expectedVersion, actual)
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO coverage_requirement (
		tenant_id, row_id, requirement_id, scenario_ref, version,
			signal_refs, supply_refs, canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8)
		ON CONFLICT DO NOTHING`, tenantID, uuid.New(), value.RequirementID, value.Scenario, value.Version,
		string(payload), jsonArray(value.SupplyRefs), storageDigest(value.CanonicalDigest))
	if err != nil {
		return classify(err, ErrDuplicateRevision, "insert coverage requirement")
	}
	if affected == 0 {
		return refusal(CodeDuplicateRevision, ErrDuplicateRevision, "coverage requirement revision already exists")
	}
	return nil
}

func currentCoverageVersion(ctx context.Context, ex Executor, tenantID uuid.UUID, requirementID string) (string, bool, error) {
	var version string
	err := ex.QueryRow(ctx, `SELECT version FROM coverage_requirement WHERE tenant_id=$1 AND requirement_id=$2 ORDER BY version DESC LIMIT 1`, tenantID, requirementID).Scan(&version)
	if errors.Is(err, dbport.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, refusal(CodeStorage, err, "find coverage requirement head")
	}
	return version, true, nil
}

func loadCoverage(ctx context.Context, ex Executor, tenantID uuid.UUID, requirementID, version string) (demand.CoverageRequirement, error) {
	if tenantID == uuid.Nil || requirementID == "" || version == "" {
		return demand.CoverageRequirement{}, refusal(CodeInvalid, ErrInvalid, "tenant, requirement id and version are required")
	}
	var storedDigest string
	var payload, supplyPayload []byte
	var storedScenario string
	err := ex.QueryRow(ctx, `SELECT scenario_ref, canonical_digest, signal_refs, supply_refs FROM coverage_requirement WHERE tenant_id=$1 AND requirement_id=$2 AND version=$3`, tenantID, requirementID, version).Scan(&storedScenario, &storedDigest, &payload, &supplyPayload)
	if errors.Is(err, dbport.ErrNoRows) {
		return demand.CoverageRequirement{}, refusal(CodeNotFound, ErrNotFound, "coverage requirement revision is absent")
	}
	if err != nil {
		return demand.CoverageRequirement{}, refusal(CodeStorage, err, "load coverage requirement")
	}
	var signalRefs []signalWire
	if err := json.Unmarshal(payload, &signalRefs); err != nil {
		return demand.CoverageRequirement{}, refusal(CodeIntegrityViolation, ErrIntegrityViolation, "coverage requirement wire is invalid JSON")
	}
	var supplyRefs []string
	if len(supplyPayload) > 0 {
		if err := json.Unmarshal(supplyPayload, &supplyRefs); err != nil {
			return demand.CoverageRequirement{}, refusal(CodeIntegrityViolation, ErrIntegrityViolation, "coverage requirement supply refs are invalid JSON")
		}
	}
	value := demand.CoverageRequirement{RequirementID: requirementID, Scenario: storedScenario, Version: version, SupplyRefs: supplyRefs}
	for _, encoded := range signalRefs {
		signal, err := signalFromWire(encoded)
		if err != nil {
			return demand.CoverageRequirement{}, refusal(CodeIntegrityViolation, ErrIntegrityViolation, err.Error())
		}
		value.Signals = append(value.Signals, signal)
	}
	if len(value.Signals) == 0 {
		return demand.CoverageRequirement{}, refusal(CodeIntegrityViolation, ErrIntegrityViolation, "coverage requirement has no signal wire values")
	}
	value.CanonicalDigest = domainDigest(storedDigest)
	if err := value.Validate(); err != nil {
		return demand.CoverageRequirement{}, refusal(CodeIntegrityViolation, ErrIntegrityViolation, err.Error())
	}
	return value, nil
}

func listCoverages(ctx context.Context, ex Executor, tenantID uuid.UUID, requirementID string) ([]demand.CoverageRequirement, error) {
	rows, err := ex.Query(ctx, `SELECT version FROM coverage_requirement WHERE tenant_id=$1 AND requirement_id=$2 ORDER BY version`, tenantID, requirementID)
	if err != nil {
		return nil, refusal(CodeStorage, err, "list coverage requirements")
	}
	defer rows.Close()
	var versions []string
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, refusal(CodeStorage, err, "scan coverage requirement version")
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, refusal(CodeStorage, err, "list coverage requirements")
	}
	rows.Close()
	var out []demand.CoverageRequirement
	for _, version := range versions {
		value, err := loadCoverage(ctx, ex, tenantID, requirementID, version)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil, refusal(CodeNotFound, ErrNotFound, "coverage requirement has no revisions")
	}
	return out, nil
}

func putScenario(ctx context.Context, ex Executor, tenantID uuid.UUID, value scenario.ScenarioRevision, expectedRevision uint64) error {
	if tenantID == uuid.Nil {
		return refusal(CodeTenant, ErrTenant, "tenant is required")
	}
	if err := value.Validate(); err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	horizon, err := intervalToWire(value.Horizon)
	if err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	from, to, err := intervalBounds(value.Horizon)
	if err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	payload := scenarioWire{Horizon: horizon}
	for _, assumption := range value.Assumptions {
		payload.Assumptions = append(payload.Assumptions, assumptionToWire(assumption))
	}
	assumptions, err := json.Marshal(payload)
	if err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	existsRevision, err := revisionExists(ctx, ex, "scenario_revision", tenantID, value.ScenarioID, int64(value.Revision))
	if err != nil {
		return err
	}
	if existsRevision {
		return refusal(CodeDuplicateRevision, ErrDuplicateRevision, "scenario revision already exists")
	}
	actual, exists, err := currentScenarioRevision(ctx, ex, tenantID, value.ScenarioID)
	if err != nil {
		return err
	}
	if expectedRevision != actual && (expectedRevision != 0 || exists) {
		return stale(strconv.FormatUint(expectedRevision, 10), strconv.FormatUint(actual, 10))
	}
	if value.Revision == 1 && expectedRevision != 0 {
		return stale("0", strconv.FormatUint(expectedRevision, 10))
	}
	if value.Revision > 1 && (value.ParentRevision != expectedRevision || !exists) {
		return stale(strconv.FormatUint(expectedRevision, 10), strconv.FormatUint(actual, 10))
	}
	if value.Revision > 1 {
		var parentDigest string
		err := ex.QueryRow(ctx, `SELECT canonical_digest FROM scenario_revision WHERE tenant_id=$1 AND scenario_id=$2 AND revision=$3`, tenantID, value.ScenarioID, int64(value.ParentRevision)).Scan(&parentDigest)
		if errors.Is(err, dbport.ErrNoRows) {
			return stale(strconv.FormatUint(value.ParentRevision, 10), "missing")
		}
		if err != nil {
			return refusal(CodeStorage, err, "load scenario parent")
		}
		if parentDigest != storageDigest(value.ParentDigest) {
			return stale(storageDigest(value.ParentDigest), parentDigest)
		}
	}
	var parentRevision any
	var parentDigest any
	if value.ParentRevision != 0 {
		parentRevision = int64(value.ParentRevision)
		parentDigest = storageDigest(value.ParentDigest)
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO scenario_revision (
			tenant_id, row_id, scenario_id, revision, parent_revision, parent_digest,
			owner, scope, horizon_from, horizon_to, assumptions, baseline_snapshot_ref,
			author, authority_disclaimer, lifecycle, canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13,$14,$15,$16)
		ON CONFLICT DO NOTHING`, tenantID, uuid.New(), value.ScenarioID, int64(value.Revision), parentRevision,
		parentDigest, value.Owner, value.Scope, from, to, string(assumptions), value.BaselineSnapshotRef,
		value.Author, value.AuthorityDisclaimer, string(value.Lifecycle), storageDigest(value.CanonicalDigest))
	if err != nil {
		return classify(err, ErrDuplicateRevision, "insert scenario revision")
	}
	if affected == 0 {
		return refusal(CodeDuplicateRevision, ErrDuplicateRevision, "scenario revision already exists")
	}
	return nil
}

func currentScenarioRevision(ctx context.Context, ex Executor, tenantID uuid.UUID, scenarioID string) (uint64, bool, error) {
	var revision int64
	err := ex.QueryRow(ctx, `SELECT revision FROM scenario_revision WHERE tenant_id=$1 AND scenario_id=$2 ORDER BY revision DESC LIMIT 1`, tenantID, scenarioID).Scan(&revision)
	if errors.Is(err, dbport.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, refusal(CodeStorage, err, "find scenario head")
	}
	if revision < 0 {
		return 0, false, refusal(CodeIntegrityViolation, ErrIntegrityViolation, "negative scenario revision")
	}
	return uint64(revision), true, nil
}

func loadScenario(ctx context.Context, ex Executor, tenantID uuid.UUID, scenarioID string, revision uint64) (scenario.ScenarioRevision, error) {
	if tenantID == uuid.Nil || scenarioID == "" || revision == 0 {
		return scenario.ScenarioRevision{}, refusal(CodeInvalid, ErrInvalid, "tenant, scenario id and positive revision are required")
	}
	var (
		storedDigest, assumptionsJSON                                   string
		storedID, owner, scope, baseline, author, disclaimer, lifecycle string
		parentRevision                                                  *int64
		parentDigest                                                    *string
		from, to                                                        *time.Time
	)
	err := ex.QueryRow(ctx, `
		SELECT scenario_id, parent_revision, parent_digest, owner, scope,
			horizon_from, horizon_to, assumptions::text, baseline_snapshot_ref,
			author, authority_disclaimer, lifecycle, canonical_digest
		FROM scenario_revision
		WHERE tenant_id=$1 AND scenario_id=$2 AND revision=$3`, tenantID, scenarioID, int64(revision)).Scan(
		&storedID, &parentRevision, &parentDigest, &owner, &scope, &from, &to, &assumptionsJSON,
		&baseline, &author, &disclaimer, &lifecycle, &storedDigest)
	if errors.Is(err, dbport.ErrNoRows) {
		return scenario.ScenarioRevision{}, refusal(CodeNotFound, ErrNotFound, "scenario revision is absent")
	}
	if err != nil {
		return scenario.ScenarioRevision{}, refusal(CodeStorage, err, "load scenario revision")
	}
	var wire scenarioWire
	if err := json.Unmarshal([]byte(assumptionsJSON), &wire); err != nil {
		return scenario.ScenarioRevision{}, refusal(CodeIntegrityViolation, ErrIntegrityViolation, "scenario assumptions are invalid JSON")
	}
	horizon, err := intervalFromWire(wire.Horizon)
	if err != nil {
		return scenario.ScenarioRevision{}, refusal(CodeIntegrityViolation, ErrIntegrityViolation, err.Error())
	}
	value := scenario.ScenarioRevision{
		ScenarioID: storedID, Revision: revision, Owner: owner, Scope: scope, Horizon: horizon,
		BaselineSnapshotRef: baseline, Author: author, AuthorityDisclaimer: disclaimer,
		Lifecycle: scenario.Lifecycle(lifecycle), CanonicalDigest: domainDigest(storedDigest),
	}
	if parentRevision != nil {
		if *parentRevision < 0 {
			return scenario.ScenarioRevision{}, refusal(CodeIntegrityViolation, ErrIntegrityViolation, "negative parent revision")
		}
		value.ParentRevision = uint64(*parentRevision)
	}
	if parentDigest != nil {
		value.ParentDigest = domainDigest(*parentDigest)
	}
	for _, encoded := range wire.Assumptions {
		assumption, err := assumptionFromWire(encoded)
		if err != nil {
			return scenario.ScenarioRevision{}, refusal(CodeIntegrityViolation, ErrIntegrityViolation, err.Error())
		}
		value.Assumptions = append(value.Assumptions, assumption)
	}
	if from == nil || (value.Horizon.IsOpenEnded() && to != nil) {
		return scenario.ScenarioRevision{}, refusal(CodeIntegrityViolation, ErrIntegrityViolation, "scenario horizon projection is inconsistent")
	}
	validated, err := scenario.NewScenarioRevision(value)
	if err != nil || storageDigest(validated.CanonicalDigest) != storedDigest {
		return scenario.ScenarioRevision{}, refusal(CodeIntegrityViolation, ErrIntegrityViolation, "scenario digest does not match payload")
	}
	return validated, nil
}

func listScenarios(ctx context.Context, ex Executor, tenantID uuid.UUID, scenarioID string) ([]scenario.ScenarioRevision, error) {
	rows, err := ex.Query(ctx, `SELECT revision FROM scenario_revision WHERE tenant_id=$1 AND scenario_id=$2 ORDER BY revision`, tenantID, scenarioID)
	if err != nil {
		return nil, refusal(CodeStorage, err, "list scenario revisions")
	}
	defer rows.Close()
	var revisions []int64
	for rows.Next() {
		var revision int64
		if err := rows.Scan(&revision); err != nil {
			return nil, refusal(CodeStorage, err, "scan scenario revision")
		}
		revisions = append(revisions, revision)
	}
	if err := rows.Err(); err != nil {
		return nil, refusal(CodeStorage, err, "list scenario revisions")
	}
	rows.Close()
	var out []scenario.ScenarioRevision
	for _, revision := range revisions {
		value, err := loadScenario(ctx, ex, tenantID, scenarioID, uint64(revision))
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil, refusal(CodeNotFound, ErrNotFound, "scenario has no revisions")
	}
	return out, nil
}

func classify(err, duplicate error, detail string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return refusal(CodeDuplicateRevision, duplicate, detail)
		case "23503", "23514", "42501":
			return refusal(CodeInvalid, ErrInvalid, detail+": "+pgErr.Message)
		}
	}
	return refusal(CodeStorage, err, detail)
}

func revisionExists(ctx context.Context, ex Executor, table string, tenantID uuid.UUID, identity string, revision any) (bool, error) {
	var exists bool
	var err error
	switch table {
	case "demand_signal":
		err = ex.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM demand_signal WHERE tenant_id=$1 AND signal_id=$2 AND version=$3)`, tenantID, identity, revision).Scan(&exists)
	case "coverage_requirement":
		err = ex.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM coverage_requirement WHERE tenant_id=$1 AND requirement_id=$2 AND version=$3)`, tenantID, identity, revision).Scan(&exists)
	case "scenario_revision":
		err = ex.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM scenario_revision WHERE tenant_id=$1 AND scenario_id=$2 AND revision=$3)`, tenantID, identity, revision).Scan(&exists)
	default:
		return false, refusal(CodeInvalid, ErrInvalid, "unknown planning revision table")
	}
	if err != nil {
		return false, refusal(CodeStorage, err, "check planning revision identity")
	}
	return exists, nil
}

func jsonArray(values []string) string {
	if values == nil {
		return "[]"
	}
	b, _ := json.Marshal(values)
	return string(b)
}
