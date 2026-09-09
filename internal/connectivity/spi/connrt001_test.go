package spi_test

// This file is the CONN-RT-001 ticket-contract test matrix: TestTodo_CONN_RT_001
// (primary), TestTodo_CONN_RT_001_Golden, FuzzTodo_CONN_RT_001,
// TestTodo_CONN_RT_001_Integration and TestTodo_CONN_RT_001_Fault. It drives
// [spiconform.MemoryAdapter], the reference fixture, through the SPI defined
// by this package.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/spi"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/spi/spiconform"
)

func conn001Manifest() spi.AdapterManifest {
	return spi.AdapterManifest{
		AdapterID: "conn-rt-001.fixture",
		Vendor:    "Fixture",
		Product:   "HRIS",
		Version:   "1.0.0",
		Objects:   []connectivity.ObjectKind{connectivity.ObjectWorker},
		Capabilities: []spi.Capability{
			{Object: connectivity.ObjectWorker, Operation: spi.OpRead, Version: "v1"},
			{Object: connectivity.ObjectWorker, Operation: spi.OpObserve, Version: "v1"},
		},
		Bounds:      connectivity.Bounds{MaxPageSize: 2, MaxPagesPerRun: 5, MaxRecordsPerRun: 50, MaxRecordBytes: 4096},
		GeneratedAt: time.Unix(500, 0).UTC(),
	}
}

func conn001Records() []connectivity.Record {
	return []connectivity.Record{
		{ExternalID: "r1", SortKey: "a", SourceVersion: "1", ObservedAt: time.Unix(10, 0).UTC(), Fields: map[string]string{"name": "Ada"}},
		{ExternalID: "r2", SortKey: "b", SourceVersion: "1", ObservedAt: time.Unix(20, 0).UTC(), Fields: map[string]string{"name": "Grace"}},
	}
}

func newConn001Adapter(t testing.TB) *spiconform.MemoryAdapter {
	t.Helper()
	a, err := spiconform.NewMemoryAdapter(spiconform.MemoryAdapterConfig{
		Manifest:      conn001Manifest(),
		SnapshotID:    "snap-conn-rt-001",
		SchemaVersion: "v1",
		Records:       map[spi.ObjectKind][]connectivity.Record{connectivity.ObjectWorker: conn001Records()},
		Probe:         spi.ProbeResult{Health: spi.HealthHealthy, CheckedAt: time.Unix(600, 0).UTC()},
		Now:           func() time.Time { return time.Unix(700, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("NewMemoryAdapter: %v", err)
	}
	return a
}

// TestTodo_CONN_RT_001 is the primary connector adapter SPI test: typed
// read/observe, pagination, errors, cancellation and capability negotiation
// compile against and behave correctly through a fake connector.
//
// This SPI structurally has no write or subscribe method to typecheck at
// all - GREEN's "typed write" and "subscribe" clauses are satisfied here by
// proving the only write-shaped path, [spi.DeclareWriteCapability], never
// grants, and by ObserveChanges standing in for subscribe-style change
// observation, which is what this release actually ships.
func TestTodo_CONN_RT_001(t *testing.T) {
	var _ spi.Adapter = (*spiconform.MemoryAdapter)(nil) // compiles against a fake connector.

	a := newConn001Adapter(t)
	ctx := context.Background()

	// Capability negotiation.
	manifest, err := a.Describe(ctx)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	want := spi.Capability{Object: connectivity.ObjectWorker, Operation: spi.OpRead, Version: "v1"}
	if !manifest.Supports(want) {
		t.Fatalf("negotiated manifest does not support %v: %+v", want, manifest)
	}

	probe, err := a.Probe(ctx)
	if err != nil || probe.Health != spi.HealthHealthy {
		t.Fatalf("Probe: %+v %v", probe, err)
	}

	// Pagination.
	page1, err := a.ReadSnapshot(ctx, connectivity.ReadRequest{Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Limit: 1})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Page.Records) != 1 || page1.Page.Complete {
		t.Fatalf("page1=%+v", page1.Page)
	}
	page2, err := a.ReadSnapshot(ctx, connectivity.ReadRequest{Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Cursor: page1.Page.NextCursor, Limit: 1})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Page.Records) != 1 || !page2.Page.Complete {
		t.Fatalf("page2=%+v", page2.Page)
	}

	// Errors: bounds and unsupported object.
	if _, err := a.ReadSnapshot(ctx, connectivity.ReadRequest{Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Limit: 3}); !errors.Is(err, connectivity.ErrBounds) {
		t.Fatalf("got %v, want ErrBounds", err)
	}
	if _, err := a.ReadSnapshot(ctx, connectivity.ReadRequest{Object: connectivity.ObjectPosition, Mode: connectivity.ReadFull, Limit: 1}); !errors.Is(err, connectivity.ErrUnsupported) {
		t.Fatalf("got %v, want ErrUnsupported", err)
	}

	// Cancellation.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := a.ReadSnapshot(canceled, connectivity.ReadRequest{Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Limit: 1}); err == nil {
		t.Fatal("expected an error for a canceled context")
	}

	// Bounded observation.
	changes, err := a.ObserveChanges(ctx, spi.ObserveRequest{Object: connectivity.ObjectWorker, Since: time.Unix(0, 0).UTC(), Limit: 2})
	if err != nil {
		t.Fatalf("ObserveChanges: %v", err)
	}
	if len(changes.Changed) != 2 || !changes.Complete {
		t.Fatalf("changes=%+v", changes)
	}

	// Write capability is negotiated as refused, never granted.
	decision, err := spi.DeclareWriteCapability(spi.WriteCapabilityDeclaration{Object: connectivity.ObjectWorker, RequestedBy: t.Name()}, time.Unix(800, 0).UTC())
	if err == nil || !decision.Refused {
		t.Fatalf("write capability was not refused: decision=%+v err=%v", decision, err)
	}
}

