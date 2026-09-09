package industrypack

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const bindingSchemaVersion = 1

// ContentKind identifies the three PACK-002 content families. The kind is
// part of the lookup key: a rule and a formula with the same local name are
// still different governed objects.
type ContentKind string

const (
	ContentReferenceData ContentKind = "REFERENCE_DATA"
	ContentRule          ContentKind = "RULE"
	ContentFormula       ContentKind = "FORMULA"
)

func (k ContentKind) Valid() bool {
	switch k {
	case ContentReferenceData, ContentRule, ContentFormula:
		return true
	default:
		return false
	}
}

func (k ContentKind) String() string { return string(k) }

// ContentRef is an exact, namespaced content identity. Version is deliberately
// text rather than an integer because registries may use semantic versions or
// a governed release token such as "2026.1".
type ContentRef struct {
	Kind      ContentKind
	Namespace string
	ID        string
	Version   string
}

// Reference is a short alias for callers building a binding fixture.
type ContentReference = ContentRef

func (r ContentRef) Validate() error {
	if !r.Kind.Valid() {
		return fmt.Errorf("%w: content kind %q", ErrInvalidBinding, r.Kind)
	}
	for field, value := range map[string]string{
		"namespace": r.Namespace,
		"id":        r.ID,
		"version":   r.Version,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidBinding, field)
		}
		if strings.TrimSpace(value) != value || strings.IndexFunc(value, func(c rune) bool {
			return c == '/' || c == '\\' || c == ' ' || c == '\t' || c == '\r' || c == '\n'
		}) >= 0 {
			return fmt.Errorf("%w: %s contains a separator or whitespace", ErrInvalidBinding, field)
		}
	}
	return nil
}

// NamespacedID is the stable logical identity without a version.
func (r ContentRef) NamespacedID() string { return r.Namespace + "/" + r.ID }

// Key is the stable exact identity used by the binder.
func (r ContentRef) Key() string { return r.Kind.String() + "|" + r.NamespacedID() + "@" + r.Version }

// EffectiveWindow is a half-open [Start, End) window. An absent end is open.
type EffectiveWindow struct {
	Start  values.LocalDate
	End    values.LocalDate
	HasEnd bool
}

func OpenWindow(start values.LocalDate) EffectiveWindow { return EffectiveWindow{Start: start} }

func ClosedWindow(start, end values.LocalDate) EffectiveWindow {
	return EffectiveWindow{Start: start, End: end, HasEnd: true}
}

func (w EffectiveWindow) Validate() error {
	if err := w.Start.Validate(); err != nil {
		return fmt.Errorf("%w: effective start: %v", ErrInvalidBinding, err)
	}
	if w.HasEnd {
		if err := w.End.Validate(); err != nil {
			return fmt.Errorf("%w: effective end: %v", ErrInvalidBinding, err)
		}
		if w.Start.Compare(w.End) >= 0 {
			return fmt.Errorf("%w: effective window [%s,%s) is empty or inverted", ErrInvalidBinding, w.Start, w.End)
		}
	}
	return nil
}

func (w EffectiveWindow) String() string {
	if w.HasEnd {
		return "[" + w.Start.String() + "," + w.End.String() + ")"
	}
	return "[" + w.Start.String() + ",)"
}

// Contains reports whether date is inside the half-open window.
func (w EffectiveWindow) Contains(date values.LocalDate) bool {
	if w.Validate() != nil || date.Validate() != nil || date.Compare(w.Start) < 0 {
		return false
	}
	return !w.HasEnd || date.Compare(w.End) < 0
}

// Content is an immutable registry entry. Digest identifies the exact
// published body; this package intentionally never interprets the body.
// Dependencies are exact refs and are closed transitively by Bind.
type Content struct {
	Ref          ContentRef
	Effective    EffectiveWindow
	Digest       string
	Dependencies []ContentRef
	// Overridable records the published content's capability metadata. A
	// binding still requires the supplying pack's OverridableRefs declaration;
	// this bit alone never authorizes an override.
	Overridable bool
	OverrideOf  *ContentRef
}

