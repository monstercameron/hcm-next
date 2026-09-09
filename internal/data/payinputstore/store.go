// Package payinputstore persists payinput definition revisions and immutable
// worker assignments from migrations/00104_payinput.sql.
package payinputstore

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/payinput"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DB is the transaction-opening capability used by Store.
type DB interface{ dbport.Beginner }

// Store implements [payinput.Store] over PostgreSQL. Every operation owns a
// short transaction and establishes the RLS tenant setting before touching a
// tenant-scoped row.
type Store struct{ db DB }

var _ payinput.Store = (*Store)(nil)

// New returns a PostgreSQL-backed pay-input store.
func New(db DB) *Store { return &Store{db: db} }

type definitionBasisWire struct {
	Basis              string `json:"basis"`
	Currency           string `json:"currency"`
	FormulaRef         string `json:"formula_ref"`
	AccountingCode     string `json:"accounting_code"`
	OwnerRef           string `json:"owner_ref"`
	EffectiveCanonical string `json:"effective_canonical"`
}

type limitsWire struct {
	Minimum *string `json:"minimum,omitempty"`
	Maximum *string `json:"maximum,omitempty"`
}

func invalid(detail string) error {
	return &payinput.StoreError{Code: payinput.StoreInvalidCode, Detail: detail}
}

func notFound(detail string) error {
	return &payinput.StoreError{Code: payinput.StoreNotFoundCode, Detail: detail}
}

func duplicate(detail string) error {
	return &payinput.StoreError{Code: payinput.StoreDuplicateCode, Detail: detail}
}

func stale(expected, actual, detail string) error {
	return &payinput.StoreError{Code: payinput.StoreStaleCASCode, Expected: expected, Actual: actual, Detail: detail}
}

func reference(detail string) error {
	return &payinput.StoreError{Code: payinput.StoreReferenceCode, Detail: detail}
}

func (s *Store) withTenant(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if s == nil || s.db == nil {
		return invalid("database capability is required")
	}
	if tenantID == uuid.Nil {
		return invalid("tenant id is required")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("payinputstore: begin transaction: %w", err)
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
		_ = tx.Rollback(ctx)
		return fmt.Errorf("payinputstore: commit transaction: %w", err)
	}
	return nil
}

func parseTenant(tenantID string) (uuid.UUID, error) {
	id, err := uuid.Parse(tenantID)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, invalid(fmt.Sprintf("tenant id %q is not a non-nil UUID", tenantID))
	}
	return id, nil
}

// SaveDefinition appends a definition revision. expectedRevision is zero for
// an initial revision and otherwise must name the current head.
func (s *Store) SaveDefinition(ctx context.Context, tenantID string, definition payinput.Definition, expectedRevision uint64) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	if definition.CanonicalDigest == "" {
		definition, err = payinput.NewDefinition(definition)
	} else {
		err = definition.Validate()
	}
	if err != nil {
		return invalid(err.Error())
	}
	definitionID := definition.DefinitionID
	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		if err := lockDefinition(ctx, tx, tid, definitionID); err != nil {
			return err
		}
		current, hasCurrent, err := currentDefinitionTx(ctx, tx, tid, definitionID)
		if err != nil {
			return err
		}
		if exists, err := definitionRevisionExists(ctx, tx, tid, definitionID, definition.Revision); err != nil {
			return err
		} else if exists {
			return duplicate(fmt.Sprintf("definition %s/%d", definitionID, definition.Revision))
		}
		if expectedRevision == 0 {
			if hasCurrent {
				return stale("", fmt.Sprint(current.Revision), "definition already has a current revision")
			}
			if definition.Revision != 1 || definition.SupersedesRevision != 0 || definition.SupersedesDigest != "" {
				return invalid("initial definition must be revision 1 without a predecessor")
			}
		} else {
			if !hasCurrent || current.Revision != expectedRevision || definition.Revision != expectedRevision+1 || definition.SupersedesRevision != expectedRevision || definition.SupersedesDigest != current.CanonicalDigest {
				actual := ""
				if hasCurrent {
					actual = fmt.Sprint(current.Revision)
				}
				return stale(fmt.Sprint(expectedRevision), actual, "definition predecessor is not the current head")
			}
		}
		return insertDefinition(ctx, tx, tid, definition)
	})
}

