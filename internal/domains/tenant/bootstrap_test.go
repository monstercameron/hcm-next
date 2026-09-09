package tenant_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/tenant"
)

func manifest(t *testing.T, mutate ...func(*tenant.BootstrapManifest)) tenant.BootstrapManifest {
	t.Helper()
	m := tenant.BootstrapManifest{
		ManifestID:       "onboard:acme-pilot",
		Tenant:           "acme-pilot",
		Cell:             "cell-local",
		Region:           "us-east",
		ResidencyProfile: "us-standard",
		IsolationTier:    "SHARED",
		Revision:         1,
		OwnerRef:         "person:cs-lead",
		CreatedAt:        time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		CreatedBy:        "person:cs-lead",
	}
	for _, fn := range mutate {
		fn(&m)
	}
	return m
}

// TestTodo_TENANT_002 proves the reduced bootstrap-idempotency slice this
// lane implements: a first bootstrap always applies; replaying the identical
// revision is a no-op; changing content under the same revision number is
// refused; a strictly newer revision applies; a stale (older) revision is
// refused; and a manifest naming a different manifest identity than the one
// already applied is refused.
func TestTodo_TENANT_002(t *testing.T) {
	m1 := manifest(t)

	t.Run("a tenant never bootstrapped always applies", func(t *testing.T) {
		outcome, err := tenant.ResolveBootstrap(nil, m1)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if outcome.Decision != tenant.BootstrapApply {
			t.Fatalf("decision %s, want APPLY", outcome.Decision)
		}
		if outcome.Digest == "" {
			t.Fatal("APPLY outcome carries no digest")
		}
	})

	digest1, err := m1.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	current := &tenant.BootstrapRecord{
		ManifestID: m1.ManifestID,
		Revision:   m1.Revision,
		Digest:     digest1,
		AppliedAt:  time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC),
	}

	t.Run("replaying the identical revision is a no-op", func(t *testing.T) {
		replay := manifest(t) // identical content and revision
		outcome, err := tenant.ResolveBootstrap(current, replay)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if outcome.Decision != tenant.BootstrapNoop {
			t.Fatalf("decision %s, want NOOP", outcome.Decision)
		}
		if outcome.Digest != digest1 {
			t.Fatalf("noop digest %s, want %s", outcome.Digest, digest1)
		}
	})

	t.Run("changing content under the same revision is refused", func(t *testing.T) {
		changed := manifest(t, func(m *tenant.BootstrapManifest) { m.Region = "eu-west" })
		outcome, err := tenant.ResolveBootstrap(current, changed)
		if err == nil {
			t.Fatal("a same-revision content change was accepted")
		}
		if !errors.Is(err, tenant.ErrBootstrapRejected) {
			t.Fatalf("error %v, want ErrBootstrapRejected", err)
		}
		if outcome.Decision != tenant.BootstrapRejected {
			t.Fatalf("decision %s, want REJECTED", outcome.Decision)
		}
		if outcome.Reason == "" {
			t.Fatal("rejected outcome carries no reason")
		}
	})

	t.Run("a strictly newer revision applies regardless of content", func(t *testing.T) {
		next := manifest(t, func(m *tenant.BootstrapManifest) {
			m.Revision = 2
			m.Region = "eu-west"
		})
		outcome, err := tenant.ResolveBootstrap(current, next)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if outcome.Decision != tenant.BootstrapApply {
			t.Fatalf("decision %s, want APPLY", outcome.Decision)
		}
		if outcome.Digest == digest1 {
			t.Fatal("a manifest with different content produced the same digest as revision 1")
		}
	})

	t.Run("a stale revision is refused", func(t *testing.T) {
		ahead := &tenant.BootstrapRecord{ManifestID: m1.ManifestID, Revision: 3, Digest: "whatever-revision-3-hashed-to"}
		stale := manifest(t) // revision 1, older than 3
		outcome, err := tenant.ResolveBootstrap(ahead, stale)
		if err == nil {
			t.Fatal("a stale revision was accepted")
		}
		if !errors.Is(err, tenant.ErrBootstrapRejected) {
			t.Fatalf("error %v, want ErrBootstrapRejected", err)
		}
		if outcome.Decision != tenant.BootstrapRejected {
			t.Fatalf("decision %s, want REJECTED", outcome.Decision)
		}
	})

	t.Run("a different manifest identity for the same tenant is refused", func(t *testing.T) {
		other := manifest(t, func(m *tenant.BootstrapManifest) { m.ManifestID = "onboard:acme-pilot-v2" })
		outcome, err := tenant.ResolveBootstrap(current, other)
		if err == nil {
			t.Fatal("a different manifest identity was accepted")
		}
		if !errors.Is(err, tenant.ErrBootstrapRejected) {
			t.Fatalf("error %v, want ErrBootstrapRejected", err)
		}
		if outcome.Decision != tenant.BootstrapRejected {
			t.Fatalf("decision %s, want REJECTED", outcome.Decision)
		}
	})
}

