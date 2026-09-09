// Package disposition owns the reviewed BusinessIntent-to-user-flow join.
// It is deliberately a read-only experience-plane index: it does not grant
// authority and it never treats a display name as an identity.
package disposition

import (
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
)

// Kind is the complete UXFLOW-003 disposition vocabulary.
type Kind string

const (
	ParticipantRoot    Kind = "PARTICIPANT_ROOT"
	ParticipantChild   Kind = "PARTICIPANT_CHILD"
	VisibleSystemStage Kind = "VISIBLE_SYSTEM_STAGE"
	AdminOperator      Kind = "ADMIN_OPERATOR"
	NoUserFlow         Kind = "NO_USER_FLOW"
)

func (k Kind) Valid() bool {
	switch k {
	case ParticipantRoot, ParticipantChild, VisibleSystemStage, AdminOperator, NoUserFlow:
		return true
	}
	return false
}

// Entry is one reviewed, stable-definition disposition. DefinitionRef is the
// only identity key; DisplayName is copied only as a presentation convenience.
type Entry struct {
	DefinitionRef string   `json:"definition_ref"`
	DisplayName   string   `json:"display_name"`
	Kind          Kind     `json:"kind"`
	Reason        string   `json:"reason,omitempty"`
	FlowIDs       []string `json:"flow_ids,omitempty"`
	Archetypes    []string `json:"archetypes,omitempty"`
	Delta         string   `json:"delta,omitempty"`
	ParentRef     string   `json:"parent_ref,omitempty"`
	Discoverable  bool     `json:"discoverable"`
	Source        string   `json:"source"` // BASELINE, EXTENSION, SOURCE_UNBOUND
}

// Report keeps denominators separate so an extension or an unbound source
// cannot make baseline coverage appear complete.
type Report struct{ Baseline, Extension, SourceUnbound int }

// Registry is immutable after construction. Returned slices are copies.
type Registry struct {
	entries []Entry
	byRef   map[string]Entry
	byFlow  map[string][]string
	report  Report
}

var reviewed = map[string]Entry{
	"hcmnext.people.change_manager/v1":               {Kind: ParticipantRoot, FlowIDs: []string{"UF-017"}, Archetypes: []string{"UF-A1"}, Delta: "effective-dated manager relationship", Discoverable: true, Source: "BASELINE"},
	"hcmnext.people.explain_worker_state/v1":         {Kind: ParticipantRoot, FlowIDs: []string{"UF-001", "UF-015"}, Archetypes: []string{"UF-A1", "UF-A8"}, Delta: "authorized worker state and provenance", Discoverable: true, Source: "BASELINE"},
	"hcmnext.people.promote_worker/v1":               {Kind: ParticipantRoot, FlowIDs: []string{"UF-007"}, Archetypes: []string{"UF-A2", "UF-A4", "UF-A6"}, Delta: "promotion proposal and execution", Discoverable: true, Source: "BASELINE"},
	"hcmnext.rewards.change_base_pay/v1":             {Kind: ParticipantRoot, FlowIDs: []string{"UF-003"}, Archetypes: []string{"UF-A2", "UF-A6"}, Delta: "governed compensation change", Discoverable: true, Source: "BASELINE"},
	"hcmnext.rewards.simulate_compensation/v1":       {Kind: ParticipantChild, FlowIDs: []string{"UF-003"}, Archetypes: []string{"UF-A6"}, Delta: "compensation preview", ParentRef: "hcmnext.rewards.change_base_pay/v1", Discoverable: false, Source: "BASELINE"},
	"hcmnext.rewards.evaluate_pay_band_position/v1":  {Kind: ParticipantChild, FlowIDs: []string{"UF-003"}, Archetypes: []string{"UF-A6"}, Delta: "pay-band comparison", ParentRef: "hcmnext.rewards.change_base_pay/v1", Discoverable: false, Source: "BASELINE"},
	"hcmnext.rewards.reserve_compensation_budget/v1": {Kind: ParticipantChild, FlowIDs: []string{"UF-003"}, Archetypes: []string{"UF-A6"}, Delta: "budget reservation", ParentRef: "hcmnext.rewards.change_base_pay/v1", Discoverable: false, Source: "BASELINE"},
	"hcmnext.rewards.release_compensation_budget/v1": {Kind: ParticipantChild, FlowIDs: []string{"UF-003"}, Archetypes: []string{"UF-A6"}, Delta: "budget reservation release", ParentRef: "hcmnext.rewards.change_base_pay/v1", Discoverable: false, Source: "BASELINE"},
	"hcmnext.work.approve_proposal/v1":               {Kind: ParticipantChild, FlowIDs: []string{"UF-004"}, Archetypes: []string{"UF-A4"}, Delta: "proposal approval", Discoverable: false, Source: "BASELINE"},
	"hcmnext.work.reject_proposal/v1":                {Kind: ParticipantChild, FlowIDs: []string{"UF-004"}, Archetypes: []string{"UF-A4"}, Delta: "proposal rejection", Discoverable: false, Source: "BASELINE"},
	"hcmnext.intelligence.explain_transaction/v1":    {Kind: ParticipantChild, FlowIDs: []string{"UF-005", "UF-015"}, Archetypes: []string{"UF-A8"}, Delta: "transaction lineage explanation", ParentRef: "hcmnext.people.explain_worker_state/v1", Discoverable: false, Source: "BASELINE"},
	"hcmnext.operations.detect_drift/v1":             {Kind: VisibleSystemStage, FlowIDs: []string{"UF-012", "UF-013", "UF-016"}, Archetypes: []string{"UF-A6", "UF-A10"}, Delta: "background drift detection with visible status", Discoverable: false, Source: "BASELINE"},
	"hcmnext.operations.create_repair_plan/v1":       {Kind: AdminOperator, FlowIDs: []string{"UF-012", "UF-016"}, Archetypes: []string{"UF-A6", "UF-A10"}, Delta: "bounded repair plan", Discoverable: true, Source: "BASELINE"},
	"hcmnext.operations.simulate_repair/v1":          {Kind: AdminOperator, FlowIDs: []string{"UF-012", "UF-016"}, Archetypes: []string{"UF-A6", "UF-A10"}, Delta: "repair simulation", Discoverable: true, Source: "BASELINE"},
}

