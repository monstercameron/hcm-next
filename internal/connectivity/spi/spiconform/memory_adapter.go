package spiconform

import (
	"context"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity"
	"github.com/monstercameron/hcm-next/internal/connectivity/spi"
)

// MemoryAdapterConfig fixtures one [MemoryAdapter].
type MemoryAdapterConfig struct {
	// Manifest is the manifest Describe returns. It must validate.
	Manifest spi.AdapterManifest
	// SnapshotID is the fixed snapshot every ReadSnapshot call pins to.
	SnapshotID string
	// SchemaVersion is the fixed external schema version every page reports.
	SchemaVersion string
	// Records is the fixture data per object, pre-sorted ascending by
	// (SortKey, ExternalID). MemoryAdapter does not sort it: an unsorted
	// fixture is a fixture bug, not something the adapter should paper over.
	Records map[spi.ObjectKind][]connectivity.Record
	// Probe is the fixed result Probe returns.
	Probe spi.ProbeResult
	// Now supplies the adapter's clock. Defaults to time.Now if nil. Tests
	// that need a fixed clock (so RetrievedAt does not vary run to run) pass
	// one; PageDigest never includes RetrievedAt, so the digest determinism
	// this package proves does not depend on it.
	Now func() time.Time
}

// MemoryAdapter is a small in-memory reference [spi.Adapter]. It is a
// fixture, not a production connector: its whole state is the config it was
// built from.
type MemoryAdapter struct {
	manifest      spi.AdapterManifest
	snapshotID    string
	schemaVersion string
	records       map[spi.ObjectKind][]connectivity.Record
	probe         spi.ProbeResult
	now           func() time.Time
}

