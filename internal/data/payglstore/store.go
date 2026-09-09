// Package payglstore persists the immutable paygl and labor revisions and the
// append-only mapping-result evidence created by migration 00047.
package payglstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/labor"
	paygldomain "github.com/monstercameron/human-capital-management-suite/internal/domains/paygl"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DB is the transaction-opening capability used by Store. The caller supplies
// a connection already able to SET ROLE hcmnext_app when RLS is to be tested.
type DB interface{ dbport.Beginner }

// Store implements [paygl.Store] over migration 00047. It opens a short
// transaction for each operation and scopes it with [tenancy.WithTenant], so
// no tenant setting can leak to a later operation on a pooled connection.
type Store struct{ db DB }

// New returns a PostgreSQL-backed paygl store.
func New(db DB) *Store { return &Store{db: db} }

var _ paygldomain.Store = (*Store)(nil)

func (s *Store) withTenant(ctx context.Context, tenant string, fn func(dbport.Tx) error) error {
	tenantID, err := uuid.Parse(tenant)
	if err != nil || tenantID == uuid.Nil {
		return fmt.Errorf("payglstore: tenant %q is not a non-nil UUID", tenant)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("payglstore: begin transaction: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("payglstore: commit transaction: %w", err)
	}
	return nil
}

func readTenant(tenant string) (uuid.UUID, error) {
	tenantID, err := uuid.Parse(tenant)
	if err != nil || tenantID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("payglstore: tenant %q is not a non-nil UUID", tenant)
	}
	return tenantID, nil
}

func conflict(code, key string, cause error) error {
	return paygldomain.ConflictError{Code: code, Key: key, Err: cause}
}

func duplicateFrom(err error, key string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return conflict(paygldomain.ConflictDuplicateRevision, key, paygldomain.ErrPersistenceDuplicate)
	}
	return err
}

// PutLaborRule inserts one immutable labor rule revision.
func (s *Store) PutLaborRule(ctx context.Context, tenant string, rule labor.LaborRule) error {
	if err := rule.Validate(); err != nil {
		return err
	}
	tenantID, err := readTenant(tenant)
	if err != nil {
		return err
	}
	dimensions, err := marshalLaborDimensions(rule)
	if err != nil {
		return err
	}
	from, to, err := intervalBounds(rule.Effective)
	if err != nil {
		return err
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		if err := requireLaborPredecessor(ctx, tx, tenantID, rule); err != nil {
			return err
		}
		affected, err := tx.Exec(ctx, `
			INSERT INTO labor_rule (
				row_id, tenant_id, rule_id, version, effective_from, effective_to,
				dimensions, base_rate, differential_rate, overtime_multiplier,
				employer_burden_rate, supersedes, canonical_digest)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::numeric, $9::numeric,
				$10::numeric, $11::numeric, $12, $13)
			ON CONFLICT DO NOTHING`,
			uuid.New(), tenantID, rule.ID, rule.Version, from, to, dimensions,
			rule.BaseRate.String(), rule.DifferentialRate.String(),
			rule.OvertimeMultiplier.String(), rule.EmployerBurdenRate.String(),
			nilIfEmpty(rule.Supersedes), storedDigest(rule.CanonicalDigest))
		if err != nil {
			return duplicateFrom(err, rule.ID+"/"+rule.Version)
		}
		if affected == 0 {
			return conflict(paygldomain.ConflictDuplicateRevision, rule.ID+"/"+rule.Version, paygldomain.ErrPersistenceDuplicate)
		}
		return nil
	})
}

