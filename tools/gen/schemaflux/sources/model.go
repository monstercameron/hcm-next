package sources

import "strconv"

// Metamodel is the parsed schema/schemaflux/metamodel/v1/metamodel.yaml
// (MSRC-002): the shared vocabulary every entity/relationship/registry
// source is validated against.
type Metamodel struct {
	SourceFile string

	MetamodelID string
	Version     int

	PresenceRules         []string
	TemporalBehaviors     []string
	CorrectionBehaviors   []string
	EntityClasses         []string
	DefinitionStatuses    []string
	ClassificationLabels  []string
	AuthorityKinds        []string
	MergePolicies         []string
	ConsistencyBoundaries []string
	Cardinalities         []string
	NoBusinessLifecycle   string
}

// PresenceSet, TemporalSet, etc. are built once by [NewVocabulary] for O(1)
// membership checks during [Compile].
type Vocabulary struct {
	Presence         map[string]bool
	Temporal         map[string]bool
	Correction       map[string]bool
	EntityClass      map[string]bool
	Status           map[string]bool
	Classification   map[string]bool
	AuthorityKind    map[string]bool
	MergePolicy      map[string]bool
	Boundary         map[string]bool
	Cardinality      map[string]bool
	NoBusinessMarker string
}

// NewVocabulary compiles m's declared enumerations into O(1)-lookup sets.
func NewVocabulary(m Metamodel) Vocabulary {
	toSet := func(vals []string) map[string]bool {
		out := make(map[string]bool, len(vals))
		for _, v := range vals {
			out[v] = true
		}
		return out
	}
	return Vocabulary{
		Presence:         toSet(m.PresenceRules),
		Temporal:         toSet(m.TemporalBehaviors),
		Correction:       toSet(m.CorrectionBehaviors),
		EntityClass:      toSet(m.EntityClasses),
		Status:           toSet(m.DefinitionStatuses),
		Classification:   toSet(m.ClassificationLabels),
		AuthorityKind:    toSet(m.AuthorityKinds),
		MergePolicy:      toSet(m.MergePolicies),
		Boundary:         toSet(m.ConsistencyBoundaries),
		Cardinality:      toSet(m.Cardinalities),
		NoBusinessMarker: m.NoBusinessLifecycle,
	}
}

// PropertySource is one property entry of an EntitySource (see
// schema/schemaflux/entities/v1/*.yaml's `properties` list).
type PropertySource struct {
	Name              string
	GoType            string
	SchemaPath        string
	Presence          string
	Classification    string
	Temporal          string
	AuthorityRef      string
	Correction        string
	RetentionClassRef string
	Status            string
}

// Ref returns the canonical "<entity_key>.<name>" PropertyRef form.
func (p PropertySource) Ref(entityKey string) string { return entityKey + "." + p.Name }

// EntitySource is one parsed entity entry from a
// schema/schemaflux/entities/v1/*.yaml family file.
type EntitySource struct {
	SourceFile string
	SourceLine int
	Family     string

	Name    string
	Version int
	Key     string
	Aliases []string

	OwnerDomain string
	Class       string
	Status      string
	Covered     bool

	TenantScoped        bool
	LifecycleAssignment string
	CommandBoundary     string
	StreamKind          string
	Invariants          []string
	ChildRefs           []string

	SourceRef string
	Summary   string

	Properties []PropertySource
}

// Ref returns the canonical "Name/vN" EntityRef form.
func (e EntitySource) Ref() string { return e.Name + "/v" + strconv.Itoa(e.Version) }

// RelationshipSource is one parsed relationship entry from a
// schema/schemaflux/entities/v1/*.yaml family file.
type RelationshipSource struct {
	SourceFile string
	SourceLine int
	Family     string

	Name         string
	Version      int
	SourceEntity string
	TargetEntity string
	Cardinality  string
	Exclusive    bool
	AllowCycles  bool
	TenantScoped bool
	Covered      bool
	SourceRef    string
}

// Ref returns the canonical "Name/vN" RelationshipRef form.
func (r RelationshipSource) Ref() string { return r.Name + "/v" + strconv.Itoa(r.Version) }

// AuthoritySource is one parsed authority entry from
// schema/schemaflux/registries/v1/registries.yaml.
type AuthoritySource struct {
	SourceFile string
	SourceLine int

	Ref              string
	Kind             string
	DomainScope      string
	EffectiveFrom    string
	Exclusive        bool
	FreshnessSeconds uint32
	Merge            string
	EvidenceRef      string
	Covered          bool
}

// RetentionClassSource is one parsed retention-class entry from
// schema/schemaflux/registries/v1/registries.yaml.
type RetentionClassSource struct {
	SourceFile string
	SourceLine int

	Ref                   string
	DefaultPeriodDays     uint32
	JurisdictionOverrides map[string]uint32
	TriggerEvent          string
	DispositionOwner      string
	AuthorityRef          string
	Covered               bool
}

// Bundle is every parsed source a caller assembles before calling [Compile]:
// the metamodel, the registries, and every family's entities/relationships.
type Bundle struct {
	Metamodel     Metamodel
	Authorities   []AuthoritySource
	Retentions    []RetentionClassSource
	Entities      []EntitySource
	Relationships []RelationshipSource
}

// Manifest is the deterministic, compiled result of [Compile]: every entity,
// relationship, authority and retention-class source, plus the covered
// subset's cross-check status against internal/intent/model.Catalog().
type Manifest struct {
	Entities      []EntitySource
	Relationships []RelationshipSource
	Authorities   []AuthoritySource
	Retentions    []RetentionClassSource

	// FamilyCounts maps family name -> entity count, for a per-family GREEN
	// assertion (e.g. "kernel has exactly 11 entities").
	FamilyCounts map[string]int
}