// ContentEntry is a descriptive alias used by registry-oriented callers.
type ContentEntry = Content

func (c Content) Validate() error {
	if err := c.Ref.Validate(); err != nil {
		return err
	}
	if err := c.Effective.Validate(); err != nil {
		return fmt.Errorf("%s: %w", c.Ref.NamespacedID(), err)
	}
	if strings.TrimSpace(c.Digest) == "" {
		return fmt.Errorf("%w: %s has no content digest", ErrInvalidBinding, c.Ref.NamespacedID())
	}
	for _, dep := range c.Dependencies {
		if err := dep.Validate(); err != nil {
			return fmt.Errorf("%s dependency: %w", c.Ref.NamespacedID(), err)
		}
	}
	if c.OverrideOf != nil {
		if err := c.OverrideOf.Validate(); err != nil {
			return fmt.Errorf("%s override: %w", c.Ref.NamespacedID(), err)
		}
		if c.OverrideOf.Kind != c.Ref.Kind || c.OverrideOf.Namespace != c.Ref.Namespace || c.OverrideOf.ID != c.Ref.ID {
			return fmt.Errorf("%w: %s overrides a different logical identity %s", ErrIllegalOverride, c.Ref.NamespacedID(), c.OverrideOf.NamespacedID())
		}
	}
	return nil
}

// Precedence declares that HigherPack wins over LowerPack for a colliding
// logical content identity. A direct or transitive declaration is required;
// slice order is never treated as precedence.
type Precedence struct {
	HigherPack string
	LowerPack  string
}

// Pack is a manifest plus its immutable content entries. References is the
// explicit PACK-002 declaration. When it is empty, the PACK-001 rule and
// configuration reference sections are projected into rule and reference-data
// refs respectively; formulas must be listed explicitly because PACK-001 has
// no formula-specific section yet.
type Pack struct {
	Manifest IndustryPack
	Contents []Content

	References      []ContentRef
	OverridableRefs []ContentRef
	Precedence      []Precedence
}

// PackSource and BindingInput are descriptive aliases for the same pure
// binding input shape.
type PackSource = Pack

type BindingSpec struct {
	Packs    []Pack
	Contents []Content
}

type BindingInput = BindingSpec

type bindingCandidate struct {
	content Content
	pack    string
	version int
	global  bool
}

var (
	ErrInvalidBinding       = errors.New("industrypack: invalid binding")
	ErrUnresolvedID         = errors.New("industrypack: unresolved content id")
	ErrUnresolvedVersion    = errors.New("industrypack: unresolved content version")
	ErrIllegalOverride      = errors.New("industrypack: illegal content override")
	ErrAmbiguousPrecedence  = errors.New("industrypack: ambiguous content precedence")
	ErrDependencyIncomplete = errors.New("industrypack: incomplete content dependency")
)

// Refusal is the common typed refusal shape. Ref, when present, is always the
// exact namespaced id/version that caused the refusal, making a denial safe to
// place in an audit record without exposing content bodies.
type Refusal struct {
	Ref    ContentRef
	PackID string
	Detail string
	Cause  error
}

func (e *Refusal) Error() string {
	where := e.Ref.NamespacedID()
	if e.Ref.Version != "" {
		where += "@" + e.Ref.Version
	}
	if e.PackID != "" {
		where += " (pack " + e.PackID + ")"
	}
	if e.Detail == "" {
		return fmt.Sprintf("%v: %s", e.Cause, where)
	}
	return fmt.Sprintf("%v: %s: %s", e.Cause, where, e.Detail)
}

func (e *Refusal) Unwrap() error { return e.Cause }

// BoundContent is the selected, exact entry in a Binding. It contains no
// executable or body data, only the provenance needed to reproduce a binding.
type BoundContent struct {
	Content
	PackID      string
	PackVersion int
}

// Binding is the detached, deterministic closure of the content named by one
// or more pack manifests.
type Binding struct {
	Packs           []IndustryPack
	Contents        []BoundContent
	CanonicalDigest string
}

