package spiconform_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/connectivity"
	"github.com/monstercameron/hcm-next/internal/connectivity/spi"
	"github.com/monstercameron/hcm-next/internal/connectivity/spi/spiconform"
)

// fakeTB records every Fatalf call instead of unwinding the goroutine, so a
// test can assert on which conformance check tripped without the failure
// aborting the test itself.
type fakeTB struct{ failed []string }

func (f *fakeTB) Helper() {}
func (f *fakeTB) Fatalf(format string, args ...any) {
	f.failed = append(f.failed, fmt.Sprintf(format, args...))
}

func TestRunPassesForConformingAdapter(t *testing.T) {
	a := newFixtureAdapter(t)
	spiconform.Run(t, context.Background(), a)
}

func TestRunRejectsNilAdapter(t *testing.T) {
	fb := &fakeTB{}
	spiconform.Run(fb, context.Background(), nil)
	if len(fb.failed) != 1 {
		t.Fatalf("failed=%v", fb.failed)
	}
}

// flakyDescribeAdapter breaks Describe's idempotence: its second call reports
// a different version, which changes the manifest digest.
type flakyDescribeAdapter struct {
	*spiconform.MemoryAdapter
	calls int
}

func (f *flakyDescribeAdapter) Describe(ctx context.Context) (spi.AdapterManifest, error) {
	f.calls++
	m, err := f.MemoryAdapter.Describe(ctx)
	if err != nil {
		return m, err
	}
	if f.calls == 2 {
		m.Version = "2.0.0"
	}
	return m, nil
}

func TestRunCatchesNonIdempotentDescribe(t *testing.T) {
	flaky := &flakyDescribeAdapter{MemoryAdapter: newFixtureAdapter(t)}
	fb := &fakeTB{}
	spiconform.Run(fb, context.Background(), flaky)
	if len(fb.failed) == 0 {
		t.Fatal("expected Run to fail for a non-idempotent Describe")
	}
	if !strings.Contains(fb.failed[0], "not idempotent") {
		t.Fatalf("unexpected failure: %v", fb.failed)
	}
}

// boundsIgnoringAdapter silently clamps an over-limit request instead of
// refusing it, which is exactly the failure mode connectivity.Bounds' own
// doc comment warns about: "silently returning fewer records ... is how a
// complete observation quietly becomes partial."
type boundsIgnoringAdapter struct{ *spiconform.MemoryAdapter }

func (b *boundsIgnoringAdapter) ReadSnapshot(ctx context.Context, req connectivity.ReadRequest) (spi.Snapshot, error) {
	if req.Limit > 2 {
		req.Limit = 2
	}
	return b.MemoryAdapter.ReadSnapshot(ctx, req)
}

func TestRunCatchesAdapterThatIgnoresBounds(t *testing.T) {
	leaky := &boundsIgnoringAdapter{MemoryAdapter: newFixtureAdapter(t)}
	fb := &fakeTB{}
	spiconform.Run(fb, context.Background(), leaky)
	if len(fb.failed) == 0 {
		t.Fatal("expected Run to fail for an adapter that silently ignores bounds")
	}
	if !strings.Contains(fb.failed[0], "did not refuse with ErrBounds") {
		t.Fatalf("unexpected failure: %v", fb.failed)
	}
}

// leakyObjectAdapter answers a read for an object kind its own manifest never
// declared by quietly repackaging another object's data - the vendor-leakage
// / incompatible-adapter shape the RED line names.
type leakyObjectAdapter struct{ *spiconform.MemoryAdapter }

func (l *leakyObjectAdapter) ReadSnapshot(ctx context.Context, req connectivity.ReadRequest) (spi.Snapshot, error) {
	if req.Object == connectivity.ObjectWorker {
		return l.MemoryAdapter.ReadSnapshot(ctx, req)
	}
	borrowed := req
	borrowed.Object = connectivity.ObjectWorker
	s, err := l.MemoryAdapter.ReadSnapshot(ctx, borrowed)
	if err != nil {
		return spi.Snapshot{}, err
	}
	s.Page.Object = req.Object
	digest, err := spi.PageDigest(s.Page)
	if err != nil {
		return spi.Snapshot{}, err
	}
	s.Digest = digest
	return s, nil
}

func TestRunCatchesUndeclaredObjectLeakage(t *testing.T) {
	leaky := &leakyObjectAdapter{MemoryAdapter: newFixtureAdapter(t)}
	fb := &fakeTB{}
	spiconform.Run(fb, context.Background(), leaky)
	if len(fb.failed) == 0 {
		t.Fatal("expected Run to fail for an adapter that answers an undeclared object")
	}
	if !strings.Contains(fb.failed[0], "manifest does not declare") {
		t.Fatalf("unexpected failure: %v", fb.failed)
	}
}
