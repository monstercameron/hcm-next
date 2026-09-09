package productslice

import (
	"fmt"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/intentmanifests"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/todogovernance"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/phaseonegate"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/widgetreg"
)

// Registries is the resolved set of every reference vocabulary a
// ProductSliceDefinition may name. Each field is built directly from the
// real, already-governed registry it names -- never a second
// hand-maintained list that could drift from it.
//
// [Validate] takes a Registries value rather than touching disk or
// generated code itself, mirroring internal/capability/binding's
// Build/BuildFrom split: the pure rule (Validate) can be proved against a
// controlled fixture, and [LoadLiveRegistries] is the one place this
// package's loader touches the real tree.
type Registries struct {
	// BusinessIntents holds every bound intent id
	// (definitions/governance/feature-intent-coverage.yaml's bound_intent,
	// INTENT-010), excluding the DEFERRED sentinel.
	BusinessIntents map[string]bool
	// Features holds every feature_id in the same coverage registry.
	Features map[string]bool
	// Pages holds every PageDefinition.PageID published by
	// tools/uxqual/pagedef (WEB-002).
	Pages map[string]bool
	// Widgets holds every "<id>@<version>" ref published by
	// tools/uxqual/widgetreg's governed registry (WEB-005).
	Widgets map[string]bool
	// Capabilities holds every capability id (ignoring version) published
	// by the internal/capability BOOTSTRAP registry.
	Capabilities map[string]bool
	// Packages holds every package's full module import path reachable
	// from cmd/hcmnext today -- the live Phase 1 production closure
	// (tools/policy/phaseonegate, ARCH-GO-018).
	Packages map[string]bool
	// Todos holds every todo id in definitions/planning/todo-registry.json.
	Todos map[string]bool
}

// LoadCapabilityIDs returns every capability id the compiled-in BOOTSTRAP
// registry publishes, deduplicated across versions: a ProductSliceDefinition
// names a capability "by id" (types.go), not by exact version.
func LoadCapabilityIDs() (map[string]bool, error) {
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		return nil, fmt.Errorf("productslice: building the capability BOOTSTRAP registry: %w", err)
	}
	out := make(map[string]bool)
	for _, rec := range registry.List() {
		out[rec.Definition.ID] = true
	}
	return out, nil
}

// LoadCoverageRegistry reads and validates the governed feature/intent
// coverage registry (definitions/governance/feature-intent-coverage.yaml,
// INTENT-010) rooted at root.
func LoadCoverageRegistry(root string) (intentmanifests.FeatureIntentCoverageRegistry, error) {
	path := filepath.Join(root, "definitions", "governance", "feature-intent-coverage.yaml")
	registry, err := intentmanifests.LoadFeatureIntentCoverageYAML(path)
	if err != nil {
		return intentmanifests.FeatureIntentCoverageRegistry{}, fmt.Errorf("productslice: loading feature/intent coverage registry %s: %w", path, err)
	}
	return registry, nil
}

// FeatureIDs projects every feature_id out of a loaded coverage registry.
func FeatureIDs(reg intentmanifests.FeatureIntentCoverageRegistry) map[string]bool {
	out := make(map[string]bool, len(reg.Features))
	for _, f := range reg.Features {
		out[f.FeatureID] = true
	}
	return out
}

// BusinessIntentIDs projects every distinct bound intent id out of a loaded
// coverage registry, excluding the registry's DEFERRED sentinel (a feature
// with no drafted intent yet names no business intent a slice can deliver).
func BusinessIntentIDs(reg intentmanifests.FeatureIntentCoverageRegistry) map[string]bool {
	out := make(map[string]bool)
	for _, f := range reg.Features {
		if f.BoundIntentID != "" && f.BoundIntentID != intentmanifests.DeferredIntentBinding {
			out[f.BoundIntentID] = true
		}
	}
	return out
}

// PageIDs returns every PageDefinition.PageID tools/uxqual/pagedef
// currently publishes. pagedef has no separate registry type (unlike
// floorplan.Registry / widgetreg.Registry): its published pages are exactly
// the PageDefinition-returning functions it exports today.
func PageIDs() map[string]bool {
	return map[string]bool{
		pagedef.PromotionListPageDefinition().PageID:   true,
		pagedef.PromotionDetailPageDefinition().PageID: true,
	}
}

// WidgetRefs returns every "<id>@<version>" ref the governed Promotion
// widget registry (tools/uxqual/widgetreg) currently publishes.
func WidgetRefs() map[string]bool {
	out := make(map[string]bool)
	for _, ref := range widgetreg.PromotionRegistry().Refs() {
		out[ref] = true
	}
	return out
}

// LoadPackageAllowlist computes the live Phase 1 production package closure
// of cmd/hcmnext (tools/policy/phaseonegate, ARCH-GO-018) rooted at root.
// This runs `go list -deps -json` against the real tree -- the same
// mechanism ARCH-GO-018's own gate uses -- rather than reading a possibly
// stale committed fixture, so a renamed or removed package is caught
// immediately instead of only when someone happens to regenerate a golden
// file.
func LoadPackageAllowlist(root string) (map[string]bool, error) {
	graph, err := phaseonegate.BuildProductionGraph(root, phaseonegate.EntryPoint)
	if err != nil {
		return nil, fmt.Errorf("productslice: building the live Phase 1 production graph: %w", err)
	}
	out := make(map[string]bool, len(graph.Packages))
	for _, pkg := range graph.Packages {
		out[pkg.Path] = true
	}
	return out, nil
}

// LoadTodoIDs reads every todo id out of the compiled todo registry
// (definitions/planning/todo-registry.json) rooted at root.
func LoadTodoIDs(root string) (map[string]bool, error) {
	path := filepath.Join(root, "definitions", "planning", "todo-registry.json")
	todos, err := todogovernance.LoadRegistry(path)
	if err != nil {
		return nil, fmt.Errorf("productslice: loading todo registry %s: %w", path, err)
	}
	out := make(map[string]bool, len(todos))
	for _, t := range todos {
		out[t.ID] = true
	}
	return out, nil
}

// LoadLiveRegistries builds a Registries snapshot against the real
// repository at root: the one place ALIGN-001's loader touches disk,
// generated code and `go list` together. Validate itself never calls this;
// callers (the generator command and this package's conformance tests) call
// it once and pass the result to
// [ProductSliceDefinition.Validate]/[Registry.Validate].
func LoadLiveRegistries(root string) (Registries, error) {
	capIDs, err := LoadCapabilityIDs()
	if err != nil {
		return Registries{}, err
	}
	coverage, err := LoadCoverageRegistry(root)
	if err != nil {
		return Registries{}, err
	}
	packages, err := LoadPackageAllowlist(root)
	if err != nil {
		return Registries{}, err
	}
	todos, err := LoadTodoIDs(root)
	if err != nil {
		return Registries{}, err
	}
	return Registries{
		BusinessIntents: BusinessIntentIDs(coverage),
		Features:        FeatureIDs(coverage),
		Pages:           PageIDs(),
		Widgets:         WidgetRefs(),
		Capabilities:    capIDs,
		Packages:        packages,
		Todos:           todos,
	}, nil
}