func lockDefinition(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, definitionID string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, tenantID.String()+":"+definitionID); err != nil {
		return fmt.Errorf("payinputstore: lock definition %s: %w", definitionID, err)
	}
	return nil
}

func definitionRevisionExists(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, definitionID string, revision uint64) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM payinput_definition WHERE tenant_id=$1 AND definition_id=$2 AND revision=$3)`, tenantID, definitionID, int64(revision)).Scan(&exists); err != nil {
		return false, fmt.Errorf("payinputstore: check definition revision: %w", err)
	}
	return exists, nil
}

func currentDefinitionTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, definitionID string) (payinput.Definition, bool, error) {
	var revision uint64
	err := tx.QueryRow(ctx, `SELECT revision FROM payinput_definition WHERE tenant_id=$1 AND definition_id=$2 ORDER BY revision DESC LIMIT 1`, tenantID, definitionID).Scan(&revision)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return payinput.Definition{}, false, nil
		}
		return payinput.Definition{}, false, fmt.Errorf("payinputstore: find definition head: %w", err)
	}
	definition, err := loadDefinitionTx(ctx, tx, tenantID, definitionID, revision)
	return definition, true, err
}

func insertDefinition(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, definition payinput.Definition) error {
	taxability, err := json.Marshal(definitionTaxability(definition))
	if err != nil {
		return fmt.Errorf("payinputstore: encode taxability: %w", err)
	}
	basis, err := json.Marshal(definitionBasisWire{
		Basis: string(definition.CalculationBasis), Currency: definition.Currency,
		FormulaRef: definition.FormulaRef, AccountingCode: definition.AccountingCode,
		OwnerRef:           definition.OwnerRef,
		EffectiveCanonical: base64.StdEncoding.EncodeToString(definition.Effective.Canonical()),
	})
	if err != nil {
		return fmt.Errorf("payinputstore: encode calculation basis: %w", err)
	}
	limits, err := json.Marshal(definitionLimits(definition))
	if err != nil {
		return fmt.Errorf("payinputstore: encode limits: %w", err)
	}
	from, to := effectiveProjection(definition.Effective)
	_, err = tx.Exec(ctx, `
		INSERT INTO payinput_definition (
			row_id, tenant_id, definition_id, code, kind, taxability,
			calculation_basis, limits, effective_from, effective_to, version,
			revision, state, supersedes_digest, supersedes_revision, canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8::jsonb,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT DO NOTHING`,
		uuid.New(), tenantID, definition.DefinitionID, definition.Code,
		definition.Kind, taxability, basis, limits, from, to, definition.Version,
		int64(definition.Revision), definition.State, nullableDigest(definition.SupersedesDigest),
		nullableRevision(definition.SupersedesRevision), storageDigest(definition.CanonicalDigest))
	if err != nil {
		return fmt.Errorf("payinputstore: insert definition %s/%d: %w", definition.DefinitionID, definition.Revision, err)
	}
	return nil
}

func (s *Store) LoadDefinition(ctx context.Context, tenantID, definitionID string, revision uint64) (payinput.Definition, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return payinput.Definition{}, err
	}
	if strings.TrimSpace(definitionID) == "" || revision == 0 {
		return payinput.Definition{}, invalid("definition id and positive revision are required")
	}
	var out payinput.Definition
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var err error
		out, err = loadDefinitionTx(ctx, tx, tid, definitionID, revision)
		return err
	})
	return out, err
}

func loadDefinitionTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, definitionID string, revision uint64) (payinput.Definition, error) {
	var (
		rowID, code, kind, version, state, digest string
		taxabilityJSON, basisJSON, limitsJSON     []byte
		from, to                                  *time.Time
		supersedesDigest                          *string
		supersedesRevision                        *int64
		storedRevision                            int64
	)
	err := tx.QueryRow(ctx, `
		SELECT row_id, code, kind, taxability, calculation_basis, limits,
			effective_from, effective_to, version, revision, state,
			supersedes_digest, supersedes_revision, canonical_digest
		FROM payinput_definition
		WHERE tenant_id=$1 AND definition_id=$2 AND revision=$3`, tenantID, definitionID, int64(revision)).Scan(
		&rowID, &code, &kind, &taxabilityJSON, &basisJSON, &limitsJSON, &from, &to,
		&version, &storedRevision, &state, &supersedesDigest, &supersedesRevision, &digest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return payinput.Definition{}, notFound(fmt.Sprintf("definition %s/%d", definitionID, revision))
		}
		return payinput.Definition{}, fmt.Errorf("payinputstore: load definition %s/%d: %w", definitionID, revision, err)
	}
	_ = rowID
	var basis definitionBasisWire
	if err := json.Unmarshal(basisJSON, &basis); err != nil {
		return payinput.Definition{}, invalid("stored calculation_basis is not valid JSON")
	}
	interval, err := decodeInterval(basis.EffectiveCanonical)
	if err != nil {
		interval, err = projectedInterval(from, to)
		if err != nil {
			return payinput.Definition{}, invalid("stored effective interval is invalid")
		}
	}
	var flags []payinput.TaxabilityFlag
	if len(taxabilityJSON) > 0 {
		if err := json.Unmarshal(taxabilityJSON, &flags); err != nil {
			return payinput.Definition{}, invalid("stored taxability is not valid JSON")
		}
	}
	var storedLimits limitsWire
	if len(limitsJSON) > 0 {
		if err := json.Unmarshal(limitsJSON, &storedLimits); err != nil {
			return payinput.Definition{}, invalid("stored limits are not valid JSON")
		}
	}
	minimum, err := decimalFromText(storedLimits.Minimum)
	if err != nil {
		return payinput.Definition{}, invalid("stored minimum is not a valid decimal")
	}
	maximum, err := decimalFromText(storedLimits.Maximum)
	if err != nil {
		return payinput.Definition{}, invalid("stored maximum is not a valid decimal")
	}
	in := payinput.Definition{
		DefinitionID: definitionID, ID: definitionID, Code: code, Kind: payinput.DefinitionKind(kind),
		TaxabilityFlags: flags, Currency: basis.Currency, CalculationBasis: payinput.CalculationBasis(basis.Basis),
		Limits: payinput.DecimalLimits{Minimum: minimum, Maximum: maximum}, FormulaRef: basis.FormulaRef,
		AccountingCode: basis.AccountingCode, OwnerRef: basis.OwnerRef, Effective: interval,
		Version: version, Revision: uint64(storedRevision), State: payinput.DefinitionState(state),
		SupersedesDigest: domainDigest(stringValue(supersedesDigest)), SupersedesRevision: uint64Value(supersedesRevision),
		CanonicalDigest: domainDigest(digest),
	}
	validated, err := payinput.NewDefinition(in)
	if err != nil || validated.CanonicalDigest != in.CanonicalDigest {
		return payinput.Definition{}, invalid("stored definition digest does not match its payload")
	}
	return validated, nil
}

func (s *Store) CurrentDefinition(ctx context.Context, tenantID, definitionID string) (payinput.Definition, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return payinput.Definition{}, err
	}
	if strings.TrimSpace(definitionID) == "" {
		return payinput.Definition{}, invalid("definition id is required")
	}
	var out payinput.Definition
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var revision uint64
		if err := tx.QueryRow(ctx, `SELECT revision FROM payinput_definition WHERE tenant_id=$1 AND definition_id=$2 ORDER BY revision DESC LIMIT 1`, tid, definitionID).Scan(&revision); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return notFound(fmt.Sprintf("definition %s", definitionID))
			}
			return err
		}
		var err error
		out, err = loadDefinitionTx(ctx, tx, tid, definitionID, revision)
		return err
	})
	return out, err
}

func (s *Store) ListDefinitionRevisions(ctx context.Context, tenantID, definitionID string) ([]payinput.Definition, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(definitionID) == "" {
		return nil, invalid("definition id is required")
	}
	var out []payinput.Definition
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT revision FROM payinput_definition WHERE tenant_id=$1 AND definition_id=$2 ORDER BY revision`, tid, definitionID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var revision uint64
			if err := rows.Scan(&revision); err != nil {
				return err
			}
			definition, err := loadDefinitionTx(ctx, tx, tid, definitionID, revision)
			if err != nil {
				return err
			}
			out = append(out, definition)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(out) == 0 {
			return notFound(fmt.Sprintf("definition %s", definitionID))
		}
		return nil
	})
	return out, err
}