func requireLaborPredecessor(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, rule labor.LaborRule) error {
	if rule.Supersedes == "" {
		return nil
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM labor_rule WHERE tenant_id=$1 AND rule_id=$2 AND version=$3)`, tenantID, rule.ID, rule.Supersedes).Scan(&exists); err != nil {
		return fmt.Errorf("payglstore: check labor predecessor: %w", err)
	}
	if !exists {
		return conflict(paygldomain.ConflictStaleRevision, rule.ID+"/"+rule.Version, paygldomain.ErrPersistenceVersionConflict)
	}
	return nil
}

// GetLaborRule loads one immutable labor rule revision.
func (s *Store) GetLaborRule(ctx context.Context, tenant, ruleID, version string) (labor.LaborRule, error) {
	tenantID, err := readTenant(tenant)
	if err != nil {
		return labor.LaborRule{}, err
	}
	var out labor.LaborRule
	err = s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		var (
			from, to         *time.Time
			dimensions       []byte
			base, diff       string
			overtime, burden string
			supersedes       *string
		)
		err := tx.QueryRow(ctx, `
			SELECT effective_from, effective_to, dimensions, base_rate::text,
				differential_rate::text, overtime_multiplier::text,
				employer_burden_rate::text, supersedes, canonical_digest
			FROM labor_rule
			WHERE tenant_id=$1 AND rule_id=$2 AND version=$3`, tenantID, ruleID, version).Scan(
			&from, &to, &dimensions, &base, &diff, &overtime, &burden,
			&supersedes, &out.CanonicalDigest)
		if err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return fmt.Errorf("%w: labor_rule %s/%s", paygldomain.ErrPersistenceNotFound, ruleID, version)
			}
			return fmt.Errorf("payglstore: load labor rule: %w", err)
		}
		out.ID, out.Version = ruleID, version
		if supersedes != nil {
			out.Supersedes = *supersedes
		}
		out.CanonicalDigest = domainDigest(out.CanonicalDigest)
		if err := unmarshalLaborDimensions(dimensions, &out); err != nil {
			return err
		}
		iv, err := intervalFromBounds(from, to, out.Effective, dimensions)
		if err != nil {
			return err
		}
		out.Effective = iv
		out.BaseRate, err = decimalFromStored(base, dimensions, "base_rate")
		if err != nil {
			return err
		}
		out.DifferentialRate, err = decimalFromStored(diff, dimensions, "differential_rate")
		if err != nil {
			return err
		}
		out.OvertimeMultiplier, err = decimalFromStored(overtime, dimensions, "overtime_multiplier")
		if err != nil {
			return err
		}
		out.EmployerBurdenRate, err = decimalFromStored(burden, dimensions, "employer_burden_rate")
		return err
	})
	return out, err
}

// PutAccountingRule inserts one immutable payroll-to-GL rule revision.
func (s *Store) PutAccountingRule(ctx context.Context, tenant string, rule paygldomain.AccountingRule) error {
	if err := rule.Validate(); err != nil {
		return err
	}
	tenantID, err := readTenant(tenant)
	if err != nil {
		return err
	}
	dimensions, err := marshalAccountingDimensions(rule)
	if err != nil {
		return err
	}
	from, to, err := intervalBounds(rule.Effective)
	if err != nil {
		return err
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		if err := requireAccountingPredecessor(ctx, tx, tenantID, rule); err != nil {
			return err
		}
		affected, err := tx.Exec(ctx, `
			INSERT INTO paygl_accounting_rule (
				row_id, tenant_id, rule_id, version, effective_from, effective_to,
				component_kind, component_code, dimensions, debit_account,
				credit_account, supersedes, canonical_digest)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			ON CONFLICT DO NOTHING`,
			uuid.New(), tenantID, rule.ID, rule.Version, from, to,
			rule.ComponentKind, accountingCode(rule), dimensions,
			accountingDebit(rule), accountingCredit(rule), nilIfEmpty(rule.Supersedes), storedDigest(rule.CanonicalDigest))
		if err != nil {
			return duplicateFrom(err, rule.ID+"/"+rule.Version)
		}
		if affected == 0 {
			return conflict(paygldomain.ConflictDuplicateRevision, rule.ID+"/"+rule.Version, paygldomain.ErrPersistenceDuplicate)
		}
		return nil
	})
}

func requireAccountingPredecessor(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, rule paygldomain.AccountingRule) error {
	if rule.Supersedes == "" {
		return nil
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM paygl_accounting_rule WHERE tenant_id=$1 AND rule_id=$2 AND version=$3)`, tenantID, rule.ID, rule.Supersedes).Scan(&exists); err != nil {
		return fmt.Errorf("payglstore: check accounting predecessor: %w", err)
	}
	if !exists {
		return conflict(paygldomain.ConflictStaleRevision, rule.ID+"/"+rule.Version, paygldomain.ErrPersistenceVersionConflict)
	}
	return nil
}

