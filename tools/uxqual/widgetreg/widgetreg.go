// Package widgetreg defines WEB-005's renderer-independent, governed widget
// registry. A widget is admitted by an immutable id/version contract; its
// renderer implementation remains owned by the rendering package.
package widgetreg

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

var (
	ErrInvalidWidget       = errors.New("widgetreg: invalid widget definition")
	ErrDuplicateWidget     = errors.New("widgetreg: duplicate widget")
	ErrUnknownWidget       = errors.New("widgetreg: unknown widget")
	ErrUnpublishedWidget   = errors.New("widgetreg: widget is not published")
	ErrInvalidTransition   = errors.New("widgetreg: invalid lifecycle transition")
	ErrSuccessorRequired   = errors.New("widgetreg: deprecation requires a successor reference")
	ErrWidgetInUse         = errors.New("widgetreg: widget is still referenced by a page definition")
	ErrRegionNotAllowed    = errors.New("widgetreg: widget is not allowed in region")
	ErrRPCBindingMissing   = errors.New("widgetreg: page does not declare a widget RPC binding")
	ErrUnknownRPCBinding   = errors.New("widgetreg: widget names an unknown RPC")
	ErrInvalidWidgetRef    = errors.New("widgetreg: invalid widget reference")
	ErrInvalidSuccessorRef = errors.New("widgetreg: invalid successor reference")
)

// LifecycleState is the governed widget lifecycle. The transitions are
// intentionally one-way: DRAFT -> PUBLISHED -> DEPRECATED -> RETIRED.
type LifecycleState string

// Lifecycle is retained as a concise alias for callers that model the
// lifecycle field directly.
type Lifecycle = LifecycleState

const (
	LifecycleDraft      LifecycleState = "DRAFT"
	LifecyclePublished  LifecycleState = "PUBLISHED"
	LifecycleDeprecated LifecycleState = "DEPRECATED"
	LifecycleRetired    LifecycleState = "RETIRED"

	// Concise aliases for the four governed lifecycle values.
	Draft      = LifecycleDraft
	Published  = LifecyclePublished
	Deprecated = LifecycleDeprecated
	Retired    = LifecycleRetired
)

// AccessibilityContract is the publication contract for a widget. These
// properties describe obligations to the renderer; they do not contain HTML.
type AccessibilityContract struct {
	Name               string   `json:"name"`
	RequiredAttributes []string `json:"required_attributes"`
	KeyboardAccessible bool     `json:"keyboard_accessible"`
	VisibleFocus       bool     `json:"visible_focus"`
	SemanticStates     bool     `json:"semantic_states"`
	ErrorAssociation   bool     `json:"error_association"`
	ResponsiveReflow   bool     `json:"responsive_reflow"`
	ReducedMotion      bool     `json:"reduced_motion"`
	HighContrast       bool     `json:"high_contrast"`
}

// WidgetDefinition is the governed identity and trust contract for one
// versioned widget. RPCBindings and BrandTokenRefs are references only; the
// registry never executes a widget or resolves a token value.
type WidgetDefinition struct {
	ID             string                `json:"id"`
	Version        int                   `json:"version"`
	RegionKinds    []pagedef.RegionKind  `json:"region_kinds"`
	RPCBindings    []string              `json:"rpc_bindings"`
	Accessibility  AccessibilityContract `json:"accessibility"`
	BrandTokenRefs []string              `json:"brand_token_refs"`
	Owner          string                `json:"owner"`
	Lifecycle      LifecycleState        `json:"lifecycle"`
	SuccessorRef   string                `json:"successor_ref,omitempty"`
}

// Ref returns the canonical registry reference used in lifecycle evidence.
func (w WidgetDefinition) Ref() string { return widgetKey(w.ID, w.Version) }

