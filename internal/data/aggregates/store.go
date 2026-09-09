package aggregates

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Executor is the minimal database capability every Put/read helper in this
// package needs. A [dbport.Tx] and a [dbport.Conn] both satisfy it, but a Put
// call that closes a live predecessor (see put, below) issues two statements
// -- the UPDATE that sets the predecessor's superseded_at, then the INSERT of
// the new row -- and this package never opens a transaction of its own to bind
// them together. Pass a [dbport.Tx] (begun by the caller, committed or rolled
// back by the caller) to every Put whenever the entity being written might
// already have a live row: on a bare connection those two statements
// autocommit independently, so an INSERT failure (a refused cycle, an
// over-committed FTE/budget, a currency mismatch) would leave the
// predecessor superseded with no successor. A read-only Current*/KnownAsOf*
// call is a single statement and needs no transaction.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// Envelope is the bitemporal fields every table in this package shares. See
// doc.go for the full shape rationale.
type Envelope struct {
	RowID    uuid.UUID
	Tenant   uuid.UUID
	EntityID uuid.UUID
	// Kind is the entity kind put registers this EntityID under in
	// aggregate_entity (migrations/00011), the shared identity spine every
	// table's entity_id -- and every cross-entity reference column -- is
	// foreign-keyed to.
	Kind            values.Kind
	CanonicalID     string
	EffectiveFrom   time.Time
	EffectiveTo     *time.Time // nil = open-ended
	RecordedAt      time.Time
	SupersededAt    *time.Time // always nil on the value passed to Put; Put decides it for rows it closes
	DigestAlgorithm string
	Digest          string
}