// GetAccountingRule loads one immutable payroll-to-GL rule revision.
func (s *Store) GetAccountingRule(ctx context.Context, tenant, ruleID, version string) (paygldomain.AccountingRule, error) {
	tenantID, err := readTenant(tenant)
	if err != nil {
		return paygldomain.AccountingRule{}, err
	}
	var out paygldomain.AccountingRule
	err = s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		var from, to *time.Time
		var dimensions []byte
		var supersedes *string
		err := tx.QueryRow(ctx, `
			SELECT effective_from, effective_to, component_kind, component_code,
				dimensions, debit_account, credit_account, supersedes, canonical_digest
			FROM paygl_accounting_rule
			WHERE tenant_id=$1 AND rule_id=$2 AND version=$3`, tenantID, ruleID, version).Scan(
			&from, &to, &out.ComponentKind, &out.ComponentCode, &dimensions,
			&out.DebitAccount, &out.CreditAccount, &supersedes, &out.CanonicalDigest)
		if err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return fmt.Errorf("%w: paygl_accounting_rule %s/%s", paygldomain.ErrPersistenceNotFound, ruleID, version)
			}
			return fmt.Errorf("payglstore: load accounting rule: %w", err)
		}
		out.ID, out.Version = ruleID, version
		if supersedes != nil {
			out.Supersedes = *supersedes
		}
		out.CanonicalDigest = domainDigest(out.CanonicalDigest)
		if err := unmarshalAccountingDimensions(dimensions, &out); err != nil {
			return err
		}
		out.Effective, err = intervalFromBounds(from, to, out.Effective, dimensions)
		return err
	})
	return out, err
}

// AppendMappingResult appends immutable mapping evidence for one run.
func (s *Store) AppendMappingResult(ctx context.Context, tenant string, result paygldomain.MappingResult, eventSequence uint64) error {
	if eventSequence == 0 {
		return fmt.Errorf("payglstore: event sequence must be positive")
	}
	if err := result.Validate(); err != nil {
		return err
	}
	tenantID, err := readTenant(tenant)
	if err != nil {
		return err
	}
	mappings, err := marshalMappingResult(result)
	if err != nil {
		return err
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		affected, err := tx.Exec(ctx, `
			INSERT INTO paygl_mapping_result (
				row_id, tenant_id, run_id, run_revision, run_digest, mappings,
				digest, event_sequence)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT DO NOTHING`,
			uuid.New(), tenantID, result.RunID, int64(result.RunRevision), storedDigestOrNil(result.RunDigest),
			mappings, storedDigest(result.Digest), int64(eventSequence))
		if err != nil {
			return duplicateFrom(err, result.RunID+fmt.Sprintf("/%d", eventSequence))
		}
		if affected == 0 {
			return conflict(paygldomain.ConflictDuplicateRevision, result.RunID+fmt.Sprintf("/%d", eventSequence), paygldomain.ErrPersistenceDuplicate)
		}
		return nil
	})
}

// GetMappingResult loads one append-only mapping result event.
func (s *Store) GetMappingResult(ctx context.Context, tenant, runID string, eventSequence uint64) (paygldomain.MappingResult, error) {
	tenantID, err := readTenant(tenant)
	if err != nil {
		return paygldomain.MappingResult{}, err
	}
	var out paygldomain.MappingResult
	err = s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		var mappings []byte
		var runRevision int64
		var runDigest, digest *string
		err := tx.QueryRow(ctx, `
			SELECT run_revision, run_digest, mappings, digest
			FROM paygl_mapping_result
			WHERE tenant_id=$1 AND run_id=$2 AND event_sequence=$3`, tenantID, runID, int64(eventSequence)).Scan(
			&runRevision, &runDigest, &mappings, &digest)
		if err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return fmt.Errorf("%w: paygl_mapping_result %s/%d", paygldomain.ErrPersistenceNotFound, runID, eventSequence)
			}
			return fmt.Errorf("payglstore: load mapping result: %w", err)
		}
		if runDigest == nil || digest == nil {
			return fmt.Errorf("payglstore: mapping result has null digest")
		}
		if err := unmarshalMappingResult(mappings, &out); err != nil {
			return err
		}
		out.RunID, out.RunRevision, out.RunDigest, out.Digest = runID, uint64(runRevision), domainDigest(*runDigest), domainDigest(*digest)
		computed, err := out.DigestValue()
		if err != nil {
			return err
		}
		if computed != out.Digest {
			return fmt.Errorf("payglstore: mapping result digest mismatch: got %s want %s", out.Digest, computed)
		}
		return nil
	})
	return out, err
}

