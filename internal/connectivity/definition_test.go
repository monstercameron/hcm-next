package connectivity_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/fakeincumbent"
)

// publishedAt is a fixed publication instant. Publication is a pure function of
// its inputs here, so a golden digest stays golden.
var publishedAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func meta() connectivity.PublicationMeta {
	return connectivity.PublicationMeta{PublishedBy: "user:platform@hcmnext", PublishedAt: publishedAt}
}

// validDefinition returns a complete, publishable definition. Every negative
// case below starts from this and removes exactly one thing, so a failure names
// the missing contract rather than a pile of them.
func validDefinition() connectivity.ConnectorDefinition {
	return fakeincumbent.DefaultDefinition()
}

// TestTodo_INTG_001 is the INTG-001 primary test.
//
// RED: a definition missing any of vendor, version, object, capability, auth,
// read, write, event, pagination, rate, idempotency, observation,
// reconciliation or health contract fails publication.
// GREEN: the exact supported surface and maturity level resolve by stable
// definition id and version, and a published version is immutable.
func TestTodo_INTG_001(t *testing.T) {
	t.Parallel()

	t.Run("incomplete definitions fail publication", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name   string
			mutate func(*connectivity.ConnectorDefinition)
			want   string
		}{
			{"no connector id", func(d *connectivity.ConnectorDefinition) { d.ConnectorID = "" }, "connector id"},
			{"no vendor", func(d *connectivity.ConnectorDefinition) { d.Vendor = "" }, "vendor"},
			{"no product", func(d *connectivity.ConnectorDefinition) { d.Product = "" }, "product"},
			{"no version", func(d *connectivity.ConnectorDefinition) { d.Version = connectivity.Version{} }, "version"},
			{"no maturity", func(d *connectivity.ConnectorDefinition) { d.Maturity = "" }, "maturity"},
			{"no objects", func(d *connectivity.ConnectorDefinition) { d.Objects = nil }, "objects"},
			{"no capabilities", func(d *connectivity.ConnectorDefinition) { d.Capabilities = nil }, "capabilities"},
			{"no auth modes", func(d *connectivity.ConnectorDefinition) { d.AuthModes = nil }, "auth modes"},
			{"no read modes", func(d *connectivity.ConnectorDefinition) { d.ReadModes = nil }, "read modes"},
			{"undeclared write surface", func(d *connectivity.ConnectorDefinition) { d.WriteModes = nil }, "write surface"},
			{"undeclared event surface", func(d *connectivity.ConnectorDefinition) { d.EventModes = nil }, "event surface"},
			{"no schema refs", func(d *connectivity.ConnectorDefinition) { d.SchemaRefs = nil }, "schema refs"},
			{"no bounds", func(d *connectivity.ConnectorDefinition) { d.Bounds = connectivity.Bounds{} }, "page size"},
			{"no pagination contract", func(d *connectivity.ConnectorDefinition) {
				d.Pagination = connectivity.PaginationContract{}
			}, "pagination"},
			{"no rate contract", func(d *connectivity.ConnectorDefinition) {
				d.Rate = connectivity.RateContract{}
			}, "rate contract"},
			{"no idempotency contract", func(d *connectivity.ConnectorDefinition) {
				d.Idempotency = connectivity.IdempotencyContract{}
			}, "idempotent"},
			{"no observation contract", func(d *connectivity.ConnectorDefinition) {
				d.Observation = connectivity.ObservationContract{}
			}, "observation contract"},
			{"no reconciliation contract", func(d *connectivity.ConnectorDefinition) {
				d.Reconciliation = connectivity.ReconciliationContract{}
			}, "reconciliation contract"},
			{"no health contract", func(d *connectivity.ConnectorDefinition) {
				d.Health = connectivity.HealthContract{}
			}, "health contract"},
			{"write capability without a write surface", func(d *connectivity.ConnectorDefinition) {
				d.Capabilities = append(d.Capabilities, connectivity.Capability{
					Object: connectivity.ObjectWorker, Operation: connectivity.OperationWrite,
				})
			}, "write surface"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				registry := connectivity.NewRegistry()
				def := validDefinition()
				tc.mutate(&def)
				_, err := registry.Publish(def, meta())
				if err == nil {
					t.Fatalf("publication succeeded with %s missing", tc.name)
				}
				if !errors.Is(err, connectivity.ErrInvalid) {
					t.Fatalf("error is not ErrInvalid: %v", err)
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error %q does not mention %q", err.Error(), tc.want)
				}
				if _, err := registry.Resolve(def.ConnectorID, def.Version); !errors.Is(err, connectivity.ErrNotFound) {
					t.Fatalf("a rejected definition became resolvable: %v", err)
				}
			})
		}
	})

	t.Run("published surface resolves by id and version", func(t *testing.T) {
		t.Parallel()
		registry := connectivity.NewRegistry()
		def := validDefinition()
		pub, err := registry.Publish(def, meta())
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		if pub.Digest == "" {
			t.Fatal("publication has no digest")
		}

		got, err := registry.Resolve(def.ConnectorID, def.Version)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if got.Digest != pub.Digest {
			t.Fatalf("resolved digest %s, published %s", got.Digest, pub.Digest)
		}
		if got.Definition.Maturity != connectivity.MaturityPreview {
			t.Fatalf("resolved maturity %q, want PREVIEW", got.Definition.Maturity)
		}
		for _, want := range connectivity.ReadCapabilities(connectivity.ObjectKinds()...) {
			if !got.Definition.Supports(want) {
				t.Fatalf("resolved surface does not support %s", want)
			}
		}
		if got.Definition.Supports(connectivity.Capability{
			Object: connectivity.ObjectWorker, Operation: connectivity.OperationWrite,
		}) {
			t.Fatal("a P1A connector resolved a write capability")
		}
		if _, ok := got.Definition.SchemaRefFor(connectivity.ObjectWorker); !ok {
			t.Fatal("resolved surface has no worker schema ref")
		}
	})

	t.Run("versions are immutable and ordered", func(t *testing.T) {
		t.Parallel()
		registry := connectivity.NewRegistry()
		v1 := validDefinition()
		if _, err := registry.Publish(v1, meta()); err != nil {
			t.Fatalf("publish v1: %v", err)
		}

		// Republishing byte-identical content is idempotent.
		again, err := registry.Publish(v1, meta())
		if err != nil {
			t.Fatalf("idempotent republish: %v", err)
		}
		if again.PublishedAt != publishedAt.UTC() {
			t.Fatalf("idempotent republish changed publication time to %v", again.PublishedAt)
		}

		// Changing a published version is refused.
		edited := validDefinition()
		edited.Maturity = connectivity.MaturityCertified
		if _, err := registry.Publish(edited, meta()); !errors.Is(err, connectivity.ErrImmutable) {
			t.Fatalf("editing a published version returned %v, want ErrImmutable", err)
		}
		resolved, err := registry.Resolve(v1.ConnectorID, v1.Version)
		if err != nil {
			t.Fatalf("resolve after refused edit: %v", err)
		}
		if resolved.Definition.Maturity != connectivity.MaturityPreview {
			t.Fatalf("refused edit still changed the published surface to %q", resolved.Definition.Maturity)
		}

		// A change is a new version.
		v2 := edited
		v2.Version = connectivity.Version{Major: 1, Minor: 1, Patch: 0}
		if _, err := registry.Publish(v2, meta()); err != nil {
			t.Fatalf("publish v2: %v", err)
		}
		versions := registry.Versions(v1.ConnectorID)
		if len(versions) != 2 || versions[0].Compare(versions[1]) >= 0 {
			t.Fatalf("versions are not ascending: %v", versions)
		}
		latest, err := registry.Latest(v1.ConnectorID)
		if err != nil {
			t.Fatalf("latest: %v", err)
		}
		if latest.Definition.Version != v2.Version {
			t.Fatalf("latest is %s, want %s", latest.Definition.Version, v2.Version)
		}
		// Resolving the older version still yields the older surface: history
		// is not rewritten by a newer publication.
		old, err := registry.Resolve(v1.ConnectorID, v1.Version)
		if err != nil {
			t.Fatalf("resolve v1 after v2: %v", err)
		}
		if old.Definition.Maturity != connectivity.MaturityPreview {
			t.Fatalf("v1 maturity changed to %q after v2 was published", old.Definition.Maturity)
		}
	})

	t.Run("publication requires governance context", func(t *testing.T) {
		t.Parallel()
		registry := connectivity.NewRegistry()
		if _, err := registry.Publish(validDefinition(), connectivity.PublicationMeta{
			PublishedAt: publishedAt,
		}); !errors.Is(err, connectivity.ErrInvalid) {
			t.Fatalf("publication without a publisher returned %v", err)
		}
		if _, err := registry.Publish(validDefinition(), connectivity.PublicationMeta{
			PublishedBy: "user:x",
		}); !errors.Is(err, connectivity.ErrInvalid) {
			t.Fatalf("publication without a time returned %v", err)
		}
	})
}

