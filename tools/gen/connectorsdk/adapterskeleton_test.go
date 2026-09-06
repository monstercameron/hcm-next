package connectorsdk

import (
	"bytes"
	"go/format"
	"os"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity"
	"github.com/monstercameron/hcm-next/internal/connectivity/spi"
)

func goldenSpec() AdapterSkeleton {
	return AdapterSkeleton{
		Package:  "workdayreadonly",
		TypeName: "Adapter",
		Manifest: spi.AdapterManifest{
			AdapterID: "workday.hcm.readonly",
			Vendor:    "Workday",
			Product:   "HCM",
			Version:   "1.0.0",
			Objects:   []connectivity.ObjectKind{connectivity.ObjectWorker, connectivity.ObjectPosition},
			Capabilities: []spi.Capability{
				{Object: connectivity.ObjectPosition, Operation: spi.OpRead, Version: "v1"},
				{Object: connectivity.ObjectWorker, Operation: spi.OpRead, Version: "v1"},
				{Object: connectivity.ObjectWorker, Operation: spi.OpObserve, Version: "v1"},
			},
			Bounds:      connectivity.Bounds{MaxPageSize: 200, MaxPagesPerRun: 500, MaxRecordsPerRun: 100000, MaxRecordBytes: 65536},
			GeneratedAt: time.Unix(1, 0).UTC(),
		},
	}
}

func TestRenderAdapterSkeletonGolden(t *testing.T) {
	got, err := RenderAdapterSkeleton(goldenSpec())
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/adapter_skeleton.golden")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("skeleton does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderConformanceTestGolden(t *testing.T) {
	got, err := RenderConformanceTest(goldenSpec())
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/conformance_test.golden")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("conformance test does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderAdapterSkeletonIsDeterministicAcrossInputOrder(t *testing.T) {
	spec := goldenSpec()
	reordered := spec
	reordered.Manifest.Objects = []connectivity.ObjectKind{connectivity.ObjectPosition, connectivity.ObjectWorker}
	reordered.Manifest.Capabilities = []spi.Capability{spec.Manifest.Capabilities[2], spec.Manifest.Capabilities[0], spec.Manifest.Capabilities[1]}
	// GeneratedAt must not affect the rendered output at all: it is
	// deliberately excluded and replaced with a runtime call.
	reordered.Manifest.GeneratedAt = time.Unix(999999, 0).UTC()

	one, err := RenderAdapterSkeleton(spec)
	if err != nil {
		t.Fatal(err)
	}
	two, err := RenderAdapterSkeleton(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(one, two) {
		t.Fatalf("rendering is not deterministic under reordering:\n--- one ---\n%s\n--- two ---\n%s", one, two)
	}
}

func TestRenderAdapterSkeletonAndConformanceTestAreValidGo(t *testing.T) {
	for name, render := range map[string]func(AdapterSkeleton) ([]byte, error){
		"skeleton":    RenderAdapterSkeleton,
		"conformance": RenderConformanceTest,
	} {
		src, err := render(goldenSpec())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		formatted, err := format.Source(src)
		if err != nil {
			t.Fatalf("%s: generated source does not parse: %v\n%s", name, err, src)
		}
		if !bytes.Equal(formatted, src) {
			t.Fatalf("%s: generated source is not already gofmt-clean", name)
		}
	}
}

func TestRenderAdapterSkeletonRejectsInvalidSpec(t *testing.T) {
	cases := []struct {
		name string
		spec AdapterSkeleton
	}{
		{"no package", AdapterSkeleton{TypeName: "Adapter", Manifest: goldenSpec().Manifest}},
		{"no type name", AdapterSkeleton{Package: "p", Manifest: goldenSpec().Manifest}},
		{"unexported type name", AdapterSkeleton{Package: "p", TypeName: "adapter", Manifest: goldenSpec().Manifest}},
		{"invalid manifest", AdapterSkeleton{Package: "p", TypeName: "Adapter", Manifest: spi.AdapterManifest{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := RenderAdapterSkeleton(tc.spec); err == nil {
				t.Fatal("expected an error")
			}
			if _, err := RenderConformanceTest(tc.spec); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestRenderAdapterSkeletonRejectsWriteCapableManifest(t *testing.T) {
	spec := goldenSpec()
	spec.Manifest.Capabilities = append(spec.Manifest.Capabilities, spi.Capability{Object: connectivity.ObjectWorker, Operation: spi.OpWrite, Version: "v1"})
	if _, err := RenderAdapterSkeleton(spec); err == nil {
		t.Fatal("expected RenderAdapterSkeleton to refuse a manifest that declares write as active")
	}
}