func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func storedDigest(value string) string {
	return strings.TrimPrefix(value, "sha256:")
}

func storedDigestOrNil(value string) any {
	if value == "" {
		return nil
	}
	return storedDigest(value)
}

func domainDigest(value string) string {
	if value == "" || strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}
func accountingCode(rule paygldomain.AccountingRule) string {
	if rule.ComponentCode != "" {
		return rule.ComponentCode
	}
	return rule.Code
}
func accountingDebit(rule paygldomain.AccountingRule) string {
	if rule.DebitAccount != "" {
		return rule.DebitAccount
	}
	return rule.DebitAccountRef
}
func accountingCredit(rule paygldomain.AccountingRule) string {
	if rule.CreditAccount != "" {
		return rule.CreditAccount
	}
	return rule.CreditAccountRef
}

type dimensionWire struct {
	Kind    string `json:"kind"`
	Value   string `json:"value"`
	Version string `json:"version"`
}
type decimalWire struct {
	Text     string `json:"text"`
	Scale    int32  `json:"scale"`
	Rounding string `json:"rounding"`
}
type intervalWire struct {
	Kind            string `json:"kind"`
	CalendarRef     string `json:"calendar_ref,omitempty"`
	CalendarVersion string `json:"calendar_version,omitempty"`
	RateScale       int32  `json:"rate_scale,omitempty"`
	RateRounding    string `json:"rate_rounding,omitempty"`
}
type laborDimensionsWire struct {
	Dimensions []dimensionWire        `json:"dimensions"`
	Interval   intervalWire           `json:"interval"`
	Rates      map[string]decimalWire `json:"rates"`
}
type accountingDimensionsWire struct {
	Dimensions []dimensionWire `json:"dimensions"`
	Interval   intervalWire    `json:"interval"`
}

func toDimensionWire(d labor.Dimension) dimensionWire {
	return dimensionWire{Kind: string(d.Kind), Value: d.Value, Version: d.Version}
}
func fromDimensionWire(d dimensionWire) labor.Dimension {
	return labor.Dimension{Kind: labor.DimensionKind(d.Kind), Value: d.Value, Version: d.Version}
}
func decimalToWire(d values.Decimal) decimalWire {
	return decimalWire{Text: d.String(), Scale: d.Scale(), Rounding: d.Rounding().String()}
}
func decimalFromWire(d decimalWire) (values.Decimal, error) {
	mode, err := values.ParseRoundingMode(d.Rounding)
	if err != nil {
		return values.Decimal{}, err
	}
	return values.NewDecimal(d.Text, d.Scale, mode)
}

func intervalWireFor(iv values.EffectiveInterval) (intervalWire, error) {
	if err := iv.Validate(); err != nil {
		return intervalWire{}, err
	}
	w := intervalWire{Kind: iv.Kind().String()}
	if iv.Kind() == values.IntervalKindLocalDate {
		w.CalendarRef, w.CalendarVersion = iv.Calendar().Ref, iv.Calendar().Version
	}
	return w, nil
}

func marshalLaborDimensions(rule labor.LaborRule) ([]byte, error) {
	iw, err := intervalWireFor(rule.Effective)
	if err != nil {
		return nil, err
	}
	return json.Marshal(laborDimensionsWire{Dimensions: mapDimensionKinds(rule.Dimensions), Interval: iw, Rates: map[string]decimalWire{
		"base_rate": decimalToWire(rule.BaseRate), "differential_rate": decimalToWire(rule.DifferentialRate), "overtime_multiplier": decimalToWire(rule.OvertimeMultiplier), "employer_burden_rate": decimalToWire(rule.EmployerBurdenRate),
	}})
}

