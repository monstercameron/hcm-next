// Package floorplan defines the versioned, renderer-independent layout
// contract consumed by tools/uxqual/pagedef. A floorplan describes semantic
// regions and bounded responsive choices; it never contains CSS, markup,
// coordinates, or executable customer content.
package floorplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/hcm-next/tools/uxqual/pagedef"
)

var (
	ErrInvalidFloorplan = errors.New("floorplan: invalid floorplan")
	ErrUnknownReference = errors.New("floorplan: unknown reference")
	ErrUnknownRegion    = errors.New("floorplan: page region is not declared")
)

// Breakpoint is a closed responsive vocabulary. The names are semantic
// breakpoints, not media-query expressions.
type Breakpoint string

const (
	BreakpointNarrow   Breakpoint = "narrow"
	BreakpointCompact  Breakpoint = "compact"
	BreakpointStandard Breakpoint = "standard"
	BreakpointWide     Breakpoint = "wide"

	// Friendly aliases used by callers that describe surfaces by device class.
	BreakpointMobile  = BreakpointNarrow
	BreakpointTablet  = BreakpointCompact
	BreakpointDesktop = BreakpointStandard
)

func Breakpoints() []Breakpoint {
	return []Breakpoint{BreakpointNarrow, BreakpointCompact, BreakpointStandard, BreakpointWide}
}

func (b Breakpoint) Valid() bool {
	for _, known := range Breakpoints() {
		if b == known {
			return true
		}
	}
	return false
}

// LayoutMode names the two layout primitives admitted by the frontend plan.
type LayoutMode string

const (
	LayoutFlow LayoutMode = "flow"
	LayoutGrid LayoutMode = "grid"
)

// LayoutConstraint is a bounded semantic layout constraint. Gap is a token
// reference such as "space.2", never a CSS length.
type LayoutConstraint struct {
	Mode       LayoutMode
	MinColumns int
	MaxColumns int
	Gap        string
}

// Region is a named semantic region in the floorplan. Kind is the closed
// vocabulary owned by pagedef; this package intentionally does not duplicate
// that vocabulary.
type Region struct {
	Name   string
	Kind   pagedef.RegionKind
	Layout LayoutConstraint
}

// ResponsiveRule changes one region's bounded layout at a declared
// breakpoint. A zero Mode means the base region mode is retained.
type ResponsiveRule struct {
	Breakpoint Breakpoint
	Region     string
	Mode       LayoutMode
	Columns    int
	Stacked    bool
}

// Floorplan is an immutable-by-convention versioned layout description. The
// registry copies all slices on admission and on lookup so callers cannot
// mutate the registered value through shared backing arrays.
type Floorplan struct {
	ID              string
	Version         int
	Breakpoints     []Breakpoint
	Regions         []Region
	ResponsiveRules []ResponsiveRule
}

