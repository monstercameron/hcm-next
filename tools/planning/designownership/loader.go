// Live snapshot loading for the design ownership register: accepted
// definitions and validated design records shared by import with
// tools/planning/workflowdesignjoin/WF-DISC-006, descriptor dimensions
// shared with tools/planning/intentcoverage/GOV-026, engine owners from
// the internal/engines inventory with the Version and Explain contract,
// and entity owners from the generated model sources. Capability
// invocation binding rides BIND-001 and is not re-derived here.
package designownership

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/intentmanifests"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesign"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesignjoin"
)

// LoadSnapshot reads the live ownership inputs below root. It fails
// closed on unreadable sources; missing contracts become candidates at
// compile time, never loader errors.
func LoadSnapshot(root string) (Snapshot, []string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("resolve repository root: %w", err)
	}
	var snap Snapshot
	snap.EngineOwners = make(map[string]EngineOwner)
	snap.EntityOwners = make(map[string]EntityOwner)
	snap.DescriptorEntities = make(map[string][]string)
	snap.DescriptorProperties = make(map[string][]string)
	snap.DescriptorAuthorities = make(map[string][]string)
	snap.DefinitionPhase = make(map[string]string)

	accepted, records, err := workflowdesignjoin.LoadSnapshot(root)
	if err != nil {
		return Snapshot{}, nil, err
	}
	known := make(map[string]bool, len(accepted))
	for _, id := range accepted {
		known[id] = true
	}

	descriptors, err := intentmanifests.LoadIntentManifestYAML(filepath.Join(root, "definitions", "governance", "intent-conformance-descriptors.yaml"))
	if err != nil {
		return Snapshot{}, nil, err
	}
	if err := intentmanifests.ValidateIntentManifestYAML(descriptors); err != nil {
		return Snapshot{}, nil, fmt.Errorf("accepted intent catalog failed validation: %w", err)
	}
	for _, d := range descriptors {
		id := fmt.Sprintf("%s/v%d", d.IntentTypeID, d.Version)
		if !known[id] {
			continue
		}
		snap.DefinitionPhase[id] = d.Phase
		snap.DescriptorEntities[id] = cleanStrings(d.Entities)
		snap.DescriptorProperties[id] = cleanStrings(d.Properties)
		snap.DescriptorAuthorities[id] = cleanStrings(d.Authority)
	}
	for _, record := range records {
		if !known[record.Definition] {
			continue
		}
		snap.Designs = append(snap.Designs, DesignRef{
			Definition: record.Definition,
			Intent:     record.Intent,
			Engines:    dimensionNames(record.Engines),
			Phase:      snap.DefinitionPhase[record.Definition],
		})
	}

	enginesRoot := filepath.Join(root, "internal", "engines")
	entries, err := os.ReadDir(enginesRoot)
	if err != nil {
		return Snapshot{}, nil, err
	}
	inventory := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() {
			inventory[entry.Name()] = true
		}
	}
	seenEngine := make(map[string]bool)
	for _, design := range snap.Designs {
		for _, engine := range design.Engines {
			if seenEngine[engine] {
				continue
			}
			seenEngine[engine] = true
			if !inventory[engine] {
				continue
			}
			hasVersion, hasExplain, err := scanEngineContract(filepath.Join(enginesRoot, engine))
			if err != nil {
				return Snapshot{}, nil, fmt.Errorf("scan engine package %s: %w", engine, err)
			}
			snap.EngineOwners[engine] = EngineOwner{Engine: engine, Package: "internal/engines/" + engine, HasVersion: hasVersion, HasExplain: hasExplain}
		}
	}

	sources, err := loadModelSources(filepath.Join(root, "definitions", "generation", "model-sources.yaml"))
	if err != nil {
		return Snapshot{}, nil, err
	}
	for _, entity := range sources {
		for _, name := range []string{entity.RefBase, entity.Key} {
			if name == "" {
				continue
			}
			if _, dup := snap.EntityOwners[name]; !dup {
				snap.EntityOwners[name] = EntityOwner{Entity: name, Owner: entity.Owner, Source: "model-sources.yaml#" + entity.Ref}
			}
		}
	}
	return snap, accepted, nil
}

// dimensionNames flattens an execution dimension to its declared names,
// dropping reasoned NOT_APPLICABLE markers.
func dimensionNames(dimension workflowdesign.Dimension) []string {
	if dimension.Value == workflowdesign.NotApplicable || strings.TrimSpace(dimension.Value) == "" {
		return cleanStrings(dimension.Items)
	}
	return []string{dimension.Value}
}

func cleanStrings(values []string) []string {
	var out []string
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	sort.Strings(out)
	return out
}

// scanEngineContract reports whether a package declares the Version and
// Explain contract. It mirrors the enforced engine contract: Version
// must be a plain function while Explain may be a method, exactly as
// tools/policy/enginecoverage/ENGINE-COVERAGE-001 checks.
func scanEngineContract(dir string) (hasVersion, hasExplain bool, err error) {
	set := token.NewFileSet()
	// parser.ParseDir is deprecated; a ReadDir plus ParseFile loop collects
	// the same file set (build-tag precision is irrelevant here).
	var files []*ast.File
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, false, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(set, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return false, false, err
		}
		files = append(files, file)
	}
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Name.Name == "Version" && fn.Recv == nil {
				hasVersion = true
			}
			if fn.Name.Name == "Explain" {
				hasExplain = true
			}
		}
	}
	return hasVersion, hasExplain, nil
}

type modelSourceEntity struct {
	RefBase string
	Key     string
	Ref     string
	Owner   string
}

func loadModelSources(path string) ([]modelSourceEntity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var document struct {
		Entities []struct {
			Ref         string `yaml:"ref"`
			Key         string `yaml:"key"`
			OwnerDomain string `yaml:"owner_domain"`
		} `yaml:"entities"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse model sources %s: %w", path, err)
	}
	var out []modelSourceEntity
	for _, entity := range document.Entities {
		base := entity.Ref
		if i := strings.Index(base, "/"); i >= 0 {
			base = base[:i]
		}
		out = append(out, modelSourceEntity{RefBase: base, Key: entity.Key, Ref: entity.Ref, Owner: entity.OwnerDomain})
	}
	return out, nil
}