// NewMemoryAdapter validates cfg and returns a ready adapter.
func NewMemoryAdapter(cfg MemoryAdapterConfig) (*MemoryAdapter, error) {
	if err := cfg.Manifest.Validate(); err != nil {
		return nil, err
	}
	if cfg.SnapshotID == "" {
		return nil, connectivity.Fail("spiconform.NewMemoryAdapter", connectivity.ErrInvalid, "snapshot id is required")
	}
	if cfg.SchemaVersion == "" {
		return nil, connectivity.Fail("spiconform.NewMemoryAdapter", connectivity.ErrInvalid, "schema version is required")
	}
	if err := cfg.Probe.Validate(); err != nil {
		return nil, err
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	records := make(map[spi.ObjectKind][]connectivity.Record, len(cfg.Records))
	for object, recs := range cfg.Records {
		records[object] = append([]connectivity.Record(nil), recs...)
	}
	return &MemoryAdapter{
		manifest:      cfg.Manifest,
		snapshotID:    cfg.SnapshotID,
		schemaVersion: cfg.SchemaVersion,
		records:       records,
		probe:         cfg.Probe,
		now:           now,
	}, nil
}

// Describe implements [spi.Adapter].
func (a *MemoryAdapter) Describe(ctx context.Context) (spi.AdapterManifest, error) {
	if err := spi.CheckContext(ctx); err != nil {
		return spi.AdapterManifest{}, err
	}
	return a.manifest, nil
}

// Probe implements [spi.Adapter].
func (a *MemoryAdapter) Probe(ctx context.Context) (spi.ProbeResult, error) {
	if err := spi.CheckContext(ctx); err != nil {
		return spi.ProbeResult{}, err
	}
	return a.probe, nil
}

// ReadSnapshot implements [spi.Adapter].
func (a *MemoryAdapter) ReadSnapshot(ctx context.Context, req connectivity.ReadRequest) (spi.Snapshot, error) {
	const op = "spiconform.MemoryAdapter.ReadSnapshot"
	if err := spi.CheckContext(ctx); err != nil {
		return spi.Snapshot{}, err
	}
	if err := req.Validate(a.manifest.Bounds); err != nil {
		return spi.Snapshot{}, err
	}
	if _, ok := a.manifest.CapabilityVersion(req.Object, spi.OpRead); !ok {
		return spi.Snapshot{}, connectivity.Fail(op, connectivity.ErrUnsupported, "object %q has no read capability", string(req.Object))
	}
	all, ok := a.records[req.Object]
	if !ok {
		return spi.Snapshot{}, connectivity.Fail(op, connectivity.ErrUnsupported, "object %q is not fixtured", string(req.Object))
	}

	cursor := req.Cursor
	if cursor.IsStart() {
		cursor = connectivity.StartCursor(a.snapshotID)
	} else if cursor.SnapshotID != a.snapshotID {
		return spi.Snapshot{}, connectivity.Fail(op, connectivity.ErrCursor, "cursor pins snapshot %q, adapter serves %q", cursor.SnapshotID, a.snapshotID)
	}
	if cursor.Expired(a.now()) {
		return spi.Snapshot{}, connectivity.Fail(op, connectivity.ErrCursor, "cursor is expired")
	}

	start := 0
	for start < len(all) && !after(all[start], cursor.LastSortKey, cursor.LastExternalID) {
		start++
	}
	end := start + req.Limit
	if end > len(all) {
		end = len(all)
	}
	page := append([]connectivity.Record(nil), all[start:end]...)

	next := connectivity.Cursor{SnapshotID: a.snapshotID, Page: cursor.Page + 1}
	if len(page) > 0 {
		last := page[len(page)-1]
		next.LastSortKey, next.LastExternalID = last.SortKey, last.ExternalID
	} else {
		next.LastSortKey, next.LastExternalID = cursor.LastSortKey, cursor.LastExternalID
	}

	watermark := a.now()
	for _, r := range page {
		if r.ObservedAt.After(watermark) {
			watermark = r.ObservedAt
		}
	}

	p := connectivity.Page{
		Object:        req.Object,
		SchemaVersion: a.schemaVersion,
		SnapshotID:    a.snapshotID,
		Records:       page,
		NextCursor:    next,
		Complete:      end >= len(all),
		RetrievedAt:   a.now(),
		Watermark:     watermark,
	}
	if err := p.Validate(); err != nil {
		return spi.Snapshot{}, err
	}
	digest, err := spi.PageDigest(p)
	if err != nil {
		return spi.Snapshot{}, err
	}
	return spi.Snapshot{Page: p, Digest: digest}, nil
}

// ObserveChanges implements [spi.Adapter].
func (a *MemoryAdapter) ObserveChanges(ctx context.Context, req spi.ObserveRequest) (spi.ChangeSet, error) {
	const op = "spiconform.MemoryAdapter.ObserveChanges"
	if err := spi.CheckContext(ctx); err != nil {
		return spi.ChangeSet{}, err
	}
	if err := req.Validate(a.manifest.Bounds); err != nil {
		return spi.ChangeSet{}, err
	}
	if _, ok := a.manifest.CapabilityVersion(req.Object, spi.OpObserve); !ok {
		return spi.ChangeSet{}, connectivity.Fail(op, connectivity.ErrUnsupported, "object %q has no observe capability", string(req.Object))
	}
	all, ok := a.records[req.Object]
	if !ok {
		return spi.ChangeSet{}, connectivity.Fail(op, connectivity.ErrUnsupported, "object %q is not fixtured", string(req.Object))
	}

	var eligible []connectivity.Record
	for _, r := range all {
		if !r.ObservedAt.Before(req.Since) {
			eligible = append(eligible, r)
		}
	}
	limit := req.Limit
	if limit > len(eligible) {
		limit = len(eligible)
	}
	changed := append([]connectivity.Record(nil), eligible[:limit]...)

	watermark := req.Since
	for _, r := range changed {
		if r.ObservedAt.After(watermark) {
			watermark = r.ObservedAt
		}
	}

	cs := spi.ChangeSet{
		Object:      req.Object,
		Changed:     changed,
		Watermark:   watermark,
		Complete:    limit == len(eligible),
		RetrievedAt: a.now(),
	}
	if err := cs.Validate(); err != nil {
		return spi.ChangeSet{}, err
	}
	return cs, nil
}

// after reports whether a record's (SortKey, ExternalID) strictly follows
// (sortKey, externalID). An empty (sortKey, externalID) pair - the start of a
// traversal - is followed by every record.
func after(r connectivity.Record, sortKey, externalID string) bool {
	if r.SortKey != sortKey {
		return r.SortKey > sortKey
	}
	return r.ExternalID > externalID
}
