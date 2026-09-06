package spi_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity"
	"github.com/monstercameron/hcm-next/internal/connectivity/spi"
)

func validPage() connectivity.Page {
	return connectivity.Page{
		Object:        connectivity.ObjectWorker,
		SchemaVersion: "v1",
		SnapshotID:    "snap-1",
		Records: []connectivity.Record{
			{ExternalID: "w1", SortKey: "a", SourceVersion: "1", ObservedAt: time.Unix(10, 0).UTC(), Fields: map[string]string{"name": "Ada"}},
			{ExternalID: "w2", SortKey: "b", SourceVersion: "1", ObservedAt: time.Unix(20, 0).UTC(), Fields: map[string]string{"name": "Grace"}},
		},
		NextCursor:  connectivity.Cursor{SnapshotID: "snap-1", LastSortKey: "b", LastExternalID: "w2", Page: 1},
		Complete:    true,
		RetrievedAt: time.Unix(30, 0).UTC(),
		Watermark:   time.Unix(20, 0).UTC(),
	}
}

func TestPageDigestDeterministicAndOrderSensitive(t *testing.T) {
	p := validPage()
	d1, err := spi.PageDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := spi.PageDigest(p)
	if err != nil || d1 != d2 {
		t.Fatalf("not deterministic: %v %q vs %q", err, d1, d2)
	}

	reordered := p
	reordered.Records = []connectivity.Record{p.Records[1], p.Records[0]}
	if err := reordered.Validate(); err == nil {
		t.Fatal("reordered fixture should already be invalid (ordering check)")
	}

	changed := p
	changed.Records = append([]connectivity.Record(nil), p.Records...)
	changed.Records[0].Fields = map[string]string{"name": "Someone Else"}
	d3, err := spi.PageDigest(changed)
	if err != nil {
		t.Fatal(err)
	}
	if d3 == d1 {
		t.Fatal("digest did not change when record content changed")
	}
}

func TestPageDigestRejectsInvalidPage(t *testing.T) {
	if _, err := spi.PageDigest(connectivity.Page{}); !errors.Is(err, connectivity.ErrInvalid) && !errors.Is(err, connectivity.ErrSchema) {
		t.Fatalf("got %v", err)
	}
}

func TestSnapshotValidate(t *testing.T) {
	p := validPage()
	digest, err := spi.PageDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	s := spi.Snapshot{Page: p, Digest: digest}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	tampered := spi.Snapshot{Page: p, Digest: "not-the-real-digest"}
	if err := tampered.Validate(); !errors.Is(err, connectivity.ErrSchema) {
		t.Fatalf("got %v, want ErrSchema", err)
	}

	noDigest := spi.Snapshot{Page: p}
	if err := noDigest.Validate(); !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
}

func TestObserveRequestValidate(t *testing.T) {
	bounds := connectivity.Bounds{MaxPageSize: 10, MaxPagesPerRun: 1, MaxRecordsPerRun: 10, MaxRecordBytes: 1024}
	ok := spi.ObserveRequest{Object: connectivity.ObjectWorker, Since: time.Unix(1, 0), Limit: 5}
	if err := ok.Validate(bounds); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	badObject := ok
	badObject.Object = "NOT_A_KIND"
	if err := badObject.Validate(bounds); !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}

	noSince := ok
	noSince.Since = time.Time{}
	if err := noSince.Validate(bounds); !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}

	badLimit := ok
	badLimit.Limit = 0
	if err := badLimit.Validate(bounds); !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}

	overBounds := ok
	overBounds.Limit = 11
	if err := overBounds.Validate(bounds); !errors.Is(err, connectivity.ErrBounds) {
		t.Fatalf("got %v, want ErrBounds", err)
	}
}

func TestChangeSetValidate(t *testing.T) {
	valid := spi.ChangeSet{
		Object:      connectivity.ObjectWorker,
		Changed:     []connectivity.Record{{ExternalID: "w1", SortKey: "a", SourceVersion: "1", ObservedAt: time.Unix(1, 0)}},
		RetrievedAt: time.Unix(2, 0),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	noRetrieval := valid
	noRetrieval.RetrievedAt = time.Time{}
	if err := noRetrieval.Validate(); !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}

	badObject := valid
	badObject.Object = ""
	if err := badObject.Validate(); !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}

	badRecord := valid
	badRecord.Changed = []connectivity.Record{{}}
	if err := badRecord.Validate(); err == nil {
		t.Fatal("expected an error for an invalid record")
	}
}

func TestProbeResultValidate(t *testing.T) {
	ok := spi.ProbeResult{Health: spi.HealthHealthy, CheckedAt: time.Unix(1, 0)}
	if err := ok.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	badHealth := ok
	badHealth.Health = "UNKNOWN"
	if err := badHealth.Validate(); !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
	noTime := spi.ProbeResult{Health: spi.HealthHealthy}
	if err := noTime.Validate(); !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
	negLatency := ok
	negLatency.Latency = -time.Second
	if err := negLatency.Validate(); !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
}

func TestCheckContext(t *testing.T) {
	// A nil context is a caller defect CheckContext must reject explicitly
	// (see spi.CheckContext's doc comment); it is assigned through a typed
	// nil variable, rather than passed as a literal nil, so this exercises
	// the same nil-interface value without tripping nil-context linters that
	// cannot see the deliberate RED case being tested here.
	var nilCtx context.Context
	if err := spi.CheckContext(nilCtx); !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
	if err := spi.CheckContext(context.Background()); err != nil {
		t.Fatalf("got %v, want nil", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := spi.CheckContext(ctx); !errors.Is(err, connectivity.ErrTransient) {
		t.Fatalf("got %v, want ErrTransient", err)
	}
}
