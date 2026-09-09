package explorer

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/bitemporal"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/internal/viewdigest"
)

// BitemporalView is one page of authorized bitemporal facts
// (internal/data/bitemporal.Result), plus a content digest. Every field of
// bitemporal.Fact this view carries was already authorized at the SQL
// layer by the Decision the caller supplied - see doc.go's "Authorization"
// section - so this package applies no further redaction to it.
type BitemporalView struct {
	Facts      []bitemporal.Fact
	NextCursor string
	Digest     string
}

func toBitemporalView(result bitemporal.Result) BitemporalView {
	v := BitemporalView{Facts: result.Facts, NextCursor: result.NextCursor}
	v.Digest = digestBitemporal(v)
	return v
}

// BitemporalAsOf resolves the fact effective at effectiveAt for one
// subject/field, using everything known now (or by dec's knowledge
// ceiling, if lower). It wraps internal/data/bitemporal.AsOf directly; the
// "what was true on this business date" axis of the effective-date
// debugger (specs/hris-admin-dataops.md).
func BitemporalAsOf(ctx context.Context, q bitemporal.Querier, tenant uuid.UUID, subject, field string, effectiveAt time.Time, dec bitemporal.Decision, opts ...bitemporal.Option) (BitemporalView, error) {
	result, err := bitemporal.AsOf(ctx, q, tenant, subject, field, effectiveAt, dec, opts...)
	if err != nil {
		return BitemporalView{}, fmt.Errorf("explorer: bitemporal as-of %s/%s at %s: %w", subject, field, effectiveAt, err)
	}
	return toBitemporalView(result), nil
}

// BitemporalKnownAt resolves what the ledger had recorded, as of knownAt,
// about the fact effective now (or at a Request.EffectiveAt a caller sets
// via opts). It wraps internal/data/bitemporal.KnownAt directly; the "what
// had the ledger recorded by this instant" axis.
func BitemporalKnownAt(ctx context.Context, q bitemporal.Querier, tenant uuid.UUID, subject, field string, knownAt time.Time, dec bitemporal.Decision, opts ...bitemporal.Option) (BitemporalView, error) {
	result, err := bitemporal.KnownAt(ctx, q, tenant, subject, field, knownAt, dec, opts...)
	if err != nil {
		return BitemporalView{}, fmt.Errorf("explorer: bitemporal known-at %s/%s at %s: %w", subject, field, knownAt, err)
	}
	return toBitemporalView(result), nil
}

// BitemporalQuery runs an arbitrary internal/data/bitemporal.Request
// (BETWEEN, HISTORY, or a caller-built CURRENT/EFFECTIVE_AS_OF/KNOWN_AS_OF
// request needing options AsOf/KnownAt do not expose, such as pagination
// via Cursor) through internal/data/bitemporal.Query directly.
func BitemporalQuery(ctx context.Context, q bitemporal.Querier, req bitemporal.Request, dec bitemporal.Decision, opts ...bitemporal.Option) (BitemporalView, error) {
	result, err := bitemporal.Query(ctx, q, req, dec, opts...)
	if err != nil {
		return BitemporalView{}, fmt.Errorf("explorer: bitemporal query: %w", err)
	}
	return toBitemporalView(result), nil
}

func digestFact(b *viewdigest.Builder, prefix string, f bitemporal.Fact) *viewdigest.Builder {
	b.String(prefix+".stream_key", f.StreamKey).
		Int(prefix+".sequence", f.Sequence).
		String(prefix+".event_id", f.EventID.String()).
		String(prefix+".schema_ref", f.SchemaRef).
		String(prefix+".assertion_class", string(f.AssertionClass)).
		String(prefix+".correction_kind", string(f.CorrectionKind)).
		String(prefix+".digest", f.Digest).
		String(prefix+".effective_at", f.EffectiveAt.UTC().Format(time.RFC3339Nano)).
		String(prefix+".recorded_at", f.RecordedAt.UTC().Format(time.RFC3339Nano))
	return b
}

func digestBitemporal(v BitemporalView) string {
	b := viewdigest.New().
		Int("facts", int64(len(v.Facts))).
		String("next_cursor", v.NextCursor)
	for i, f := range v.Facts {
		digestFact(b, fmt.Sprintf("fact[%d]", i), f)
	}
	return b.Digest()
}
