package runtime

import (
	"context"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
)

// continuationNamespace is the fixed UUIDv5 namespace a continuation record's
// identity is derived under -- see [ContinuationID].
var continuationNamespace = uuid.MustParse("7c1d9e2a-3f6b-4c8d-9a1e-6b2f4d8c7a5e")

// ContinuationID derives one continuation record's identity from exactly the
// tuple that defines "this advancement raised this intent for this target
// node": the same reasoning [NodeExecutionID] documents for node attempts.
// A caller (or a retried transaction) that submits the same tuple twice
// collides on this identity rather than recording the intent a second time,
// which is the structural half of WF-RUN-025's "persists ... exactly once" --
// [ContinuationStore] additionally makes the insert itself a no-op on
// collision, so a retried write is silent rather than a refusal.
func ContinuationID(tenantID, instanceID uuid.UUID, sourceNodeID string, sourceAttempt int, targetNodeID string, kind frontier.IntentKind) uuid.UUID {
	name := tenantID.String() + "\x00" + instanceID.String() + "\x00" + sourceNodeID + "\x00" +
		strconv.Itoa(sourceAttempt) + "\x00" + targetNodeID + "\x00" + string(kind)
	return uuid.NewSHA1(continuationNamespace, []byte(name))
}

// ContinuationStore is the durable [ContinuationSink] backed by
// migrations/00018_workflow_continuation.sql. It holds no state; every method
// takes its [Executor] explicitly, matching [Store]'s own shape.
//
// It is this package's own append-only ledger of scheduling intents raised,
// not a WorkItem, timer or signal subscription store -- those belong to the
// systems that actually own that governed state (internal/humanwork and
// whatever owns timers/signals), which a caller composes as a further
// [ContinuationSink] of its own around or instead of this one. What
// ContinuationStore exists to prove is WF-RUN-025's atomicity claim: every
// derived intent lands in the same transaction as the node and instance
// writes, or none of it does.
type ContinuationStore struct{}

var _ ContinuationSink = ContinuationStore{}

// RequireWorkItem implements [ContinuationSink].
func (ContinuationStore) RequireWorkItem(ctx context.Context, ex Executor, rec ContinuationRecord) error {
	return insertContinuation(ctx, ex, rec)
}

// RequireSignalSubscription implements [ContinuationSink].
func (ContinuationStore) RequireSignalSubscription(ctx context.Context, ex Executor, rec ContinuationRecord) error {
	return insertContinuation(ctx, ex, rec)
}

// RequireTimer implements [ContinuationSink].
func (ContinuationStore) RequireTimer(ctx context.Context, ex Executor, rec ContinuationRecord) error {
	return insertContinuation(ctx, ex, rec)
}

// MarkReady implements [ContinuationSink].
func (ContinuationStore) MarkReady(ctx context.Context, ex Executor, rec ContinuationRecord) error {
	return insertContinuation(ctx, ex, rec)
}

// Complete implements [ContinuationSink].
func (ContinuationStore) Complete(ctx context.Context, ex Executor, rec ContinuationRecord) error {
	return insertContinuation(ctx, ex, rec)
}

const continuationColumns = `tenant_id, continuation_id, instance_id, source_node_id, source_attempt,
	target_node_id, kind, route_key, ref, terminal_code, recorded_at,
	correlation_id, causation_id, logical_operation_id, attempt_id, trace_id, trace_span_id, trace_flags, trace_state, trace_link_expires_at`

