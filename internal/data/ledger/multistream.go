package ledger

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// StreamAppend is one stream's ordered portion of a multi-stream append.
// ExpectedHead is the head observed when the caller prepared the batch. The
// optional ExpectedSequence spelling is accepted for callers that use the
// stream-head vocabulary; when both are supplied they must agree.
type StreamAppend struct {
	StreamKey        string
	ExpectedHead     int64
	ExpectedSequence int64
	Events           []AppendRequest
}

// MultiStreamAppendRequest describes one atomic append across local streams.
// All streams must belong to Tenant and all events are written in canonical
// stream-key order, preserving the order supplied inside each stream.
type MultiStreamAppendRequest struct {
	Tenant  uuid.UUID
	Streams []StreamAppend
}

// MultiAppendRequest is a concise compatibility alias.
type MultiAppendRequest = MultiStreamAppendRequest

// StreamHeadTransition records the head before and after one stream's batch.
type StreamHeadTransition struct {
	Tenant       uuid.UUID
	StreamKey    string
	Before       int64
	After        int64
	BeforeDigest string
	AfterDigest  string
}

// MultiStreamAppendReceipt contains every event receipt and every resulting
// head, both ordered by canonical stream key.
type MultiStreamAppendReceipt struct {
	Events []AppendReceipt
	Heads  []StreamHeadTransition
}

// MultiAppendReceipt is a concise compatibility alias.
type MultiAppendReceipt = MultiStreamAppendReceipt