// Bind resolves every declared ref, checks override and precedence policy,
// closes dependencies, and returns a detached binding. It performs no I/O.
func Bind(spec BindingSpec) (Binding, error) {
	if len(spec.Packs) == 0 {
		return Binding{}, fmt.Errorf("%w: at least one pack is required", ErrInvalidBinding)
	}

	byExact := make(map[string][]bindingCandidate)
	byLogical := make(map[string][]bindingCandidate)
	packNames := make(map[string]struct{}, len(spec.Packs))
	orders := make([]Precedence, 0)
	declaredOverride := make(map[string]bool)

	for _, pack := range spec.Packs {
		if err := pack.Manifest.Validate(); err != nil {
			return Binding{}, &Refusal{PackID: pack.Manifest.packID(), Detail: err.Error(), Cause: ErrInvalidBinding}
		}
		packID := pack.Manifest.packID()
		if _, exists := packNames[packID]; exists {
			return Binding{}, &Refusal{PackID: packID, Detail: "pack id is supplied more than once", Cause: ErrAmbiguousPrecedence}
		}
		packNames[packID] = struct{}{}
		orders = append(orders, pack.Precedence...)
		for _, ref := range pack.OverridableRefs {
			if err := ref.Validate(); err != nil {
				return Binding{}, &Refusal{Ref: ref, PackID: packID, Detail: err.Error(), Cause: ErrInvalidBinding}
			}
			declaredOverride[ref.Key()] = true
		}
		for _, content := range pack.Contents {
			if err := content.Validate(); err != nil {
				return Binding{}, &Refusal{Ref: content.Ref, PackID: packID, Detail: err.Error(), Cause: causeOfContent(err)}
			}
			c := bindingCandidate{content: cloneContent(content), pack: packID, version: pack.Manifest.Version}
			byExact[content.Ref.Key()] = append(byExact[content.Ref.Key()], c)
			logicalKey := content.Ref.Kind.String() + "|" + content.Ref.NamespacedID()
			byLogical[logicalKey] = append(byLogical[logicalKey], c)
		}
	}
	for _, content := range spec.Contents {
		if err := content.Validate(); err != nil {
			return Binding{}, &Refusal{Ref: content.Ref, Detail: err.Error(), Cause: causeOfContent(err)}
		}
		c := bindingCandidate{content: cloneContent(content), pack: "registry", global: true}
		byExact[content.Ref.Key()] = append(byExact[content.Ref.Key()], c)
		logicalKey := content.Ref.Kind.String() + "|" + content.Ref.NamespacedID()
		byLogical[logicalKey] = append(byLogical[logicalKey], c)
	}

	higher := precedenceClosure(orders)
	declared := func(pack Pack) []ContentRef {
		if len(pack.References) != 0 {
			return append([]ContentRef(nil), pack.References...)
		}
		refs := make([]ContentRef, 0, len(pack.Manifest.RulePackRefs)+len(pack.Manifest.ConfigurationObjectRefs))
		for _, ref := range pack.Manifest.RulePackRefs {
			refs = append(refs, ContentRef{Kind: ContentRule, Namespace: refNamespace(ref), ID: ref.Identity(), Version: ref.Version})
		}
		for _, ref := range pack.Manifest.ConfigurationObjectRefs {
			refs = append(refs, ContentRef{Kind: ContentReferenceData, Namespace: refNamespace(ref), ID: ref.Identity(), Version: ref.Version})
		}
		return refs
	}

	overridable := make(map[string]bool)
	for key := range declaredOverride {
		overridable[key] = true
	}
	for _, group := range byLogical {
		for _, item := range group {
			if item.content.OverrideOf == nil {
				continue
			}
			target := item.content.OverrideOf
			if len(byExact[target.Key()]) == 0 {
				return Binding{}, &Refusal{Ref: *target, PackID: item.pack, Detail: "override target is not registered", Cause: ErrUnresolvedID}
			}
			if !overridable[target.Key()] {
				return Binding{}, &Refusal{Ref: item.content.Ref, PackID: item.pack, Detail: "manifest does not declare the target overridable", Cause: ErrIllegalOverride}
			}
		}
	}

	for _, group := range byLogical {
		if len(group) < 2 {
			continue
		}
		for left := 0; left < len(group); left++ {
			for right := left + 1; right < len(group); right++ {
				if group[left].global || group[right].global || group[left].pack == group[right].pack {
					continue
				}
				if group[left].content.OverrideOf != nil && !overridable[group[left].content.OverrideOf.Key()] {
					return Binding{}, &Refusal{Ref: group[left].content.Ref, Detail: "manifest does not declare the target overridable", Cause: ErrIllegalOverride}
				}
				if group[right].content.OverrideOf != nil && !overridable[group[right].content.OverrideOf.Key()] {
					return Binding{}, &Refusal{Ref: group[right].content.Ref, Detail: "manifest does not declare the target overridable", Cause: ErrIllegalOverride}
				}
				if !ordered(higher, group[left].pack, group[right].pack) && !ordered(higher, group[right].pack, group[left].pack) {
					return Binding{}, &Refusal{Ref: group[left].content.Ref, Detail: "supplied by " + group[left].pack + " and " + group[right].pack + " without declared order", Cause: ErrAmbiguousPrecedence}
				}
			}
		}
	}

	rootGroups := make(map[string][]bindingCandidate)
	for _, pack := range spec.Packs {
		refs := declared(pack)
		for _, ref := range refs {
			if err := ref.Validate(); err != nil {
				return Binding{}, &Refusal{Ref: ref, PackID: pack.Manifest.packID(), Detail: err.Error(), Cause: ErrInvalidBinding}
			}
			candidates := byExact[ref.Key()]
			if len(candidates) == 0 {
				logical := byLogical[ref.Kind.String()+"|"+ref.NamespacedID()]
				cause := ErrUnresolvedID
				detail := "no content entry is registered"
				if len(logical) != 0 {
					cause = ErrUnresolvedVersion
					detail = "content id exists but the requested version is not registered"
				}
				return Binding{}, &Refusal{Ref: ref, PackID: pack.Manifest.packID(), Detail: detail, Cause: cause}
			}
			chosen, err := chooseCandidate(candidates, higher, overridable)
			if err != nil {
				return Binding{}, &Refusal{Ref: ref, PackID: pack.Manifest.packID(), Detail: err.Error(), Cause: causeOfContent(err)}
			}
			logicalKey := ref.Kind.String() + "|" + ref.NamespacedID()
			rootGroups[logicalKey] = append(rootGroups[logicalKey], chosen)
		}
	}
	roots := make([]ContentRef, 0, len(rootGroups))
	for _, candidates := range rootGroups {
		chosen, err := chooseCandidate(candidates, higher, overridable)
		if err != nil {
			return Binding{}, &Refusal{Ref: candidates[0].content.Ref, Detail: err.Error(), Cause: causeOfContent(err)}
		}
		roots = append(roots, chosen.content.Ref)
	}

	selected := make(map[string]bindingCandidate)
	visiting := make(map[string]bool)
	var visit func(ContentRef, string) error
	visit = func(ref ContentRef, requestingPack string) error {
		if err := ref.Validate(); err != nil {
			return &Refusal{Ref: ref, PackID: requestingPack, Detail: err.Error(), Cause: ErrInvalidBinding}
		}
		key := ref.Key()
		if _, ok := selected[key]; ok {
			return nil
		}
		if visiting[key] {
			return &Refusal{Ref: ref, PackID: requestingPack, Detail: "dependency cycle", Cause: ErrDependencyIncomplete}
		}
		visiting[key] = true
		defer delete(visiting, key)
		candidates := byExact[key]
		if len(candidates) == 0 {
			logical := byLogical[ref.Kind.String()+"|"+ref.NamespacedID()]
			cause := ErrUnresolvedID
			detail := "no content entry is registered"
			if len(logical) != 0 {
				cause = ErrUnresolvedVersion
				detail = "content id exists but the requested version is not registered"
			}
			return &Refusal{Ref: ref, PackID: requestingPack, Detail: detail, Cause: cause}
		}
		chosen, err := chooseCandidate(candidates, higher, overridable)
		if err != nil {
			return &Refusal{Ref: ref, PackID: requestingPack, Detail: err.Error(), Cause: causeOfContent(err)}
		}
		selected[key] = chosen
		for _, dep := range chosen.content.Dependencies {
			if err := visit(dep, chosen.pack); err != nil {
				return err
			}
		}
		return nil
	}
	for _, root := range roots {
		if err := visit(root, ""); err != nil {
			return Binding{}, err
		}
	}

	contents := make([]BoundContent, 0, len(selected))
	for _, item := range selected {
		contents = append(contents, BoundContent{Content: item.content, PackID: item.pack, PackVersion: item.version})
	}
	sort.Slice(contents, func(i, j int) bool { return contents[i].Content.Ref.Key() < contents[j].Content.Ref.Key() })
	out := Binding{Contents: contents}
	for _, pack := range spec.Packs {
		out.Packs = append(out.Packs, pack.Manifest)
	}
	digest, err := out.computeDigest()
	if err != nil {
		return Binding{}, err
	}
	out.CanonicalDigest = digest
	return out, nil
}

