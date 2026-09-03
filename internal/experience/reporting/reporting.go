// Package reporting owns the semantic, versioned contracts used by reports
// and dashboards.  It deliberately contains definitions only: execution and
// rendering consume a pinned definition and cannot mutate it.
package reporting

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	ErrInvalidDefinition = errors.New("reporting: invalid definition")
	ErrUnknownReference  = errors.New("reporting: unknown governed reference")
	ErrUnsafeDefault     = errors.New("reporting: unsafe parameter default")
	ErrImmutable         = errors.New("reporting: immutable version")
)

// Specific aliases make policy failures easy for callers and audit tooling
// to classify while preserving the small sentinel set used internally.
var (
	ErrUnknownField                  = ErrUnknownReference
	ErrUnknownMetric                 = ErrUnknownReference
	ErrUnknownPopulation             = ErrUnknownReference
	ErrParameterDefaultBroadensScope = ErrUnsafeDefault
)

type Source struct{ ID, Version, Classification, Owner string }
type Field struct{ ID, Type, SourceID, Classification string }
type Metric struct{ ID, Type, SourceID, Expression string }
type Population struct{ ID, SourceID, Predicate string }

// Parameter is typed and has an explicit, bounded default. A missing default
// is preferable to an implicit all-population query.
type Parameter struct {
	ID, Type, Default, Scope string
	Required                 bool
}
type Filter struct{ Field, Operator, Parameter string }
type Sort struct {
	Field      string
	Descending bool
}
type Aggregation struct{ Metric, Function string }
type DisclosurePolicy struct {
	Classification      string
	SuppressSmallGroups bool
	MinimumGroupSize    int
}

type Definition struct {
	ID, Version, Owner  string
	SourceIDs           []string
	Fields              []string
	Metrics             []string
	Population          string
	Parameters          []Parameter
	Filters             []Filter
	Sorts               []Sort
	Aggregations        []Aggregation
	Disclosure          DisclosurePolicy
	RenderFormats       []string
	ExportFormats       []string
	CompatibilityDigest string
}

type Panel struct{ ID, ReportID, ReportDigest, Title string }
type Dashboard struct {
	ID, Version, Owner  string
	Panels              []Panel
	RenderFormats       []string
	ExportFormats       []string
	CompatibilityDigest string
}

func clone[T any](in []T) []T { return append([]T(nil), in...) }
func (d Definition) clone() Definition {
	d.SourceIDs = clone(d.SourceIDs)
	d.Fields = clone(d.Fields)
	d.Metrics = clone(d.Metrics)
	d.Parameters = clone(d.Parameters)
	d.Filters = clone(d.Filters)
	d.Sorts = clone(d.Sorts)
	d.Aggregations = clone(d.Aggregations)
	d.RenderFormats = clone(d.RenderFormats)
	d.ExportFormats = clone(d.ExportFormats)
	return d
}
func (d Dashboard) clone() Dashboard {
	d.Panels = clone(d.Panels)
	d.RenderFormats = clone(d.RenderFormats)
	d.ExportFormats = clone(d.ExportFormats)
	return d
}

func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func content(d Definition) any {
	return struct {
		ID, Version, Owner           string
		SourceIDs, Fields, Metrics   []string
		Population                   string
		Parameters                   []Parameter
		Filters                      []Filter
		Sorts                        []Sort
		Aggregations                 []Aggregation
		Disclosure                   DisclosurePolicy
		RenderFormats, ExportFormats []string
	}{d.ID, d.Version, d.Owner, d.SourceIDs, d.Fields, d.Metrics, d.Population, d.Parameters, d.Filters, d.Sorts, d.Aggregations, d.Disclosure, d.RenderFormats, d.ExportFormats}
}
func (d Definition) Digest() string { return digest(content(d)) }

// Validate checks intrinsic shape. Reference validation requires a Registry.
func (d Definition) Validate() error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Version) == "" || strings.TrimSpace(d.Owner) == "" || d.Population == "" {
		return ErrInvalidDefinition
	}
	if len(d.SourceIDs) == 0 || len(d.Fields) == 0 && len(d.Metrics) == 0 {
		return ErrInvalidDefinition
	}
	return nil
}