func (f Floorplan) Validate() error {
	if strings.TrimSpace(f.ID) == "" || f.Version < 1 {
		return fmt.Errorf("%w: id and positive version are required", ErrInvalidFloorplan)
	}
	if len(f.Breakpoints) == 0 {
		return fmt.Errorf("%w: at least one breakpoint is required", ErrInvalidFloorplan)
	}
	seenBreakpoints := make(map[Breakpoint]bool, len(f.Breakpoints))
	for _, b := range f.Breakpoints {
		if !b.Valid() {
			return fmt.Errorf("%w: breakpoint %q is not in the closed vocabulary", ErrInvalidFloorplan, b)
		}
		if seenBreakpoints[b] {
			return fmt.Errorf("%w: breakpoint %q is declared twice", ErrInvalidFloorplan, b)
		}
		seenBreakpoints[b] = true
	}
	if len(f.Regions) == 0 {
		return fmt.Errorf("%w: at least one region is required", ErrInvalidFloorplan)
	}
	regions := make(map[string]bool, len(f.Regions))
	for _, r := range f.Regions {
		if strings.TrimSpace(r.Name) == "" {
			return fmt.Errorf("%w: region name is required", ErrInvalidFloorplan)
		}
		if strings.ContainsAny(r.Name, "<>\r\n") {
			return fmt.Errorf("%w: region name %q contains forbidden markup or line breaks", ErrInvalidFloorplan, r.Name)
		}
		if regions[r.Name] {
			return fmt.Errorf("%w: duplicate region name %q", ErrInvalidFloorplan, r.Name)
		}
		regions[r.Name] = true
		if pagedef.RegionKindDoc(r.Kind) == "" {
			return fmt.Errorf("%w: region %q has unknown kind %q", ErrInvalidFloorplan, r.Name, r.Kind)
		}
		if err := validateLayout(r.Layout); err != nil {
			return fmt.Errorf("%w: region %q layout: %v", ErrInvalidFloorplan, r.Name, err)
		}
	}
	seenRules := make(map[string]bool, len(f.ResponsiveRules))
	for _, rule := range f.ResponsiveRules {
		if !seenBreakpoints[rule.Breakpoint] {
			return fmt.Errorf("%w: rule names undeclared breakpoint %q", ErrInvalidFloorplan, rule.Breakpoint)
		}
		if !regions[rule.Region] {
			return fmt.Errorf("%w: breakpoint rule names undeclared region %q", ErrUnknownRegion, rule.Region)
		}
		if rule.Mode != "" && rule.Mode != LayoutFlow && rule.Mode != LayoutGrid {
			return fmt.Errorf("%w: responsive rule has unknown mode %q", ErrInvalidFloorplan, rule.Mode)
		}
		if rule.Columns < 0 || rule.Columns > 12 {
			return fmt.Errorf("%w: responsive rule columns must be between 0 and 12", ErrInvalidFloorplan)
		}
		if rule.Stacked && rule.Columns > 1 {
			return fmt.Errorf("%w: stacked responsive rule cannot declare multiple columns", ErrInvalidFloorplan)
		}
		if rule.Mode == LayoutFlow && (rule.Columns > 1 || rule.Stacked) {
			return fmt.Errorf("%w: flow responsive rule cannot declare multiple columns or stacking", ErrInvalidFloorplan)
		}
		key := string(rule.Breakpoint) + "\x00" + rule.Region
		if seenRules[key] {
			return fmt.Errorf("%w: duplicate responsive rule for %q at %q", ErrInvalidFloorplan, rule.Region, rule.Breakpoint)
		}
		seenRules[key] = true
	}
	// Validate the cumulative cascade as well as each rule in isolation. A
	// columns-only rule applied to a flow region, for example, must not create
	// an effective multi-column flow layout even though both fields are valid
	// separately.
	effective := make(map[string]ResponsiveRegion, len(f.Regions))
	for _, region := range f.Regions {
		if region.Layout.Mode == LayoutGrid {
			region.Layout.MaxColumns = region.Layout.MinColumns
		}
		effective[region.Name] = ResponsiveRegion{Region: region}
	}
	for _, rule := range sortedResponsiveRules(f.ResponsiveRules) {
		region := effective[rule.Region]
		applyResponsiveRule(&region, rule)
		if err := validateLayout(region.Region.Layout); err != nil {
			return fmt.Errorf("%w: responsive result for region %q at %q: %v", ErrInvalidFloorplan, region.Region.Name, rule.Breakpoint, err)
		}
		effective[rule.Region] = region
	}
	return nil
}

func validateLayout(l LayoutConstraint) error {
	if l.Mode != LayoutFlow && l.Mode != LayoutGrid {
		return fmt.Errorf("mode must be flow or grid")
	}
	if l.MinColumns < 1 || l.MaxColumns < l.MinColumns || l.MaxColumns > 12 {
		return fmt.Errorf("column bounds must be 1..12 with max >= min")
	}
	if l.Mode == LayoutFlow && (l.MinColumns != 1 || l.MaxColumns != 1) {
		return fmt.Errorf("flow layout must have one column")
	}
	if l.Gap == "" || !strings.HasPrefix(l.Gap, "space.") {
		return fmt.Errorf("gap must be a semantic space token")
	}
	return nil
}

// Canonical returns deterministic JSON for a validated floorplan. Declared
// region order is preserved because it is document order; breakpoint and
// responsive-rule order is normalized because it is not presentation order.
func (f Floorplan) Canonical() []byte {
	c := cloneFloorplan(f)
	sort.Slice(c.Breakpoints, func(i, j int) bool { return breakpointRank(c.Breakpoints[i]) < breakpointRank(c.Breakpoints[j]) })
	sort.Slice(c.ResponsiveRules, func(i, j int) bool {
		if c.ResponsiveRules[i].Breakpoint != c.ResponsiveRules[j].Breakpoint {
			return breakpointRank(c.ResponsiveRules[i].Breakpoint) < breakpointRank(c.ResponsiveRules[j].Breakpoint)
		}
		return c.ResponsiveRules[i].Region < c.ResponsiveRules[j].Region
	})
	b, _ := json.Marshal(struct {
		Schema          string
		SchemaVersion   int
		ID              string
		Version         int
		Breakpoints     []Breakpoint
		Regions         []Region
		ResponsiveRules []ResponsiveRule
	}{"hcmnext.uxqual.floorplan.Floorplan", 1, c.ID, c.Version, c.Breakpoints, c.Regions, c.ResponsiveRules})
	return b
}