// Validate checks the complete admission contract, including the closed
// region vocabulary and the generated RPC registry owned by pagedef.
func (w WidgetDefinition) Validate() error {
	if strings.TrimSpace(w.ID) == "" || strings.ContainsAny(w.ID, "@<>\r\n") || w.Version < 1 {
		return fmt.Errorf("%w: id and positive version are required", ErrInvalidWidget)
	}
	if strings.Contains(w.ID, ".v") {
		return fmt.Errorf("%w: id must not carry a version suffix: %q", ErrInvalidWidget, w.ID)
	}
	if len(w.RegionKinds) == 0 {
		return fmt.Errorf("%w: at least one region kind is required", ErrInvalidWidget)
	}
	seenRegions := make(map[pagedef.RegionKind]bool, len(w.RegionKinds))
	for _, kind := range w.RegionKinds {
		if pagedef.RegionKindDoc(kind) == "" {
			return fmt.Errorf("%w: unknown region kind %q", ErrInvalidWidget, kind)
		}
		if seenRegions[kind] {
			return fmt.Errorf("%w: region kind %q is repeated", ErrInvalidWidget, kind)
		}
		seenRegions[kind] = true
	}
	seenRPCs := make(map[string]bool, len(w.RPCBindings))
	knownRPCs := pagedef.KnownRPCs()
	for _, rpc := range w.RPCBindings {
		if strings.TrimSpace(rpc) == "" {
			return fmt.Errorf("%w: RPC binding is empty", ErrInvalidWidget)
		}
		if !knownRPCs[rpc] {
			return fmt.Errorf("%w: %q", ErrUnknownRPCBinding, rpc)
		}
		if seenRPCs[rpc] {
			return fmt.Errorf("%w: RPC binding %q is repeated", ErrInvalidWidget, rpc)
		}
		seenRPCs[rpc] = true
	}
	if err := w.Accessibility.validate(); err != nil {
		return fmt.Errorf("%w: accessibility: %v", ErrInvalidWidget, err)
	}
	if strings.TrimSpace(w.Owner) == "" {
		return fmt.Errorf("%w: owner is required", ErrInvalidWidget)
	}
	if !validLifecycle(w.Lifecycle) {
		return fmt.Errorf("%w: unknown lifecycle %q", ErrInvalidWidget, w.Lifecycle)
	}
	if w.Lifecycle == LifecycleDeprecated && strings.TrimSpace(w.SuccessorRef) == "" {
		return fmt.Errorf("%w", ErrSuccessorRequired)
	}
	if w.Lifecycle != LifecycleDeprecated && w.SuccessorRef != "" {
		return fmt.Errorf("%w: successor only belongs to DEPRECATED definitions", ErrInvalidWidget)
	}
	if err := validateRefs("brand token", w.BrandTokenRefs, "brand."); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidWidget, err)
	}
	return nil
}

func (a AccessibilityContract) validate() error {
	if strings.TrimSpace(a.Name) == "" {
		return errors.New("name is required")
	}
	if len(a.RequiredAttributes) == 0 {
		return errors.New("required attributes are required")
	}
	seen := make(map[string]bool, len(a.RequiredAttributes))
	for _, attribute := range a.RequiredAttributes {
		if strings.TrimSpace(attribute) == "" || strings.ContainsAny(attribute, "<>\r\n") {
			return fmt.Errorf("invalid required attribute %q", attribute)
		}
		if seen[attribute] {
			return fmt.Errorf("required attribute %q is repeated", attribute)
		}
		seen[attribute] = true
	}
	if !a.KeyboardAccessible || !a.VisibleFocus || !a.SemanticStates || !a.ErrorAssociation || !a.ResponsiveReflow || !a.ReducedMotion || !a.HighContrast {
		return errors.New("keyboard, focus, semantic-state, error, reflow, reduced-motion, and high-contrast support are required")
	}
	return nil
}

func validateRefs(label string, refs []string, prefix string) error {
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" || !strings.HasPrefix(ref, prefix) || strings.ContainsAny(ref, "<>\r\n") {
			return fmt.Errorf("%s reference %q is not a semantic %s reference", label, ref, prefix)
		}
		if seen[ref] {
			return fmt.Errorf("%s reference %q is repeated", label, ref)
		}
		seen[ref] = true
	}
	return nil
}

// Canonical returns deterministic JSON for the definition. Slice order is
// normalized for references because it has no presentation meaning.
func (w WidgetDefinition) Canonical() []byte {
	c := cloneWidget(w)
	sort.Slice(c.RegionKinds, func(i, j int) bool { return string(c.RegionKinds[i]) < string(c.RegionKinds[j]) })
	sort.Strings(c.RPCBindings)
	sort.Strings(c.BrandTokenRefs)
	b, _ := json.Marshal(struct {
		Schema        string           `json:"schema"`
		SchemaVersion int              `json:"schema_version"`
		Widget        WidgetDefinition `json:"widget"`
	}{"hcmnext.uxqual.widgetreg.widget_definition", 1, c})
	return b
}

