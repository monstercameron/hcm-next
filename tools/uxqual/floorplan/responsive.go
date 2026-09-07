package floorplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// ResponsiveRegion is the effective, semantic layout for one floorplan
// region at a breakpoint. It intentionally contains no pixels, CSS, markup,
// content, or actions: those remain owned by the renderer and widget
// contracts. The region order is the floorplan's document order.
type ResponsiveRegion struct {
	// Region is copied from the registered floorplan; its name and semantic
	// kind are therefore not re-declared in a second vocabulary.
	Region  Region
	Stacked bool
}

// ResponsiveLayout is the immutable result of applying a floorplan's
// mobile-first rules at one semantic breakpoint. A rule at a breakpoint also
// applies to wider breakpoints until a later rule replaces it, matching the
// cascade without exposing media-query code in a PageDefinition.
type ResponsiveLayout struct {
	FloorplanID      string
	FloorplanVersion int
	Breakpoint       Breakpoint
	Regions          []ResponsiveRegion
}

// TransformAt applies the bounded responsive rules to a validated floorplan.
// It never mutates the floorplan or its slices. Every grid starts at its
// minimum column count before the mobile-first cascade is applied. This makes
// the narrow result the safe fallback at every width for which no rule exists,
// instead of accidentally restoring the floorplan's widest column count.
func (f Floorplan) TransformAt(b Breakpoint) (ResponsiveLayout, error) {
	if err := f.Validate(); err != nil {
		return ResponsiveLayout{}, err
	}
	if !b.Valid() {
		return ResponsiveLayout{}, fmt.Errorf("%w: %q", ErrInvalidFloorplan, b)
	}
	return f.transformAtValidated(b), nil
}

// TransformAll computes every renderer-owned breakpoint projection after one
// validation pass. Results always follow Breakpoints order, independent of
// declaration order in the source floorplan.
func (f Floorplan) TransformAll() ([]ResponsiveLayout, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	layouts := make([]ResponsiveLayout, 0, len(Breakpoints()))
	for _, breakpoint := range Breakpoints() {
		layouts = append(layouts, f.transformAtValidated(breakpoint))
	}
	return layouts, nil
}

func (f Floorplan) transformAtValidated(b Breakpoint) ResponsiveLayout {
	regions := make([]ResponsiveRegion, len(f.Regions))
	indexes := make(map[string]int, len(f.Regions))
	for i, region := range f.Regions {
		layout := region.Layout
		baseMaxColumns := layout.MaxColumns
		stacked := false
		if layout.Mode == LayoutGrid {
			// MinColumns is the safe, semantic narrow fallback. Keep the
			// constraint exact so consumers cannot accidentally re-expand it.
			layout.MaxColumns = layout.MinColumns
			stacked = layout.MinColumns == 1 && baseMaxColumns > 1
		}
		region.Layout = layout
		regions[i] = ResponsiveRegion{Region: region, Stacked: stacked}
		indexes[region.Name] = i
	}

	targetRank := breakpointRank(b)
	for _, rule := range sortedResponsiveRules(f.ResponsiveRules) {
		if breakpointRank(rule.Breakpoint) > targetRank {
			continue
		}
		i := indexes[rule.Region]
		region := &regions[i]
		applyResponsiveRule(region, rule)
	}

	return ResponsiveLayout{
		FloorplanID: f.ID, FloorplanVersion: f.Version, Breakpoint: b,
		Regions: regions,
	}
}

func sortedResponsiveRules(source []ResponsiveRule) []ResponsiveRule {
	rules := append([]ResponsiveRule(nil), source...)
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Breakpoint != rules[j].Breakpoint {
			return breakpointRank(rules[i].Breakpoint) < breakpointRank(rules[j].Breakpoint)
		}
		return rules[i].Region < rules[j].Region
	})
	return rules
}

func applyResponsiveRule(region *ResponsiveRegion, rule ResponsiveRule) {
	if rule.Mode != "" {
		region.Region.Layout.Mode = rule.Mode
		if rule.Mode == LayoutFlow {
			region.Region.Layout.MinColumns = 1
			region.Region.Layout.MaxColumns = 1
		}
	}
	if rule.Columns > 0 {
		region.Region.Layout.MinColumns = rule.Columns
		region.Region.Layout.MaxColumns = rule.Columns
	}
	if rule.Stacked {
		region.Region.Layout.Mode = LayoutGrid
		region.Region.Layout.MinColumns = 1
		region.Region.Layout.MaxColumns = 1
	}
	// Stacked is an explicit presentation decision. A later rule with
	// Stacked=false therefore restores the non-stacked state.
	region.Stacked = rule.Stacked
}

// Canonical returns deterministic bytes for an effective responsive layout.
func (l ResponsiveLayout) Canonical() []byte {
	b, _ := json.Marshal(struct {
		Schema           string
		SchemaVersion    int
		FloorplanID      string
		FloorplanVersion int
		Breakpoint       Breakpoint
		Regions          []ResponsiveRegion
	}{"hcmnext.uxqual.floorplan.ResponsiveLayout", 1, l.FloorplanID, l.FloorplanVersion, l.Breakpoint, l.Regions})
	return b
}

// Digest identifies the exact semantic responsive result without exposing
// any business or authorization data.
func (l ResponsiveLayout) Digest() string {
	sum := sha256.Sum256(l.Canonical())
	return "sha256:" + hex.EncodeToString(sum[:])
}