// New builds a total registry for accepted definitions. An optional source
// map classifies source coverage; absent entries are SOURCE_UNBOUND.
func New(defs []intent.Definition, source ...map[string]string) (*Registry, error) {
	src := map[string]string{}
	if len(source) > 0 && source[0] != nil {
		src = source[0]
	}
	r := &Registry{byRef: map[string]Entry{}, byFlow: map[string][]string{}}
	for _, d := range defs {
		if !d.Maturity.InCatalog() {
			continue
		}
		ref := d.Ref.String()
		if _, exists := r.byRef[ref]; exists {
			return nil, fmt.Errorf("uxflow-003: duplicate disposition for %q", ref)
		}
		e, ok := reviewed[ref]
		if !ok {
			return nil, fmt.Errorf("uxflow-003: accepted intent %q has no disposition", ref)
		}
		e.DefinitionRef, e.DisplayName = ref, d.DisplayName
		if declared, present := src[ref]; present {
			e.Source = declared
		}
		if e.Source == "" {
			e.Source = "SOURCE_UNBOUND"
		}
		if e.Kind == NoUserFlow && e.Reason == "" {
			return nil, fmt.Errorf("uxflow-003: %q no-user-flow disposition has no reason", ref)
		}
		if !e.Kind.Valid() || e.Discoverable && len(e.FlowIDs) == 0 {
			return nil, fmt.Errorf("uxflow-003: invalid disposition for %q", ref)
		}
		r.entries = append(r.entries, e)
		r.byRef[ref] = e
		for _, flow := range e.FlowIDs {
			r.byFlow[flow] = append(r.byFlow[flow], ref)
		}
		switch e.Source {
		case "BASELINE":
			r.report.Baseline++
		case "EXTENSION":
			r.report.Extension++
		case "SOURCE_UNBOUND":
			r.report.SourceUnbound++
		default:
			return nil, fmt.Errorf("uxflow-003: unknown source %q", e.Source)
		}
	}
	sort.Slice(r.entries, func(i, j int) bool { return r.entries[i].DefinitionRef < r.entries[j].DefinitionRef })
	for f := range r.byFlow {
		sort.Strings(r.byFlow[f])
	}
	return r, nil
}

func (r *Registry) Entries() []Entry {
	out := append([]Entry(nil), r.entries...)
	for i := range out {
		out[i].FlowIDs = append([]string(nil), out[i].FlowIDs...)
		out[i].Archetypes = append([]string(nil), out[i].Archetypes...)
	}
	return out
}
func (r *Registry) Lookup(ref string) (Entry, bool) {
	e, ok := r.byRef[ref]
	e.FlowIDs = append([]string(nil), e.FlowIDs...)
	return e, ok
}
func (r *Registry) ForFlow(flow string) []string { return append([]string(nil), r.byFlow[flow]...) }

// ReverseIndex is the flow-to-stable-intent index. It is a deep copy and may
// be safely modified by callers when preparing a view.
func (r *Registry) ReverseIndex() map[string][]string {
	out := make(map[string][]string, len(r.byFlow))
	for flow, refs := range r.byFlow {
		out[flow] = append([]string(nil), refs...)
	}
	return out
}
func (r *Registry) Report() Report { return r.report }
func (r *Registry) Len() int       { return len(r.entries) }

// NewDefault compiles the canonical accepted BusinessIntent catalog.
func NewDefault() (*Registry, error) {
	return New(definitions.All())
}

// BuildDefaultRegistry is a descriptive alias used by conformance tooling.
func BuildDefaultRegistry() (*Registry, error) { return NewDefault() }