// Digest returns the stable definition digest used by transition evidence.
func (w WidgetDefinition) Digest() string {
	sum := sha256.Sum256(w.Canonical())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Explain returns a short, safe diagnostic containing no widget payload.
func (w WidgetDefinition) Explain() string {
	return fmt.Sprintf("widget %s (%s; owner=%s; %s)", w.Ref(), w.Lifecycle, w.Owner, w.Digest())
}

// LifecycleTransition is an append-only digest of one lifecycle change.
type LifecycleTransition struct {
	WidgetRef    string         `json:"widget_ref"`
	From         LifecycleState `json:"from"`
	To           LifecycleState `json:"to"`
	SuccessorRef string         `json:"successor_ref,omitempty"`
	Digest       string         `json:"digest"`
}

// Registry stores immutable-by-convention widget definitions and lifecycle
// evidence. Admission and lifecycle operations clone values at both edges.
type Registry struct {
	widgets     map[string]WidgetDefinition
	transitions []LifecycleTransition
}

// NewRegistry admits definitions after validating each one.
func NewRegistry(definitions ...WidgetDefinition) (*Registry, error) {
	r := &Registry{widgets: make(map[string]WidgetDefinition, len(definitions))}
	for _, definition := range definitions {
		if err := definition.Validate(); err != nil {
			return nil, err
		}
		key := definition.Ref()
		if _, exists := r.widgets[key]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateWidget, key)
		}
		r.widgets[key] = cloneWidget(definition)
	}
	return r, nil
}

// Lookup accepts the canonical id@version form, the pagedef-style id.vN
// form, or an id plus one version argument.
func (r *Registry) Lookup(ref string, version ...int) (WidgetDefinition, bool) {
	if r == nil {
		return WidgetDefinition{}, false
	}
	key, ok := parseReference(ref, version...)
	if !ok {
		return WidgetDefinition{}, false
	}
	widget, ok := r.widgets[key]
	if !ok {
		return WidgetDefinition{}, false
	}
	return cloneWidget(widget), true
}