// TestTodo_INTG_001_Golden pins the published surface and its digest. A change
// to the canonical encoding, the field set, or the digest algorithm breaks this
// test, which is the point: a definition digest that drifts silently would let
// a pending approval verify against a surface nobody approved.
func TestTodo_INTG_001_Golden(t *testing.T) {
	t.Parallel()

	def := validDefinition()
	digest, err := def.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	got := renderDefinition(def, digest)
	compareGolden(t, filepath.Join("testdata", "intg001_definition.golden"), got)

	// The digest is a function of meaning, not of field order or slice order.
	shuffled := validDefinition()
	shuffled.Objects = []connectivity.ObjectKind{
		connectivity.ObjectCompensation, connectivity.ObjectWorker, connectivity.ObjectPosition,
	}
	shuffled.Capabilities = connectivity.ReadCapabilities(
		connectivity.ObjectCompensation, connectivity.ObjectPosition, connectivity.ObjectWorker)
	shuffledDigest, err := shuffled.Digest()
	if err != nil {
		t.Fatalf("shuffled digest: %v", err)
	}
	if shuffledDigest != digest {
		t.Fatalf("reordering declarations changed the digest:\n got %s\nwant %s", shuffledDigest, digest)
	}

	// A material change does change it.
	changed := validDefinition()
	changed.Maturity = connectivity.MaturityCertified
	changedDigest, err := changed.Digest()
	if err != nil {
		t.Fatalf("changed digest: %v", err)
	}
	if changedDigest == digest {
		t.Fatal("changing the maturity level did not change the digest")
	}
}