func mapDimensionKinds(in []labor.DimensionKind) []dimensionWire {
	out := make([]dimensionWire, 0, len(in))
	for _, kind := range in {
		out = append(out, dimensionWire{Kind: string(kind)})
	}
	return out
}
func mapDimensions(in []labor.Dimension) []dimensionWire {
	out := make([]dimensionWire, 0, len(in))
	for _, d := range in {
		out = append(out, toDimensionWire(d))
	}
	return out
}

func unmarshalLaborDimensions(raw []byte, rule *labor.LaborRule) error {
	var w laborDimensionsWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return fmt.Errorf("payglstore: decode labor dimensions: %w", err)
	}
	rule.Dimensions = make([]labor.DimensionKind, 0, len(w.Dimensions))
	for _, d := range w.Dimensions {
		rule.Dimensions = append(rule.Dimensions, labor.DimensionKind(d.Kind))
	}
	return nil
}

func marshalAccountingDimensions(rule paygldomain.AccountingRule) ([]byte, error) {
	iw, err := intervalWireFor(rule.Effective)
	if err != nil {
		return nil, err
	}
	return json.Marshal(accountingDimensionsWire{Dimensions: mapDimensions(accountingDimensions(rule)), Interval: iw})
}

func accountingDimensions(rule paygldomain.AccountingRule) []labor.Dimension {
	if len(rule.Dimensions) > 0 {
		return rule.Dimensions
	}
	if len(rule.LaborDimensions) > 0 {
		return rule.LaborDimensions
	}
	return []labor.Dimension{rule.Dimension}
}

func unmarshalAccountingDimensions(raw []byte, rule *paygldomain.AccountingRule) error {
	var w accountingDimensionsWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return fmt.Errorf("payglstore: decode accounting dimensions: %w", err)
	}
	rule.Dimensions = make([]labor.Dimension, 0, len(w.Dimensions))
	for _, d := range w.Dimensions {
		rule.Dimensions = append(rule.Dimensions, fromDimensionWire(d))
	}
	if len(rule.Dimensions) == 1 {
		rule.Dimension = rule.Dimensions[0]
	}
	return nil
}

func intervalBounds(iv values.EffectiveInterval) (*time.Time, *time.Time, error) {
	if err := iv.Validate(); err != nil {
		return nil, nil, err
	}
	if start, ok := iv.StartInstant(); ok {
		from := start.Time()
		var to *time.Time
		if end, has := iv.EndInstant(); has {
			value := end.Time()
			to = &value
		}
		return &from, to, nil
	}
	start, _ := iv.StartDate()
	from := time.Date(int(start.Year()), start.Month(), int(start.Day()), 0, 0, 0, 0, time.UTC)
	var to *time.Time
	if end, has := iv.EndDate(); has {
		value := time.Date(int(end.Year()), end.Month(), int(end.Day()), 0, 0, 0, 0, time.UTC)
		to = &value
	}
	return &from, to, nil
}

