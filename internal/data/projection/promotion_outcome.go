package projection

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/ledger"
)

// PromotionOutcomeProjectionName is the critical checkpoint used by the
// promotion terminal writer. The projection is reconstructed from ledger
// envelopes only; no workflow or outbox state is consulted during replay.
const PromotionOutcomeProjectionName = "workflow.promotion_outcome"

// PromotionOutcomeSchemaRef is the schema accepted by the promotion outcome
// replay. A schema mismatch is evidence that the source is not this projection.
const PromotionOutcomeSchemaRef = "hcmnext.workflow.PromotionOutcome/v2"

// PromotionOutcomeRow is the semantic row reconstructed from one ledger
// event. The event digest is the payload/content identity; EventID and the
// stream coordinate make the row key unambiguous.
type PromotionOutcomeRow struct {
	Tenant      uuid.UUID
	StreamKey   string
	Sequence    int64
	EventID     uuid.UUID
	EventDigest string
	SchemaRef   string
}

// PromotionOutcomeReport is the complete deterministic result of replaying a
// promotion-outcome stream.
type PromotionOutcomeReport struct {
	Tenant     uuid.UUID
	StreamKey  string
	SourceHead int64
	RowCount   int64
	Digest     string
	Rows       []PromotionOutcomeRow
}

// PromotionOutcomeDivergence names the first row that differs between a
// ledger rebuild and the live projection.
type PromotionOutcomeDivergence struct {
	RowKey   string
	Field    string
	Expected string
	Actual   string
}

// ErrPromotionOutcomeDiverged refuses promotion when the first differing row
// is not identical. The row key is always included in the error for repair
// tooling and operator evidence.
type ErrPromotionOutcomeDiverged struct {
	Difference PromotionOutcomeDivergence
}

func (e ErrPromotionOutcomeDiverged) Error() string {
	return fmt.Sprintf("projection %s diverged at row %s field %s: expected %q, actual %q", PromotionOutcomeProjectionName, e.Difference.RowKey, e.Difference.Field, e.Difference.Expected, e.Difference.Actual)
}

// RebuildPromotionOutcome replays a committed ledger slice into semantic
// rows. It refuses missing, reordered, duplicate, foreign, or wrong-schema
// events before producing a digest, so an unsafe rebuild can never be
// mistaken for a clean empty projection.
func RebuildPromotionOutcome(events []ledger.EventRecord, tenant uuid.UUID, streamKey string) (PromotionOutcomeReport, error) {
	if tenant == uuid.Nil {
		return PromotionOutcomeReport{}, fmt.Errorf("promotion outcome rebuild: tenant is required")
	}
	if streamKey == "" {
		return PromotionOutcomeReport{}, fmt.Errorf("promotion outcome rebuild: stream is required")
	}
	rows := make([]PromotionOutcomeRow, 0, len(events))
	for index, event := range events {
		if event.Tenant != tenant {
			return PromotionOutcomeReport{}, fmt.Errorf("promotion outcome rebuild: event %d belongs to tenant %s, want %s", event.Sequence, event.Tenant, tenant)
		}
		if event.StreamKey != streamKey {
			return PromotionOutcomeReport{}, fmt.Errorf("promotion outcome rebuild: event %d is on stream %q, want %q", event.Sequence, event.StreamKey, streamKey)
		}
		want := int64(index + 1)
		if event.Sequence != want {
			return PromotionOutcomeReport{}, fmt.Errorf("promotion outcome rebuild: expected sequence %d, got %d", want, event.Sequence)
		}
		if event.EventID == uuid.Nil || event.Digest == "" {
			return PromotionOutcomeReport{}, fmt.Errorf("promotion outcome rebuild: row %s@%d has no event identity or digest", streamKey, event.Sequence)
		}
		if event.SchemaRef != PromotionOutcomeSchemaRef {
			return PromotionOutcomeReport{}, fmt.Errorf("promotion outcome rebuild: row %s@%d cites schema %q, want %q", streamKey, event.Sequence, event.SchemaRef, PromotionOutcomeSchemaRef)
		}
		rows = append(rows, PromotionOutcomeRow{
			Tenant: tenant, StreamKey: streamKey, Sequence: event.Sequence,
			EventID: event.EventID, EventDigest: event.Digest, SchemaRef: event.SchemaRef,
		})
	}
	return PromotionOutcomeReport{
		Tenant: tenant, StreamKey: streamKey, SourceHead: int64(len(rows)),
		RowCount: int64(len(rows)), Digest: DigestPromotionOutcomeRows(rows), Rows: rows,
	}, nil
}