func renderDefinition(def connectivity.ConnectorDefinition, digest string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "connector_id: %s\n", def.ConnectorID)
	fmt.Fprintf(&b, "vendor: %s\n", def.Vendor)
	fmt.Fprintf(&b, "product: %s\n", def.Product)
	fmt.Fprintf(&b, "version: %s\n", def.Version)
	fmt.Fprintf(&b, "maturity: %s\n", def.Maturity)
	fmt.Fprintf(&b, "objects: %v\n", def.Objects)
	b.WriteString("capabilities:\n")
	for _, c := range def.Capabilities {
		fmt.Fprintf(&b, "  - %s\n", c)
	}
	fmt.Fprintf(&b, "auth_modes: %v\n", def.AuthModes)
	fmt.Fprintf(&b, "read_modes: %v\n", def.ReadModes)
	fmt.Fprintf(&b, "write_modes: %v\n", def.WriteModes)
	fmt.Fprintf(&b, "event_modes: %v\n", def.EventModes)
	b.WriteString("schema_refs:\n")
	for _, s := range def.SchemaRefs {
		fmt.Fprintf(&b, "  - %s -> %s/%s (%s)\n", s.Object, s.SchemaID, s.SchemaVer, s.Descriptor)
	}
	fmt.Fprintf(&b, "bounds: page=%d pages=%d records=%d bytes=%d interval=%s\n",
		def.Bounds.MaxPageSize, def.Bounds.MaxPagesPerRun, def.Bounds.MaxRecordsPerRun,
		def.Bounds.MaxRecordBytes, def.Bounds.MinRequestInterval)
	fmt.Fprintf(&b, "pagination: %s sort=%s tie=%s stable=%t ttl=%s\n",
		def.Pagination.Style, def.Pagination.SortKeyField, def.Pagination.TieBreakField,
		def.Pagination.StableUnderSnapshot, def.Pagination.TokenTTL)
	fmt.Fprintf(&b, "rate: rpm=%d concurrency=%d burst=%d\n",
		def.Rate.RequestsPerMinute, def.Rate.ConcurrentReads, def.Rate.BurstRequests)
	fmt.Fprintf(&b, "idempotency: field=%s retention=%s reads_idempotent=%t\n",
		def.Idempotency.KeyField, def.Idempotency.RetentionWindow, def.Idempotency.ReadsAreIdempotent)
	fmt.Fprintf(&b, "observation: watermark=%s budget=%s completeness=%t\n",
		def.Observation.WatermarkField, def.Observation.FreshnessBudget,
		def.Observation.SupportsCompleteness)
	fmt.Fprintf(&b, "reconciliation: keys=%v comparable=%v point_read=%t\n",
		def.Reconciliation.KeyFields, def.Reconciliation.ComparableFields,
		def.Reconciliation.SupportsPointRead)
	fmt.Fprintf(&b, "health: probe=%s interval=%s degrade_after=%d\n",
		def.Health.ProbeObject, def.Health.Interval, def.Health.DegradedAfterFailures)
	fmt.Fprintf(&b, "digest: %s\n", digest)
	return b.String()
}

