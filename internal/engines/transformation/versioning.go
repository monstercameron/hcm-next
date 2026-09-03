package transformation

import (
	"errors"
	"fmt"
	"sort"
)

// ReleaseState is the lifecycle state of an immutable transformation release.
type ReleaseState string

const (
	ReleaseDraft      ReleaseState = "DRAFT"
	ReleaseValidated  ReleaseState = "VALIDATED"
	ReleaseApproved   ReleaseState = "APPROVED"
	ReleaseActive     ReleaseState = "ACTIVE"
	ReleaseSuperseded ReleaseState = "SUPERSEDED"
	ReleaseRetired    ReleaseState = "RETIRED"
)

var (
	ErrVersionExists       = errors.New("transformation: version already exists")
	ErrVersionNotFound     = errors.New("transformation: version not found")
	ErrPinnedVersion       = errors.New("transformation: pinned version is unavailable")
	ErrIncompatibleVersion = errors.New("transformation: incompatible version")
)

// Consumer identifies an adoption point. The Kind is deliberately open so
// callers can use WORKFLOW, MAPPING, IMPORT, EXPORT, or CONSUMER (and future
// categories) without coupling this engine to a domain package.
type Consumer struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// VersionedTransformation is an immutable, named release of a definition.
// Definition is copied on registration and cannot be changed through the
// registry after publication.
type VersionedTransformation struct {
	Name       string                   `json:"name"`
	Revision   int                      `json:"revision"`
	Definition TransformationDefinition `json:"definition"`
	Digest     string                   `json:"digest"`
	State      ReleaseState             `json:"state"`
	Consumers  []Consumer               `json:"consumers,omitempty"`
}

// CompatibilityReport describes whether a new release may replace an old
// release and which adoption points need review. A release can be published
// as a new version even when it is breaking; it may not be published as a
// compatible replacement in that case.
type CompatibilityReport struct {
	Compatible bool       `json:"compatible"`
	Breaking   bool       `json:"breaking"`
	Reasons    []string   `json:"reasons,omitempty"`
	Affected   []Consumer `json:"affected,omitempty"`
	// Category projections make the impact report convenient for governance
	// callers while Affected remains the canonical, extensible representation.
	AffectedWorkflows []string `json:"affected_workflows,omitempty"`
	AffectedMappings  []string `json:"affected_mappings,omitempty"`
	AffectedImports   []string `json:"affected_imports,omitempty"`
	AffectedExports   []string `json:"affected_exports,omitempty"`
	AffectedConsumers []string `json:"affected_consumers,omitempty"`
}

func (r CompatibilityReport) AffectedKinds(kind string) []string {
	var out []string
	for _, c := range r.Affected {
		if c.Kind == kind {
			out = append(out, c.ID)
		}
	}
	sort.Strings(out)
	return out
}

// CompareVersions performs conservative structural compatibility analysis.
// Removing/renaming fields, changing types, changing source bounds, or
// changing the operation program is breaking because pinned executions must
// retain their original semantics.
func CompareVersions(old, next TransformationDefinition, consumers []Consumer) (CompatibilityReport, error) {
	if err := old.Validate(); err != nil {
		return CompatibilityReport{}, err
	}
	if err := next.Validate(); err != nil {
		return CompatibilityReport{}, err
	}
	r := CompatibilityReport{Compatible: true, Affected: append([]Consumer(nil), consumers...)}
	if old.Name != next.Name {
		r.Reasons = append(r.Reasons, "transformation name changed")
	}
	if old.Source.Name != next.Source.Name || old.Source.Version != next.Source.Version {
		r.Reasons = append(r.Reasons, "source schema changed")
	}
	if old.Destination.Name != next.Destination.Name {
		r.Reasons = append(r.Reasons, "destination schema changed")
	}
	if !sameFields(old.Destination.Fields, next.Destination.Fields) {
		r.Reasons = append(r.Reasons, "destination fields or types changed")
	}
	if old.Compatibility.MinimumSourceVersion != next.Compatibility.MinimumSourceVersion || old.Compatibility.MaximumSourceVersion != next.Compatibility.MaximumSourceVersion {
		r.Reasons = append(r.Reasons, "source compatibility bounds changed")
	}
	a, _ := old.CanonicalBytes()
	b, _ := next.CanonicalBytes()
	if string(a) != string(b) && len(r.Reasons) == 0 {
		r.Reasons = append(r.Reasons, "transformation semantics changed")
	}
	r.Breaking = len(r.Reasons) > 0
	r.Compatible = !r.Breaking
	for _, c := range r.Affected {
		switch c.Kind {
		case "WORKFLOW":
			r.AffectedWorkflows = append(r.AffectedWorkflows, c.ID)
		case "MAPPING":
			r.AffectedMappings = append(r.AffectedMappings, c.ID)
		case "IMPORT":
			r.AffectedImports = append(r.AffectedImports, c.ID)
		case "EXPORT":
			r.AffectedExports = append(r.AffectedExports, c.ID)
		case "CONSUMER":
			r.AffectedConsumers = append(r.AffectedConsumers, c.ID)
		}
	}
	sort.Strings(r.AffectedWorkflows)
	sort.Strings(r.AffectedMappings)
	sort.Strings(r.AffectedImports)
	sort.Strings(r.AffectedExports)
	sort.Strings(r.AffectedConsumers)
	return r, nil
}

