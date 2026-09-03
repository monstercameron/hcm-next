package bitemporal

import (
	"fmt"
	"strings"
	"time"
)

// selectColumns is shared by every query this package issues. The trailing
// CASE expression computes CorrectionKind in SQL from a self-join against the
// row a CORRECTION names as its target, rather than in Go after the fact, so
// the classification rule lives in one place next to the data it reads.
//
// A correction whose target cannot be resolved (target.sequence IS NULL - the
// partitioned ledger_event table has no cross-partition foreign key enforcing
// that corrects_stream_key/corrects_sequence names a real row) is
// conservatively classified CORRECTION: an unresolved target must never be
// treated as having proven a different effective_at.
const selectColumns = `
	e.tenant_id, e.stream_key, e.sequence, e.event_id, e.assertion_class, e.authority_ref,
	e.source_ref, e.schema_ref, e.payload, e.artifact_ref, e.digest, e.digest_algorithm,
	e.occurred_at, e.effective_at, e.recorded_at, e.correlation_id, e.causation_id,
	e.idempotency_key, e.corrects_stream_key, e.corrects_sequence,
	CASE
		WHEN e.assertion_class <> 'CORRECTION' THEN 'ORIGINAL'
		WHEN target.sequence IS NULL THEN 'CORRECTION'
		WHEN target.effective_at = e.effective_at THEN 'CORRECTION'
		ELSE 'SUPERSESSION'
	END AS correction_kind`

const fromJoin = `
	FROM ledger_event e
	LEFT JOIN ledger_event target
		ON target.tenant_id = e.tenant_id
	   AND target.stream_key = e.corrects_stream_key
	   AND target.sequence = e.corrects_sequence`

// argBuilder accumulates positional SQL parameters and hands back the
// placeholder each one was assigned.
type argBuilder struct{ args []any }

func (b *argBuilder) add(v any) string {
	b.args = append(b.args, v)
	return fmt.Sprintf("$%d", len(b.args))
}

// authFilter appends every predicate the request's exact subject/field scope
// and the decision's allow/deny lists impose. Each clause here is evaluated
// by PostgreSQL as part of the query's WHERE clause: a denied field or a
// withheld subject never leaves the server, because no clause here is ever
// applied to a row after it has already been fetched.
func authFilter(b *argBuilder, req Request, dec Decision) []string {
	var clauses []string
	if req.Subject != "" {
		clauses = append(clauses, "e.stream_key = "+b.add(req.Subject))
	}
	if dec.AllowSubjects != nil {
		clauses = append(clauses, "e.stream_key = ANY("+b.add(dec.AllowSubjects)+"::text[])")
	}
	if len(dec.DenySubjects) > 0 {
		clauses = append(clauses, "NOT (e.stream_key = ANY("+b.add(dec.DenySubjects)+"::text[]))")
	}
	if req.Field != "" {
		clauses = append(clauses, "e.schema_ref = "+b.add(req.Field))
	}
	if dec.AllowFields != nil {
		clauses = append(clauses, "e.schema_ref = ANY("+b.add(dec.AllowFields)+"::text[])")
	}
	if len(dec.DenyFields) > 0 {
		clauses = append(clauses, "NOT (e.schema_ref = ANY("+b.add(dec.DenyFields)+"::text[]))")
	}
	return clauses
}

// bounds is the resolved effective/known-at window one query executes
// against, after Mode-specific defaulting (computeBounds) has already run.
type bounds struct {
	// effectiveAt is the single business-time point CURRENT/EFFECTIVE_AS_OF/
	// KNOWN_AS_OF resolve against. Nil for BETWEEN/HISTORY.
	effectiveAt *time.Time
	// effectiveFrom/effectiveTo bound BETWEEN's half-open window. Both nil
	// for point-resolution modes and for HISTORY's unbounded window.
	effectiveFrom *time.Time
	effectiveTo   *time.Time
	// knownAt is always present: no mode reads without a knowledge horizon.
	knownAt time.Time
	// resolved marks CURRENT/EFFECTIVE_AS_OF/KNOWN_AS_OF: these return the one
	// winning fact per (subject, field) rather than every visible fact.
	resolved bool
}

// computeBounds applies Mode-specific defaulting to a validated Request,
// clamping the resulting knowledge horizon to the decision's ceiling.
func computeBounds(req Request, dec Decision, now time.Time) bounds {
	switch req.Mode {
	case ModeCurrent:
		at := now
		return bounds{effectiveAt: &at, knownAt: dec.clampKnownAt(now), resolved: true}
	case ModeEffectiveAsOf:
		at := req.EffectiveAt
		knownAt := req.KnownAt
		if knownAt.IsZero() {
			knownAt = now
		}
		return bounds{effectiveAt: &at, knownAt: dec.clampKnownAt(knownAt), resolved: true}
	case ModeKnownAsOf:
		at := req.EffectiveAt
		if at.IsZero() {
			at = now
		}
		return bounds{effectiveAt: &at, knownAt: dec.clampKnownAt(req.KnownAt), resolved: true}
	case ModeBetween:
		from := req.EffectiveFrom
		var to *time.Time
		if !req.EffectiveTo.IsZero() {
			t := req.EffectiveTo
			to = &t
		}
		knownAt := req.KnownAt
		if knownAt.IsZero() {
			knownAt = now
		}
		return bounds{effectiveFrom: &from, effectiveTo: to, knownAt: dec.clampKnownAt(knownAt), resolved: false}
	case ModeHistory:
		knownAt := req.KnownAt
		if knownAt.IsZero() {
			knownAt = now
		}
		return bounds{knownAt: dec.clampKnownAt(knownAt), resolved: false}
	default:
		return bounds{knownAt: now}
	}
}

