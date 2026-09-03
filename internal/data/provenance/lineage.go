package provenance

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/ledger"
)

// Status is Lineage's honest completeness verdict for one intent's
// provenance graph (specs/provenance-graph-and-lineage.md: "Missing/
// lagging/quarantined publishers produce PARTIAL_LINEAGE; the UI/agent/audit
// package may not call it complete").
type Status string

const (
	// StatusComplete reports that every ledger event on the queried stream
	// has a matching provenance_record.
	StatusComplete Status = "COMPLETE"
	// StatusPartial reports that at least one ledger event on the queried
	// stream has no matching provenance_record yet.
	StatusPartial Status = "PARTIAL"
	// StatusUnknown reports that the queried stream has no events to judge
	// completeness against - not evidence of completeness, and not
	// evidence of a problem either.
	StatusUnknown Status = "UNKNOWN"
)

// Result is one Lineage call's answer: the provenance edges published for
// an intent, plus whether that is the whole story yet.
type Result struct {
	IntentRef string
	Status    Status
	Edges     []Record
	// LedgerEventCount is how many events the queried stream holds.
	LedgerEventCount int
	// PublishedLedgerEventCount is how many of those events have a matching
	// LEDGER_EVENT provenance_record.
	PublishedLedgerEventCount int
}

// Lineage returns the provenance graph for one intent: every
// provenance_record published under intentRef, ordered by publication time,
// plus a [Status] computed by comparing streamKey's ledger events against
// the LEDGER_EVENT records found. q may be a bare connection, a pool or an
// open transaction; it performs no writes.
func Lineage(ctx context.Context, q ledger.Querier, reader *ledger.Reader, tenant uuid.UUID, streamKey, intentRef string) (Result, error) {
	events, err := reader.ReadStream(ctx, q, tenant, streamKey)
	if err != nil {
		return Result{}, fmt.Errorf("provenance: lineage: read stream %s: %w", streamKey, err)
	}

	rows, err := q.Query(ctx, selectRecordSQL+` WHERE tenant_id = $1 AND intent_ref = $2 ORDER BY published_at ASC, source_ref ASC`,
		tenant, intentRef)
	if err != nil {
		return Result{}, fmt.Errorf("provenance: lineage: read records for %s: %w", intentRef, err)
	}
	defer rows.Close()

	var edges []Record
	published := make(map[string]bool)
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return Result{}, fmt.Errorf("provenance: lineage: %w", err)
		}
		edges = append(edges, rec)
		if rec.SourceKind == SourceLedgerEvent {
			published[rec.SourceRef] = true
		}
	}
	if err := rows.Err(); err != nil {
		return Result{}, fmt.Errorf("provenance: lineage: %w", err)
	}

	publishedCount := 0
	for _, ev := range events {
		if published[LedgerEventSourceRef(streamKey, ev.Sequence)] {
			publishedCount++
		}
	}

	status := StatusUnknown
	switch {
	case len(events) == 0:
		status = StatusUnknown
	case publishedCount == len(events):
		status = StatusComplete
	default:
		status = StatusPartial
	}

	return Result{
		IntentRef:                 intentRef,
		Status:                    status,
		Edges:                     edges,
		LedgerEventCount:          len(events),
		PublishedLedgerEventCount: publishedCount,
	}, nil
}
