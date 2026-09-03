package onboarding

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity"
)

// FieldFilterConnector wraps a [connectivity.Connector], redacting every
// field not on the manifest's allow-list for its object before a page ever
// leaves this process. It changes nothing else: descriptor, bounds,
// capabilities, schema version, snapshot and cursor continuation are the
// underlying connector's own, so wrapping a connector in a filter never
// changes what a caller resumes from.
type FieldFilterConnector struct {
	connectivity.Connector
	// Allow maps an object to the fields admitted for it. An object absent
	// from the map, or mapped to an empty slice, admits every field.
	Allow map[connectivity.ObjectKind][]string
}

// FilterFields returns a [connectivity.Connector] that narrows c's Read
// results to the fields allow lists.
func FilterFields(c connectivity.Connector, allow map[connectivity.ObjectKind][]string) *FieldFilterConnector {
	return &FieldFilterConnector{Connector: c, Allow: allow}
}

// Read implements [connectivity.Connector]. It delegates to the wrapped
// connector and then strips every field not on the object's allow list.
func (f *FieldFilterConnector) Read(ctx context.Context, req connectivity.ReadRequest) (connectivity.Page, error) {
	page, err := f.Connector.Read(ctx, req)
	if err != nil {
		return page, err
	}
	allowed := f.Allow[req.Object]
	if len(allowed) == 0 {
		return page, nil
	}
	allowSet := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		allowSet[a] = true
	}
	filtered := make([]connectivity.Record, len(page.Records))
	for i, rec := range page.Records {
		fields := make(map[string]string, len(allowed))
		for k, v := range rec.Fields {
			if allowSet[k] {
				fields[k] = v
			}
		}
		rec.Fields = fields
		filtered[i] = rec
	}
	page.Records = filtered
	return page, nil
}

// QuarantinedRecord is one record isolated from a page because it failed
// structural validation or exceeded the connector's per-record byte bound.
//
// Isolating it, rather than failing the page it arrived in, is what
// "poison-row isolation" means: one malformed row must never block every
// well-formed row behind it in the same page, and must never abort a batch
// that is otherwise importable.
type QuarantinedRecord struct {
	Object        connectivity.ObjectKind
	ExternalID    string
	Reason        string
	QuarantinedAt time.Time
}

// BudgetGuard wraps a [connectivity.Connector], enforcing an onboarding job's
// whole-run resource [Budget] and isolating malformed records rather than
// failing the page they arrived in.
//
// Budget accounting is cumulative across every Read this guard serves. A
// BudgetGuard is safe for concurrent use, matching the concurrency contract
// [connectivity.Connector] itself declares.
type BudgetGuard struct {
	// Connector is the wrapped, unbounded connector.
	Connector connectivity.Connector
	// Budget is the whole-run ceiling this guard enforces.
	Budget Budget
	// Now supplies wall-clock readings for the wall-time budget. Nil uses
	// the host wall clock; tests inject a deterministic one so a wall-time
	// bound can be proven without sleeping.
	Now func() time.Time

	mu         sync.Mutex
	startedAt  time.Time
	pages      uint64
	records    uint64
	bytes      uint64
	quarantine []QuarantinedRecord
}

// NewBudgetGuard returns a guard enforcing budget over c.
func NewBudgetGuard(c connectivity.Connector, budget Budget, now func() time.Time) *BudgetGuard {
	return &BudgetGuard{Connector: c, Budget: budget, Now: now}
}

func (g *BudgetGuard) now() time.Time {
	if g.Now == nil {
		return time.Now().UTC()
	}
	return g.Now().UTC()
}

// Descriptor implements [connectivity.Connector].
func (g *BudgetGuard) Descriptor() connectivity.Descriptor { return g.Connector.Descriptor() }

// Bounds implements [connectivity.Connector].
func (g *BudgetGuard) Bounds() connectivity.Bounds { return g.Connector.Bounds() }

// Capabilities implements [connectivity.Connector].
func (g *BudgetGuard) Capabilities() []connectivity.Capability { return g.Connector.Capabilities() }

// SchemaVersion implements [connectivity.Connector].
func (g *BudgetGuard) SchemaVersion(ctx context.Context, object connectivity.ObjectKind) (string, error) {
	return g.Connector.SchemaVersion(ctx, object)
}

// Snapshot implements [connectivity.Connector].
func (g *BudgetGuard) Snapshot(ctx context.Context, object connectivity.ObjectKind) (string, error) {
	return g.Connector.Snapshot(ctx, object)
}

