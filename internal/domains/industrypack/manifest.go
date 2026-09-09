// Package industrypack owns the pure manifest and composition contract for
// industry-specific product configuration. A pack names immutable references
// only; it never carries executable code, opens a database, or performs a
// provider call.
package industrypack

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

const schemaVersion = 1

// Version reports the manifest vocabulary version.
func Version() int { return schemaVersion }

// Explain describes the manifest contract without exposing reference values.
func Explain(pack IndustryPack) (Explanation, error) { return pack.Explain() }

var (
	ErrInvalidIndustryPack = errors.New("industrypack: invalid manifest")
	ErrInvalidReference    = errors.New("industrypack: invalid pinned reference")
	ErrUnpinnedReference   = errors.New("industrypack: reference is not pinned")
	ErrCompositionConflict = errors.New("industrypack: composition pins conflicting versions")
	ErrInvalidComposition  = errors.New("industrypack: invalid composition")

	// Short aliases are retained for callers that use the contract as a small
	// registry validator rather than as a manifest builder.
	ErrConflict = ErrCompositionConflict
	ErrUnpinned = ErrUnpinnedReference
)

// Industry is the closed industry vocabulary accepted by a manifest.
type Industry string

const (
	IndustryGeneral              Industry = "GENERAL"
	IndustryHealthcare           Industry = "HEALTHCARE"
	IndustryFinancialServices    Industry = "FINANCIAL_SERVICES"
	IndustryPublicSector         Industry = "PUBLIC_SECTOR"
	IndustryTechnology           Industry = "TECHNOLOGY"
	IndustryRetail               Industry = "RETAIL"
	IndustryManufacturing        Industry = "MANUFACTURING"
	IndustryProfessionalServices Industry = "PROFESSIONAL_SERVICES"
	IndustryNonprofit            Industry = "NONPROFIT"
	IndustryEducation            Industry = "EDUCATION"

	General              = IndustryGeneral
	Healthcare           = IndustryHealthcare
	FinancialServices    = IndustryFinancialServices
	PublicSector         = IndustryPublicSector
	Technology           = IndustryTechnology
	Retail               = IndustryRetail
	Manufacturing        = IndustryManufacturing
	ProfessionalServices = IndustryProfessionalServices
	Nonprofit            = IndustryNonprofit
	Education            = IndustryEducation
)

func (i Industry) Valid() bool {
	switch i {
	case IndustryGeneral, IndustryHealthcare, IndustryFinancialServices,
		IndustryPublicSector, IndustryTechnology, IndustryRetail,
		IndustryManufacturing, IndustryProfessionalServices, IndustryNonprofit,
		IndustryEducation:
		return true
	default:
		return false
	}
}

func (i Industry) String() string { return string(i) }

// PinnedRef identifies one immutable object in a pack. ID is the preferred
// spelling; Ref and Name are compatibility spellings for manifests produced by
// adjacent registries. Exactly one identity spelling may be populated.
type PinnedRef struct {
	ID      string `json:"id" yaml:"id"`
	Ref     string `json:"ref,omitempty" yaml:"ref,omitempty"`
	Name    string `json:"name,omitempty" yaml:"name,omitempty"`
	Version string `json:"version" yaml:"version"`
	Digest  string `json:"digest,omitempty" yaml:"digest,omitempty"`
}

type Reference = PinnedRef

// These aliases make the four manifest sections self-documenting without
// introducing four subtly different reference contracts.
type RulePackRef = PinnedRef
type ConfigurationObjectRef = PinnedRef
type WorkflowDefinitionRef = PinnedRef
type ProductSliceRef = PinnedRef

func (r PinnedRef) identity() string {
	for _, candidate := range []string{r.ID, r.Ref, r.Name} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	return ""
}

// Identity returns the normalized logical identity of the reference.
func (r PinnedRef) Identity() string { return r.identity() }

