package temporal

import (
	"fmt"
	"sort"
	"strings"

	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
)

// reconstructColumns is the projection [LedgerPlan] reads: the ledger
// envelope plus the same CORRECTION/SUPERSESSION classification
// internal/data/bitemporal computes.
//
// The classification lives in SQL for the same reason it does in
// internal/data/bitemporal: it is a statement about two rows, and computing
// it next to the join that resolves the second row keeps it from drifting
// away from the data it reads. A correction whose target does not resolve is
// classified CORRECTION, never SUPERSESSION, so an unresolved target can
// never be treated as having proven a different effective instant.
//
// The join deliberately does not read the target's assertion class. Truth
// class is resolved only from the visible, authorized set
// ([resolveTruthClass]); reading it through this join would let a caller
// learn the class of an assertion their own decision withholds.
const reconstructColumns = `
	e.tenant_id, e.stream_key, e.sequence, e.event_id, e.assertion_class, e.authority_ref,
	e.source_ref, e.schema_ref, e.payload, e.artifact_ref, e.digest, e.digest_algorithm,
	e.occurred_at, e.effective_at, e.recorded_at, e.correlation_id, e.causation_id,
	e.corrects_stream_key, e.corrects_sequence,
	CASE
		WHEN e.assertion_class <> 'CORRECTION' THEN 'ORIGINAL'
		WHEN target.sequence IS NULL THEN 'CORRECTION'
		WHEN target.effective_at = e.effective_at THEN 'CORRECTION'
		ELSE 'SUPERSESSION'
	END AS correction_kind`

const reconstructFrom = `
	FROM ledger_event e
	LEFT JOIN ledger_event target
		ON target.tenant_id = e.tenant_id
	   AND target.stream_key = e.corrects_stream_key
	   AND target.sequence = e.corrects_sequence`

// argBuilder accumulates positional SQL parameters and returns the
// placeholder each was assigned.
type argBuilder struct{ args []any }

func (b *argBuilder) add(v any) string {
	b.args = append(b.args, v)
	return fmt.Sprintf("$%d", len(b.args))
}

// authClauses compiles the request's own subject/field scope and every
// allow/deny boundary the decision states into SQL predicates.
//
// This deliberately mirrors internal/data/bitemporal's unexported authFilter
// rather than calling it: that adapter's builder is package-private, and its
// point-resolution mode collapses every assertion class into one winner per
// (subject, field), which is exactly the collapse this package must not
// perform (see the package doc). The duplication is one screen of
// predicates, and TestTodo_LEDGER_006_Property proves the two plans return
// the same authorized rows rather than trusting that they do.
//
// Every predicate here runs inside PostgreSQL. A denied field or a withheld
// subject is never selected, so it never reaches this process to be filtered
// out in Go.
func authClauses(b *argBuilder, req Request, dec Decision) []string {
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

// sortedUnique returns a sorted, deduplicated copy of values so that
// caller-supplied ordering can never change the predicate set. A nil input
// stays nil: "not restricted" and "restricted to nothing" are different
// states in a [Decision] and normalization must not merge them.
func sortedUnique(values []string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	sort.Strings(out)
	deduped := out[:0]
	for i, v := range out {
		if i == 0 || v != out[i-1] {
			deduped = append(deduped, v)
		}
	}
	if len(deduped) == 0 {
		return []string{}
	}
	return deduped
}

// normalizeDecision applies [sortedUnique] to every allow/deny list.
// internal/data/bitemporal normalizes its own copy identically before
// building SQL; this package must do the same so both plans compile the
// same predicate set from the same decision value.
func normalizeDecision(dec Decision) Decision {
	dec.AllowSubjects = sortedUnique(dec.AllowSubjects)
	dec.DenySubjects = sortedUnique(dec.DenySubjects)
	dec.AllowFields = sortedUnique(dec.AllowFields)
	dec.DenyFields = sortedUnique(dec.DenyFields)
	return dec
}

// BuildReconstructSQL returns the exact statement and parameters
// [LedgerPlan] executes for a reconstruction, without running it.
//
// It exists for the same reason internal/data/bitemporal.BuildSQL does: so a
// test can run the statement directly and observe that PostgreSQL itself
// never returns a denied field or a withheld subject, rather than trusting
// that Go filtered them afterwards.
//
// Both temporal bounds are inclusive at a point coordinate - effective_at <=
// EffectiveAt and recorded_at <= KnownAt - which is what "as of this
// instant" means. limit is applied as limit+1 rows so the caller can detect
// that a subject has more history than it is willing to fold.
func BuildReconstructSQL(req Request, dec Decision, coord Coordinate, limit int) (string, []any, error) {
	if err := req.Validate(); err != nil {
		return "", nil, err
	}
	if req.Mode != ModeReconstruct {
		return "", nil, ErrRequestInvalid{Field: "Mode", Reason: "BuildReconstructSQL builds the RECONSTRUCT statement only"}
	}
	if err := dec.Validate(); err != nil {
		return "", nil, err
	}
	if req.Tenant != dec.Tenant {
		return "", nil, ErrTenantMismatch{RequestTenant: req.Tenant, DecisionTenant: dec.Tenant}
	}
	if coord.EffectiveAt.IsZero() || coord.KnownAt.IsZero() {
		return "", nil, ErrRequestInvalid{Field: "Coordinate", Reason: "both EffectiveAt and KnownAt must be resolved before building the statement"}
	}

	dec = normalizeDecision(dec)
	b := &argBuilder{}
	where := []string{"e.tenant_id = " + b.add(req.Tenant)}
	where = append(where, authClauses(b, req, dec)...)
	where = append(where, "e.effective_at <= "+b.add(coord.EffectiveAt))
	where = append(where, "e.recorded_at <= "+b.add(coord.KnownAt))

	sqlText := fmt.Sprintf(`
		SELECT %s
		%s
		WHERE %s
		ORDER BY e.effective_at ASC, e.recorded_at ASC, e.sequence ASC
		LIMIT %s`,
		reconstructColumns, reconstructFrom, strings.Join(where, " AND "), b.add(limit+1))
	return sqlText, b.args, nil
}

// scanAssertion reads one row in reconstructColumns order. The authority
// label is left unresolved here; [loadAuthorityLabels] fills it in once for
// the whole result set rather than once per row.
func scanAssertion(row interface{ Scan(dest ...any) error }) (Assertion, error) {
	var (
		a                Assertion
		assertionClass   string
		correctionKind   string
		authority        *string
		artifact         *string
		correctsStream   *string
		correctsSequence *int64
	)
	if err := row.Scan(
		&a.Tenant, &a.Ref.StreamKey, &a.Ref.Sequence, &a.SourceEventID, &assertionClass, &authority,
		&a.SourceRef, &a.SchemaRef, &a.Payload, &artifact, &a.Digest, &a.DigestAlgorithm,
		&a.OccurredAt, &a.EffectiveAt, &a.RecordedAt, &a.CorrelationID, &a.CausationID,
		&correctsStream, &correctsSequence, &correctionKind,
	); err != nil {
		return Assertion{}, err
	}
	a.AssertionClass = datalogger.AssertionClass(assertionClass)
	a.CorrectionKind = CorrectionKind(correctionKind)
	if authority != nil {
		a.Authority = AuthorityLabel{Present: true, Ref: *authority}
	}
	if artifact != nil {
		a.ArtifactRef = *artifact
	}
	if correctsStream != nil && correctsSequence != nil {
		a.Corrects = &datalogger.EventRef{StreamKey: *correctsStream, Sequence: *correctsSequence}
	}
	return a, nil
}
