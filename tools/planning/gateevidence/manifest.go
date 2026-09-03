package gateevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// P1AManifest is the schema for definitions/planning/gates/p1a-manifest.yaml
// (NEXT-002). Field order is fixed by struct declaration order, which is
// also the order json.Marshal emits them in - that fixed order is what
// makes CanonicalDigest stable across re-serialization.
type P1AManifest struct {
	SchemaVersion           int                     `yaml:"schema_version" json:"schema_version"`
	Release                 string                  `yaml:"release" json:"release"`
	TodoID                  string                  `yaml:"todo_id" json:"todo_id"`
	SignedDate              string                  `yaml:"signed_date" json:"signed_date"`
	FreshnessWindowDays     int                     `yaml:"freshness_window_days" json:"freshness_window_days"`
	Workflow                string                  `yaml:"workflow" json:"workflow"`
	Intents                 []Intent                `yaml:"intents" json:"intents"`
	EffectCeiling           []string                `yaml:"effect_ceiling" json:"effect_ceiling"`
	Capabilities            []Capability            `yaml:"capabilities" json:"capabilities"`
	Commands                []Command               `yaml:"commands" json:"commands"`
	Migrations              Migrations              `yaml:"migrations" json:"migrations"`
	QualificationDecisions  []QualificationDecision `yaml:"qualification_decisions" json:"qualification_decisions"`
	Evidence                []EvidenceEntry         `yaml:"evidence" json:"evidence"`
	ForbiddenImportPrefixes []string                `yaml:"forbidden_import_prefixes" json:"forbidden_import_prefixes"`
	Signature               *Signature              `yaml:"signature,omitempty" json:"signature,omitempty"`
}

// Intent is one of the eight P1A executable intent contracts
// (next-steps.md "P1A - paid observation, preflight and simulation").
type Intent struct {
	Order       int      `yaml:"order" json:"order"`
	ID          string   `yaml:"id" json:"id"`
	Modes       []string `yaml:"modes,omitempty" json:"modes,omitempty"`
	Disposition string   `yaml:"disposition" json:"disposition"`
}

// Capability is one bootstrap capability-registry entry
// (internal/capability/bootstrap.go bootstrapDefinitions, cross-checked by
// TOOL-004 against tools/gen/schemaflux/testdata/capability_manifest.yaml).
type Capability struct {
	ID          string `yaml:"id" json:"id"`
	Version     int    `yaml:"version" json:"version"`
	OwnerDomain string `yaml:"owner_domain" json:"owner_domain"`
	EffectClass string `yaml:"effect_class" json:"effect_class"`
	TestRef     string `yaml:"test_ref" json:"test_ref"`
}

// Command is one of the four P1A composition-root binaries (NEXT-004).
type Command struct {
	Name    string `yaml:"name" json:"name"`
	Package string `yaml:"package" json:"package"`
	Purpose string `yaml:"purpose" json:"purpose"`
}

// MigrationFile is one embedded Goose migration (migrations/migrations.go
// Files()), named and checksummed exactly as that function computes it.
type MigrationFile struct {
	Version  int64  `yaml:"version" json:"version"`
	Name     string `yaml:"name" json:"name"`
	Checksum string `yaml:"checksum" json:"checksum"`
}

// MigrationGap records a skipped version number in the migration sequence,
// so an unexplained gap is a recorded fact rather than a silent omission.
type MigrationGap struct {
	Version int64  `yaml:"version" json:"version"`
	Reason  string `yaml:"reason" json:"reason"`
}

// Migrations is the P1A manifest's migration closure.
type Migrations struct {
	Dialect string          `yaml:"dialect" json:"dialect"`
	Files   []MigrationFile `yaml:"files" json:"files"`
	Gaps    []MigrationGap  `yaml:"gaps,omitempty" json:"gaps,omitempty"`
}

// QualificationDecision is one toolchain qualification decision the P1A
// manifest depends on (TOOL-004, TOOL-008, UX-QUAL-001, WF-RUN-000).
type QualificationDecision struct {
	TodoID   string `yaml:"todo_id" json:"todo_id"`
	Decision string `yaml:"decision" json:"decision"`
	Record   string `yaml:"record" json:"record"`
}

// EvidenceEntry names exactly one Test function, in exactly one package,
// that substantiates exactly one todo's manifest inclusion. Compile
// resolves each entry to a Verdict.
type EvidenceEntry struct {
	TodoID  string `yaml:"todo_id" json:"todo_id"`
	Test    string `yaml:"test" json:"test"`
	Package string `yaml:"package" json:"package"`
}

// Signature is an Ed25519 signature over a manifest's CanonicalDigest.
type Signature struct {
	Algorithm  string `yaml:"algorithm" json:"algorithm"`
	PublicKey  string `yaml:"public_key" json:"public_key"`
	Value      string `yaml:"value" json:"value"`
	KeyFixture string `yaml:"key_fixture" json:"key_fixture"`
}