func (r PinnedRef) Validate() error {
	names := 0
	for _, candidate := range []string{r.ID, r.Ref, r.Name} {
		if strings.TrimSpace(candidate) != "" {
			names++
		}
	}
	if names == 0 {
		return fmt.Errorf("%w: id is required", ErrInvalidReference)
	}
	if names > 1 {
		return fmt.Errorf("%w: only one of id, ref, or name may be set", ErrInvalidReference)
	}
	if strings.TrimSpace(r.Version) == "" {
		return ErrUnpinnedReference
	}
	if strings.TrimSpace(r.Version) != r.Version || strings.IndexFunc(r.Version, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' }) >= 0 {
		return fmt.Errorf("%w: version contains whitespace", ErrInvalidReference)
	}
	if strings.TrimSpace(r.Digest) != r.Digest {
		return fmt.Errorf("%w: digest contains surrounding whitespace", ErrInvalidReference)
	}
	return nil
}

func (r PinnedRef) Canonical() []byte {
	if err := r.Validate(); err != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.industrypack.PinnedRef", schemaVersion).
		String("id", r.identity()).String("version", r.Version).String("digest", r.Digest).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// CompatibilityDeclaration constrains a named runtime or platform contract.
// Component is preferred; Target is a compatibility spelling.
type CompatibilityDeclaration struct {
	Component      string `json:"component" yaml:"component"`
	Target         string `json:"target,omitempty" yaml:"target,omitempty"`
	MinimumVersion string `json:"minimum_version,omitempty" yaml:"minimum_version,omitempty"`
	MaximumVersion string `json:"maximum_version,omitempty" yaml:"maximum_version,omitempty"`
}

type Compatibility = CompatibilityDeclaration

func (c CompatibilityDeclaration) identity() string {
	if strings.TrimSpace(c.Component) != "" {
		return strings.TrimSpace(c.Component)
	}
	return strings.TrimSpace(c.Target)
}

func (c CompatibilityDeclaration) Validate() error {
	if c.identity() == "" {
		return fmt.Errorf("compatibility component is required")
	}
	if c.Component != "" && c.Target != "" && strings.TrimSpace(c.Component) != strings.TrimSpace(c.Target) {
		return fmt.Errorf("compatibility component and target disagree")
	}
	if strings.TrimSpace(c.MinimumVersion) == "" && strings.TrimSpace(c.MaximumVersion) == "" {
		return fmt.Errorf("compatibility version range is required")
	}
	for name, version := range map[string]string{"minimum_version": c.MinimumVersion, "maximum_version": c.MaximumVersion} {
		if version != "" && (strings.TrimSpace(version) != version || strings.IndexFunc(version, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' }) >= 0) {
			return fmt.Errorf("%s contains whitespace", name)
		}
	}
	return nil
}

func (c CompatibilityDeclaration) Canonical() []byte {
	if c.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.industrypack.CompatibilityDeclaration", schemaVersion).
		String("component", c.identity()).String("minimum_version", c.MinimumVersion).
		String("maximum_version", c.MaximumVersion).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// IndustryPack is an immutable-by-value manifest revision. All composed
// content is represented by pinned references; arbitrary executable code has
// no field in this contract.
type IndustryPack struct {
	PackID   string   `json:"pack_id" yaml:"pack_id"`
	ID       string   `json:"id,omitempty" yaml:"id,omitempty"`
	Version  int      `json:"version" yaml:"version"`
	Industry Industry `json:"industry" yaml:"industry"`
	Owner    string   `json:"owner" yaml:"owner"`
	Scope    string   `json:"scope" yaml:"scope"`
	Support  string   `json:"support" yaml:"support"`

	ParentVersion int    `json:"parent_version,omitempty" yaml:"parent_version,omitempty"`
	ParentDigest  string `json:"parent_digest,omitempty" yaml:"parent_digest,omitempty"`

	Dependencies              []PinnedRef                `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	RulePackRefs              []PinnedRef                `json:"rule_pack_refs" yaml:"rule_pack_refs"`
	ConfigurationObjectRefs   []PinnedRef                `json:"configuration_object_refs" yaml:"configuration_object_refs"`
	WorkflowDefinitionRefs    []PinnedRef                `json:"workflow_definition_refs" yaml:"workflow_definition_refs"`
	ProductSliceRefs          []PinnedRef                `json:"product_slice_refs" yaml:"product_slice_refs"`
	FormRefs                  []PinnedRef                `json:"form_refs,omitempty" yaml:"form_refs,omitempty"`
	SkillRefs                 []PinnedRef                `json:"skill_refs,omitempty" yaml:"skill_refs,omitempty"`
	MetricRefs                []PinnedRef                `json:"metric_refs,omitempty" yaml:"metric_refs,omitempty"`
	Compatibility             []CompatibilityDeclaration `json:"compatibility" yaml:"compatibility"`
	CompatibilityDeclarations []CompatibilityDeclaration `json:"compatibility_declarations,omitempty" yaml:"compatibility_declarations,omitempty"`

	CanonicalDigest string `json:"canonical_digest,omitempty" yaml:"canonical_digest,omitempty"`
}

// Manifest is the concise name for an IndustryPack record.
type Manifest = IndustryPack

// NewIndustryPack validates, detaches, and digests a manifest.
func NewIndustryPack(pack IndustryPack) (IndustryPack, error) {
	pack = pack.clone()
	pack.CanonicalDigest = ""
	if err := pack.Validate(); err != nil {
		return IndustryPack{}, err
	}
	pack.CanonicalDigest = pack.computedDigest()
	return pack, nil
}

func New(pack IndustryPack) (IndustryPack, error) { return NewIndustryPack(pack) }

func (p IndustryPack) packID() string {
	if strings.TrimSpace(p.PackID) != "" {
		return strings.TrimSpace(p.PackID)
	}
	return strings.TrimSpace(p.ID)
}

// Validate checks the manifest shape and, when present, its recorded digest.
func (p IndustryPack) Validate() error {
	if err := p.validateWithoutDigest(); err != nil {
		return err
	}
	if p.CanonicalDigest != "" && p.CanonicalDigest != p.computedDigest() {
		return manifestError("canonical_digest", "", "digest mismatch", nil)
	}
	return nil
}

func (p IndustryPack) validateWithoutDigest() error {
	if p.packID() == "" {
		return manifestError("pack_id", "", "pack id is required", nil)
	}
	if p.PackID != "" && p.ID != "" && strings.TrimSpace(p.PackID) != strings.TrimSpace(p.ID) {
		return manifestError("pack_id", "", "pack id and id disagree", nil)
	}
	if p.Version <= 0 {
		return manifestError("version", "", "version must be positive", nil)
	}
	if !p.Industry.Valid() {
		return manifestError("industry", string(p.Industry), "industry is not declared", nil)
	}
	for field, value := range map[string]string{"owner": p.Owner, "scope": p.Scope, "support": p.Support} {
		if strings.TrimSpace(value) == "" {
			return manifestError(field, "", "field is required", nil)
		}
	}
	if p.Version == 1 && (p.ParentVersion != 0 || p.ParentDigest != "") {
		return manifestError("parent_version", "", "first revision cannot have a parent", nil)
	}
	if p.Version > 1 && (p.ParentVersion <= 0 || p.ParentVersion >= p.Version || strings.TrimSpace(p.ParentDigest) == "") {
		return manifestError("parent_version", "", "successor revision requires an earlier parent version and digest", nil)
	}
	if err := validateReferences("dependencies", p.Dependencies); err != nil {
		return err
	}
	if err := validateReferences("rule_pack_refs", p.RulePackRefs); err != nil {
		return err
	}
	if err := validateReferences("configuration_object_refs", p.ConfigurationObjectRefs); err != nil {
		return err
	}
	if err := validateReferences("workflow_definition_refs", p.WorkflowDefinitionRefs); err != nil {
		return err
	}
	if err := validateReferences("product_slice_refs", p.ProductSliceRefs); err != nil {
		return err
	}
	for _, section := range []struct {
		name string
		refs []PinnedRef
	}{
		{"form_refs", p.FormRefs}, {"skill_refs", p.SkillRefs}, {"metric_refs", p.MetricRefs},
	} {
		if err := validateReferences(section.name, section.refs); err != nil {
			return err
		}
	}
	compatibility := p.compatibility()
	if len(compatibility) == 0 {
		return manifestError("compatibility", "", "at least one compatibility declaration is required", nil)
	}
	for i, declaration := range compatibility {
		if err := declaration.Validate(); err != nil {
			return manifestError(fmt.Sprintf("compatibility[%d]", i), declaration.identity(), err.Error(), err)
		}
	}
	return nil
}

func validateReferences(field string, refs []PinnedRef) error {
	seen := make(map[string]struct{}, len(refs))
	for i, ref := range refs {
		if err := ref.Validate(); err != nil {
			return manifestError(fmt.Sprintf("%s[%d]", field, i), ref.identity(), err.Error(), err)
		}
		key := ref.identity() + "\x00" + ref.Version
		if _, exists := seen[key]; exists {
			return manifestError(field, ref.identity(), "duplicate pinned reference", nil)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func (p IndustryPack) compatibility() []CompatibilityDeclaration {
	if len(p.Compatibility) == 0 {
		return append([]CompatibilityDeclaration(nil), p.CompatibilityDeclarations...)
	}
	if len(p.CompatibilityDeclarations) == 0 {
		return append([]CompatibilityDeclaration(nil), p.Compatibility...)
	}
	out := append([]CompatibilityDeclaration(nil), p.Compatibility...)
	out = append(out, p.CompatibilityDeclarations...)
	return out
}

func (p IndustryPack) clone() IndustryPack {
	p.Dependencies = append([]PinnedRef(nil), p.Dependencies...)
	p.RulePackRefs = append([]PinnedRef(nil), p.RulePackRefs...)
	p.ConfigurationObjectRefs = append([]PinnedRef(nil), p.ConfigurationObjectRefs...)
	p.WorkflowDefinitionRefs = append([]PinnedRef(nil), p.WorkflowDefinitionRefs...)
	p.ProductSliceRefs = append([]PinnedRef(nil), p.ProductSliceRefs...)
	p.FormRefs = append([]PinnedRef(nil), p.FormRefs...)
	p.SkillRefs = append([]PinnedRef(nil), p.SkillRefs...)
	p.MetricRefs = append([]PinnedRef(nil), p.MetricRefs...)
	p.Compatibility = append([]CompatibilityDeclaration(nil), p.Compatibility...)
	p.CompatibilityDeclarations = append([]CompatibilityDeclaration(nil), p.CompatibilityDeclarations...)
	return p
}

func sortedRefs(refs []PinnedRef) []PinnedRef {
	out := append([]PinnedRef(nil), refs...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].identity() != out[j].identity() {
			return out[i].identity() < out[j].identity()
		}
		if out[i].Version != out[j].Version {
			return out[i].Version < out[j].Version
		}
		return out[i].Digest < out[j].Digest
	})
	return out
}

func sortedCompatibility(in []CompatibilityDeclaration) []CompatibilityDeclaration {
	out := append([]CompatibilityDeclaration(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].identity() != out[j].identity() {
			return out[i].identity() < out[j].identity()
		}
		if out[i].MinimumVersion != out[j].MinimumVersion {
			return out[i].MinimumVersion < out[j].MinimumVersion
		}
		return out[i].MaximumVersion < out[j].MaximumVersion
	})
	return out
}

func (p IndustryPack) body() []byte {
	if err := p.validateWithoutDigest(); err != nil {
		return nil
	}
	compatibility := sortedCompatibility(p.compatibility())
	w := canonicalbytes.New("hcmnext.domains.industrypack.IndustryPack", schemaVersion).
		String("pack_id", p.packID()).Int("version", int64(p.Version)).String("industry", string(p.Industry)).
		String("owner", p.Owner).String("scope", p.Scope).String("support", p.Support).
		Int("parent_version", int64(p.ParentVersion)).String("parent_digest", p.ParentDigest)
	sections := []struct {
		name string
		refs []PinnedRef
	}{
		{"dependency", sortedRefs(p.Dependencies)}, {"rule_pack", sortedRefs(p.RulePackRefs)},
		{"configuration_object", sortedRefs(p.ConfigurationObjectRefs)}, {"workflow_definition", sortedRefs(p.WorkflowDefinitionRefs)},
		{"product_slice", sortedRefs(p.ProductSliceRefs)}, {"form", sortedRefs(p.FormRefs)},
		{"skill", sortedRefs(p.SkillRefs)}, {"metric", sortedRefs(p.MetricRefs)},
	}
	for _, section := range sections {
		w.Count(section.name, len(section.refs))
		for _, ref := range section.refs {
			w.Value(section.name, ref)
		}
	}
	w.Count("compatibility", len(compatibility))
	for _, declaration := range compatibility {
		w.Value("compatibility", declaration)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (p IndustryPack) computedDigest() string {
	body := p.body()
	if body == nil {
		return ""
	}
	return canonicalbytes.Digest(body)
}

// Canonical returns the deterministic manifest bytes without its derived
// digest, or nil when the manifest is invalid.
func (p IndustryPack) Canonical() []byte { return p.body() }

// Digest returns the canonical digest of the manifest revision.
func (p IndustryPack) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return p.computedDigest(), nil
}

// Explanation is a bounded, audit-safe description of one manifest.
type Explanation struct {
	PackID                   string
	Version                  int
	Industry                 Industry
	DependencyCount          int
	RulePackCount            int
	ConfigurationObjectCount int
	WorkflowDefinitionCount  int
	ProductSliceCount        int
	CompatibilityCount       int
	Digest                   string
}

func (p IndustryPack) Explain() (Explanation, error) {
	if err := p.Validate(); err != nil {
		return Explanation{}, err
	}
	return Explanation{
		PackID: p.packID(), Version: p.Version, Industry: p.Industry,
		DependencyCount: len(p.Dependencies), RulePackCount: len(p.RulePackRefs),
		ConfigurationObjectCount: len(p.ConfigurationObjectRefs), WorkflowDefinitionCount: len(p.WorkflowDefinitionRefs),
		ProductSliceCount: len(p.ProductSliceRefs), CompatibilityCount: len(p.compatibility()),
		Digest: p.computedDigest(),
	}, nil
}

type compositionRef struct {
	Kind string
	Ref  PinnedRef
}

// Composition is the detached, deterministic result of checking a set of
// packs. It contains no executable content and is safe to retain by value.
type Composition struct {
	Packs           []IndustryPack
	References      []PinnedRef
	CanonicalDigest string
}

// CheckComposition refuses unpinned references and conflicting pins for one
// logical object. Reusing the same exact pin across packs is allowed.
func CheckComposition(packs ...IndustryPack) error {
	if len(packs) == 0 {
		return manifestError("packs", "", "at least one pack is required", ErrInvalidComposition)
	}
	packVersions := make(map[string]int, len(packs))
	objects := make(map[string]PinnedRef)
	for i, pack := range packs {
		if err := pack.Validate(); err != nil {
			return manifestError(fmt.Sprintf("packs[%d]", i), pack.packID(), err.Error(), err)
		}
		id := pack.packID()
		if prior, exists := packVersions[id]; exists && prior != pack.Version {
			return manifestError("packs", id, fmt.Sprintf("versions %d and %d are both pinned", prior, pack.Version), ErrCompositionConflict)
		}
		packVersions[id] = pack.Version
		for _, item := range packReferences(pack) {
			if err := item.Ref.Validate(); err != nil {
				return manifestError(item.Kind, item.Ref.identity(), err.Error(), err)
			}
			key := item.Kind + "\x00" + item.Ref.identity()
			if prior, exists := objects[key]; exists && prior.Version != item.Ref.Version {
				return manifestError(item.Kind, item.Ref.identity(), fmt.Sprintf("versions %q and %q are both pinned", prior.Version, item.Ref.Version), ErrCompositionConflict)
			}
			objects[key] = item.Ref
		}
	}
	return nil
}

// ValidateComposition is the slice-oriented spelling of CheckComposition.
func ValidateComposition(packs []IndustryPack) error { return CheckComposition(packs...) }

// Compose validates and returns a detached composition result.
func Compose(packs ...IndustryPack) (Composition, error) {
	if err := CheckComposition(packs...); err != nil {
		return Composition{}, err
	}
	refs := make([]PinnedRef, 0)
	for _, pack := range packs {
		for _, item := range packReferences(pack) {
			refs = append(refs, item.Ref)
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].identity() != refs[j].identity() {
			return refs[i].identity() < refs[j].identity()
		}
		return refs[i].Version < refs[j].Version
	})
	out := Composition{Packs: make([]IndustryPack, len(packs)), References: refs}
	for i, pack := range packs {
		out.Packs[i] = pack.clone()
	}
	w := canonicalbytes.New("hcmnext.domains.industrypack.Composition", schemaVersion).Count("packs", len(out.Packs))
	for _, pack := range out.Packs {
		w.Value("pack", pack)
	}
	out.CanonicalDigest, _ = w.Digest()
	return out, nil
}

func packReferences(pack IndustryPack) []compositionRef {
	refs := make([]compositionRef, 0,
		len(pack.Dependencies)+len(pack.RulePackRefs)+len(pack.ConfigurationObjectRefs)+
			len(pack.WorkflowDefinitionRefs)+len(pack.ProductSliceRefs)+len(pack.FormRefs)+
			len(pack.SkillRefs)+len(pack.MetricRefs))
	appendRefs := func(kind string, values []PinnedRef) {
		for _, ref := range values {
			refs = append(refs, compositionRef{Kind: kind, Ref: ref})
		}
	}
	appendRefs("dependency", pack.Dependencies)
	appendRefs("rule_pack", pack.RulePackRefs)
	appendRefs("configuration_object", pack.ConfigurationObjectRefs)
	appendRefs("workflow_definition", pack.WorkflowDefinitionRefs)
	appendRefs("product_slice", pack.ProductSliceRefs)
	appendRefs("form", pack.FormRefs)
	appendRefs("skill", pack.SkillRefs)
	appendRefs("metric", pack.MetricRefs)
	return refs
}

func manifestError(field, ref, detail string, cause error) error {
	return &ValidationError{Field: field, Ref: ref, Detail: detail, Cause: cause}
}

// ValidationError names the field and, when applicable, reference that caused
// a manifest or composition refusal.
type ValidationError struct {
	Field  string
	Ref    string
	Detail string
	Cause  error
}

func (e *ValidationError) Error() string {
	where := e.Field
	if e.Ref != "" {
		where += " " + fmt.Sprintf("%q", e.Ref)
	}
	if e.Cause != nil {
		return fmt.Sprintf("industrypack: %s: %s: %v", where, e.Detail, e.Cause)
	}
	return fmt.Sprintf("industrypack: %s: %s", where, e.Detail)
}

func (e *ValidationError) Unwrap() error {
	if e.Cause != nil {
		return e.Cause
	}
	return ErrInvalidIndustryPack
}