// Refs returns all admitted references in stable order.
func (r *Registry) Refs() []string {
	if r == nil {
		return nil
	}
	refs := make([]string, 0, len(r.widgets))
	for ref := range r.widgets {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

// Transition applies one legal lifecycle transition and records its digest.
// A DEPRECATED definition must name a published successor. A RETIRED
// definition is refused while any supplied PageDefinition references it.
func (r *Registry) Transition(ref string, to LifecycleState, successorRef string, pages ...pagedef.PageDefinition) error {
	if r == nil {
		return ErrUnknownWidget
	}
	key, ok := parseReference(ref)
	if !ok {
		return fmt.Errorf("%w: %q", ErrInvalidWidgetRef, ref)
	}
	current, exists := r.widgets[key]
	if !exists {
		return fmt.Errorf("%w: %q", ErrUnknownWidget, ref)
	}
	if !legalTransition(current.Lifecycle, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Lifecycle, to)
	}
	if to == LifecycleDeprecated && strings.TrimSpace(successorRef) == "" {
		return ErrSuccessorRequired
	}
	if to != LifecycleDeprecated && successorRef != "" {
		return fmt.Errorf("%w: successor is only valid when deprecating", ErrInvalidTransition)
	}
	if successorRef != "" {
		successorKey, successorOK := parseReference(successorRef)
		if !successorOK {
			return fmt.Errorf("%w: %q", ErrInvalidSuccessorRef, successorRef)
		}
		successor, found := r.widgets[successorKey]
		if !found || successor.Lifecycle != LifecyclePublished {
			return fmt.Errorf("%w: successor %q is not published", ErrInvalidSuccessorRef, successorRef)
		}
	}
	if to == LifecycleRetired {
		for _, page := range pages {
			for _, region := range page.Regions {
				for _, slot := range region.Widgets {
					if slotRef, slotOK := parseReference(slot.WidgetRef); slotOK && slotRef == key {
						return fmt.Errorf("%w: %s in page %s region %s slot %s", ErrWidgetInUse, ref, page.PageID, region.ID, slot.ID)
					}
				}
			}
		}
	}
	updated := cloneWidget(current)
	updated.Lifecycle = to
	updated.SuccessorRef = successorRef
	r.widgets[key] = updated
	transition := LifecycleTransition{WidgetRef: key, From: current.Lifecycle, To: to, SuccessorRef: successorRef, Digest: transitionDigest(current, updated)}
	r.transitions = append(r.transitions, transition)
	return nil
}

// Retire is the explicit safety-named form of Transition for retirement.
func (r *Registry) Retire(ref string, pages ...pagedef.PageDefinition) error {
	return r.Transition(ref, LifecycleRetired, "", pages...)
}

// History returns lifecycle evidence in transition order.
func (r *Registry) History() []LifecycleTransition {
	if r == nil {
		return nil
	}
	return append([]LifecycleTransition(nil), r.transitions...)
}

// Resolution binds every page slot to a published widget definition.
type Resolution struct {
	Page  pagedef.PageDefinition `json:"page"`
	Slots []SlotBinding          `json:"slots"`
}

// SlotBinding is the resolved identity of one page widget slot.
type SlotBinding struct {
	RegionID     string           `json:"region_id"`
	SlotID       string           `json:"slot_id"`
	RequestedRef string           `json:"requested_ref"`
	Widget       WidgetDefinition `json:"widget"`
}

// Resolve binds the page's slots and enforces region, RPC, and publication
// boundaries. No renderer or external service is involved.
func (r *Registry) Resolve(page pagedef.PageDefinition) (Resolution, error) {
	if r == nil {
		return Resolution{}, ErrUnknownWidget
	}
	if violations := page.Validate(); len(violations) != 0 {
		return Resolution{}, fmt.Errorf("%w: page definition is invalid: %s", ErrInvalidWidget, violations[0])
	}
	resolution := Resolution{Page: page, Slots: make([]SlotBinding, 0)}
	for _, region := range page.Regions {
		declaredRPCs := make(map[string]bool, len(region.Bindings)+len(region.Actions))
		for _, binding := range region.Bindings {
			declaredRPCs[binding.RPC] = true
		}
		for _, action := range region.Actions {
			declaredRPCs[action.RPC] = true
		}
		for _, slot := range region.Widgets {
			widget, ok := r.Lookup(slot.WidgetRef)
			if !ok {
				return Resolution{}, fmt.Errorf("%w: %q named by region %s slot %s", ErrUnknownWidget, slot.WidgetRef, region.ID, slot.ID)
			}
			if widget.Lifecycle != LifecyclePublished {
				return Resolution{}, fmt.Errorf("%w: %s named by region %s slot %s", ErrUnpublishedWidget, widget.Ref(), region.ID, slot.ID)
			}
			allowed := false
			for _, kind := range widget.RegionKinds {
				if kind == region.Kind {
					allowed = true
					break
				}
			}
			if !allowed {
				return Resolution{}, fmt.Errorf("%w: %s cannot occupy %s", ErrRegionNotAllowed, widget.Ref(), region.Kind)
			}
			for _, rpc := range widget.RPCBindings {
				if !declaredRPCs[rpc] {
					return Resolution{}, fmt.Errorf("%w: widget %s requires %s in region %s", ErrRPCBindingMissing, widget.Ref(), rpc, region.ID)
				}
			}
			resolution.Slots = append(resolution.Slots, SlotBinding{RegionID: region.ID, SlotID: slot.ID, RequestedRef: slot.WidgetRef, Widget: widget})
		}
	}
	return resolution, nil
}

// ResolvePage is a descriptive alias for Resolve.
func (r *Registry) ResolvePage(page pagedef.PageDefinition) (Resolution, error) {
	return r.Resolve(page)
}

// Canonical returns the deterministic registry table and lifecycle evidence.
func (r *Registry) Canonical() []byte {
	if r == nil {
		return []byte(`{"schema":"hcmnext.uxqual.widgetreg.registry","schema_version":1,"widgets":[],"transitions":[]}`)
	}
	widgets := make([]WidgetDefinition, 0, len(r.widgets))
	for _, widget := range r.widgets {
		widgets = append(widgets, cloneWidget(widget))
	}
	sort.Slice(widgets, func(i, j int) bool { return widgets[i].Ref() < widgets[j].Ref() })
	transitions := append([]LifecycleTransition(nil), r.transitions...)
	b, _ := json.Marshal(struct {
		Schema        string                `json:"schema"`
		SchemaVersion int                   `json:"schema_version"`
		Widgets       []WidgetDefinition    `json:"widgets"`
		Transitions   []LifecycleTransition `json:"transitions"`
	}{"hcmnext.uxqual.widgetreg.registry", 1, widgets, transitions})
	return b
}

// Digest returns the stable digest of the registry state.
func (r *Registry) Digest() string {
	sum := sha256.Sum256(r.Canonical())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Explain returns a bounded registry diagnostic.
func (r *Registry) Explain() string {
	if r == nil {
		return "widget registry <nil>"
	}
	return fmt.Sprintf("widget registry (%d widgets, %d transitions; %s)", len(r.widgets), len(r.transitions), r.Digest())
}

// PromotionRegistry is the governed widget fixture required by the two
// Promotion PageDefinitions. It is pure and renderer-independent.
func PromotionRegistry() *Registry {
	definitions := []WidgetDefinition{
		promotionWidget("widget.table.workforce", 1, []pagedef.RegionKind{pagedef.RegionPrimary}, []string{pagedef.RPCRef(pagedef.JourneyServiceName, "ListWorkers")}),
		promotionWidget("widget.form.create-worker", 1, []pagedef.RegionKind{pagedef.RegionPrimary}, []string{pagedef.RPCRef(pagedef.JourneyServiceName, "CreateWorker")}),
		promotionWidget("widget.list.journeys", 1, []pagedef.RegionKind{pagedef.RegionSupporting}, []string{pagedef.RPCRef(pagedef.JourneyServiceName, "ListJourneys")}),
		promotionWidget("widget.form.propose-journey", 1, []pagedef.RegionKind{pagedef.RegionSupporting}, []string{pagedef.RPCRef(pagedef.JourneyServiceName, "ProposeJourney")}),
		promotionWidget("widget.stepper.journey-stage", 1, []pagedef.RegionKind{pagedef.RegionLocalNavigation}, nil),
		promotionWidget("widget.table.comparison", 1, []pagedef.RegionKind{pagedef.RegionPrimary}, nil),
		promotionWidget("widget.gauge.pay-band", 1, []pagedef.RegionKind{pagedef.RegionPrimary}, nil),
		promotionWidget("widget.gauge.budget", 1, []pagedef.RegionKind{pagedef.RegionPrimary}, nil),
		promotionWidget("widget.factlist", 1, []pagedef.RegionKind{pagedef.RegionSupporting}, nil),
		promotionWidget("widget.table.work-items", 1, []pagedef.RegionKind{pagedef.RegionSupporting}, nil),
		promotionWidget("widget.timeline", 1, []pagedef.RegionKind{pagedef.RegionSupporting}, nil),
	}
	r, err := NewRegistry(definitions...)
	if err != nil {
		panic(err)
	}
	return r
}

// PromotionWidgetRegistry is a descriptive alias for PromotionRegistry.
func PromotionWidgetRegistry() *Registry { return PromotionRegistry() }

func promotionWidget(id string, version int, regions []pagedef.RegionKind, rpcs []string) WidgetDefinition {
	return WidgetDefinition{
		ID: id, Version: version, RegionKinds: regions, RPCBindings: rpcs,
		Accessibility: AccessibilityContract{
			Name: "Promotion widget", RequiredAttributes: []string{"aria-label"},
			KeyboardAccessible: true, VisibleFocus: true, SemanticStates: true,
			ErrorAssociation: true, ResponsiveReflow: true, ReducedMotion: true, HighContrast: true,
		},
		BrandTokenRefs: []string{"brand.color.primary", "brand.typography.heading", "brand.spacing.md"},
		Owner:          "experience.promotion", Lifecycle: LifecyclePublished,
	}
}

func validLifecycle(state LifecycleState) bool {
	switch state {
	case LifecycleDraft, LifecyclePublished, LifecycleDeprecated, LifecycleRetired:
		return true
	default:
		return false
	}
}

func legalTransition(from, to LifecycleState) bool {
	return (from == LifecycleDraft && to == LifecyclePublished) ||
		(from == LifecyclePublished && to == LifecycleDeprecated) ||
		(from == LifecycleDeprecated && to == LifecycleRetired)
}

func widgetKey(id string, version int) string { return id + "@" + strconv.Itoa(version) }

func parseReference(ref string, version ...int) (string, bool) {
	if len(version) > 1 {
		return "", false
	}
	ref = strings.TrimSpace(ref)
	if len(version) == 1 {
		if ref == "" || version[0] < 1 || strings.Contains(ref, "@") {
			return "", false
		}
		return widgetKey(ref, version[0]), true
	}
	if at := strings.LastIndexByte(ref, '@'); at > 0 {
		v, err := strconv.Atoi(ref[at+1:])
		if err != nil || v < 1 || strings.Contains(ref[:at], "@") {
			return "", false
		}
		return widgetKey(ref[:at], v), true
	}
	marker := strings.LastIndex(ref, ".v")
	if marker <= 0 {
		return "", false
	}
	v, err := strconv.Atoi(ref[marker+2:])
	if err != nil || v < 1 {
		return "", false
	}
	return widgetKey(ref[:marker], v), true
}

func transitionDigest(from, to WidgetDefinition) string {
	b, _ := json.Marshal(struct {
		From string `json:"from"`
		To   string `json:"to"`
	}{from.Digest(), to.Digest()})
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func cloneWidget(w WidgetDefinition) WidgetDefinition {
	w.RegionKinds = append([]pagedef.RegionKind(nil), w.RegionKinds...)
	w.RPCBindings = append([]string(nil), w.RPCBindings...)
	w.BrandTokenRefs = append([]string(nil), w.BrandTokenRefs...)
	w.Accessibility.RequiredAttributes = append([]string(nil), w.Accessibility.RequiredAttributes...)
	return w
}
