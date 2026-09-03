package schemaflux

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// yamlCapability mirrors one entry of the "capabilities" list in
// testdata/capability_manifest.yaml.
type yamlCapability struct {
	ID                 string   `yaml:"id"`
	Version            uint32   `yaml:"version"`
	OwnerDomain        string   `yaml:"owner_domain"`
	EffectClass        string   `yaml:"effect_class"`
	ReadDomains        []string `yaml:"read_domains"`
	RiskClass          string   `yaml:"risk_class"`
	IdempotencyPolicy  string   `yaml:"idempotency_policy_ref"`
	AuthZScope         string   `yaml:"authz_scope_ref"`
	LegalBasis         string   `yaml:"legal_basis_ref"`
	Entitlement        string   `yaml:"entitlement_ref"`
	SLOClass           string   `yaml:"slo_class_ref"`
	TestRef            string   `yaml:"test_ref"`
	StorageDisposition string   `yaml:"storage_disposition"`
}

// yamlCapabilityManifest mirrors testdata/capability_manifest.yaml's top
// level.
type yamlCapabilityManifest struct {
	SchemaVersion int              `yaml:"schema_version"`
	Source        string           `yaml:"source"`
	Capabilities  []yamlCapability `yaml:"capabilities"`
}

// LoadCapabilityManifest parses the P1A capability manifest fixture at path.
// No manifest file exists yet under schema/schemaflux for capabilities, so
// this package carries its own at testdata/capability_manifest.yaml, derived
// from internal/capability's BOOTSTRAP table
// (internal/capability/bootstrap.go bootstrapDefinitions). CrossCheckCapability
// Manifest detects drift between this fixture and that live table.
func LoadCapabilityManifest(path string) ([]CapabilityManifestEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("schemaflux: read %s: %w", path, err)
	}
	// Store an absolute SourceFile regardless of whether the caller passed a
	// relative one, matching LoadDefinitions (whose dir argument is always
	// absolute in practice): a caller that later relativizes SourceFile
	// against a repo root (MSRC-001's manifest) must not have to guess
	// whether this path is relative to the process cwd or to something else.
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}

	var manifest yamlCapabilityManifest
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("schemaflux: parse %s: %w", path, err)
	}

	out := make([]CapabilityManifestEntry, 0, len(manifest.Capabilities))
	for _, c := range manifest.Capabilities {
		out = append(out, CapabilityManifestEntry{
			SourceFile:         path,
			ID:                 c.ID,
			Version:            c.Version,
			OwnerDomain:        c.OwnerDomain,
			EffectClass:        c.EffectClass,
			ReadDomains:        c.ReadDomains,
			RiskClass:          c.RiskClass,
			IdempotencyPolicy:  c.IdempotencyPolicy,
			AuthZScope:         c.AuthZScope,
			LegalBasis:         c.LegalBasis,
			Entitlement:        c.Entitlement,
			SLOClass:           c.SLOClass,
			TestRef:            c.TestRef,
			StorageDisposition: c.StorageDisposition,
		})
	}
	return out, nil
}
