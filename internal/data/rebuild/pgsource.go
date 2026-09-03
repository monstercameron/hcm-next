package rebuild

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/projection"
)

// PGSource is the [Source] against the real ledger: committed events through
// the ledger's own reader, the head through the projection plane's stream
// head, and the schema and correction checks against the ledger's own tables.
// It reads; it writes nothing.
type PGSource struct {
	reader *ledger.Reader
}

// NewPGSource returns the live source built on the given ledger reader.
func NewPGSource(reader *ledger.Reader) *PGSource {
	return &PGSource{reader: reader}
}

// Head implements Source.
func (s *PGSource) Head(ctx context.Context, q dbport.Querier, tenant uuid.UUID, stream string) (int64, error) {
	return projection.StreamHead(ctx, q, tenant, stream)
}

// Events implements Source.
func (s *PGSource) Events(ctx context.Context, q dbport.Querier, tenant uuid.UUID, stream string) ([]ledger.EventRecord, error) {
	return s.reader.ReadStream(ctx, q, tenant, stream)
}

// SchemaRegistered implements Source.
func (s *PGSource) SchemaRegistered(ctx context.Context, q dbport.Querier, tenant uuid.UUID, schemaRef string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM payload_schema
			WHERE tenant_id = $1 AND schema_ref = $2)`,
		tenant, schemaRef).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("check schema registration %s: %w", schemaRef, err)
	}
	return ok, nil
}

// CorrectionTarget implements Source. The ledger records a correction's
// target on the event itself; this resolves it and reports whether the
// referenced assertion actually exists for the same tenant.
func (s *PGSource) CorrectionTarget(ctx context.Context, q dbport.Querier, ev ledger.EventRecord) (CorrectionRef, error) {
	var (
		stream *string
		seq    *int64
	)
	err := q.QueryRow(ctx, `
		SELECT corrects_stream_key, corrects_sequence
		FROM ledger_event
		WHERE tenant_id = $1 AND stream_key = $2 AND sequence = $3`,
		ev.Tenant, ev.StreamKey, ev.Sequence).Scan(&stream, &seq)
	if errors.Is(err, dbport.ErrNoRows) {
		return CorrectionRef{}, fmt.Errorf("correction event %d not found on stream %s", ev.Sequence, ev.StreamKey)
	}
	if err != nil {
		return CorrectionRef{}, fmt.Errorf("read correction reference at sequence %d: %w", ev.Sequence, err)
	}
	if stream == nil || seq == nil {
		return CorrectionRef{}, nil
	}
	ref := CorrectionRef{HasReference: true, Stream: *stream, Sequence: *seq}

	var class *string
	err = q.QueryRow(ctx, `
		SELECT assertion_class
		FROM ledger_event
		WHERE tenant_id = $1 AND stream_key = $2 AND sequence = $3`,
		ev.Tenant, ref.Stream, ref.Sequence).Scan(&class)
	switch {
	case errors.Is(err, dbport.ErrNoRows):
		return ref, nil
	case err != nil:
		return CorrectionRef{}, fmt.Errorf("resolve correction target %s@%d: %w", ref.Stream, ref.Sequence, err)
	}
	ref.Resolved = true
	ref.Class = *class
	return ref, nil
}