func validateDefinition(d Definition, sources map[string]Source, fields map[string]Field, metrics map[string]Metric, populations map[string]Population) error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Version) == "" || strings.TrimSpace(d.Owner) == "" || d.Population == "" {
		return ErrInvalidDefinition
	}
	if len(d.SourceIDs) == 0 || len(d.Fields) == 0 && len(d.Metrics) == 0 {
		return fmt.Errorf("%w: empty governed selection", ErrInvalidDefinition)
	}
	seen := map[string]bool{}
	for _, id := range d.SourceIDs {
		if seen[id] {
			return fmt.Errorf("%w: duplicate source %s", ErrInvalidDefinition, id)
		}
		seen[id] = true
		if _, ok := sources[id]; !ok {
			return fmt.Errorf("%w: source %s", ErrUnknownReference, id)
		}
	}
	if _, ok := populations[d.Population]; !ok {
		return fmt.Errorf("%w: population %s", ErrUnknownReference, d.Population)
	}
	for _, id := range d.Fields {
		f, ok := fields[id]
		if !ok {
			return fmt.Errorf("%w: field %s", ErrUnknownReference, id)
		}
		if !seen[f.SourceID] {
			return fmt.Errorf("%w: field source %s", ErrInvalidDefinition, f.SourceID)
		}
	}
	for _, id := range d.Metrics {
		m, ok := metrics[id]
		if !ok {
			return fmt.Errorf("%w: metric %s", ErrUnknownReference, id)
		}
		if !seen[m.SourceID] {
			return fmt.Errorf("%w: metric source %s", ErrInvalidDefinition, m.SourceID)
		}
	}
	ps := map[string]bool{}
	for _, p := range d.Parameters {
		if p.ID == "" || p.Type == "" || ps[p.ID] {
			return ErrInvalidDefinition
		}
		ps[p.ID] = true
		if p.Default == "" && !p.Required {
			return fmt.Errorf("%w: %s", ErrUnsafeDefault, p.ID)
		}
		if strings.EqualFold(p.Scope, "all") || strings.EqualFold(p.Default, "*") {
			return fmt.Errorf("%w: %s broadens scope", ErrUnsafeDefault, p.ID)
		}
	}
	for _, f := range d.Filters {
		if !contains(d.Fields, f.Field) {
			return fmt.Errorf("%w: filter field %s", ErrUnknownReference, f.Field)
		}
		if f.Parameter != "" && !ps[f.Parameter] {
			return fmt.Errorf("%w: parameter %s", ErrUnknownReference, f.Parameter)
		}
	}
	for _, s := range d.Sorts {
		if !contains(d.Fields, s.Field) {
			return fmt.Errorf("%w: sort field %s", ErrUnknownReference, s.Field)
		}
	}
	for _, a := range d.Aggregations {
		if !contains(d.Metrics, a.Metric) {
			return fmt.Errorf("%w: aggregation metric %s", ErrUnknownReference, a.Metric)
		}
	}
	if d.Disclosure.SuppressSmallGroups && d.Disclosure.MinimumGroupSize < 1 {
		return ErrInvalidDefinition
	}
	return nil
}
func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

type Registry struct {
	mu          sync.RWMutex
	sources     map[string]Source
	fields      map[string]Field
	metrics     map[string]Metric
	populations map[string]Population
	reports     map[string]Definition
	dashboards  map[string]Dashboard
}

func NewRegistry() *Registry {
	return &Registry{sources: map[string]Source{}, fields: map[string]Field{}, metrics: map[string]Metric{}, populations: map[string]Population{}, reports: map[string]Definition{}, dashboards: map[string]Dashboard{}}
}
func (r *Registry) RegisterSource(v Source) error {
	if v.ID == "" || v.Version == "" {
		return ErrInvalidDefinition
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sources[v.ID] = v
	return nil
}
func (r *Registry) RegisterField(v Field) error {
	if v.ID == "" || v.Type == "" || v.SourceID == "" {
		return ErrInvalidDefinition
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fields[v.ID] = v
	return nil
}
func (r *Registry) RegisterMetric(v Metric) error {
	if v.ID == "" || v.Type == "" || v.SourceID == "" {
		return ErrInvalidDefinition
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics[v.ID] = v
	return nil
}
func (r *Registry) RegisterPopulation(v Population) error {
	if v.ID == "" || v.SourceID == "" {
		return ErrInvalidDefinition
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.populations[v.ID] = v
	return nil
}
func (r *Registry) Publish(d Definition) (Definition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := validateDefinition(d, r.sources, r.fields, r.metrics, r.populations); err != nil {
		return Definition{}, err
	}
	d.CompatibilityDigest = d.Digest()
	key := d.ID + "/" + d.Version
	if old, ok := r.reports[key]; ok {
		if old.CompatibilityDigest != d.CompatibilityDigest {
			return Definition{}, ErrImmutable
		}
		return old.clone(), nil
	}
	r.reports[key] = d.clone()
	return d.clone(), nil
}
func (r *Registry) Report(id, version string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.reports[id+"/"+version]
	if !ok {
		return Definition{}, false
	}
	return d.clone(), true
}
func (r *Registry) PublishDashboard(d Dashboard) (Dashboard, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d.ID == "" || d.Version == "" || d.Owner == "" || len(d.Panels) == 0 {
		return Dashboard{}, ErrInvalidDefinition
	}
	seen := map[string]bool{}
	for _, p := range d.Panels {
		if p.ID == "" || seen[p.ID] || p.ReportID == "" || p.ReportDigest == "" {
			return Dashboard{}, ErrInvalidDefinition
		}
		seen[p.ID] = true
		var found bool
		for _, v := range r.reports {
			if v.ID == p.ReportID && v.CompatibilityDigest == p.ReportDigest {
				found = true
				break
			}
		}
		if !found {
			return Dashboard{}, fmt.Errorf("%w: report panel %s", ErrUnknownReference, p.ReportID)
		}
	}
	d.CompatibilityDigest = digest(struct {
		ID, Version, Owner string
		Panels             []Panel
		Render, Export     []string
	}{d.ID, d.Version, d.Owner, d.Panels, d.RenderFormats, d.ExportFormats})
	key := d.ID + "/" + d.Version
	if old, ok := r.dashboards[key]; ok {
		if old.CompatibilityDigest != d.CompatibilityDigest {
			return Dashboard{}, ErrImmutable
		}
		return old.clone(), nil
	}
	r.dashboards[key] = d.clone()
	return d.clone(), nil
}
func (r *Registry) Dashboard(id, version string) (Dashboard, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.dashboards[id+"/"+version]
	if !ok {
		return Dashboard{}, false
	}
	return d.clone(), true
}
func (r *Registry) Versions(id string) []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []Definition{}
	for _, d := range r.reports {
		if d.ID == id {
			out = append(out, d.clone())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out
}