// DigestPromotionOutcomeRows computes the semantic digest for a projection's
// rows. It is intentionally independent of database row order and excludes
// mutable timestamps or outbox status.
func DigestPromotionOutcomeRows(rows []PromotionOutcomeRow) string {
	h := sha256.New()
	for _, row := range rows {
		writePromotionField(h, row.Tenant[:])
		writePromotionField(h, []byte(row.StreamKey))
		var sequence [8]byte
		binary.BigEndian.PutUint64(sequence[:], uint64(row.Sequence))
		writePromotionField(h, sequence[:])
		writePromotionField(h, row.EventID[:])
		writePromotionField(h, []byte(row.EventDigest))
		writePromotionField(h, []byte(row.SchemaRef))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ComparePromotionOutcome compares row count, semantic digest, and then rows
// in order. The row comparison is retained even when the digest differs so
// the returned error names the first differing row rather than only reporting
// an opaque hash mismatch.
func ComparePromotionOutcome(rebuilt, live PromotionOutcomeReport) error {
	if rebuilt.Tenant != live.Tenant {
		return ErrPromotionOutcomeDiverged{Difference: PromotionOutcomeDivergence{RowKey: "tenant", Field: "tenant", Expected: rebuilt.Tenant.String(), Actual: live.Tenant.String()}}
	}
	if rebuilt.StreamKey != live.StreamKey {
		return ErrPromotionOutcomeDiverged{Difference: PromotionOutcomeDivergence{RowKey: "stream", Field: "stream_key", Expected: rebuilt.StreamKey, Actual: live.StreamKey}}
	}
	if rebuilt.RowCount != live.RowCount {
		return ErrPromotionOutcomeDiverged{Difference: PromotionOutcomeDivergence{RowKey: "projection", Field: "row_count", Expected: fmt.Sprint(rebuilt.RowCount), Actual: fmt.Sprint(live.RowCount)}}
	}
	for i := range rebuilt.Rows {
		if diff, ok := firstPromotionDifference(rebuilt.Rows[i], live.Rows[i]); ok {
			return ErrPromotionOutcomeDiverged{Difference: diff}
		}
	}
	if rebuilt.Digest != live.Digest {
		return ErrPromotionOutcomeDiverged{Difference: PromotionOutcomeDivergence{RowKey: "projection", Field: "digest", Expected: rebuilt.Digest, Actual: live.Digest}}
	}
	return nil
}

// ComparePromotionOutcomeDigests is the compact form used when the live
// projection has already supplied its row count and semantic digest.
func ComparePromotionOutcomeDigests(rebuilt PromotionOutcomeReport, liveRows int64, liveDigest string) error {
	if rebuilt.RowCount != liveRows {
		return ErrPromotionOutcomeDiverged{Difference: PromotionOutcomeDivergence{RowKey: "projection", Field: "row_count", Expected: fmt.Sprint(rebuilt.RowCount), Actual: fmt.Sprint(liveRows)}}
	}
	if rebuilt.Digest != liveDigest {
		return ErrPromotionOutcomeDiverged{Difference: PromotionOutcomeDivergence{RowKey: "projection", Field: "digest", Expected: rebuilt.Digest, Actual: liveDigest}}
	}
	return nil
}

func firstPromotionDifference(expected, actual PromotionOutcomeRow) (PromotionOutcomeDivergence, bool) {
	rowKey := fmt.Sprintf("%s@%d", expected.StreamKey, expected.Sequence)
	fields := []struct{ name, expected, actual string }{
		{"tenant", expected.Tenant.String(), actual.Tenant.String()},
		{"stream_key", expected.StreamKey, actual.StreamKey},
		{"sequence", fmt.Sprint(expected.Sequence), fmt.Sprint(actual.Sequence)},
		{"event_id", expected.EventID.String(), actual.EventID.String()},
		{"event_digest", expected.EventDigest, actual.EventDigest},
		{"schema_ref", expected.SchemaRef, actual.SchemaRef},
	}
	for _, field := range fields {
		if field.expected != field.actual {
			return PromotionOutcomeDivergence{RowKey: rowKey, Field: field.name, Expected: field.expected, Actual: field.actual}, true
		}
	}
	return PromotionOutcomeDivergence{}, false
}

func writePromotionField(h interface{ Write([]byte) (int, error) }, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = h.Write(length[:])
	_, _ = h.Write(value)
}