// SaveAssignment inserts one immutable worker binding after resolving its
// exact tenant-local definition revision.
func (s *Store) SaveAssignment(ctx context.Context, tenantID string, assignment payinput.WorkerAssignment) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	worker, err := uuid.Parse(assignment.WorkerRef)
	if err != nil || worker == uuid.Nil {
		return invalid("worker reference must be a non-nil UUID")
	}
	refID, refVersion, refDigest := assignment.DefinitionID, assignment.DefinitionVersion, assignment.DefinitionDigest
	if assignment.DefinitionRef.ID != "" {
		refID, refVersion, refDigest = assignment.DefinitionRef.ID, assignment.DefinitionRef.Version, assignment.DefinitionRef.Digest
	}
	if refID == "" || refVersion == "" {
		return invalid("assignment definition id and version are required")
	}
	var bound payinput.WorkerAssignment
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		definition, err := definitionForReference(ctx, tx, tid, refID, refVersion, refDigest)
		if err != nil {
			return err
		}
		bound, err = payinput.NewWorkerAssignment(definition, assignment)
		if err != nil {
			return invalid(err.Error())
		}
		from, to := effectiveProjection(bound.Effective)
		var amount, rate any
		if bound.Amount.Validate() == nil {
			amount = bound.Amount.String()
		}
		if bound.Rate.Validate() == nil {
			rate = bound.Rate.String()
		}
		affected, err := tx.Exec(ctx, `
			INSERT INTO payinput_worker_assignment (
				row_id, tenant_id, assignment_id, worker_ref, definition_ref,
				definition_revision, effective_from, effective_to, amount, rate,
				recurrence, canonical_digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::numeric,$10::numeric,$11,$12)
			ON CONFLICT DO NOTHING`, uuid.New(), tid, bound.AssignmentID, worker, bound.DefinitionID,
			definition.Revision, from, to, amount, rate, bound.Recurrence, storageDigest(bound.CanonicalDigest))
		if err != nil {
			return fmt.Errorf("payinputstore: insert assignment %s: %w", bound.AssignmentID, err)
		}
		if affected == 0 {
			return duplicate(fmt.Sprintf("assignment %s", bound.AssignmentID))
		}
		return nil
	})
	return err
}

