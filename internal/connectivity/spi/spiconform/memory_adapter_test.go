package spiconform_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/spi"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/spi/spiconform"
)

func fixtureManifest() spi.AdapterManifest {
	return spi.AdapterManifest{
		AdapterID: "fixture.readonly",
		Vendor:    "Fixture",
		Product:   "HRIS",
		Version:   "1.0.0",
		Objects:   []connectivity.ObjectKind{connectivity.ObjectWorker},
		Capabilities: []spi.Capability{
			{Object: connectivity.ObjectWorker, Operation: spi.OpRead, Version: "v1"},
			{Object: connectivity.ObjectWorker, Operation: spi.OpObserve, Version: "v1"},
		},
		Bounds: connectivity.Bounds{
			MaxPageSize:      2,
			MaxPagesPerRun:   10,
			MaxRecordsPerRun: 100,
			MaxRecordBytes:   4096,
		},
		GeneratedAt: time.Unix(1000, 0).UTC(),
	}
}

func fixtureRecords() []connectivity.Record {
	return []connectivity.Record{
		{ExternalID: "w1", SortKey: "a", SourceVersion: "1", ObservedAt: time.Unix(10, 0).UTC(), Fields: map[string]string{"name": "Ada"}},
		{ExternalID: "w2", SortKey: "b", SourceVersion: "1", ObservedAt: time.Unix(20, 0).UTC(), Fields: map[string]string{"name": "Grace"}},
		{ExternalID: "w3", SortKey: "c", SourceVersion: "1", ObservedAt: time.Unix(30, 0).UTC(), Fields: map[string]string{"name": "Hedy"}},
	}
}

func newFixtureAdapter(t *testing.T) *spiconform.MemoryAdapter {
	t.Helper()
	a, err := spiconform.NewMemoryAdapter(spiconform.MemoryAdapterConfig{
		Manifest:      fixtureManifest(),
		SnapshotID:    "snap-1",
		SchemaVersion: "v1",
		Records:       map[spi.ObjectKind][]connectivity.Record{connectivity.ObjectWorker: fixtureRecords()},
		Probe:         spi.ProbeResult{Health: spi.HealthHealthy, CheckedAt: time.Unix(999, 0).UTC()},
		Now:           func() time.Time { return time.Unix(2000, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("NewMemoryAdapter: %v", err)
	}
	return a
}

func TestMemoryAdapterDescribeAndProbe(t *testing.T) {
	a := newFixtureAdapter(t)
	ctx := context.Background()
	m, err := a.Describe(ctx)
	if err != nil || m.AdapterID != "fixture.readonly" {
		t.Fatalf("Describe: %+v %v", m, err)
	}
	p, err := a.Probe(ctx)
	if err != nil || p.Health != spi.HealthHealthy {
		t.Fatalf("Probe: %+v %v", p, err)
	}
}

func TestMemoryAdapterReadSnapshotPaginatesAndIsDeterministic(t *testing.T) {
	a := newFixtureAdapter(t)
	ctx := context.Background()

	req1 := connectivity.ReadRequest{Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Limit: 2}
	s1, err := a.ReadSnapshot(ctx, req1)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(s1.Page.Records) != 2 || s1.Page.Complete {
		t.Fatalf("first page=%+v", s1.Page)
	}
	if s1.Page.Records[0].ExternalID != "w1" || s1.Page.Records[1].ExternalID != "w2" {
		t.Fatalf("first page records=%+v", s1.Page.Records)
	}

	s1b, err := a.ReadSnapshot(ctx, req1)
	if err != nil || s1b.Digest != s1.Digest {
		t.Fatalf("repeated first page not deterministic: %v %q vs %q", err, s1b.Digest, s1.Digest)
	}

	req2 := connectivity.ReadRequest{Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Cursor: s1.Page.NextCursor, Limit: 2}
	s2, err := a.ReadSnapshot(ctx, req2)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(s2.Page.Records) != 1 || !s2.Page.Complete || s2.Page.Records[0].ExternalID != "w3" {
		t.Fatalf("second page=%+v", s2.Page)
	}
}

func TestMemoryAdapterReadSnapshotRejectsOverBoundsLimit(t *testing.T) {
	a := newFixtureAdapter(t)
	req := connectivity.ReadRequest{Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Limit: 3}
	if _, err := a.ReadSnapshot(context.Background(), req); !errors.Is(err, connectivity.ErrBounds) {
		t.Fatalf("got %v, want ErrBounds", err)
	}
}

func TestMemoryAdapterReadSnapshotRejectsUndeclaredObject(t *testing.T) {
	a := newFixtureAdapter(t)
	req := connectivity.ReadRequest{Object: connectivity.ObjectPosition, Mode: connectivity.ReadFull, Limit: 1}
	if _, err := a.ReadSnapshot(context.Background(), req); !errors.Is(err, connectivity.ErrUnsupported) {
		t.Fatalf("got %v, want ErrUnsupported", err)
	}
}

func TestMemoryAdapterReadSnapshotRejectsCanceledContext(t *testing.T) {
	a := newFixtureAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := connectivity.ReadRequest{Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Limit: 1}
	if _, err := a.ReadSnapshot(ctx, req); err == nil {
		t.Fatal("expected an error for a canceled context")
	}
}

func TestMemoryAdapterObserveChangesIsBounded(t *testing.T) {
	a := newFixtureAdapter(t)
	ctx := context.Background()

	full, err := a.ObserveChanges(ctx, spi.ObserveRequest{Object: connectivity.ObjectWorker, Since: time.Unix(0, 0).UTC(), Limit: 2})
	if err != nil {
		t.Fatalf("ObserveChanges: %v", err)
	}
	if len(full.Changed) != 2 || full.Complete {
		t.Fatalf("changeset=%+v", full)
	}

	since20, err := a.ObserveChanges(ctx, spi.ObserveRequest{Object: connectivity.ObjectWorker, Since: time.Unix(20, 0).UTC(), Limit: 2})
	if err != nil {
		t.Fatalf("ObserveChanges since 20: %v", err)
	}
	if len(since20.Changed) != 2 || !since20.Complete {
		t.Fatalf("changeset since 20=%+v", since20)
	}

	if _, err := a.ObserveChanges(ctx, spi.ObserveRequest{Object: connectivity.ObjectWorker, Since: time.Unix(0, 0).UTC(), Limit: 3}); !errors.Is(err, connectivity.ErrBounds) {
		t.Fatalf("got %v, want ErrBounds", err)
	}
}
