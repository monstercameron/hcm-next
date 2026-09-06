package spi_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity"
	"github.com/monstercameron/hcm-next/internal/connectivity/spi"
)

func validManifest() spi.AdapterManifest {
	return spi.AdapterManifest{
		AdapterID: "fixture.readonly",
		Vendor:    "Fixture",
		Product:   "HRIS",
		Version:   "1.0.0",
		Objects:   []connectivity.ObjectKind{connectivity.ObjectWorker, connectivity.ObjectPosition},
		Capabilities: []spi.Capability{
			{Object: connectivity.ObjectWorker, Operation: spi.OpRead, Version: "v1"},
			{Object: connectivity.ObjectPosition, Operation: spi.OpObserve, Version: "v1"},
		},
		Bounds: connectivity.Bounds{
			MaxPageSize:      50,
			MaxPagesPerRun:   10,
			MaxRecordsPerRun: 500,
			MaxRecordBytes:   4096,
		},
		GeneratedAt: time.Unix(1000, 0).UTC(),
	}
}

func TestAdapterManifestValidateAccepts(t *testing.T) {
	if err := validManifest().Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestAdapterManifestValidateRejectsWriteCapability(t *testing.T) {
	m := validManifest()
	m.Capabilities = append(m.Capabilities, spi.Capability{Object: connectivity.ObjectWorker, Operation: spi.OpWrite, Version: "v1"})
	err := m.Validate()
	if !errors.Is(err, connectivity.ErrUnsupported) {
		t.Fatalf("got %v, want ErrUnsupported", err)
	}
}

func TestAdapterManifestValidateRejectsUndeclaredObjectCapability(t *testing.T) {
	m := validManifest()
	m.Capabilities = []spi.Capability{{Object: connectivity.ObjectCompensation, Operation: spi.OpRead, Version: "v1"}}
	if err := m.Validate(); !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
}

func TestAdapterManifestValidateRejectsDuplicateObject(t *testing.T) {
	m := validManifest()
	m.Objects = append(m.Objects, connectivity.ObjectWorker)
	if err := m.Validate(); !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
}

func TestAdapterManifestValidateRejectsMissingFields(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*spi.AdapterManifest)
	}{
		{"adapter id", func(m *spi.AdapterManifest) { m.AdapterID = "" }},
		{"vendor", func(m *spi.AdapterManifest) { m.Vendor = "" }},
		{"product", func(m *spi.AdapterManifest) { m.Product = "" }},
		{"version", func(m *spi.AdapterManifest) { m.Version = "" }},
		{"objects", func(m *spi.AdapterManifest) { m.Objects = nil }},
		{"capabilities", func(m *spi.AdapterManifest) { m.Capabilities = nil }},
		{"generated at", func(m *spi.AdapterManifest) { m.GeneratedAt = time.Time{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validManifest()
			tc.mutate(&m)
			if err := m.Validate(); !errors.Is(err, connectivity.ErrInvalid) {
				t.Fatalf("got %v, want ErrInvalid", err)
			}
		})
	}
}

func TestAdapterManifestDigestIsDeterministicAndOrderIndependent(t *testing.T) {
	a := validManifest()
	b := validManifest()
	b.Objects = []connectivity.ObjectKind{connectivity.ObjectPosition, connectivity.ObjectWorker}
	b.Capabilities = []spi.Capability{b.Capabilities[1], b.Capabilities[0]}
	// GeneratedAt differs; the digest must not depend on it.
	b.GeneratedAt = time.Unix(9999, 0).UTC()

	da, err := a.Digest()
	if err != nil {
		t.Fatal(err)
	}
	db, err := b.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if da != db {
		t.Fatalf("digest depends on order or generation time: %q vs %q", da, db)
	}

	c := validManifest()
	c.Version = "2.0.0"
	dc, err := c.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if dc == da {
		t.Fatal("digest did not change for a different published surface")
	}
}

func TestAdapterManifestDigestRejectsInvalidManifest(t *testing.T) {
	m := spi.AdapterManifest{}
	if _, err := m.Digest(); !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
}

func TestAdapterManifestSupportsAndCapabilityVersion(t *testing.T) {
	m := validManifest()
	want := spi.Capability{Object: connectivity.ObjectWorker, Operation: spi.OpRead, Version: "v1"}
	if !m.Supports(want) {
		t.Fatalf("Supports(%v) = false", want)
	}
	if v, ok := m.CapabilityVersion(connectivity.ObjectWorker, spi.OpRead); !ok || v != "v1" {
		t.Fatalf("CapabilityVersion = %q, %v", v, ok)
	}
	if _, ok := m.CapabilityVersion(connectivity.ObjectCompensation, spi.OpRead); ok {
		t.Fatal("CapabilityVersion reported a capability that was not declared")
	}
}

func TestCapabilityStringAndValid(t *testing.T) {
	c := spi.Capability{Object: connectivity.ObjectWorker, Operation: spi.OpRead, Version: "v1"}
	if !c.Valid() {
		t.Fatal("expected valid capability")
	}
	if got, want := c.String(), "WORKER:READ@v1"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if (spi.Capability{}).Valid() {
		t.Fatal("zero capability must not be valid")
	}
}

func TestOperationValid(t *testing.T) {
	for _, op := range []spi.Operation{spi.OpRead, spi.OpObserve, spi.OpWrite} {
		if !op.Valid() {
			t.Fatalf("%v should be valid", op)
		}
	}
	if spi.Operation("DELETE").Valid() {
		t.Fatal("unknown operation must not be valid")
	}
}