func definitionForReference(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, id, version, digest string) (payinput.Definition, error) {
	var revision uint64
	err := tx.QueryRow(ctx, `
		SELECT revision FROM payinput_definition
		WHERE tenant_id=$1 AND definition_id=$2 AND version=$3
		  AND ($4='' OR canonical_digest=$4)
		ORDER BY revision DESC LIMIT 1`, tenantID, id, version, storageDigest(digest)).Scan(&revision)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return payinput.Definition{}, reference(fmt.Sprintf("definition %s/%s is absent for this tenant", id, version))
		}
		return payinput.Definition{}, fmt.Errorf("payinputstore: resolve definition reference: %w", err)
	}
	return loadDefinitionTx(ctx, tx, tenantID, id, revision)
}

func (s *Store) LoadAssignment(ctx context.Context, tenantID, assignmentID string) (payinput.WorkerAssignment, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return payinput.WorkerAssignment{}, err
	}
	if strings.TrimSpace(assignmentID) == "" {
		return payinput.WorkerAssignment{}, invalid("assignment id is required")
	}
	var out payinput.WorkerAssignment
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var (
			rowID, worker                    uuid.UUID
			definitionID, recurrence, digest string
			definitionRevision               uint64
			from, to                         *time.Time
			amount, rate                     *string
		)
		err := tx.QueryRow(ctx, `
			SELECT row_id, worker_ref, definition_ref, definition_revision,
				effective_from, effective_to, amount::text, rate::text,
				recurrence, canonical_digest
			FROM payinput_worker_assignment
			WHERE tenant_id=$1 AND assignment_id=$2`, tid, assignmentID).Scan(
			&rowID, &worker, &definitionID, &definitionRevision, &from, &to,
			&amount, &rate, &recurrence, &digest)
		if err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return notFound(fmt.Sprintf("assignment %s", assignmentID))
			}
			return fmt.Errorf("payinputstore: load assignment %s: %w", assignmentID, err)
		}
		_ = rowID
		definition, err := loadDefinitionTx(ctx, tx, tid, definitionID, definitionRevision)
		if err != nil {
			return reference(fmt.Sprintf("assignment %s definition is not readable", assignmentID))
		}
		interval, err := projectedInterval(from, to)
		if err != nil {
			return invalid("stored assignment effective interval is invalid")
		}
		decodedAmount, err := decimalFromText(amount)
		if err != nil {
			return invalid("stored assignment amount is not a valid decimal")
		}
		decodedRate, err := decimalFromText(rate)
		if err != nil {
			return invalid("stored assignment rate is not a valid decimal")
		}
		out = payinput.WorkerAssignment{
			ID: assignmentID, AssignmentID: assignmentID, WorkerRef: worker.String(),
			DefinitionRef: payinput.DefinitionRef{ID: definition.DefinitionID, Version: definition.Version, Digest: definition.CanonicalDigest},
			DefinitionID:  definition.DefinitionID, DefinitionVersion: definition.Version, DefinitionDigest: definition.CanonicalDigest,
			Effective: interval, Amount: decodedAmount, Rate: decodedRate,
			Recurrence: payinput.Recurrence(recurrence), RecurrenceRule: defaultRecurrenceRule(payinput.Recurrence(recurrence)),
			FormulaRef: definition.FormulaRef, CanonicalDigest: domainDigest(digest),
		}
		return nil
	})
	return out, err
}