// LoadP1AManifest reads and parses a P1A manifest YAML file.
func LoadP1AManifest(path string) (*P1AManifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m P1AManifest
	if err := yaml.Unmarshal(content, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &m, nil
}

// Violation names one manifest defect.
type Violation struct {
	Field string
	Issue string
}

func (v Violation) String() string { return fmt.Sprintf("%s: %s", v.Field, v.Issue) }

// Validate returns every structural violation on m. It does not verify the
// signature (see Verify) or evidence freshness (see Compile).
func (m P1AManifest) Validate() []Violation {
	var violations []Violation
	add := func(field, issue string) { violations = append(violations, Violation{Field: field, Issue: issue}) }

	if m.SchemaVersion == 0 {
		add("schema_version", "missing")
	}
	if m.Release == "" {
		add("release", "missing")
	}
	if m.TodoID == "" {
		add("todo_id", "missing")
	}
	if m.SignedDate == "" {
		add("signed_date", "missing")
	}
	if m.FreshnessWindowDays <= 0 {
		add("freshness_window_days", "must be positive")
	}
	if len(m.Intents) == 0 {
		add("intents", "missing")
	}
	if len(m.EffectCeiling) == 0 {
		add("effect_ceiling", "missing - a P1A manifest must state its zero-effect ceiling")
	}
	if len(m.Capabilities) == 0 {
		add("capabilities", "missing")
	}
	for _, c := range m.Capabilities {
		if c.EffectClass != "READ_ONLY" {
			add("capabilities", fmt.Sprintf("%s: effect_class %q is not READ_ONLY; P1A grants no other effect class", c.ID, c.EffectClass))
		}
	}
	if len(m.Commands) == 0 {
		add("commands", "missing")
	}
	if len(m.Migrations.Files) == 0 {
		add("migrations", "missing")
	}
	if len(m.QualificationDecisions) == 0 {
		add("qualification_decisions", "missing")
	}
	if len(m.Evidence) == 0 {
		add("evidence", "missing")
	}
	if m.Signature == nil {
		add("signature", "missing - a P1A manifest must be signed")
	} else {
		if m.Signature.Algorithm != "ed25519" {
			add("signature.algorithm", fmt.Sprintf("unsupported algorithm %q", m.Signature.Algorithm))
		}
		if m.Signature.PublicKey == "" {
			add("signature.public_key", "missing")
		}
		if m.Signature.Value == "" {
			add("signature.value", "missing")
		}
	}

	return violations
}

// digestPayload is the canonical projection hashed by CanonicalDigest and
// signed by Sign/Verify: every manifest field except the signature itself,
// so a signature can never cover its own bytes.
type digestPayload struct {
	SchemaVersion           int                     `json:"schema_version"`
	Release                 string                  `json:"release"`
	TodoID                  string                  `json:"todo_id"`
	SignedDate              string                  `json:"signed_date"`
	FreshnessWindowDays     int                     `json:"freshness_window_days"`
	Workflow                string                  `json:"workflow"`
	Intents                 []Intent                `json:"intents"`
	EffectCeiling           []string                `json:"effect_ceiling"`
	Capabilities            []Capability            `json:"capabilities"`
	Commands                []Command               `json:"commands"`
	Migrations              Migrations              `json:"migrations"`
	QualificationDecisions  []QualificationDecision `json:"qualification_decisions"`
	Evidence                []EvidenceEntry         `json:"evidence"`
	ForbiddenImportPrefixes []string                `json:"forbidden_import_prefixes"`
}

func (m P1AManifest) payload() digestPayload {
	return digestPayload{
		SchemaVersion:           m.SchemaVersion,
		Release:                 m.Release,
		TodoID:                  m.TodoID,
		SignedDate:              m.SignedDate,
		FreshnessWindowDays:     m.FreshnessWindowDays,
		Workflow:                m.Workflow,
		Intents:                 m.Intents,
		EffectCeiling:           m.EffectCeiling,
		Capabilities:            m.Capabilities,
		Commands:                m.Commands,
		Migrations:              m.Migrations,
		QualificationDecisions:  m.QualificationDecisions,
		Evidence:                m.Evidence,
		ForbiddenImportPrefixes: m.ForbiddenImportPrefixes,
	}
}

// CanonicalDigest returns the hex-encoded sha256 digest of m's canonical
// JSON projection (every field except Signature). Two manifests with
// identical content but a different (or absent) signature digest
// identically; this is exactly the value Sign covers and Verify checks.
func (m P1AManifest) CanonicalDigest() (string, error) {
	b, err := json.Marshal(m.payload())
	if err != nil {
		return "", fmt.Errorf("marshal canonical manifest: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