// Quarantine returns every record isolated so far, in isolation order.
func (g *BudgetGuard) Quarantine() []QuarantinedRecord {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]QuarantinedRecord(nil), g.quarantine...)
}

// Spent reports this guard's cumulative resource consumption and the elapsed
// wall time since its first Read, as measured by its injected clock.
func (g *BudgetGuard) Spent() (pages, records, bytesSpent uint64, elapsed time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.startedAt.IsZero() {
		elapsed = g.now().Sub(g.startedAt)
	}
	return g.pages, g.records, g.bytes, elapsed
}

// Read implements [connectivity.Connector].
//
// It refuses before ever calling the wrapped connector once any budget axis
// is exhausted, which is what makes the refusal cheap and the resulting
// [observe.Checkpoint] exactly the last page this guard actually admitted:
// nothing partial, nothing wasted. On success it isolates every malformed or
// oversized record into the quarantine log and returns the page with only
// the admitted records, preserving their relative order and the underlying
// connector's own cursor and completeness verdict.
func (g *BudgetGuard) Read(ctx context.Context, req connectivity.ReadRequest) (connectivity.Page, error) {
	const op = "onboarding.BudgetGuard.Read"

	now := g.now()
	g.mu.Lock()
	if g.startedAt.IsZero() {
		g.startedAt = now
	}
	elapsed := now.Sub(g.startedAt)
	pages, records, bytesSpent := g.pages, g.records, g.bytes
	g.mu.Unlock()

	switch {
	case pages >= g.Budget.MaxPages:
		return connectivity.Page{}, newError(op, ErrBudgetExceeded, "page budget of %d exhausted", g.Budget.MaxPages)
	case records >= g.Budget.MaxRecords:
		return connectivity.Page{}, newError(op, ErrBudgetExceeded, "record budget of %d exhausted", g.Budget.MaxRecords)
	case bytesSpent >= g.Budget.MaxBytes:
		return connectivity.Page{}, newError(op, ErrBudgetExceeded, "byte budget of %d exhausted", g.Budget.MaxBytes)
	case g.Budget.MaxWallTime > 0 && elapsed >= g.Budget.MaxWallTime:
		return connectivity.Page{}, newError(op, ErrBudgetExceeded, "wall-time budget of %s exhausted", g.Budget.MaxWallTime)
	}

	page, err := g.Connector.Read(ctx, req)
	if err != nil {
		return page, err
	}

	bounds := g.Connector.Bounds()
	clean := make([]connectivity.Record, 0, len(page.Records))
	var pageBytes uint64
	var isolated []QuarantinedRecord
	for _, rec := range page.Records {
		if reason := invalidReason(rec, bounds); reason != "" {
			isolated = append(isolated, QuarantinedRecord{
				Object: req.Object, ExternalID: rec.ExternalID, Reason: reason, QuarantinedAt: now,
			})
			continue
		}
		clean = append(clean, rec)
		pageBytes += uint64(recordSize(rec))
	}
	page.Records = clean

	g.mu.Lock()
	g.pages++
	g.records += uint64(len(clean))
	g.bytes += pageBytes
	g.quarantine = append(g.quarantine, isolated...)
	g.mu.Unlock()

	return page, nil
}

// recordSize approximates a record's canonical size as the sum of its
// identity fields and every field key/value byte length. It is a cheap,
// deterministic proxy for wire size, not a claim of exact canonical bytes.
func recordSize(rec connectivity.Record) int {
	n := len(rec.ExternalID) + len(rec.SortKey) + len(rec.SourceVersion)
	for k, v := range rec.Fields {
		n += len(k) + len(v)
	}
	return n
}

// invalidReason reports why rec cannot be admitted, or "" if it can.
func invalidReason(rec connectivity.Record, bounds connectivity.Bounds) string {
	if err := rec.Validate(); err != nil {
		return err.Error()
	}
	if size := recordSize(rec); bounds.MaxRecordBytes > 0 && size > bounds.MaxRecordBytes {
		return fmt.Sprintf("record %q is %d bytes, exceeding the %d byte bound", rec.ExternalID, size, bounds.MaxRecordBytes)
	}
	return ""
}

var _ connectivity.Connector = (*FieldFilterConnector)(nil)
var _ connectivity.Connector = (*BudgetGuard)(nil)