// AnalyzeCompatibility is the descriptive alias used by registry clients.
func AnalyzeCompatibility(old, next TransformationDefinition, consumers []Consumer) (CompatibilityReport, error) {
	return CompareVersions(old, next, consumers)
}

func sameFields(a, b []Field) bool {
	if len(a) != len(b) {
		return false
	}
	ma, mb := map[string]Field{}, map[string]Field{}
	for _, f := range a {
		ma[f.Name] = f
	}
	for _, f := range b {
		mb[f.Name] = f
	}
	if len(ma) != len(mb) {
		return false
	}
	for n, f := range ma {
		g, ok := mb[n]
		if !ok || f.Type != g.Type || f.Required != g.Required {
			return false
		}
	}
	return true
}

// Registry stores immutable transformation releases and exact pins.
type Registry struct {
	releases map[string]map[int]VersionedTransformation
}

func NewRegistry() *Registry {
	return &Registry{releases: make(map[string]map[int]VersionedTransformation)}
}

func (r *Registry) Publish(d TransformationDefinition, revision int, consumers []Consumer) (VersionedTransformation, error) {
	if r == nil || revision < 1 {
		return VersionedTransformation{}, fmt.Errorf("%w: revision", ErrInvalidDefinition)
	}
	if err := d.Validate(); err != nil {
		return VersionedTransformation{}, err
	}
	digest, err := d.Digest()
	if err != nil {
		return VersionedTransformation{}, err
	}
	if r.releases == nil {
		r.releases = make(map[string]map[int]VersionedTransformation)
	}
	if r.releases[d.Name] == nil {
		r.releases[d.Name] = make(map[int]VersionedTransformation)
	}
	if _, exists := r.releases[d.Name][revision]; exists {
		return VersionedTransformation{}, ErrVersionExists
	}
	v := VersionedTransformation{Name: d.Name, Revision: revision, Definition: d, Digest: digest, State: ReleaseActive, Consumers: append([]Consumer(nil), consumers...)}
	r.releases[d.Name][revision] = v
	return v, nil
}

// PublishCompatible publishes only a non-breaking replacement after checking
// it against the currently registered revision.
func (r *Registry) PublishCompatible(previous, next TransformationDefinition, revision int, consumers []Consumer) (VersionedTransformation, CompatibilityReport, error) {
	report, err := CompareVersions(previous, next, consumers)
	if err != nil {
		return VersionedTransformation{}, report, err
	}
	if !report.Compatible {
		return VersionedTransformation{}, report, ErrIncompatibleVersion
	}
	v, err := r.Publish(next, revision, consumers)
	return v, report, err
}

func (r *Registry) Resolve(name string, revision int) (VersionedTransformation, error) {
	if r == nil {
		return VersionedTransformation{}, ErrVersionNotFound
	}
	versions := r.releases[name]
	v, ok := versions[revision]
	if !ok {
		return VersionedTransformation{}, ErrVersionNotFound
	}
	return v, nil
}

// Pin returns an immutable execution reference. Resolving the pin never
// follows the active release, so later publication cannot alter execution.
type Pin struct {
	Name     string `json:"name"`
	Revision int    `json:"revision"`
	Digest   string `json:"digest"`
}

func (r *Registry) Pin(name string, revision int) (Pin, error) {
	v, err := r.Resolve(name, revision)
	if err != nil {
		return Pin{}, fmt.Errorf("%w: %v", ErrPinnedVersion, err)
	}
	return Pin{Name: v.Name, Revision: v.Revision, Digest: v.Digest}, nil
}

func (r *Registry) ResolvePin(p Pin) (VersionedTransformation, error) {
	v, err := r.Resolve(p.Name, p.Revision)
	if err != nil {
		return VersionedTransformation{}, fmt.Errorf("%w: %v", ErrPinnedVersion, err)
	}
	if p.Digest != "" && p.Digest != v.Digest {
		return VersionedTransformation{}, fmt.Errorf("%w: digest mismatch", ErrPinnedVersion)
	}
	return v, nil
}