// newEnvelope fills in the identity, canonical text and digest fields shared
// by every Put call. kind names the table's entity kind (e.g. "person");
// fields are the table's own business columns, in the exact order the
// caller's digest should bind them, plus the identity fields this function
// itself contributes so a digest cannot be replayed across entities or
// tenants.
func newEnvelope(
	kind values.Kind,
	tenant, entityID uuid.UUID,
	effectiveFrom time.Time,
	effectiveTo *time.Time,
	recordedAt time.Time,
	fields ...string,
) (Envelope, error) {
	id := values.EntityId{Kind: kind, Id: entityID.String()}
	if err := id.Validate(); err != nil {
		return Envelope{}, fmt.Errorf("aggregates: %s entity id: %w", kind, err)
	}
	if recordedAt.IsZero() {
		recordedAt = time.Now().UTC()
	}
	digestFields := append([]string{
		string(kind), tenant.String(), entityID.String(),
		effectiveFrom.UTC().Format(time.RFC3339Nano), formatOptionalTime(effectiveTo),
	}, fields...)
	return Envelope{
		RowID:           uuid.New(),
		Tenant:          tenant,
		EntityID:        entityID,
		Kind:            kind,
		CanonicalID:     string(id.Canonical()),
		EffectiveFrom:   effectiveFrom.UTC(),
		EffectiveTo:     normalizeOptionalTime(effectiveTo),
		RecordedAt:      recordedAt.UTC(),
		DigestAlgorithm: DigestAlgorithm,
		Digest:          computeDigest(digestFields...),
	}, nil
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func normalizeOptionalTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// decimalParam marks a bind parameter that must reach an exact-decimal
// (numeric) column without ever passing through float64. pgx's NumericCodec
// only has an encode plan for a value implementing NumericValuer/
// Float64Valuer/Int64Valuer (see pgtype/numeric.go), which a plain Go string
// does not; a decimalParam is instead bound as `$n::text::numeric`, which
// makes Postgres itself parse the exact decimal text into numeric, using
// pgx's ordinary (and exact) text encoding of a Go string in between. Every
// caller in this package that writes a numeric(_, _) column passes its
// exact-decimal text (from values.Decimal/values.Money, never a float) this
// way; the corresponding read casts the column with `col::text` for the same
// reason in reverse (NumericCodec's PlanScan has no plan for a plain
// *string either).
type decimalParam string

// decimalScale is the fixed scale every numeric(_, 4) column in this
// package's migrations declares. normalizeDecimal parses text at that scale
// (values.NewDecimal accepts fewer fractional digits than the declared
// scale, zero-padding the rest) and returns its canonical text, so that
// "1", "1.0", and "1.0000" all store, digest and compare identically.
const decimalScale = 4

func normalizeDecimal(text string) (string, error) {
	d, err := values.NewDecimal(text, decimalScale, values.RoundingHalfEven)
	if err != nil {
		return "", fmt.Errorf("aggregates: decimal %q: %w", text, err)
	}
	return d.String(), nil
}

// moneyDecimalText requantizes an already-validated values.Money's amount to
// decimalScale so the text this package inserts already matches the
// numeric(_, 4) column's own stored scale -- a value read back later
// (`col::text`) therefore always compares equal, byte for byte, to what was
// inserted.
func moneyDecimalText(m values.Money) (string, error) {
	q, err := m.Amount().Quantize(decimalScale, values.RoundingHalfEven)
	if err != nil {
		return "", fmt.Errorf("aggregates: money %s: %w", m.String(), err)
	}
	return q.String(), nil
}

// put closes every live row for (tenant, entityID) on `table` whose business
// range overlaps [env.EffectiveFrom, env.EffectiveTo), setting SupersededAt
// to env.RecordedAt, then inserts env plus the table-specific cols/vals. It
// is the one place this package ever changes a table's stored rows.
//
// cols/vals are the table's own business columns beyond the shared envelope;
// they are appended after the envelope columns in the INSERT. Column and
// table names come from this package's own Go constants, never from request
// input, so building the SQL text with fmt.Sprintf carries no injection risk
// -- every value is still bound as a parameter.
func put(ctx context.Context, ex Executor, table string, env Envelope, cols []string, vals []any) (uuid.UUID, error) {
	// Register this row's own entity in the shared identity spine before
	// inserting the fact row itself: every table's entity_id foreign-keys to
	// aggregate_entity (tenant_id, entity_id), and a cross-entity reference
	// column (person_ref, employment_ref, ...) on some other table can only
	// foreign-key to an entity that is already registered here. ON CONFLICT
	// DO NOTHING makes this idempotent across every later Put for the same
	// entity (a supersession registers nothing new).
	if _, err := ex.Exec(ctx, `
		INSERT INTO aggregate_entity (tenant_id, entity_id, kind, canonical_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, entity_id) DO NOTHING`,
		env.Tenant, env.EntityID, string(env.Kind), env.CanonicalID); err != nil {
		return uuid.Nil, fmt.Errorf("aggregates: register entity %s in aggregate_entity: %w", env.EntityID, err)
	}

	closeRows, err := ex.Query(ctx, fmt.Sprintf(`
		SELECT row_id FROM %s
		WHERE tenant_id = $1 AND entity_id = $2 AND superseded_at IS NULL
		  AND tstzrange(effective_from, COALESCE(effective_to, 'infinity'::timestamptz), '[)')
		      && tstzrange($3::timestamptz, COALESCE($4::timestamptz, 'infinity'::timestamptz), '[)')`, table),
		env.Tenant, env.EntityID, env.EffectiveFrom, env.EffectiveTo)
	if err != nil {
		return uuid.Nil, fmt.Errorf("aggregates: find live rows to supersede in %s: %w", table, err)
	}
	var toClose []uuid.UUID
	for closeRows.Next() {
		var id uuid.UUID
		if err := closeRows.Scan(&id); err != nil {
			closeRows.Close()
			return uuid.Nil, fmt.Errorf("aggregates: scan row to supersede in %s: %w", table, err)
		}
		toClose = append(toClose, id)
	}
	closeErr := closeRows.Err()
	closeRows.Close()
	if closeErr != nil {
		return uuid.Nil, fmt.Errorf("aggregates: read rows to supersede in %s: %w", table, closeErr)
	}

	for _, id := range toClose {
		if _, err := ex.Exec(ctx, fmt.Sprintf(`UPDATE %s SET superseded_at = $1 WHERE row_id = $2`, table),
			env.RecordedAt, id); err != nil {
			return uuid.Nil, fmt.Errorf("aggregates: supersede row %s in %s: %w", id, table, err)
		}
	}

	allCols := append([]string{
		"row_id", "tenant_id", "entity_id", "canonical_id",
		"effective_from", "effective_to", "recorded_at", "digest_algorithm", "digest",
	}, cols...)
	allVals := append([]any{
		env.RowID, env.Tenant, env.EntityID, env.CanonicalID,
		env.EffectiveFrom, env.EffectiveTo, env.RecordedAt, env.DigestAlgorithm, env.Digest,
	}, vals...)

	placeholders := make([]string, len(allVals))
	for i, v := range allVals {
		if dp, ok := v.(decimalParam); ok {
			placeholders[i] = fmt.Sprintf("$%d::text::numeric", i+1)
			allVals[i] = string(dp)
		} else {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
		}
	}
	insertSQL := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`,
		table, strings.Join(allCols, ", "), strings.Join(placeholders, ", "))
	if _, err := ex.Exec(ctx, insertSQL, allVals...); err != nil {
		return uuid.Nil, fmt.Errorf("aggregates: insert into %s: %w", table, err)
	}
	return env.RowID, nil
}

// currentAsOf and knownAsOf below share the same WHERE-clause shape; scan is
// supplied by each table's own file since the columns differ per table.

func currentAsOfSQL(table, selectCols string) string {
	return fmt.Sprintf(`
		SELECT %s FROM %s
		WHERE tenant_id = $1 AND entity_id = $2 AND superseded_at IS NULL
		  AND effective_from <= $3 AND (effective_to IS NULL OR effective_to > $3)`, selectCols, table)
}

func knownAsOfSQL(table, selectCols string) string {
	return fmt.Sprintf(`
		SELECT %s FROM %s
		WHERE tenant_id = $1 AND entity_id = $2
		  AND effective_from <= $3 AND (effective_to IS NULL OR effective_to > $3)
		  AND recorded_at <= $4 AND (superseded_at IS NULL OR superseded_at > $4)`, selectCols, table)
}