// AppendMulti atomically appends all events in req. It locks and validates
// every stream head before writing the first event. Consequently a stale head
// is refused with no partial append even when the caller keeps the transaction
// open after receiving the error; the caller still owns the final rollback or
// commit as required by dbport.Tx.
func AppendMulti(ctx context.Context, tx dbport.Tx, req MultiStreamAppendRequest) (result MultiStreamAppendReceipt, err error) {
	if tx == nil {
		return MultiStreamAppendReceipt{}, errors.New("ledger: multi-stream append requires a transaction")
	}
	if req.Tenant == uuid.Nil {
		return MultiStreamAppendReceipt{}, errors.New("ledger: multi-stream append requires a tenant")
	}
	streams, err := canonicalStreams(req)
	if err != nil {
		return MultiStreamAppendReceipt{}, err
	}
	if _, err := tx.Exec(ctx, `SAVEPOINT ledger_multi_stream_append`); err != nil {
		return MultiStreamAppendReceipt{}, fmt.Errorf("ledger: create multi-stream savepoint: %w", err)
	}
	defer func() {
		if err != nil {
			_, _ = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT ledger_multi_stream_append`)
		}
		_, _ = tx.Exec(ctx, `RELEASE SAVEPOINT ledger_multi_stream_append`)
	}()

	// This phase is deliberately separate from the write phase. PostgreSQL
	// keeps these row locks until the caller commits or rolls back, so no other
	// append can change a validated head while this batch is being written. The
	// canonical stream order is also the database lock order, which prevents two
	// overlapping multi-stream appends from waiting on each other in a cycle.
	streamKeys := make([]string, 0, len(streams))
	for _, stream := range streams {
		streamKeys = append(streamKeys, stream.StreamKey)
	}
	rows, err := tx.Query(ctx, `
		SELECT stream_key, head_sequence, head_digest
		FROM stream_head
		WHERE tenant_id = $1 AND stream_key = ANY($2::text[])
		ORDER BY stream_key ASC
		FOR UPDATE`, req.Tenant, streamKeys)
	if err != nil {
		return MultiStreamAppendReceipt{}, fmt.Errorf("ledger: lock stream heads: %w", err)
	}
	type lockedHead struct {
		sequence int64
		digest   *string
	}
	locked := make(map[string]lockedHead, len(streams))
	for rows.Next() {
		var (
			streamKey string
			head      lockedHead
		)
		if err := rows.Scan(&streamKey, &head.sequence, &head.digest); err != nil {
			rows.Close()
			return MultiStreamAppendReceipt{}, fmt.Errorf("ledger: scan stream heads: %w", err)
		}
		locked[streamKey] = head
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return MultiStreamAppendReceipt{}, fmt.Errorf("ledger: read stream heads: %w", err)
	}
	rows.Close()

	for i := range streams {
		stream := &streams[i]
		head, ok := locked[stream.StreamKey]
		if !ok {
			return MultiStreamAppendReceipt{}, ErrStreamNotFound{Tenant: req.Tenant, StreamKey: stream.StreamKey}
		}
		if head.sequence != stream.expected {
			return MultiStreamAppendReceipt{}, ErrStaleStream{
				Tenant: req.Tenant, StreamKey: stream.StreamKey,
				Expected: stream.expected, Actual: head.sequence,
			}
		}
		stream.actual = head.sequence
	}

	// Validate every event's tenant and expected position before the first
	// insert. canonicalStreams validates the request shape; this second phase
	// preserves the old per-event stale-head error and ordering semantics.
	for i := range streams {
		stream := &streams[i]
		for eventIndex, event := range stream.Events {
			if event.Tenant != req.Tenant {
				return MultiStreamAppendReceipt{}, fmt.Errorf("ledger: event %d on stream %s belongs to tenant %s, want %s", eventIndex, stream.StreamKey, event.Tenant, req.Tenant)
			}
			expected := stream.expected + int64(eventIndex)
			if event.ExpectedHead != expected {
				return MultiStreamAppendReceipt{}, ErrStaleStream{
					Tenant: req.Tenant, StreamKey: stream.StreamKey,
					Expected: expected, Actual: event.ExpectedHead,
				}
			}
		}
	}

	appender := New()
	result = MultiStreamAppendReceipt{
		Events: make([]AppendReceipt, 0, totalEvents(streams)),
		Heads:  make([]StreamHeadTransition, 0, len(streams)),
	}
	type pendingAppend struct {
		streamKey  string
		eventIndex int
		req        AppendRequest
		receipt    AppendReceipt
	}
	var statements []dbport.Statement
	var pending []pendingAppend
	for i := range streams {
		stream := &streams[i]
		before := stream.actual
		head := locked[stream.StreamKey]
		currentDigest := stringPointer(head.digest)
		currentHead := before
		for eventIndex, event := range stream.Events {
			algorithm, digest, length, digestErr := appender.digester.Digest(digestInput(event), event.SchemaRef)
			if digestErr != nil {
				return MultiStreamAppendReceipt{}, fmt.Errorf("ledger: append stream %s event %d: canonical digest: %w", stream.StreamKey, eventIndex, digestErr)
			}
			receipt, found, replayErr := appender.replay(ctx, tx, event, digest, algorithm, length)
			if replayErr != nil {
				return MultiStreamAppendReceipt{}, fmt.Errorf("ledger: append stream %s event %d: %w", stream.StreamKey, eventIndex, replayErr)
			}
			if found {
				result.Events = append(result.Events, receipt)
				continue
			}
			if event.AssertionClass.RequiresAuthority() {
				if err := appender.checkAuthority(ctx, tx, event); err != nil {
					return MultiStreamAppendReceipt{}, fmt.Errorf("ledger: append stream %s event %d: %w", stream.StreamKey, eventIndex, err)
				}
			}

			write := appender.planEvent(event, currentHead, digest, algorithm, length)
			statements = append(statements, write.statements[:]...)
			pending = append(pending, pendingAppend{
				streamKey: stream.StreamKey, eventIndex: eventIndex, req: event,
				receipt: write.receipt,
			})
			result.Events = append(result.Events, pending[len(pending)-1].receipt)
			currentHead = write.receipt.Sequence
			currentDigest = digest
		}
		result.Heads = append(result.Heads, StreamHeadTransition{
			Tenant: req.Tenant, StreamKey: stream.StreamKey, Before: before, After: currentHead,
			BeforeDigest: stringPointer(head.digest), AfterDigest: currentDigest,
		})
	}
	counts, batchErr := dbport.ExecAll(ctx, tx, statements)
	if batchErr != nil {
		pendingIndex := len(counts) / 2
		if pendingIndex < len(pending) {
			item := pending[pendingIndex]
			if len(counts)%2 == 0 {
				if conflict, ok := idempotencyConflict(batchErr, item.req, item.receipt.Digest); ok {
					return MultiStreamAppendReceipt{}, fmt.Errorf("ledger: append stream %s event %d: %w", item.streamKey, item.eventIndex, conflict)
				}
			}
			return MultiStreamAppendReceipt{}, fmt.Errorf("ledger: append stream %s event %d: %w", item.streamKey, item.eventIndex, batchErr)
		}
		return MultiStreamAppendReceipt{}, fmt.Errorf("ledger: batch append: %w", batchErr)
	}
	if len(counts) != len(statements) {
		return MultiStreamAppendReceipt{}, fmt.Errorf("ledger: batch append returned %d results, want %d", len(counts), len(statements))
	}
	for i, item := range pending {
		if counts[2*i+1] != 1 {
			return MultiStreamAppendReceipt{}, fmt.Errorf("ledger: append stream %s event %d: advance stream head %s: %d rows updated", item.streamKey, item.eventIndex, item.streamKey, counts[2*i+1])
		}
	}
	return result, nil
}

// AppendMany is the descriptive spelling of AppendMulti.
func AppendMany(ctx context.Context, tx dbport.Tx, req MultiStreamAppendRequest) (MultiStreamAppendReceipt, error) {
	return AppendMulti(ctx, tx, req)
}

type canonicalStream struct {
	StreamAppend
	expected int64
	actual   int64
}

func canonicalStreams(req MultiStreamAppendRequest) ([]canonicalStream, error) {
	if len(req.Streams) == 0 {
		return nil, errors.New("ledger: multi-stream append requires at least one stream")
	}
	out := make([]canonicalStream, 0, len(req.Streams))
	seen := make(map[string]struct{}, len(req.Streams))
	for _, stream := range req.Streams {
		if stream.StreamKey == "" {
			return nil, errors.New("ledger: multi-stream append stream key is required")
		}
		if _, ok := seen[stream.StreamKey]; ok {
			return nil, fmt.Errorf("ledger: duplicate stream %q in multi-stream append", stream.StreamKey)
		}
		seen[stream.StreamKey] = struct{}{}
		expected := stream.ExpectedHead
		if stream.ExpectedSequence != 0 {
			if stream.ExpectedHead != 0 && stream.ExpectedHead != stream.ExpectedSequence {
				return nil, fmt.Errorf("ledger: stream %s has conflicting expected heads %d and %d", stream.StreamKey, stream.ExpectedHead, stream.ExpectedSequence)
			}
			expected = stream.ExpectedSequence
		}
		if expected < 0 {
			return nil, fmt.Errorf("ledger: stream %s has negative expected head %d", stream.StreamKey, expected)
		}
		if len(stream.Events) == 0 {
			return nil, fmt.Errorf("ledger: stream %s has no events", stream.StreamKey)
		}
		for i, event := range stream.Events {
			if event.StreamKey != stream.StreamKey {
				return nil, fmt.Errorf("ledger: event %d targets stream %q, batch stream is %q", i, event.StreamKey, stream.StreamKey)
			}
			if event.Tenant != req.Tenant {
				return nil, fmt.Errorf("ledger: event %d on stream %s belongs to tenant %s, want %s", i, stream.StreamKey, event.Tenant, req.Tenant)
			}
			if err := validate(event); err != nil {
				return nil, fmt.Errorf("ledger: event %d on stream %s is invalid: %w", i, stream.StreamKey, err)
			}
		}
		out = append(out, canonicalStream{StreamAppend: stream, expected: expected})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StreamKey < out[j].StreamKey })
	return out, nil
}

func totalEvents(streams []canonicalStream) int {
	total := 0
	for _, stream := range streams {
		total += len(stream.Events)
	}
	return total
}

func stringPointer(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
