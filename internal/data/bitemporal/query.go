package bitemporal

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
)

// Querier is the minimal database capability a bitemporal query needs. It is
// ledger.Querier itself, so whatever handle a caller already reads the ledger
// through satisfies this one without adapting.
type Querier = ledger.Querier

// Option configures a Query call.
type Option func(*options)

type options struct {
	now func() time.Time
}

// WithClock replaces the source of "now" used to default EffectiveAt/KnownAt
// for CURRENT and to default the unspecified half of EFFECTIVE_AS_OF/
// KNOWN_AS_OF/BETWEEN/HISTORY. Tests use it to make defaulting deterministic
// and to prove replay stability without racing the wall clock.
func WithClock(now func() time.Time) Option {
	return func(o *options) { o.now = now }
}

func resolveOptions(opts []Option) options {
	o := options{now: func() time.Time { return time.Now().UTC() }}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// Query runs one bitemporal fact query and returns the authorized page of
// results. Every subject/field authorization boundary in dec, and the
// request's own subject/field/time scope, is compiled into the SQL statement
// before it runs (see BuildSQL); this function performs no additional
// filtering of rows after they are fetched.
func Query(ctx context.Context, q Querier, req Request, dec Decision, opts ...Option) (Result, error) {
	o := resolveOptions(opts)
	now := o.now()

	sqlText, args, err := BuildSQL(req, dec, now)
	if err != nil {
		return Result{}, err
	}

	rows, err := q.Query(ctx, sqlText, args...)
	if err != nil {
		return Result{}, fmt.Errorf("bitemporal: query: %w", err)
	}
	defer rows.Close()

	pageSize := req.pageSize()
	facts := make([]Fact, 0, pageSize)
	for rows.Next() {
		f, err := scanFact(rows)
		if err != nil {
			return Result{}, fmt.Errorf("bitemporal: scan fact: %w", err)
		}
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return Result{}, fmt.Errorf("bitemporal: query: %w", err)
	}

	result := Result{Facts: facts}
	if len(facts) > pageSize {
		last := facts[pageSize-1]
		result.Facts = facts[:pageSize]
		result.NextCursor = encodeCursor(req, last.EffectiveAt, last.RecordedAt, last.StreamKey, last.SchemaRef, last.Sequence)
	}
	return result, nil
}

// AsOf resolves the fact effective at the given business time for one
// subject and field, using every correction known by now (or by dec's
// knowledge ceiling, if lower). It is Query with Mode = EFFECTIVE_AS_OF.
//
// AsOf answers "what was true on this business date" - the first axis of the
// bitemporal model, independent of when the ledger learned about it.
func AsOf(ctx context.Context, q Querier, tenant uuid.UUID, subject, field string, effectiveAt time.Time, dec Decision, opts ...Option) (Result, error) {
	return Query(ctx, q, Request{
		Tenant:      tenant,
		Mode:        ModeEffectiveAsOf,
		Subject:     subject,
		Field:       field,
		EffectiveAt: effectiveAt,
	}, dec, opts...)
}

// KnownAt resolves what the ledger had recorded, as of the given system
// time, about the fact effective now for one subject and field. It is Query
// with Mode = KNOWN_AS_OF.
//
// KnownAt answers "what had the ledger recorded by this instant" - the
// second axis of the bitemporal model. A correction recorded after knownAt
// never appears in the result: TestTodo_DATA_005's security cases prove this
// directly against PostgreSQL's own result set.
func KnownAt(ctx context.Context, q Querier, tenant uuid.UUID, subject, field string, knownAt time.Time, dec Decision, opts ...Option) (Result, error) {
	return Query(ctx, q, Request{
		Tenant:  tenant,
		Mode:    ModeKnownAsOf,
		Subject: subject,
		Field:   field,
		KnownAt: knownAt,
	}, dec, opts...)
}

// scanFact reads one row in selectColumns order.
func scanFact(row interface{ Scan(dest ...any) error }) (Fact, error) {
	var (
		f                Fact
		assertionClass   string
		correctionKind   string
		authority        *string
		artifact         *string
		correctsStream   *string
		correctsSequence *int64
	)
	if err := row.Scan(
		&f.Tenant, &f.StreamKey, &f.Sequence, &f.EventID, &assertionClass, &authority,
		&f.SourceRef, &f.SchemaRef, &f.Payload, &artifact, &f.Digest, &f.DigestAlgorithm,
		&f.OccurredAt, &f.EffectiveAt, &f.RecordedAt, &f.CorrelationID, &f.CausationID,
		&f.IdempotencyKey, &correctsStream, &correctsSequence, &correctionKind,
	); err != nil {
		return Fact{}, err
	}
	f.AssertionClass = ledger.AssertionClass(assertionClass)
	f.CorrectionKind = CorrectionKind(correctionKind)
	if authority != nil {
		f.Authority = *authority
	}
	if artifact != nil {
		f.ArtifactRef = *artifact
	}
	if correctsStream != nil && correctsSequence != nil {
		f.Corrects = &ledger.EventRef{StreamKey: *correctsStream, Sequence: *correctsSequence}
	}
	return f, nil
}
