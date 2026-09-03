package schemaflux

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// yamlDefinition mirrors one entry of the "definitions" list in a
// schema/schemaflux/business_intents/v1/*.yaml source file (see that
// directory's README.md field table). Field names are the literal YAML keys.
type yamlDefinition struct {
	CatalogNumber int    `yaml:"catalog_number"`
	IntentTypeID  string `yaml:"intent_type_id"`
	Version       uint32 `yaml:"version"`
	DisplayName   string `yaml:"display_name"`
	Description   string `yaml:"description"`
	OwnerPlane    string `yaml:"owner_plane"`
	OwnerDomain   string `yaml:"owner_domain"`
	KernelFamily  string `yaml:"kernel_family"`
	Maturity      string `yaml:"maturity"`

	InputSchemaRef  string `yaml:"input_schema_ref"`
	ResultSchemaRef string `yaml:"result_schema_ref"`

	AllowedInitiators       []string `yaml:"allowed_initiators"`
	AllowedExecutionModes   []string `yaml:"allowed_execution_modes"`
	SideEffectProfile       string   `yaml:"side_effect_profile"`
	RiskClass               string   `yaml:"risk_class"`
	DataClassificationFloor string   `yaml:"data_classification_floor"`
	SubjectKinds            []string `yaml:"subject_kinds"`

	RequiredCapabilities   []string `yaml:"required_capabilities"`
	GovernanceRequirements []string `yaml:"governance_requirements"`
	Preconditions          []string `yaml:"preconditions"`
	Invariants             []string `yaml:"invariants"`

	IdempotencyScope      string `yaml:"idempotency_scope"`
	ConflictFootprintRule string `yaml:"conflict_footprint_rule"`
	ProposalBindingRule   string `yaml:"proposal_binding_rule"`
	RevalidationRule      string `yaml:"revalidation_rule"`
	CancellationRule      string `yaml:"cancellation_rule"`
	CompensationRule      string `yaml:"compensation_rule"`
	EvidenceRule          string `yaml:"evidence_rule"`
	RetentionClass        string `yaml:"retention_class"`
	OutcomeContract       string `yaml:"outcome_contract"`
	SLOClass              string `yaml:"slo_class"`
	AvailabilityPolicy    string `yaml:"availability_policy"`
	PhaseDepth            string `yaml:"phase_depth"`

	// PopulationScopeRef is read but not yet modeled in Definition: none of
	// the fourteen source definitions set it today.
	PopulationScopeRef string `yaml:"population_scope_ref"`
}

// yamlFile mirrors one schema/schemaflux/business_intents/v1/*.yaml source
// file's top level.
type yamlFile struct {
	Catalog        string           `yaml:"catalog"`
	CatalogVersion int              `yaml:"catalog_version"`
	SourceContract string           `yaml:"source_contract"`
	Definitions    []yamlDefinition `yaml:"definitions"`
}

// LoadDefinitions parses every *.yaml file directly under dir (matching
// schema/schemaflux/business_intents/v1) into [Definition] values, sorted by
// file name for a deterministic, host-independent order. It uses strict
// (KnownFields) decoding: an unrecognized YAML key is a load error, matching
// the source README's "no defaulting" rule — a silently-ignored typo in a
// field name must never compile as if the field were absent.
func LoadDefinitions(dir string) ([]Definition, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("schemaflux: read %s: %w", dir, err)
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

	var out []Definition
	for _, name := range names {
		path := filepath.Join(dir, name)
		defs, err := loadDefinitionFile(path)
		if err != nil {
			return nil, err
		}
		out = append(out, defs...)
	}
	return out, nil
}

// loadDefinitionFile loads and line-annotates every definition in one source
// file. It decodes the document twice on purpose: once into typed structs for
// the field values, and once into a [yaml.Node] tree solely to recover the
// 1-based source line of each definition's first key, so an
// [UnresolvedReferenceError] can cite a line a human can jump to. Sequence and
// struct-field order are both stable, so zipping the two decodes by index is
// exact.
func loadDefinitionFile(path string) ([]Definition, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("schemaflux: read %s: %w", path, err)
	}

	var file yamlFile
	strict := yaml.NewDecoder(bytes.NewReader(raw))
	strict.KnownFields(true)
	if err := strict.Decode(&file); err != nil {
		return nil, fmt.Errorf("schemaflux: parse %s: %w", path, err)
	}

	lines, err := definitionLines(raw)
	if err != nil {
		return nil, fmt.Errorf("schemaflux: locate definition lines in %s: %w", path, err)
	}
	if len(lines) != len(file.Definitions) {
		return nil, fmt.Errorf(
			"schemaflux: %s: found %d definition line(s) but decoded %d definition(s)",
			path, len(lines), len(file.Definitions))
	}

	out := make([]Definition, 0, len(file.Definitions))
	for i, d := range file.Definitions {
		out = append(out, Definition{
			SourceFile:              path,
			SourceLine:              lines[i],
			CatalogNumber:           d.CatalogNumber,
			IntentTypeID:            d.IntentTypeID,
			Version:                 d.Version,
			DisplayName:             d.DisplayName,
			Description:             d.Description,
			OwnerPlane:              d.OwnerPlane,
			OwnerDomain:             d.OwnerDomain,
			KernelFamily:            d.KernelFamily,
			Maturity:                d.Maturity,
			InputSchemaRef:          d.InputSchemaRef,
			ResultSchemaRef:         d.ResultSchemaRef,
			AllowedInitiators:       d.AllowedInitiators,
			AllowedExecutionModes:   d.AllowedExecutionModes,
			SideEffectProfile:       d.SideEffectProfile,
			RiskClass:               d.RiskClass,
			DataClassificationFloor: d.DataClassificationFloor,
			SubjectKinds:            d.SubjectKinds,
			RequiredCapabilities:    d.RequiredCapabilities,
			GovernanceRequirements:  d.GovernanceRequirements,
			Preconditions:           d.Preconditions,
			Invariants:              d.Invariants,
			IdempotencyScope:        d.IdempotencyScope,
			ConflictFootprintRule:   d.ConflictFootprintRule,
			ProposalBindingRule:     d.ProposalBindingRule,
			RevalidationRule:        d.RevalidationRule,
			CancellationRule:        d.CancellationRule,
			CompensationRule:        d.CompensationRule,
			EvidenceRule:            d.EvidenceRule,
			RetentionClass:          d.RetentionClass,
			OutcomeContract:         d.OutcomeContract,
			SLOClass:                d.SLOClass,
			AvailabilityPolicy:      d.AvailabilityPolicy,
			PhaseDepth:              d.PhaseDepth,
		})
	}
	return out, nil
}

// definitionLines returns the 1-based source line of each item in the
// top-level "definitions:" sequence, in document order, by walking a
// [yaml.Node] tree rather than scanning text: a Node's Line is populated by
// the parser directly from the document, so it is exact regardless of
// comments, blank lines or list-item indentation style.
func definitionLines(raw []byte) ([]int, error) {
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
		key := root.Content[i]
		if key.Value == "definitions" {
			seq = root.Content[i+1]
			break
		}
	}
	if seq == nil {
		return nil, fmt.Errorf("no top-level 'definitions' key")
	}
	if seq.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("'definitions' is not a sequence")
	}

	lines := make([]int, 0, len(seq.Content))
	for _, item := range seq.Content {
		lines = append(lines, item.Line)
	}
	return lines, nil
}
