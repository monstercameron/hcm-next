package sources

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// metamodelEntry is a superset struct covering every field used across
// metamodel.yaml's list sections (primitive_types, presence_rules, ...). A
// given section only sets the subset of fields its entries declare; strict
// decoding still rejects a genuinely unknown key because every key the file
// actually uses appears here once.
type metamodelEntry struct {
	Name                string   `yaml:"name"`
	Value               string   `yaml:"value"`
	KernelValueType     string   `yaml:"kernel_value_type"`
	GoSymbol            string   `yaml:"go_symbol"`
	GoType              string   `yaml:"go_type"`
	Description         string   `yaml:"description"`
	Pattern             string   `yaml:"pattern"`
	PatternRef          string   `yaml:"pattern_ref"`
	Example             string   `yaml:"example"`
	Fields              []string `yaml:"fields"`
	RequiredForTemporal []string `yaml:"required_for_temporal"`
	RequiresBitemporal  []string `yaml:"requires_bitemporal"`
	Rank                int      `yaml:"rank"`
	Field               string   `yaml:"field"`
}

type yamlMetamodel struct {
	Metamodel             string           `yaml:"metamodel"`
	MetamodelVersion      int              `yaml:"metamodel_version"`
	PrimitiveTypes        []metamodelEntry `yaml:"primitive_types"`
	PresenceRules         []metamodelEntry `yaml:"presence_rules"`
	IdentityKeyForms      []metamodelEntry `yaml:"identity_key_forms"`
	RevisionKeyForms      []metamodelEntry `yaml:"revision_key_forms"`
	BitemporalFields      []metamodelEntry `yaml:"bitemporal_fields"`
	TemporalBehaviors     []metamodelEntry `yaml:"temporal_behaviors"`
	CorrectionBehaviors   []metamodelEntry `yaml:"correction_behaviors"`
	EntityClasses         []metamodelEntry `yaml:"entity_classes"`
	DefinitionStatuses    []metamodelEntry `yaml:"definition_statuses"`
	ClassificationLabels  []metamodelEntry `yaml:"classification_labels"`
	RetentionAttributes   []metamodelEntry `yaml:"retention_attributes"`
	AuthorityKinds        []metamodelEntry `yaml:"authority_kinds"`
	MergePolicies         []metamodelEntry `yaml:"merge_policies"`
	ConsistencyBoundaries []metamodelEntry `yaml:"consistency_boundaries"`
	Cardinalities         []metamodelEntry `yaml:"cardinalities"`
	NoBusinessLifecycle   string           `yaml:"no_business_lifecycle"`
}

// LoadMetamodel parses schema/schemaflux/metamodel/v1/metamodel.yaml (or an
// equivalent path) using strict (KnownFields) decoding: an unrecognized key
// is a load error, matching the sibling business_intents pipeline's
// "no defaulting" posture.
func LoadMetamodel(path string) (Metamodel, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Metamodel{}, fmt.Errorf("sources: read %s: %w", path, err)
	}
	var y yamlMetamodel
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&y); err != nil {
		return Metamodel{}, fmt.Errorf("sources: parse %s: %w", path, err)
	}

	values := func(entries []metamodelEntry) []string {
		out := make([]string, 0, len(entries))
		for _, e := range entries {
			out = append(out, e.Value)
		}
		return out
	}

	return Metamodel{
		SourceFile:            path,
		MetamodelID:           y.Metamodel,
		Version:               y.MetamodelVersion,
		PresenceRules:         values(y.PresenceRules),
		TemporalBehaviors:     values(y.TemporalBehaviors),
		CorrectionBehaviors:   values(y.CorrectionBehaviors),
		EntityClasses:         values(y.EntityClasses),
		DefinitionStatuses:    values(y.DefinitionStatuses),
		ClassificationLabels:  values(y.ClassificationLabels),
		AuthorityKinds:        values(y.AuthorityKinds),
		MergePolicies:         values(y.MergePolicies),
		ConsistencyBoundaries: values(y.ConsistencyBoundaries),
		Cardinalities:         values(y.Cardinalities),
		NoBusinessLifecycle:   y.NoBusinessLifecycle,
	}, nil
}

type yamlAuthority struct {
	Ref              string `yaml:"ref"`
	Kind             string `yaml:"kind"`
	DomainScope      string `yaml:"domain_scope"`
	EffectiveFrom    string `yaml:"effective_from"`
	Exclusive        bool   `yaml:"exclusive"`
	FreshnessSeconds uint32 `yaml:"freshness_seconds"`
	Merge            string `yaml:"merge"`
	EvidenceRef      string `yaml:"evidence_ref"`
	Covered          bool   `yaml:"covered"`
}