// compareGolden compares got against the golden file, writing it when
// HCMNEXT_UPDATE_GOLDEN is set.
func compareGolden(t *testing.T, path, got string) {
	t.Helper()
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote golden %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set HCMNEXT_UPDATE_GOLDEN=1 to create it)", path, err)
	}
	if normalize(string(want)) != normalize(got) {
		t.Fatalf("golden %s mismatch\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

func normalize(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// TestTodo_INTG_001_Fault proves the registry stays coherent when publication
// fails: a rejected definition leaves no version, no digest and no latest
// pointer behind.
func TestTodo_INTG_001_Fault(t *testing.T) {
	t.Parallel()
	registry := connectivity.NewRegistry()

	broken := validDefinition()
	broken.Health = connectivity.HealthContract{}
	if _, err := registry.Publish(broken, meta()); err == nil {
		t.Fatal("a definition without a health contract published")
	}
	if ids := registry.ConnectorIDs(); len(ids) != 0 {
		t.Fatalf("a rejected publication registered connector ids %v", ids)
	}
	if _, err := registry.Latest(broken.ConnectorID); !errors.Is(err, connectivity.ErrNotFound) {
		t.Fatalf("latest after a rejected publication returned %v", err)
	}

	good := validDefinition()
	if _, err := registry.Publish(good, meta()); err != nil {
		t.Fatalf("publish after a rejection: %v", err)
	}

	// A definition whose schema ref names an object it never declared is
	// rejected even though every contract is present: an inconsistent surface
	// is as unpublishable as an incomplete one.
	inconsistent := validDefinition()
	inconsistent.Version = connectivity.Version{Major: 2}
	inconsistent.Objects = []connectivity.ObjectKind{connectivity.ObjectWorker}
	if _, err := registry.Publish(inconsistent, meta()); !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("an inconsistent surface published: %v", err)
	}
	if versions := registry.Versions(good.ConnectorID); len(versions) != 1 {
		t.Fatalf("rejected publication left %d versions", len(versions))
	}
}

// TestTodo_INTG_001_Race publishes and resolves concurrently. Immutability has
// to hold under a race or it is not immutability: exactly one distinct digest
// must survive for a version, whichever goroutine got there first.
func TestTodo_INTG_001_Race(t *testing.T) {
	t.Parallel()
	registry := connectivity.NewRegistry()

	const goroutines = 16
	var wg sync.WaitGroup
	digests := make([]string, goroutines)
	errs := make([]error, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			def := validDefinition()
			if idx%2 == 1 {
				def.Maturity = connectivity.MaturityCertified
			}
			pub, err := registry.Publish(def, meta())
			digests[idx], errs[idx] = pub.Digest, err
		}(i)
	}
	wg.Wait()

	var succeeded, refused int
	distinct := map[string]bool{}
	for i := range goroutines {
		switch {
		case errs[i] == nil:
			succeeded++
			distinct[digests[i]] = true
		case errors.Is(errs[i], connectivity.ErrImmutable):
			refused++
		default:
			t.Fatalf("goroutine %d: unexpected error %v", i, errs[i])
		}
	}
	if len(distinct) != 1 {
		t.Fatalf("concurrent publication produced %d distinct digests: %v", len(distinct), distinct)
	}
	if succeeded+refused != goroutines {
		t.Fatalf("accounted for %d of %d goroutines", succeeded+refused, goroutines)
	}
	resolved, err := registry.Resolve(validDefinition().ConnectorID, validDefinition().Version)
	if err != nil {
		t.Fatalf("resolve after race: %v", err)
	}
	for d := range distinct {
		if resolved.Digest != d {
			t.Fatalf("resolved digest %s is not the one that won: %s", resolved.Digest, d)
		}
	}
}

// TestTodo_INTG_001_Integration publishes the fake incumbent's own definition
// and drives the connector it describes, proving the published surface and the
// running connector agree rather than merely both existing.
func TestTodo_INTG_001_Integration(t *testing.T) {
	t.Parallel()
	registry := connectivity.NewRegistry()
	pub, err := registry.Publish(fakeincumbent.DefaultDefinition(), meta())
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	incumbent, err := fakeincumbent.New(fakeincumbent.Options{})
	if err != nil {
		t.Fatalf("new incumbent: %v", err)
	}
	descriptor := incumbent.Descriptor()
	if descriptor.ConnectorID != pub.Definition.ConnectorID {
		t.Fatalf("connector reports id %q, definition publishes %q",
			descriptor.ConnectorID, pub.Definition.ConnectorID)
	}
	if descriptor.Version != pub.Definition.Version {
		t.Fatalf("connector reports version %s, definition publishes %s",
			descriptor.Version, pub.Definition.Version)
	}
	if incumbent.Bounds() != pub.Definition.Bounds {
		t.Fatalf("connector bounds %+v differ from published %+v",
			incumbent.Bounds(), pub.Definition.Bounds)
	}
	for _, capability := range incumbent.Capabilities() {
		if !pub.Definition.Supports(capability) {
			t.Fatalf("connector serves %s which the definition does not publish", capability)
		}
	}
}

// FuzzTodo_INTG_001 fuzzes definition publication. Whatever the inputs, the
// registry must never accept an invalid definition, never produce an empty
// digest for an accepted one, and never resolve something it rejected.
func FuzzTodo_INTG_001(f *testing.F) {
	f.Add("workday.hcm", "Workday", uint8(1), uint8(0), "CERTIFIED", 25, 100)
	f.Add("", "", uint8(0), uint8(0), "", 0, 0)
	f.Add("x", "y", uint8(255), uint8(255), "EXPERIMENTAL", -1, 1)
	f.Add("\x00", "\xff", uint8(1), uint8(1), "preview", 1<<20, 1)

	f.Fuzz(func(t *testing.T, id, vendor string, major, minor uint8, maturity string, pageSize, rpm int) {
		def := validDefinition()
		def.ConnectorID = id
		def.Vendor = vendor
		def.Version = connectivity.Version{Major: uint32(major), Minor: uint32(minor)}
		def.Maturity = connectivity.Maturity(maturity)
		def.Bounds.MaxPageSize = pageSize
		def.Rate.RequestsPerMinute = rpm

		registry := connectivity.NewRegistry()
		pub, err := registry.Publish(def, meta())
		if err != nil {
			if !errors.Is(err, connectivity.ErrInvalid) {
				t.Fatalf("publication failed with a non-ErrInvalid cause: %v", err)
			}
			if _, resolveErr := registry.Resolve(id, def.Version); !errors.Is(resolveErr, connectivity.ErrNotFound) {
				t.Fatalf("a rejected definition resolved: %v", resolveErr)
			}
			return
		}
		if pub.Digest == "" {
			t.Fatal("accepted publication has an empty digest")
		}
		if err := def.Validate(); err != nil {
			t.Fatalf("registry accepted a definition Validate rejects: %v", err)
		}
		again, err := def.Digest()
		if err != nil {
			t.Fatalf("re-digest: %v", err)
		}
		if again != pub.Digest {
			t.Fatalf("digest is not deterministic: %s then %s", pub.Digest, again)
		}
	})
}