func NewBinding(spec BindingSpec) (Binding, error) { return Bind(spec) }

func BindPacks(packs ...Pack) (Binding, error) { return Bind(BindingSpec{Packs: packs}) }

func causeOfContent(err error) error {
	for _, cause := range []error{ErrIllegalOverride, ErrAmbiguousPrecedence, ErrDependencyIncomplete} {
		if errors.Is(err, cause) {
			return cause
		}
	}
	return ErrInvalidBinding
}

func refNamespace(ref PinnedRef) string {
	identity := ref.Identity()
	if slash := strings.IndexByte(identity, '/'); slash >= 0 {
		return identity[:slash]
	}
	return "hcmnext"
}

func cloneContent(in Content) Content {
	out := in
	out.Dependencies = append([]ContentRef(nil), in.Dependencies...)
	if in.OverrideOf != nil {
		ref := *in.OverrideOf
		out.OverrideOf = &ref
	}
	return out
}

func precedenceClosure(declarations []Precedence) map[string]map[string]bool {
	out := make(map[string]map[string]bool)
	for _, p := range declarations {
		if p.HigherPack == "" || p.LowerPack == "" || p.HigherPack == p.LowerPack {
			continue
		}
		if out[p.HigherPack] == nil {
			out[p.HigherPack] = make(map[string]bool)
		}
		out[p.HigherPack][p.LowerPack] = true
	}
	changed := true
	for changed {
		changed = false
		for _, lowers := range out {
			for lower := range lowers {
				for transitive := range out[lower] {
					if !lowers[transitive] {
						lowers[transitive] = true
						changed = true
					}
				}
			}
		}
	}
	return out
}

