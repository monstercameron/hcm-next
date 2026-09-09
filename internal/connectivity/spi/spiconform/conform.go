package spiconform

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/spi"
)

// TB is the subset of testing.TB the conformance kit needs. A caller
// normally passes a *testing.T or *testing.B, both of which satisfy it by
// interface structure. It is declared narrowly, rather than as testing.TB,
// so this package's own tests can exercise Run's failure paths with a fake
// recorder: testing.TB carries an unexported method that forbids any type
// outside the testing package from implementing it directly.
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
}

// Run drives adapter through the whole [spi.Adapter] contract and fails tb on
// the first violation it finds:
//
//   - Describe is idempotent: two calls return manifests with identical
//     digests, and the manifest validates (which alone rejects any adapter
//     that declares write capability as already active).
//   - ReadSnapshot is idempotent and its digest is deterministic: two
//     identical requests against unchanged fixture data return identical
//     [spi.Snapshot] digests.
//   - ReadSnapshot and ObserveChanges both refuse a request that exceeds the
//     manifest's declared bounds with [connectivity.ErrBounds], rather than
//     silently returning a smaller page.
//   - ReadSnapshot refuses an object the manifest does not declare (vendor
//     leakage / an incompatible adapter cannot silently succeed).
//   - Both read methods reject an already-canceled context.
//   - [spi.DeclareWriteCapability] refuses every shape of write-capability
//     declaration this release can construct: malformed, unaddressed, and
//     addressed to an amendment this build does not recognize.
func Run(tb TB, ctx context.Context, adapter spi.Adapter) {
	tb.Helper()
	if adapter == nil {
		tb.Fatalf("spiconform.Run: adapter is nil")
		return
	}

	manifest, ok := checkDescribeIdempotent(tb, ctx, adapter)
	if !ok {
		return
	}
	if !checkProbe(tb, ctx, adapter) {
		return
	}
	if !checkReadSnapshot(tb, ctx, adapter, manifest) {
		return
	}
	if !checkObserveChanges(tb, ctx, adapter, manifest) {
		return
	}
	checkWriteCapabilityRefused(tb, manifest)
}

func checkDescribeIdempotent(tb TB, ctx context.Context, adapter spi.Adapter) (spi.AdapterManifest, bool) {
	tb.Helper()
	m1, err := adapter.Describe(ctx)
	if err != nil {
		tb.Fatalf("Describe: %v", err)
		return spi.AdapterManifest{}, false
	}
	if err := m1.Validate(); err != nil {
		tb.Fatalf("Describe returned an invalid manifest: %v", err)
		return spi.AdapterManifest{}, false
	}
	digest1, err := m1.Digest()
	if err != nil {
		tb.Fatalf("manifest digest: %v", err)
		return spi.AdapterManifest{}, false
	}
	m2, err := adapter.Describe(ctx)
	if err != nil {
		tb.Fatalf("second Describe: %v", err)
		return spi.AdapterManifest{}, false
	}
	digest2, err := m2.Digest()
	if err != nil {
		tb.Fatalf("second manifest digest: %v", err)
		return spi.AdapterManifest{}, false
	}
	if digest1 != digest2 {
		tb.Fatalf("Describe is not idempotent: digest %q then %q", digest1, digest2)
		return spi.AdapterManifest{}, false
	}
	return m1, true
}

func checkProbe(tb TB, ctx context.Context, adapter spi.Adapter) bool {
	tb.Helper()
	p, err := adapter.Probe(ctx)
	if err != nil {
		tb.Fatalf("Probe: %v", err)
		return false
	}
	if err := p.Validate(); err != nil {
		tb.Fatalf("Probe returned an invalid result: %v", err)
		return false
	}
	return true
}

