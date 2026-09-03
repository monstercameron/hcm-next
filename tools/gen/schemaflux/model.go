package schemaflux

import "strconv"

// Definition is the qualification fixture's own parsed representation of one
// entry from a schema/schemaflux/business_intents/v1/*.yaml source file (see
// that directory's README.md field table). It is independent of the
// compiled-in internal/intent.Definition type: this package never imports
// internal/intent except from its cross-check test, and never writes it.
type Definition struct {
	// SourceFile is the repo-relative path this definition was parsed from.
	SourceFile string
	// SourceLine is the 1-based line of this definition's first YAML key
	// (catalog_number) within SourceFile, for diagnostics.
	SourceLine int

	CatalogNumber int
	IntentTypeID  string
	Version       uint32
	DisplayName   string
	Description   string
	OwnerPlane    string
	OwnerDomain   string
	KernelFamily  string
	Maturity      string

	InputSchemaRef  string
	ResultSchemaRef string

	AllowedInitiators       []string
	AllowedExecutionModes   []string
	SideEffectProfile       string
	RiskClass               string
	DataClassificationFloor string
	SubjectKinds            []string

	RequiredCapabilities   []string
	GovernanceRequirements []string
	Preconditions          []string
	Invariants             []string

	IdempotencyScope      string
	ConflictFootprintRule string
	ProposalBindingRule   string
	RevalidationRule      string
	CancellationRule      string
	CompensationRule      string
	EvidenceRule          string
	RetentionClass        string
	OutcomeContract       string
	SLOClass              string
	AvailabilityPolicy    string
	PhaseDepth            string
}

// Ref returns the canonical "<intent_type_id>/v<version>" identity string,
// matching internal/intent.Ref.String's convention.
func (d Definition) Ref() string {
	return d.IntentTypeID + "/v" + strconv.FormatUint(uint64(d.Version), 10)
}

// CapabilityManifestEntry is one entry of the P1A capability manifest fixture
// (tools/gen/schemaflux/testdata/capability_manifest.yaml), derived from
// internal/capability's BOOTSTRAP table because no manifest file exists yet
// under schema/schemaflux for capabilities (MSRC-001).
type CapabilityManifestEntry struct {
	SourceFile string

	ID                 string
	Version            uint32
	OwnerDomain        string
	EffectClass        string
	ReadDomains        []string
	RiskClass          string
	IdempotencyPolicy  string
	AuthZScope         string
	LegalBasis         string
	Entitlement        string
	SLOClass           string
	TestRef            string
	StorageDisposition string
}

// Ref returns the canonical "<id>/v<version>" identity string.
func (c CapabilityManifestEntry) Ref() string {
	return c.ID + "/v" + strconv.FormatUint(uint64(c.Version), 10)
}

// Catalog is the compiled qualification catalog: the bounded set of
// definitions and capability manifest entries the fixture compiles, plus the
// schema and capability reference sets it exposes to consumers (mirroring
// internal/intent/definitions.Catalog's shape for the cross-check).
type Catalog struct {
	Definitions        []Definition
	CapabilityManifest []CapabilityManifestEntry
	Schemas            []string
	Capabilities       []string
}