func (s *Store) ListAssignments(ctx context.Context, tenantID, workerRef string) ([]payinput.WorkerAssignment, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return nil, err
	}
	worker, err := uuid.Parse(workerRef)
	if err != nil || worker == uuid.Nil {
		return nil, invalid("worker reference must be a non-nil UUID")
	}
	var ids []string
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT assignment_id FROM payinput_worker_assignment WHERE tenant_id=$1 AND worker_ref=$2 ORDER BY assignment_id`, tid, worker)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, notFound(fmt.Sprintf("assignments for worker %s", workerRef))
	}
	out := make([]payinput.WorkerAssignment, 0, len(ids))
	for _, id := range ids {
		assignment, err := s.LoadAssignment(ctx, tenantID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, assignment)
	}
	return out, nil
}

func definitionTaxability(definition payinput.Definition) []payinput.TaxabilityFlag {
	flags := append([]payinput.TaxabilityFlag(nil), definition.TaxabilityFlags...)
	for jurisdiction, taxable := range definition.Taxability {
		flags = append(flags, payinput.TaxabilityFlag{Jurisdiction: jurisdiction, Taxable: taxable})
	}
	for jurisdiction, taxable := range definition.TaxabilityByJurisdiction {
		flags = append(flags, payinput.TaxabilityFlag{Jurisdiction: jurisdiction, Taxable: taxable})
	}
	return flags
}

func definitionLimits(definition payinput.Definition) limitsWire {
	limits := definition.Limits
	minimum, maximum := limits.Minimum, limits.Maximum
	var out limitsWire
	if minimum.Validate() == nil {
		value := minimum.String()
		out.Minimum = &value
	}
	if maximum.Validate() == nil {
		value := maximum.String()
		out.Maximum = &value
	}
	return out
}

func decimalFromText(text *string) (values.Decimal, error) {
	if text == nil || strings.TrimSpace(*text) == "" {
		return values.Decimal{}, nil
	}
	value := strings.TrimSpace(*text)
	scale := 0
	if dot := strings.IndexByte(value, '.'); dot >= 0 {
		scale = len(value) - dot - 1
	}
	return values.NewDecimal(value, int32(scale), values.RoundingExactRequired)
}

func defaultRecurrenceRule(recurrence payinput.Recurrence) string {
	if recurrence == payinput.RecurrenceOneTime {
		return ""
	}
	return "PAY_PERIOD"
}

func effectiveProjection(interval values.EffectiveInterval) (time.Time, any) {
	if date, ok := interval.StartDate(); ok {
		from := time.Date(int(date.Year()), date.Month(), int(date.Day()), 0, 0, 0, 0, time.UTC)
		if end, hasEnd := interval.EndDate(); hasEnd {
			return from, time.Date(int(end.Year()), end.Month(), int(end.Day()), 0, 0, 0, 0, time.UTC)
		}
		return from, nil
	}
	if instant, ok := interval.StartInstant(); ok {
		from := instant.Time()
		if end, hasEnd := interval.EndInstant(); hasEnd {
			return from, end.Time()
		}
		return from, nil
	}
	return time.Time{}, nil
}

func projectedInterval(from, to *time.Time) (values.EffectiveInterval, error) {
	if from == nil {
		return values.EffectiveInterval{}, values.ErrIntervalUnset
	}
	fromValue := timePtrUTC(from)
	toValue := timePtrUTC(to)
	if fromValue.Hour() == 0 && fromValue.Minute() == 0 && fromValue.Second() == 0 && fromValue.Nanosecond() == 0 && (to == nil || (toValue.Hour() == 0 && toValue.Minute() == 0 && toValue.Second() == 0 && toValue.Nanosecond() == 0)) {
		start, err := values.NewLocalDate(fromValue.Year(), fromValue.Month(), fromValue.Day())
		if err != nil {
			return values.EffectiveInterval{}, err
		}
		calendar := values.CalendarRef{Ref: "payroll", Version: "v1"}
		if to == nil {
			return values.NewOpenLocalDateInterval(start, calendar)
		}
		end, err := values.NewLocalDate(toValue.Year(), toValue.Month(), toValue.Day())
		if err != nil {
			return values.EffectiveInterval{}, err
		}
		return values.NewLocalDateInterval(start, end, calendar)
	}
	start, err := values.NewInstantFromUnix(fromValue.Unix(), int32(fromValue.Nanosecond()))
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	if to == nil {
		return values.NewOpenInstantInterval(start)
	}
	end, err := values.NewInstantFromUnix(toValue.Unix(), int32(toValue.Nanosecond()))
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	return values.NewInstantInterval(start, end)
}

func timePtrUTC(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

func encodeInterval(interval values.EffectiveInterval) string {
	return base64.StdEncoding.EncodeToString(interval.Canonical())
}

func decodeInterval(encoded string) (values.EffectiveInterval, error) {
	if encoded == "" {
		return values.EffectiveInterval{}, values.ErrIntervalUnset
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) < 3 || raw[0] != 0x05 {
		return values.EffectiveInterval{}, errors.New("invalid interval encoding")
	}
	kind, hasEnd := values.IntervalKind(raw[1]), raw[2] == 1
	pos := 3
	readDate := func() (values.LocalDate, error) {
		if len(raw)-pos < 7 || raw[pos] != 0x02 {
			return values.LocalDate{}, errors.New("invalid local date encoding")
		}
		date, err := values.NewLocalDate(int(int32(binary.BigEndian.Uint32(raw[pos+1:pos+5]))), time.Month(raw[pos+5]), int(raw[pos+6]))
		pos += 7
		return date, err
	}
	readInstant := func() (values.Instant, error) {
		if len(raw)-pos < 13 || raw[pos] != 0x01 {
			return values.Instant{}, errors.New("invalid instant encoding")
		}
		instant, err := values.NewInstantFromUnix(int64(binary.BigEndian.Uint64(raw[pos+1:pos+9])), int32(binary.BigEndian.Uint32(raw[pos+9:pos+13])))
		pos += 13
		return instant, err
	}
	readString := func() (string, error) {
		if len(raw)-pos < 4 {
			return "", errors.New("invalid interval string length")
		}
		n := int(binary.BigEndian.Uint32(raw[pos : pos+4]))
		pos += 4
		if n < 0 || len(raw)-pos < n {
			return "", errors.New("invalid interval string")
		}
		out := string(raw[pos : pos+n])
		pos += n
		return out, nil
	}
	var localStart, localEnd values.LocalDate
	var instantStart, instantEnd values.Instant
	if kind == values.IntervalKindLocalDate {
		localStart, err = readDate()
		if err == nil && hasEnd {
			localEnd, err = readDate()
		}
	} else if kind == values.IntervalKindInstant {
		instantStart, err = readInstant()
		if err == nil && hasEnd {
			instantEnd, err = readInstant()
		}
	} else {
		return values.EffectiveInterval{}, errors.New("invalid interval kind")
	}
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	calendarRef, err := readString()
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	calendarVersion, err := readString()
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	zoneID, err := readString()
	if err != nil || pos >= len(raw) {
		return values.EffectiveInterval{}, errors.New("invalid interval zone")
	}
	zoneVersion, err := readString()
	if err != nil || pos >= len(raw) {
		return values.EffectiveInterval{}, errors.New("invalid interval zone version")
	}
	disambiguation := values.Disambiguation(raw[pos])
	var interval values.EffectiveInterval
	if kind == values.IntervalKindLocalDate {
		if hasEnd {
			interval, err = values.NewLocalDateInterval(localStart, localEnd, values.CalendarRef{Ref: calendarRef, Version: calendarVersion})
		} else {
			interval, err = values.NewOpenLocalDateInterval(localStart, values.CalendarRef{Ref: calendarRef, Version: calendarVersion})
		}
	} else if hasEnd {
		interval, err = values.NewInstantInterval(instantStart, instantEnd)
	} else {
		interval, err = values.NewOpenInstantInterval(instantStart)
	}
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	if zoneID != "" || zoneVersion != "" {
		interval, err = interval.WithZone(values.ZoneRef{ID: zoneID, TzdbVersion: zoneVersion}, disambiguation)
	}
	return interval, err
}

func nullableDigest(value string) any {
	if value == "" {
		return nil
	}
	return storageDigest(value)
}

func nullableRevision(value uint64) any {
	if value == 0 {
		return nil
	}
	return int64(value)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func uint64Value(value *int64) uint64 {
	if value == nil || *value < 0 {
		return 0
	}
	return uint64(*value)
}

func storageDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }

func domainDigest(value string) string {
	if value == "" || strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}