func ordered(graph map[string]map[string]bool, higher, lower string) bool {
	return graph[higher][lower]
}

func chooseCandidate(candidates []bindingCandidate, higher map[string]map[string]bool, overridable map[string]bool) (bindingCandidate, error) {
	if len(candidates) == 1 {
		if err := validateOverride(candidates[0], overridable); err != nil {
			return bindingCandidate{}, err
		}
		return candidates[0], nil
	}
	for _, candidate := range candidates {
		if err := validateOverride(candidate, overridable); err != nil {
			return bindingCandidate{}, err
		}
	}
	winners := make([]bindingCandidate, 0, len(candidates))
	for i, candidate := range candidates {
		if candidate.global {
			continue
		}
		winsAll := true
		for j, other := range candidates {
			if i == j || other.global || candidate.pack == other.pack {
				continue
			}
			if !ordered(higher, candidate.pack, other.pack) {
				winsAll = false
				break
			}
		}
		if winsAll {
			winners = append(winners, candidate)
		}
	}
	if len(winners) == 1 {
		return winners[0], nil
	}
	if len(winners) == 0 && len(candidates) == 1 {
		return candidates[0], nil
	}
	return bindingCandidate{}, fmt.Errorf("%w: %s", ErrAmbiguousPrecedence, candidates[0].content.Ref.NamespacedID())
}