type yamlRetentionClass struct {
	Ref                   string            `yaml:"ref"`
	DefaultPeriodDays     uint32            `yaml:"default_period_days"`
	JurisdictionOverrides map[string]uint32 `yaml:"jurisdiction_overrides"`
	TriggerEvent          string            `yaml:"trigger_event"`
	DispositionOwner      string            `yaml:"disposition_owner"`
	AuthorityRef          string            `yaml:"authority_ref"`
	Covered               bool              `yaml:"covered"`
}

type yamlRegistries struct {
	Registry         string               `yaml:"registry"`
	RegistryVersion  int                  `yaml:"registry_version"`
	SourceContract   string               `yaml:"source_contract"`
	Authorities      []yamlAuthority      `yaml:"authorities"`
	RetentionClasses []yamlRetentionClass `yaml:"retention_classes"`
}

// LoadRegistries parses schema/schemaflux/registries/v1/registries.yaml.
func LoadRegistries(path string) ([]AuthoritySource, []RetentionClassSource, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("sources: read %s: %w", path, err)
	}
	var y yamlRegistries
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&y); err != nil {
		return nil, nil, fmt.Errorf("sources: parse %s: %w", path, err)
	}

	authLines, err := topLevelSequenceLines(raw, "authorities")
	if err != nil {
		return nil, nil, fmt.Errorf("sources: locate authorities lines in %s: %w", path, err)
	}
	if len(authLines) != len(y.Authorities) {
		return nil, nil, fmt.Errorf("sources: %s: found %d authority line(s) but decoded %d",
			path, len(authLines), len(y.Authorities))
	}
	retLines, err := topLevelSequenceLines(raw, "retention_classes")
	if err != nil {
		return nil, nil, fmt.Errorf("sources: locate retention_classes lines in %s: %w", path, err)
	}
	if len(retLines) != len(y.RetentionClasses) {
		return nil, nil, fmt.Errorf("sources: %s: found %d retention_classes line(s) but decoded %d",
			path, len(retLines), len(y.RetentionClasses))
	}

	var authorities []AuthoritySource
	for i, a := range y.Authorities {
		authorities = append(authorities, AuthoritySource{
			SourceFile: path, SourceLine: authLines[i],
			Ref: a.Ref, Kind: a.Kind, DomainScope: a.DomainScope, EffectiveFrom: a.EffectiveFrom,
			Exclusive: a.Exclusive, FreshnessSeconds: a.FreshnessSeconds, Merge: a.Merge,
			EvidenceRef: a.EvidenceRef, Covered: a.Covered,
		})
	}
	var retentions []RetentionClassSource
	for i, r := range y.RetentionClasses {
		retentions = append(retentions, RetentionClassSource{
			SourceFile: path, SourceLine: retLines[i],
			Ref: r.Ref, DefaultPeriodDays: r.DefaultPeriodDays, JurisdictionOverrides: r.JurisdictionOverrides,
			TriggerEvent: r.TriggerEvent, DispositionOwner: r.DispositionOwner, AuthorityRef: r.AuthorityRef,
			Covered: r.Covered,
		})
	}
	return authorities, retentions, nil
}

type yamlProperty struct {
	Name              string `yaml:"name"`
	GoType            string `yaml:"go_type"`
	SchemaPath        string `yaml:"schema_path"`
	Presence          string `yaml:"presence"`
	Classification    string `yaml:"classification"`
	Temporal          string `yaml:"temporal"`
	AuthorityRef      string `yaml:"authority_ref"`
	Correction        string `yaml:"correction"`
	RetentionClassRef string `yaml:"retention_class_ref"`
	Status            string `yaml:"status"`
}

type yamlEntity struct {
	Name                string         `yaml:"name"`
	Version             int            `yaml:"version"`
	Key                 string         `yaml:"key"`
	Aliases             []string       `yaml:"aliases"`
	OwnerDomain         string         `yaml:"owner_domain"`
	Class               string         `yaml:"class"`
	Status              string         `yaml:"status"`
	Covered             bool           `yaml:"covered"`
	TenantScoped        bool           `yaml:"tenant_scoped"`
	LifecycleAssignment string         `yaml:"lifecycle_assignment"`
	CommandBoundary     string         `yaml:"command_boundary"`
	StreamKind          string         `yaml:"stream_kind"`
	Invariants          []string       `yaml:"invariants"`
	ChildRefs           []string       `yaml:"child_refs"`
	SourceRef           string         `yaml:"source_ref"`
	Summary             string         `yaml:"summary"`
	Properties          []yamlProperty `yaml:"properties"`
}

type yamlRelationship struct {
	Name         string `yaml:"name"`
	Version      int    `yaml:"version"`
	SourceEntity string `yaml:"source_entity"`
	TargetEntity string `yaml:"target_entity"`
	Cardinality  string `yaml:"cardinality"`
	Exclusive    bool   `yaml:"exclusive"`
	AllowCycles  bool   `yaml:"allow_cycles"`
	TenantScoped bool   `yaml:"tenant_scoped"`
	Covered      bool   `yaml:"covered"`
	SourceRef    string `yaml:"source_ref"`
}