func checkReadSnapshot(tb TB, ctx context.Context, adapter spi.Adapter, manifest spi.AdapterManifest) bool {
	tb.Helper()

	for _, c := range manifest.SortedCapabilities() {
		if c.Operation != spi.OpRead {
			continue
		}
		limit := manifest.Bounds.MaxPageSize

		req := connectivity.ReadRequest{Object: c.Object, Mode: connectivity.ReadFull, Limit: limit}
		s1, err := adapter.ReadSnapshot(ctx, req)
		if err != nil {
			tb.Fatalf("ReadSnapshot(%s): %v", c.Object, err)
			return false
		}
		if err := s1.Validate(); err != nil {
			tb.Fatalf("ReadSnapshot(%s) returned an invalid snapshot: %v", c.Object, err)
			return false
		}
		s2, err := adapter.ReadSnapshot(ctx, req)
		if err != nil {
			tb.Fatalf("repeated ReadSnapshot(%s): %v", c.Object, err)
			return false
		}
		if s1.Digest != s2.Digest {
			tb.Fatalf("ReadSnapshot(%s) is not deterministic: digest %q then %q", c.Object, s1.Digest, s2.Digest)
			return false
		}

		over := connectivity.ReadRequest{Object: c.Object, Mode: connectivity.ReadFull, Limit: limit + 1}
		if _, err := adapter.ReadSnapshot(ctx, over); !errors.Is(err, connectivity.ErrBounds) {
			tb.Fatalf("ReadSnapshot(%s) with limit %d (bounds max %d) did not refuse with ErrBounds: %v", c.Object, limit+1, limit, err)
			return false
		}

		canceledCtx, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := adapter.ReadSnapshot(canceledCtx, req); err == nil {
			tb.Fatalf("ReadSnapshot(%s) accepted an already-canceled context", c.Object)
			return false
		}
	}

	declared := map[spi.ObjectKind]bool{}
	for _, o := range manifest.Objects {
		declared[o] = true
	}
	for _, o := range connectivity.ObjectKinds() {
		if declared[o] {
			continue
		}
		req := connectivity.ReadRequest{Object: o, Mode: connectivity.ReadFull, Limit: 1}
		if _, err := adapter.ReadSnapshot(ctx, req); err == nil {
			tb.Fatalf("ReadSnapshot(%s) succeeded for an object the manifest does not declare", o)
			return false
		}
		break
	}
	return true
}

func checkObserveChanges(tb TB, ctx context.Context, adapter spi.Adapter, manifest spi.AdapterManifest) bool {
	tb.Helper()

	for _, c := range manifest.SortedCapabilities() {
		if c.Operation != spi.OpObserve {
			continue
		}
		limit := manifest.Bounds.MaxPageSize
		since := time.Unix(0, 0).UTC()

		req := spi.ObserveRequest{Object: c.Object, Since: since, Limit: limit}
		cs, err := adapter.ObserveChanges(ctx, req)
		if err != nil {
			tb.Fatalf("ObserveChanges(%s): %v", c.Object, err)
			return false
		}
		if err := cs.Validate(); err != nil {
			tb.Fatalf("ObserveChanges(%s) returned an invalid change set: %v", c.Object, err)
			return false
		}

		over := spi.ObserveRequest{Object: c.Object, Since: since, Limit: limit + 1}
		if _, err := adapter.ObserveChanges(ctx, over); !errors.Is(err, connectivity.ErrBounds) {
			tb.Fatalf("ObserveChanges(%s) with limit %d (bounds max %d) did not refuse with ErrBounds: %v", c.Object, limit+1, limit, err)
			return false
		}

		canceledCtx, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := adapter.ObserveChanges(canceledCtx, req); err == nil {
			tb.Fatalf("ObserveChanges(%s) accepted an already-canceled context", c.Object)
			return false
		}
	}
	return true
}

// checkWriteCapabilityRefused exercises every write-capability refusal shape
// this release can produce. It does not call the adapter at all:
// [spi.DeclareWriteCapability] is adapter-independent by construction, and
// this proves that independence rather than assuming it.
func checkWriteCapabilityRefused(tb TB, manifest spi.AdapterManifest) {
	tb.Helper()
	now := time.Now().UTC()

	if _, err := spi.DeclareWriteCapability(spi.WriteCapabilityDeclaration{}, now); err == nil {
		tb.Fatalf("DeclareWriteCapability accepted an empty declaration")
		return
	}

	if len(manifest.Objects) == 0 {
		return
	}
	object := manifest.Objects[0]

	noAmendment := spi.WriteCapabilityDeclaration{Object: object, RequestedBy: "spiconform.Run"}
	dec, err := spi.DeclareWriteCapability(noAmendment, now)
	if err == nil || !dec.Refused || dec.Code != spi.RefusalNoAmendment {
		tb.Fatalf("DeclareWriteCapability with no amendment was not refused as %s: decision=%+v err=%v", spi.RefusalNoAmendment, dec, err)
		return
	}

	unknownAmendment := noAmendment
	unknownAmendment.AuthorityAmendmentDigest = "spiconform-unrecognized-amendment"
	dec, err = spi.DeclareWriteCapability(unknownAmendment, now)
	if err == nil || !dec.Refused || dec.Code != spi.RefusalUnknownAmendment {
		tb.Fatalf("DeclareWriteCapability with an unrecognized amendment was not refused as %s: decision=%+v err=%v", spi.RefusalUnknownAmendment, dec, err)
		return
	}
}