func insertContinuation(ctx context.Context, ex Executor, rec ContinuationRecord) error {
	if rec.TenantID == uuid.Nil || rec.InstanceID == uuid.Nil {
		return refuse(CodeInvalidRecord, rec.InstanceID.String(), rec.TargetNodeID,
			"continuation record carries a nil tenant or instance id")
	}
	rec.Causal = normalizeCausal(rec.Causal)
	if rec.SourceNodeID == "" || rec.TargetNodeID == "" {
		return refuse(CodeInvalidRecord, rec.InstanceID.String(), rec.TargetNodeID,
			"continuation record names no source or target node")
	}
	if !rec.Kind.Valid() {
		return refuse(CodeInvalidRecord, rec.InstanceID.String(), rec.TargetNodeID,
			"continuation record carries undeclared intent kind %q", rec.Kind)
	}
	id := ContinuationID(rec.TenantID, rec.InstanceID, rec.SourceNodeID, rec.SourceAttempt, rec.TargetNodeID, rec.Kind)
	args := []any{rec.TenantID, id, rec.InstanceID, rec.SourceNodeID, rec.SourceAttempt,
		rec.TargetNodeID, string(rec.Kind), nullableText(rec.RouteKey), nullableText(rec.Ref),
		nullableText(rec.TerminalCode), zeroTimeOrNil(rec.RecordedAt)}
	args = append(args, causalValue(rec.Causal)...)
	_, err := ex.Exec(ctx, `
		INSERT INTO workflow_continuation (`+continuationColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, COALESCE($11, now()), $12, $13, $14, $15, $16, $17, $18, $19, $20)
		ON CONFLICT (tenant_id, continuation_id) DO NOTHING`,
		args...)
	if err != nil {
		return wrap(CodeStorageFailed, rec.InstanceID.String(), rec.TargetNodeID, err,
			"insert continuation record for intent %s", rec.Kind)
	}
	return nil
}

func normalizeCausal(c *CausalMetadata) *CausalMetadata {
	if c == nil {
		return nil
	}
	for _, value := range []string{c.CorrelationID, c.CausationID, c.LogicalOperationID, c.AttemptID} {
		if strings.TrimSpace(value) == "" || len(value) > 128 {
			return nil
		}
	}
	out := *c
	out.TraceLink = nil
	if c.TraceLink != nil && validTraceID(c.TraceLink.TraceID, 16) && validTraceID(c.TraceLink.SpanID, 8) && len(c.TraceLink.TraceState) <= 256 {
		link := *c.TraceLink
		out.TraceLink = &link
	}
	return &out
}

func validTraceID(value string, size int) bool {
	if value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != size {
		return false
	}
	for _, b := range decoded {
		if b != 0 {
			return true
		}
	}
	return false
}

func causalValue(c *CausalMetadata) []any {
	c = normalizeCausal(c)
	if c == nil {
		return []any{nil, nil, nil, nil, nil, nil, nil, nil, nil}
	}
	if c.TraceLink == nil {
		return []any{c.CorrelationID, c.CausationID, c.LogicalOperationID, c.AttemptID, nil, nil, nil, nil, nil}
	}
	return []any{c.CorrelationID, c.CausationID, c.LogicalOperationID, c.AttemptID, c.TraceLink.TraceID, c.TraceLink.SpanID, c.TraceLink.TraceFlags, c.TraceLink.TraceState, zeroTimeOrNil(c.TraceLink.ExpiresAt)}
}

// MemorySink is an in-process [ContinuationSink] that only records what it
// was called with. It is the double the WF-RUN-025 test matrix uses wherever
// a real transaction is not the point of the test: it never touches storage
// and its Records are visible to the test immediately, in call order.
//
// It is safe for concurrent use, which the RACE case needs even though a
// given instance's advancements are themselves serialized by the instance
// version fence.
type MemorySink struct {
	mu      sync.Mutex
	records []ContinuationRecord
}

var _ ContinuationSink = (*MemorySink)(nil)

// NewMemorySink returns an empty sink.
func NewMemorySink() *MemorySink { return &MemorySink{} }

func (s *MemorySink) record(rec ContinuationRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, rec)
	return nil
}

// RequireWorkItem implements [ContinuationSink].
func (s *MemorySink) RequireWorkItem(_ context.Context, _ Executor, rec ContinuationRecord) error {
	return s.record(rec)
}

// RequireSignalSubscription implements [ContinuationSink].
func (s *MemorySink) RequireSignalSubscription(_ context.Context, _ Executor, rec ContinuationRecord) error {
	return s.record(rec)
}

// RequireTimer implements [ContinuationSink].
func (s *MemorySink) RequireTimer(_ context.Context, _ Executor, rec ContinuationRecord) error {
	return s.record(rec)
}

// MarkReady implements [ContinuationSink].
func (s *MemorySink) MarkReady(_ context.Context, _ Executor, rec ContinuationRecord) error {
	return s.record(rec)
}

// Complete implements [ContinuationSink].
func (s *MemorySink) Complete(_ context.Context, _ Executor, rec ContinuationRecord) error {
	return s.record(rec)
}

// Records returns a copy of every record this sink has seen, in call order.
func (s *MemorySink) Records() []ContinuationRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ContinuationRecord(nil), s.records...)
}