func validateOverride(candidate bindingCandidate, overridable map[string]bool) error {
	if candidate.content.OverrideOf == nil {
		return nil
	}
	if !overridable[candidate.content.OverrideOf.Key()] {
		return fmt.Errorf("%w: %s targets %s but that manifest did not declare it overridable", ErrIllegalOverride, candidate.content.Ref.NamespacedID(), candidate.content.OverrideOf.NamespacedID())
	}
	return nil
}

func (b Binding) computeDigest() (string, error) {
	w := canonicalbytes.New("hcmnext.domains.industrypack.Binding", bindingSchemaVersion)
	packs := append([]IndustryPack(nil), b.Packs...)
	sort.Slice(packs, func(i, j int) bool {
		if packs[i].packID() != packs[j].packID() {
			return packs[i].packID() < packs[j].packID()
		}
		return packs[i].Version < packs[j].Version
	})
	w.Count("pack", len(packs))
	for _, pack := range packs {
		w.String("pack_id", pack.packID()).Int("pack_version", int64(pack.Version))
	}
	w.Count("content", len(b.Contents))
	for _, item := range b.Contents {
		c := item.Content
		w.String("kind", c.Ref.Kind.String()).String("namespace", c.Ref.Namespace).String("id", c.Ref.ID).String("version", c.Ref.Version)
		w.String("effective", c.Effective.String()).String("digest", c.Digest).String("pack_id", item.PackID).Int("pack_version", int64(item.PackVersion)).Bool("overridable", c.Overridable)
		if c.OverrideOf == nil {
			w.Bool("has_override", false)
		} else {
			w.Bool("has_override", true).String("override_kind", c.OverrideOf.Kind.String()).String("override_namespace", c.OverrideOf.Namespace).String("override_id", c.OverrideOf.ID).String("override_version", c.OverrideOf.Version)
		}
		deps := append([]ContentRef(nil), c.Dependencies...)
		sort.Slice(deps, func(i, j int) bool { return deps[i].Key() < deps[j].Key() })
		w.Count("dependency", len(deps))
		for _, dep := range deps {
			w.String("dependency_ref", dep.Key())
		}
	}
	return w.Digest()
}

// Digest recomputes the binding digest and validates the stored digest when
// one is present.
func (b Binding) Digest() (string, error) {
	digest, err := b.computeDigest()
	if err != nil {
		return "", err
	}
	if b.CanonicalDigest != "" && b.CanonicalDigest != digest {
		return "", fmt.Errorf("%w: binding digest mismatch", ErrInvalidBinding)
	}
	return digest, nil
}

func (b Binding) ComputeDigest() string {
	digest, _ := b.computeDigest()
	return digest
}

// BindingExplanation is intentionally metadata-only: it names no rule body,
// formula expression or reference value.
type BindingExplanation struct {
	PackCount          int
	ContentCount       int
	ReferenceDataCount int
	RuleCount          int
	FormulaCount       int
	Digest             string
}

func (b Binding) Explain() (BindingExplanation, error) {
	digest, err := b.Digest()
	if err != nil {
		return BindingExplanation{}, err
	}
	explanation := BindingExplanation{PackCount: len(b.Packs), ContentCount: len(b.Contents), Digest: digest}
	for _, item := range b.Contents {
		switch item.Ref.Kind {
		case ContentReferenceData:
			explanation.ReferenceDataCount++
		case ContentRule:
			explanation.RuleCount++
		case ContentFormula:
			explanation.FormulaCount++
		}
	}
	return explanation, nil
}

func ExplainBinding(b Binding) (BindingExplanation, error) { return b.Explain() }