// buildResolvedSQL builds the point-resolution query for CURRENT,
// EFFECTIVE_AS_OF and KNOWN_AS_OF: exactly one winning row per
// (stream_key, schema_ref), chosen by the deterministic rule documented in
// doc.go (greatest effective_at, then greatest recorded_at, then greatest
// sequence).
func buildResolvedSQL(req Request, dec Decision, b bounds, cursor *cursorPayload) (string, []any) {
	ab := &argBuilder{}
	where := []string{"e.tenant_id = " + ab.add(req.Tenant)}
	where = append(where, authFilter(ab, req, dec)...)
	if b.effectiveAt != nil {
		where = append(where, "e.effective_at <= "+ab.add(*b.effectiveAt))
	}
	where = append(where, "e.recorded_at <= "+ab.add(b.knownAt))
	if cursor != nil {
		where = append(where, fmt.Sprintf("(e.stream_key, e.schema_ref) > (%s, %s)",
			ab.add(cursor.Stream), ab.add(cursor.Schema)))
	}
	limit := ab.add(req.pageSize() + 1)

	sqlText := fmt.Sprintf(`
		SELECT DISTINCT ON (e.stream_key, e.schema_ref) %s
		%s
		WHERE %s
		ORDER BY e.stream_key, e.schema_ref, e.effective_at DESC, e.recorded_at DESC, e.sequence DESC
		LIMIT %s`,
		selectColumns, fromJoin, strings.Join(where, " AND "), limit)
	return sqlText, ab.args
}

// buildHistorySQL builds the full-listing query for BETWEEN and HISTORY:
// every visible fact, in deterministic chronological order.
func buildHistorySQL(req Request, dec Decision, b bounds, cursor *cursorPayload) (string, []any) {
	ab := &argBuilder{}
	where := []string{"e.tenant_id = " + ab.add(req.Tenant)}
	where = append(where, authFilter(ab, req, dec)...)
	if b.effectiveFrom != nil {
		where = append(where, "e.effective_at >= "+ab.add(*b.effectiveFrom))
	}
	if b.effectiveTo != nil {
		where = append(where, "e.effective_at < "+ab.add(*b.effectiveTo))
	}
	where = append(where, "e.recorded_at <= "+ab.add(b.knownAt))
	if cursor != nil {
		where = append(where, fmt.Sprintf(
			"(e.effective_at, e.recorded_at, e.stream_key, e.schema_ref, e.sequence) > (%s, %s, %s, %s, %s)",
			ab.add(time.Unix(0, cursor.Effective).UTC()),
			ab.add(time.Unix(0, cursor.Recorded).UTC()),
			ab.add(cursor.Stream),
			ab.add(cursor.Schema),
			ab.add(cursor.Sequence)))
	}
	limit := ab.add(req.pageSize() + 1)

	sqlText := fmt.Sprintf(`
		SELECT %s
		%s
		WHERE %s
		ORDER BY e.effective_at ASC, e.recorded_at ASC, e.stream_key ASC, e.schema_ref ASC, e.sequence ASC
		LIMIT %s`,
		selectColumns, fromJoin, strings.Join(where, " AND "), limit)
	return sqlText, ab.args
}

// BuildSQL exposes the exact statement and parameters Query would execute for
// req/dec, without running it. It exists so tests (and evidence review) can
// prove authorization is enforced as a SQL predicate: run the returned
// statement directly against PostgreSQL and observe that PostgreSQL itself
// never returns a denied field or a withheld subject, rather than trusting
// that Go filtered them out afterward.
func BuildSQL(req Request, dec Decision, now time.Time) (string, []any, error) {
	if err := req.Validate(); err != nil {
		return "", nil, err
	}
	if err := dec.Validate(); err != nil {
		return "", nil, err
	}
	if req.Tenant != dec.Tenant {
		return "", nil, ErrTenantMismatch{RequestTenant: req.Tenant, DecisionTenant: dec.Tenant}
	}
	dec = dec.normalize()

	var cursor *cursorPayload
	if req.Cursor != "" {
		c, err := decodeCursor(req, req.Cursor)
		if err != nil {
			return "", nil, err
		}
		cursor = &c
	}

	b := computeBounds(req, dec, now)
	if b.resolved {
		sqlText, args := buildResolvedSQL(req, dec, b, cursor)
		return sqlText, args, nil
	}
	sqlText, args := buildHistorySQL(req, dec, b, cursor)
	return sqlText, args, nil
}
