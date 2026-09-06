package defaultactivation_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/defaultactivation"
)

func TestDefaultActivation_MetadataAndDecisions(t *testing.T) {
	if defaultactivation.Version() != 1 || defaultactivation.ContractExplain() == "" {
		t.Fatalf("contract metadata incomplete: version=%d explain=%q", defaultactivation.Version(), defaultactivation.ContractExplain())
	}
	allowed, err := defaultactivation.Admit(defaultactivation.Request{Feature: feature(defaultactivation.CoreRequired), Manifest: manifest("people.home")})
	if err != nil || !allowed.Allowed || !strings.Contains(defaultactivation.Explain(allowed), "allowed=true") {
		t.Fatalf("allowed decision = %+v, err=%v", allowed, err)
	}
	denied, err := defaultactivation.Admit(defaultactivation.Request{Feature: feature(defaultactivation.AvailableNotEnabled), Manifest: manifest("people.home")})
	if !errors.Is(err, defaultactivation.ErrUnadmitted) || denied.Allowed || denied.Reason != "DEFAULT_DISPOSITION_NOT_ADMITTED" || denied.EvidenceDigest == "" || !strings.Contains(defaultactivation.Explain(denied), "allowed=false") {
		t.Fatalf("denied disposition decision = %+v, err=%v", denied, err)
	}
	missing, err := defaultactivation.Admit(defaultactivation.Request{Feature: feature(defaultactivation.CoreRequired), Manifest: manifest("other")})
	if !errors.Is(err, defaultactivation.ErrUnadmitted) || missing.Allowed || missing.Reason != "FEATURE_NOT_IN_ADMITTED_MANIFEST" || missing.EvidenceDigest == "" {
		t.Fatalf("missing manifest decision = %+v, err=%v", missing, err)
	}
	if err := defaultactivation.Validate(defaultactivation.Request{Feature: feature(defaultactivation.CoreRequired), Manifest: manifest("people.home")}); err != nil {
		t.Fatalf("Validate rejected a valid request: %v", err)
	}
}

func TestDefaultActivation_RejectsMalformedFeatureAndManifest(t *testing.T) {
	featureFields := []struct {
		name   string
		mutate func(*defaultactivation.Feature)
	}{
		{"id", func(f *defaultactivation.Feature) { f.ID = " " }},
		{"version", func(f *defaultactivation.Feature) { f.Version = "" }},
		{"owner", func(f *defaultactivation.Feature) { f.Owner = "" }},
		{"business owner", func(f *defaultactivation.Feature) { f.BusinessOwner = "" }},
		{"disposition", func(f *defaultactivation.Feature) { f.DefaultDisposition = "UNKNOWN" }},
		{"capability refs", func(f *defaultactivation.Feature) { f.CapabilityRefs = nil }},
		{"query refs", func(f *defaultactivation.Feature) { f.QueryContracts = nil }},
		{"table refs", func(f *defaultactivation.Feature) { f.Tables = nil }},
		{"duplicate capability", func(f *defaultactivation.Feature) { f.CapabilityRefs = []string{"x", "x"} }},
		{"empty command ref", func(f *defaultactivation.Feature) { f.CommandContracts = []string{" "} }},
	}
	for _, tc := range featureFields {
		t.Run("feature "+tc.name, func(t *testing.T) {
			f := feature(defaultactivation.CoreRequired)
			tc.mutate(&f)
			if _, err := defaultactivation.Admit(defaultactivation.Request{Feature: f, Manifest: manifest("people.home")}); !errors.Is(err, defaultactivation.ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
	manifestFields := []struct {
		name   string
		mutate func(*defaultactivation.Manifest)
	}{
		{"id", func(m *defaultactivation.Manifest) { m.ID = "" }},
		{"version", func(m *defaultactivation.Manifest) { m.Version = " " }},
		{"digest", func(m *defaultactivation.Manifest) { m.Digest = "" }},
		{"duplicate admission", func(m *defaultactivation.Manifest) { m.AdmittedFeatureIDs = []string{"people.home", "people.home"} }},
	}
	for _, tc := range manifestFields {
		t.Run("manifest "+tc.name, func(t *testing.T) {
			m := manifest("people.home")
			tc.mutate(&m)
			if _, err := defaultactivation.Admit(defaultactivation.Request{Feature: feature(defaultactivation.CoreRequired), Manifest: m}); !errors.Is(err, defaultactivation.ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestDefaultActivation_DigestChangesWithDecisionInputs(t *testing.T) {
	core, err := defaultactivation.Admit(defaultactivation.Request{Feature: feature(defaultactivation.CoreRequired), Manifest: manifest("people.home")})
	if err != nil {
		t.Fatal(err)
	}
	domain, err := defaultactivation.Admit(defaultactivation.Request{Feature: feature(defaultactivation.DomainPackDefault), Manifest: manifest("people.home")})
	if err != nil {
		t.Fatal(err)
	}
	if core.EvidenceDigest == domain.EvidenceDigest || !strings.HasPrefix(core.EvidenceDigest, "sha256:") {
		t.Fatalf("evidence digest did not bind disposition: core=%q domain=%q", core.EvidenceDigest, domain.EvidenceDigest)
	}
}