// TestTodo_TENANT_002_Security proves a structurally invalid manifest can
// never resolve to APPLY or NOOP -- Validate's failure always surfaces
// through Digest before ResolveBootstrap can decide anything -- and that
// Digest never depends on map iteration order or any other nondeterminism:
// the same manifest content always produces the same digest, computed
// independently, twice.
func TestTodo_TENANT_002_Security(t *testing.T) {
	t.Run("a manifest missing a required field cannot be resolved", func(t *testing.T) {
		invalid := manifest(t, func(m *tenant.BootstrapManifest) { m.OwnerRef = "" })
		if _, err := tenant.ResolveBootstrap(nil, invalid); err == nil {
			t.Fatal("an invalid manifest resolved without error")
		} else if !errors.Is(err, tenant.ErrInvalidBootstrapManifest) {
			t.Fatalf("error %v, want ErrInvalidBootstrapManifest", err)
		}
	})

	t.Run("a zero revision is refused", func(t *testing.T) {
		invalid := manifest(t, func(m *tenant.BootstrapManifest) { m.Revision = 0 })
		if _, err := invalid.Digest(); !errors.Is(err, tenant.ErrInvalidBootstrapManifest) {
			t.Fatalf("error %v, want ErrInvalidBootstrapManifest", err)
		}
	})

	t.Run("digest is deterministic across independent computations", func(t *testing.T) {
		a := manifest(t)
		b := manifest(t)
		da, err := a.Digest()
		if err != nil {
			t.Fatalf("digest a: %v", err)
		}
		db, err := b.Digest()
		if err != nil {
			t.Fatalf("digest b: %v", err)
		}
		if da != db {
			t.Fatalf("two independently built, identical manifests digested to %s and %s", da, db)
		}
	})
}

// TestTodo_TENANT_002_Golden pins Digest's exact algorithm: a fixed,
// well-known manifest must always digest to the same sha256 hex value, so a
// silent change to the canonical encoding is caught here rather than by every
// caller's own tests independently -- the same discipline
// internal/data/artifacts.TestTodo_MODEL_029_Golden applies to content ids.
func TestTodo_TENANT_002_Golden(t *testing.T) {
	golden := tenant.BootstrapManifest{
		ManifestID:       "onboard:golden-pilot",
		Tenant:           "golden-pilot",
		Cell:             "cell-local",
		Region:           "us-east",
		ResidencyProfile: "us-standard",
		IsolationTier:    "SHARED",
		Revision:         1,
		OwnerRef:         "person:golden-owner",
		CreatedAt:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedBy:        "person:golden-author",
	}
	const want = "79173cfb7a61ee7a8d5ac2d4fbd1ada79d1799a52ce5c243c6d7ead84f13499f"

	got, err := golden.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if len(got) != 64 {
		t.Fatalf("digest %q is not a 64 character hex string", got)
	}
	if got != want {
		t.Logf("golden digest is %s (update the pinned constant if this is an intentional encoding change)", got)
		t.Fatalf("golden vector digested to %s, want %s", got, want)
	}
}

func TestBootstrapManifest_ValidateRejectsEachRequiredField(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*tenant.BootstrapManifest)
		field  string
	}{
		{"manifest id", func(m *tenant.BootstrapManifest) { m.ManifestID = " " }, "manifest_id"},
		{"tenant", func(m *tenant.BootstrapManifest) { m.Tenant = "" }, "tenant"},
		{"cell", func(m *tenant.BootstrapManifest) { m.Cell = "\t" }, "cell"},
		{"region", func(m *tenant.BootstrapManifest) { m.Region = "" }, "region"},
		{"residency", func(m *tenant.BootstrapManifest) { m.ResidencyProfile = " " }, "residency_profile"},
		{"isolation", func(m *tenant.BootstrapManifest) { m.IsolationTier = "" }, "isolation_tier"},
		{"owner", func(m *tenant.BootstrapManifest) { m.OwnerRef = " " }, "owner_ref"},
		{"creator", func(m *tenant.BootstrapManifest) { m.CreatedBy = "" }, "created_by"},
		{"revision", func(m *tenant.BootstrapManifest) { m.Revision = 0 }, "revision"},
		{"created at", func(m *tenant.BootstrapManifest) { m.CreatedAt = time.Time{} }, "created_at"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := manifest(t, tt.mutate)
			err := m.Validate()
			if !errors.Is(err, tenant.ErrInvalidBootstrapManifest) {
				t.Fatalf("Validate error = %v, want ErrInvalidBootstrapManifest", err)
			}
			if !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("Validate error = %q, want field %q", err, tt.field)
			}
			if _, err := m.Digest(); !errors.Is(err, tenant.ErrInvalidBootstrapManifest) {
				t.Fatalf("Digest error = %v, want ErrInvalidBootstrapManifest", err)
			}
		})
	}
}

func TestResolveBootstrap_UsesCaseInsensitiveCurrentDigest(t *testing.T) {
	m := manifest(t)
	digest, err := m.Digest()
	if err != nil {
		t.Fatal(err)
	}
	current := &tenant.BootstrapRecord{ManifestID: m.ManifestID, Revision: m.Revision, Digest: strings.ToUpper(digest)}
	outcome, err := tenant.ResolveBootstrap(current, m)
	if err != nil {
		t.Fatalf("ResolveBootstrap: %v", err)
	}
	if outcome.Decision != tenant.BootstrapNoop || outcome.Digest != digest || outcome.Reason != "" {
		t.Fatalf("outcome = %+v, want uppercase digest to be a clean NOOP", outcome)
	}
}