type yamlFamilyFile struct {
	Family        string             `yaml:"family"`
	FamilyVersion int                `yaml:"family_version"`
	SourceDocs    []string           `yaml:"source_docs"`
	Entities      []yamlEntity       `yaml:"entities"`
	Relationships []yamlRelationship `yaml:"relationships"`
}

// LoadEntityFamilies parses every *.yaml file directly under dir (matching
// schema/schemaflux/entities/v1) into entity and relationship sources,
// sorted by file name for a deterministic, host-independent order.
func LoadEntityFamilies(dir string) ([]EntitySource, []RelationshipSource, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("sources: read %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".yaml" && filepath.Ext(e.Name()) != ".yml" {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	var entOut []EntitySource
	var relOut []RelationshipSource
	for _, name := range names {
		path := filepath.Join(dir, name)
		ents, rels, err := loadFamilyFile(path)
		if err != nil {
			return nil, nil, err
		}
		entOut = append(entOut, ents...)
		relOut = append(relOut, rels...)
	}
	return entOut, relOut, nil
}

func loadFamilyFile(path string) ([]EntitySource, []RelationshipSource, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("sources: read %s: %w", path, err)
	}
	var file yamlFamilyFile
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&file); err != nil {
		return nil, nil, fmt.Errorf("sources: parse %s: %w", path, err)
	}

	entLines, err := topLevelSequenceLines(raw, "entities")
	if err != nil {
		return nil, nil, fmt.Errorf("sources: locate entities lines in %s: %w", path, err)
	}
	if len(entLines) != len(file.Entities) {
		return nil, nil, fmt.Errorf("sources: %s: found %d entity line(s) but decoded %d entities",
			path, len(entLines), len(file.Entities))
	}
	relLines, err := topLevelSequenceLines(raw, "relationships")
	if err != nil {
		return nil, nil, fmt.Errorf("sources: locate relationships lines in %s: %w", path, err)
	}
	if len(relLines) != len(file.Relationships) {
		return nil, nil, fmt.Errorf("sources: %s: found %d relationship line(s) but decoded %d relationships",
			path, len(relLines), len(file.Relationships))
	}

	var ents []EntitySource
	for i, e := range file.Entities {
		var props []PropertySource
		for _, p := range e.Properties {
			props = append(props, PropertySource{
				Name: p.Name, GoType: p.GoType, SchemaPath: p.SchemaPath, Presence: p.Presence,
				Classification: p.Classification, Temporal: p.Temporal, AuthorityRef: p.AuthorityRef,
				Correction: p.Correction, RetentionClassRef: p.RetentionClassRef, Status: p.Status,
			})
		}
		ents = append(ents, EntitySource{
			SourceFile: path, SourceLine: entLines[i], Family: file.Family,
			Name: e.Name, Version: e.Version, Key: e.Key, Aliases: e.Aliases,
			OwnerDomain: e.OwnerDomain, Class: e.Class, Status: e.Status, Covered: e.Covered,
			TenantScoped: e.TenantScoped, LifecycleAssignment: e.LifecycleAssignment,
			CommandBoundary: e.CommandBoundary, StreamKind: e.StreamKind,
			Invariants: e.Invariants, ChildRefs: e.ChildRefs,
			SourceRef: e.SourceRef, Summary: e.Summary, Properties: props,
		})
	}
	var rels []RelationshipSource
	for i, r := range file.Relationships {
		rels = append(rels, RelationshipSource{
			SourceFile: path, SourceLine: relLines[i], Family: file.Family,
			Name: r.Name, Version: r.Version, SourceEntity: r.SourceEntity, TargetEntity: r.TargetEntity,
			Cardinality: r.Cardinality, Exclusive: r.Exclusive, AllowCycles: r.AllowCycles,
			TenantScoped: r.TenantScoped, Covered: r.Covered, SourceRef: r.SourceRef,
		})
	}
	return ents, rels, nil
}

// topLevelSequenceLines returns the 1-based source line of each item in the
// top-level key's sequence, in document order, by walking a [yaml.Node] tree
// (whose Line field the parser populates directly from the document) rather
// than scanning text. A key absent from the document (e.g. a family file
// with no relationships) yields a nil, non-error result.
func topLevelSequenceLines(raw []byte, key string) ([]int, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("empty document")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("document root is not a mapping")
	}
	var seq *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			seq = root.Content[i+1]
			break
		}
	}
	if seq == nil {
		return nil, nil
	}
	if seq.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("%q is not a sequence", key)
	}
	lines := make([]int, 0, len(seq.Content))
	for _, item := range seq.Content {
		lines = append(lines, item.Line)
	}
	return lines, nil
}