func intervalFromBounds(from, to *time.Time, current values.EffectiveInterval, raw []byte) (values.EffectiveInterval, error) {
	if from == nil {
		return values.EffectiveInterval{}, fmt.Errorf("payglstore: effective_from is required")
	}
	var meta struct {
		Interval intervalWire `json:"interval"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return values.EffectiveInterval{}, err
	}
	if meta.Interval.Kind == values.IntervalKindInstant.String() {
		start, err := values.NewInstantFromUnix(from.Unix(), int32(from.Nanosecond()))
		if err != nil {
			return values.EffectiveInterval{}, err
		}
		if to == nil {
			return values.NewOpenInstantInterval(start)
		}
		end, err := values.NewInstantFromUnix(to.Unix(), int32(to.Nanosecond()))
		if err != nil {
			return values.EffectiveInterval{}, err
		}
		return values.NewInstantInterval(start, end)
	}
	start, err := values.NewLocalDate(from.Year(), from.Month(), from.Day())
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	calendar := values.CalendarRef{Ref: meta.Interval.CalendarRef, Version: meta.Interval.CalendarVersion}
	if to == nil {
		return values.NewOpenLocalDateInterval(start, calendar)
	}
	end, err := values.NewLocalDate(to.Year(), to.Month(), to.Day())
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	return values.NewLocalDateInterval(start, end, calendar)
}

func decimalFromStored(text string, raw []byte, field string) (values.Decimal, error) {
	var w laborDimensionsWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return values.Decimal{}, err
	}
	if value, ok := w.Rates[field]; ok {
		return decimalFromWire(value)
	}
	mode := values.RoundingExactRequired
	return values.NewDecimal(strings.TrimSpace(text), 6, mode)
}

type mappingWire struct {
	ComponentID      string        `json:"component_id"`
	ComponentOrdinal int           `json:"component_ordinal"`
	ComponentKind    string        `json:"component_kind"`
	ComponentCode    string        `json:"component_code"`
	ComponentAmount  decimalWire   `json:"component_amount"`
	EffectiveDate    string        `json:"effective_date,omitempty"`
	SplitOrdinal     int           `json:"split_ordinal"`
	Percent          decimalWire   `json:"percent"`
	Amount           decimalWire   `json:"amount"`
	Currency         string        `json:"currency"`
	Dimension        dimensionWire `json:"dimension"`
	DebitAccount     string        `json:"debit_account"`
	CreditAccount    string        `json:"credit_account"`
	RuleID           string        `json:"rule_id"`
	RuleVersion      string        `json:"rule_version"`
	RuleDigest       string        `json:"rule_digest"`
	SourceDigest     string        `json:"source_digest"`
}
type mappingResultWire struct {
	Mappings             []mappingWire `json:"mappings"`
	TotalComponentAmount decimalWire   `json:"total_component_amount"`
	TotalSplitAmount     decimalWire   `json:"total_split_amount"`
}

func marshalMappingResult(result paygldomain.MappingResult) ([]byte, error) {
	w := mappingResultWire{TotalComponentAmount: decimalToWire(result.TotalComponentAmount), TotalSplitAmount: decimalToWire(result.TotalSplitAmount), Mappings: make([]mappingWire, 0, len(result.Mappings))}
	for _, m := range result.Mappings {
		date := ""
		if m.EffectiveDate.IsSet() {
			date = m.EffectiveDate.String()
		}
		w.Mappings = append(w.Mappings, mappingWire{m.ComponentID, m.ComponentOrdinal, string(m.ComponentKind), m.ComponentCode, decimalToWire(m.ComponentAmount), date, m.SplitOrdinal, decimalToWire(m.Percent), decimalToWire(m.Amount), m.Currency, toDimensionWire(m.Dimension), m.DebitAccount, m.CreditAccount, m.RuleID, m.RuleVersion, m.RuleDigest, m.SourceDigest})
	}
	return json.Marshal(w)
}

func unmarshalMappingResult(raw []byte, result *paygldomain.MappingResult) error {
	var w mappingResultWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return fmt.Errorf("payglstore: decode mappings: %w", err)
	}
	var err error
	result.TotalComponentAmount, err = decimalFromWire(w.TotalComponentAmount)
	if err != nil {
		return err
	}
	result.TotalSplitAmount, err = decimalFromWire(w.TotalSplitAmount)
	if err != nil {
		return err
	}
	result.Mappings = make([]paygldomain.ComponentMapping, 0, len(w.Mappings))
	for _, m := range w.Mappings {
		componentAmount, e := decimalFromWire(m.ComponentAmount)
		if e != nil {
			return e
		}
		percent, e := decimalFromWire(m.Percent)
		if e != nil {
			return e
		}
		amount, e := decimalFromWire(m.Amount)
		if e != nil {
			return e
		}
		var date values.LocalDate
		if m.EffectiveDate != "" {
			date, e = values.ParseLocalDate(m.EffectiveDate)
			if e != nil {
				return e
			}
		}
		result.Mappings = append(result.Mappings, paygldomain.ComponentMapping{ComponentID: m.ComponentID, ComponentOrdinal: m.ComponentOrdinal, ComponentKind: paygldomain.ComponentKind(m.ComponentKind), ComponentCode: m.ComponentCode, ComponentAmount: componentAmount, EffectiveDate: date, SplitOrdinal: m.SplitOrdinal, Percent: percent, Amount: amount, Currency: m.Currency, Dimension: fromDimensionWire(m.Dimension), DebitAccount: m.DebitAccount, CreditAccount: m.CreditAccount, RuleID: m.RuleID, RuleVersion: m.RuleVersion, RuleDigest: m.RuleDigest, SourceDigest: m.SourceDigest})
	}
	return nil
}