// TestTodo_CONN_RT_001_Fault rejects untyped calls, vendor leakage and an
// incompatible adapter.
func TestTodo_CONN_RT_001_Fault(t *testing.T) {
	a := newConn001Adapter(t)
	ctx := context.Background()

	// Untyped calls: every request field is a declared, closed type; a
	// vendor-shaped or arbitrary object value is rejected before any I/O
	// rather than coerced or forwarded.
	if _, err := a.ReadSnapshot(ctx, connectivity.ReadRequest{Object: "vendor_specific_blob", Mode: connectivity.ReadFull, Limit: 1}); !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid for an untyped/vendor object", err)
	}

	// Vendor leakage: a manifest cannot publish an object outside the closed
	// ObjectKind vocabulary.
	leaky := conn001Manifest()
	leaky.Objects = append(leaky.Objects, "workday_custom_object")
	if err := leaky.Validate(); !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid for a vendor-leaking object", err)
	}

	// Incompatible adapter: a manifest that already claims write is active
	// can never be published, regardless of what the adapter's own code does.
	incompatible := conn001Manifest()
	incompatible.Capabilities = append(incompatible.Capabilities, spi.Capability{Object: connectivity.ObjectWorker, Operation: spi.OpWrite, Version: "v1"})
	if err := incompatible.Validate(); !errors.Is(err, connectivity.ErrUnsupported) {
		t.Fatalf("got %v, want ErrUnsupported for a write-claiming manifest", err)
	}
}

// TestTodo_CONN_RT_001_Golden pins the manifest and page digest algorithms so
// a future refactor that silently changes either encoding is caught here
// rather than downstream, where a changed digest would look like a changed
// adapter build.
func TestTodo_CONN_RT_001_Golden(t *testing.T) {
	manifestDigest, err := conn001Manifest().Digest()
	if err != nil {
		t.Fatalf("manifest digest: %v", err)
	}
	if got, want := manifestDigest, "sha256:bc4a828f08fffbd6b76c4e17db8cc414e2134be2afa89258d956955413a34a69"; got != want {
		t.Fatalf("manifest digest changed: got %q want %q", got, want)
	}

	page := connectivity.Page{
		Object:        connectivity.ObjectWorker,
		SchemaVersion: "v1",
		SnapshotID:    "snap-golden",
		Records: []connectivity.Record{
			{ExternalID: "r1", SortKey: "a", SourceVersion: "1", ObservedAt: time.Unix(10, 0).UTC(), Fields: map[string]string{"name": "Ada"}},
		},
		NextCursor:  connectivity.Cursor{SnapshotID: "snap-golden", LastSortKey: "a", LastExternalID: "r1", Page: 1},
		Complete:    true,
		RetrievedAt: time.Unix(30, 0).UTC(),
		Watermark:   time.Unix(10, 0).UTC(),
	}
	pageDigest, err := spi.PageDigest(page)
	if err != nil {
		t.Fatalf("page digest: %v", err)
	}
	if got, want := pageDigest, "sha256:eb3cf480f0f4af3ba17c79e822cd247f3d88b66ec98afb2da6d9ee48fccf3bed"; got != want {
		t.Fatalf("page digest changed: got %q want %q", got, want)
	}
}

// FuzzTodo_CONN_RT_001 asserts a security invariant that must hold for every
// possible input, not just the fixtures above: no write-capability
// declaration is ever granted in this release, regardless of what an
// attacker supplies as the object, requester or claimed amendment digest.
func FuzzTodo_CONN_RT_001(f *testing.F) {
	f.Add("WORKER", "requester", "")
	f.Add("WORKER", "requester", "some-amendment")
	f.Add("", "", "")
	f.Fuzz(func(t *testing.T, object, requester, amendment string) {
		decl := spi.WriteCapabilityDeclaration{
			Object:                   connectivity.ObjectKind(object),
			RequestedBy:              requester,
			AuthorityAmendmentDigest: spi.AuthorityAmendmentDigest(amendment),
		}
		decision, err := spi.DeclareWriteCapability(decl, time.Unix(1, 0).UTC())
		if !decision.Refused {
			t.Fatalf("write capability was granted for object=%q requester=%q amendment=%q", object, requester, amendment)
		}
		if err == nil {
			t.Fatalf("a refused decision returned a nil error for object=%q requester=%q amendment=%q", object, requester, amendment)
		}
	})
}

// TestTodo_CONN_RT_001_Integration drives a fake adapter through the whole
// conformance kit: negotiation, idempotent bounded reads, deterministic
// snapshot digests, bounded observation and every write-capability refusal
// shape.
func TestTodo_CONN_RT_001_Integration(t *testing.T) {
	a := newConn001Adapter(t)
	spiconform.Run(t, context.Background(), a)
}