func (f Floorplan) Digest() string {
	sum := sha256.Sum256(f.Canonical())
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (f Floorplan) Explain() string {
	return fmt.Sprintf("floorplan %s@%d (%d regions, %d breakpoints; %s)", f.ID, f.Version, len(f.Regions), len(f.Breakpoints), f.Digest())
}

type Registry struct {
	floorplans map[string]Floorplan
}

func NewRegistry(floorplans ...Floorplan) (*Registry, error) {
	r := &Registry{floorplans: make(map[string]Floorplan, len(floorplans))}
	for _, f := range floorplans {
		if err := f.Validate(); err != nil {
			return nil, err
		}
		key := registryKey(f.ID, f.Version)
		if _, exists := r.floorplans[key]; exists {
			return nil, fmt.Errorf("%w: duplicate %s", ErrInvalidFloorplan, key)
		}
		r.floorplans[key] = cloneFloorplan(f)
	}
	return r, nil
}

// Validate rechecks every registered floorplan at an explicit publication
// boundary without exposing the registry's internal map.
func (r *Registry) Validate() error {
	if r == nil || len(r.floorplans) == 0 {
		return fmt.Errorf("%w: registry is empty", ErrInvalidFloorplan)
	}
	for key, f := range r.floorplans {
		if err := f.Validate(); err != nil {
			return fmt.Errorf("%w: registry entry %s: %v", ErrInvalidFloorplan, key, err)
		}
	}
	return nil
}

// Lookup accepts id@version, id.v<version>, or an id plus a version argument.
func (r *Registry) Lookup(ref string, version ...int) (Floorplan, bool) {
	if r == nil {
		return Floorplan{}, false
	}
	key, ok := parseReference(ref, version...)
	if !ok {
		return Floorplan{}, false
	}
	f, ok := r.floorplans[key]
	if !ok {
		return Floorplan{}, false
	}
	return cloneFloorplan(f), true
}

type Resolution struct {
	Page      pagedef.PageDefinition
	Floorplan Floorplan
}

func (r *Registry) Resolve(page pagedef.PageDefinition) (Resolution, error) {
	if r == nil {
		return Resolution{}, ErrUnknownReference
	}
	if violations := page.Validate(); len(violations) != 0 {
		return Resolution{}, fmt.Errorf("%w: page definition is invalid: %s", ErrInvalidFloorplan, violations[0])
	}
	f, ok := r.Lookup(page.FloorplanRef)
	if !ok {
		return Resolution{}, fmt.Errorf("%w: %q", ErrUnknownReference, page.FloorplanRef)
	}
	declared := make(map[string]bool, len(f.Regions))
	for _, region := range f.Regions {
		declared[region.Name] = true
	}
	for _, region := range page.Regions {
		if !declared[region.ID] {
			return Resolution{}, fmt.Errorf("%w: page region %q is not in %s@%d", ErrUnknownRegion, region.ID, f.ID, f.Version)
		}
	}
	return Resolution{Page: page, Floorplan: f}, nil
}

// PromotionRegistry is the Gate A registry for the two Promotion pages
// published by pagedef: the list page uses launch and the detail page uses
// intent_workspace.
func PromotionRegistry() *Registry {
	r, err := NewRegistry(promotionLaunch(), promotionIntentWorkspace())
	if err != nil {
		panic(err)
	}
	return r
}

func promotionLaunch() Floorplan {
	return Floorplan{
		ID: "floorplan.launch", Version: 1,
		Breakpoints: Breakpoints(),
		Regions: []Region{
			{Name: "shell", Kind: pagedef.RegionShell, Layout: flowLayout()},
			{Name: "page-identity", Kind: pagedef.RegionPageIdentity, Layout: flowLayout()},
			{Name: "workforce", Kind: pagedef.RegionPrimary, Layout: gridLayout(1, 2)},
			{Name: "journeys", Kind: pagedef.RegionSupporting, Layout: gridLayout(1, 2)},
		},
		ResponsiveRules: []ResponsiveRule{
			{Breakpoint: BreakpointCompact, Region: "workforce", Mode: LayoutGrid, Columns: 1},
			{Breakpoint: BreakpointStandard, Region: "workforce", Mode: LayoutGrid, Columns: 2},
			{Breakpoint: BreakpointWide, Region: "workforce", Mode: LayoutGrid, Columns: 3},
			{Breakpoint: BreakpointCompact, Region: "journeys", Mode: LayoutGrid, Columns: 1},
			{Breakpoint: BreakpointStandard, Region: "journeys", Mode: LayoutGrid, Columns: 2},
			{Breakpoint: BreakpointWide, Region: "journeys", Mode: LayoutGrid, Columns: 3},
		},
	}
}

func promotionIntentWorkspace() Floorplan {
	return Floorplan{
		ID: "floorplan.intent_workspace", Version: 1,
		Breakpoints: Breakpoints(),
		Regions: []Region{
			{Name: "shell", Kind: pagedef.RegionShell, Layout: flowLayout()},
			{Name: "page-identity", Kind: pagedef.RegionPageIdentity, Layout: flowLayout()},
			{Name: "steps", Kind: pagedef.RegionLocalNavigation, Layout: flowLayout()},
			{Name: "comparison", Kind: pagedef.RegionPrimary, Layout: gridLayout(1, 2)},
			{Name: "engine", Kind: pagedef.RegionSupporting, Layout: gridLayout(1, 2)},
			{Name: "decision", Kind: pagedef.RegionCompletion, Layout: flowLayout()},
		},
		ResponsiveRules: []ResponsiveRule{
			{Breakpoint: BreakpointCompact, Region: "comparison", Mode: LayoutGrid, Columns: 1, Stacked: true},
			{Breakpoint: BreakpointStandard, Region: "comparison", Mode: LayoutGrid, Columns: 2},
			{Breakpoint: BreakpointWide, Region: "comparison", Mode: LayoutGrid, Columns: 3},
			{Breakpoint: BreakpointCompact, Region: "engine", Mode: LayoutGrid, Columns: 1, Stacked: true},
			{Breakpoint: BreakpointStandard, Region: "engine", Mode: LayoutGrid, Columns: 2},
			{Breakpoint: BreakpointWide, Region: "engine", Mode: LayoutGrid, Columns: 3},
		},
	}
}

func flowLayout() LayoutConstraint {
	return LayoutConstraint{Mode: LayoutFlow, MinColumns: 1, MaxColumns: 1, Gap: "space.2"}
}
func gridLayout(min, max int) LayoutConstraint {
	return LayoutConstraint{Mode: LayoutGrid, MinColumns: min, MaxColumns: max, Gap: "space.2"}
}

func registryKey(id string, version int) string { return id + "@" + strconv.Itoa(version) }

func parseReference(ref string, version ...int) (string, bool) {
	if len(version) > 1 {
		return "", false
	}
	ref = strings.TrimSpace(ref)
	if len(version) == 1 {
		if version[0] < 1 || ref == "" {
			return "", false
		}
		if at := strings.IndexByte(ref, '@'); at >= 0 {
			ref = ref[:at]
		}
		return registryKey(ref, version[0]), true
	}
	if at := strings.LastIndexByte(ref, '@'); at > 0 {
		v, err := strconv.Atoi(ref[at+1:])
		if err != nil || v < 1 {
			return "", false
		}
		return registryKey(ref[:at], v), true
	}
	marker := strings.LastIndex(ref, ".v")
	if marker <= 0 {
		return "", false
	}
	v, err := strconv.Atoi(ref[marker+2:])
	if err != nil || v < 1 {
		return "", false
	}
	return registryKey(ref[:marker], v), true
}

func breakpointRank(b Breakpoint) int {
	for i, known := range Breakpoints() {
		if b == known {
			return i
		}
	}
	return len(Breakpoints())
}

func cloneFloorplan(f Floorplan) Floorplan {
	f.Breakpoints = append([]Breakpoint(nil), f.Breakpoints...)
	f.Regions = append([]Region(nil), f.Regions...)
	f.ResponsiveRules = append([]ResponsiveRule(nil), f.ResponsiveRules...)
	return f
}
