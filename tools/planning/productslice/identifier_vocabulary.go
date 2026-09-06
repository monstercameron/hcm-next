package productslice

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// IdentifierVocabularySchema and IdentifierVocabularySchemaVersion identify
// the checked-in shape of the cross-layer identifier vocabulary.
const (
	IdentifierVocabularySchema        = "hcmnext.planning.productslices.identifier-vocabulary"
	IdentifierVocabularySchemaVersion = 1
)

// IdentifierDefinition is one stable identifier contract shared by the
// presentation, business and persistence lenses. The owning layer is the
// authority for meaning and minting; other listed layers may only carry the
// identifier across a boundary.
type IdentifierDefinition struct {
	Kind          string   `yaml:"kind" json:"kind"`
	OwningLayer   string   `yaml:"owning_layer" json:"owning_layer"`
	CanonicalForm string   `yaml:"canonical_form" json:"canonical_form"`
	DigestRule    string   `yaml:"digest_rule" json:"digest_rule"`
	MintedBy      []string `yaml:"minted_by" json:"minted_by"`
	CarriedBy     []string `yaml:"carried_by" json:"carried_by"`
}

// IdentifierVocabulary is the versioned registry of cross-layer identifier
// kinds. Digest is over the schema and sorted rows with Digest omitted.
type IdentifierVocabulary struct {
	Schema        string                 `yaml:"schema" json:"schema"`
	SchemaVersion int                    `yaml:"schema_version" json:"schema_version"`
	Identifiers   []IdentifierDefinition `yaml:"identifiers" json:"identifiers"`
	Digest        string                 `yaml:"digest" json:"digest"`
}

// IdentifierRefusal is a typed refusal from vocabulary validation. It names
// both the identifier kind and the layer at which the contract is invalid so
// policy and audit output need not parse a free-form diagnostic.
type IdentifierRefusal struct {
	Kind   string
	Layer  string
	Reason string
	Detail string
	Cause  error
}

// IdentifierVocabularyRefusal is the descriptive name used by audit and
// policy callers that do not need to know the shorter implementation name.
type IdentifierVocabularyRefusal = IdentifierRefusal

var (
	ErrIdentifierOwnerless    = errors.New("productslice: identifier kind has no owner")
	ErrIdentifierDoublyOwned  = errors.New("productslice: identifier kind has multiple owners")
	ErrIdentifierInconsistent = errors.New("productslice: identifier kind is inconsistent")
	ErrIdentifierUnknown      = errors.New("productslice: identifier kind is not registered")
	ErrOwnerlessIdentifier    = ErrIdentifierOwnerless
	ErrDoublyOwnedIdentifier  = ErrIdentifierDoublyOwned
	ErrInconsistentIdentifier = ErrIdentifierInconsistent
)

func (e *IdentifierRefusal) Error() string {
	return fmt.Sprintf("productslice: identifier kind %q at layer %q: %s", e.Kind, e.Layer, e.Detail)
}

func (e *IdentifierRefusal) Unwrap() error { return e.Cause }

func refusal(kind, layer, detail string, cause error) error {
	return &IdentifierRefusal{Kind: kind, Layer: layer, Reason: cause.Error(), Detail: detail, Cause: cause}
}

// Validate rejects ownerless, doubly-owned and internally inconsistent rows.
// The owner must be the sole authority, must be allowed to mint, and must not
// appear in the carry-only set. Layer lists are treated as sets.
func (v IdentifierVocabulary) Validate() error {
	if v.Schema != IdentifierVocabularySchema {
		return refusal("<vocabulary>", "governance", fmt.Sprintf("schema %q is not %q", v.Schema, IdentifierVocabularySchema), ErrIdentifierInconsistent)
	}
	if v.SchemaVersion != IdentifierVocabularySchemaVersion {
		return refusal("<vocabulary>", "governance", fmt.Sprintf("schema version %d is not %d", v.SchemaVersion, IdentifierVocabularySchemaVersion), ErrIdentifierInconsistent)
	}
	seen := make(map[string]IdentifierDefinition, len(v.Identifiers))
	for _, row := range v.Identifiers {
		kind := strings.TrimSpace(row.Kind)
		if kind == "" {
			return refusal("<empty>", "<none>", "kind is required", ErrIdentifierOwnerless)
		}
		owner := strings.TrimSpace(row.OwningLayer)
		if owner == "" {
			return refusal(kind, "<none>", "owning_layer is required", ErrIdentifierOwnerless)
		}
		if strings.TrimSpace(row.CanonicalForm) == "" || strings.TrimSpace(row.DigestRule) == "" {
			return refusal(kind, owner, "canonical_form and digest_rule are required", ErrIdentifierInconsistent)
		}
		minted, err := layerSet(row.MintedBy, kind, owner, "minted_by")
		if err != nil {
			return err
		}
		carried, err := layerSet(row.CarriedBy, kind, owner, "carried_by")
		if err != nil {
			return err
		}
		if !minted[owner] {
			return refusal(kind, owner, "owning_layer must be allowed to mint the identifier", ErrIdentifierInconsistent)
		}
		if carried[owner] {
			return refusal(kind, owner, "owning_layer cannot also be carry-only", ErrIdentifierInconsistent)
		}
		if prior, exists := seen[kind]; exists {
			layers := prior.OwningLayer + "," + owner
			if prior.OwningLayer != owner {
				return refusal(kind, layers, "the identifier kind has two owning layers", ErrIdentifierDoublyOwned)
			}
			return refusal(kind, owner, "the identifier kind has duplicate or conflicting rows", ErrIdentifierInconsistent)
		}
		row.Kind = kind
		row.OwningLayer = owner
		row.MintedBy = sortedUnique(row.MintedBy)
		row.CarriedBy = sortedUnique(row.CarriedBy)
		seen[kind] = row
	}
	return nil
}

func layerSet(layers []string, kind, owner, field string) (map[string]bool, error) {
	set := make(map[string]bool, len(layers))
	for _, layer := range layers {
		layer = strings.TrimSpace(layer)
		if layer == "" {
			return nil, refusal(kind, owner, field+" contains an empty layer", ErrIdentifierInconsistent)
		}
		if set[layer] {
			return nil, refusal(kind, layer, field+" contains a duplicate layer", ErrIdentifierInconsistent)
		}
		set[layer] = true
	}
	return set, nil
}

func sortedUnique(values []string) []string {
	out := append([]string(nil), values...)
	for i := range out {
		out[i] = strings.TrimSpace(out[i])
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (v IdentifierVocabulary) sorted() IdentifierVocabulary {
	out := v
	out.Identifiers = append([]IdentifierDefinition(nil), v.Identifiers...)
	for i := range out.Identifiers {
		out.Identifiers[i].Kind = strings.TrimSpace(out.Identifiers[i].Kind)
		out.Identifiers[i].OwningLayer = strings.TrimSpace(out.Identifiers[i].OwningLayer)
		out.Identifiers[i].CanonicalForm = strings.TrimSpace(out.Identifiers[i].CanonicalForm)
		out.Identifiers[i].DigestRule = strings.TrimSpace(out.Identifiers[i].DigestRule)
		out.Identifiers[i].MintedBy = sortedUnique(out.Identifiers[i].MintedBy)
		out.Identifiers[i].CarriedBy = sortedUnique(out.Identifiers[i].CarriedBy)
	}
	sort.Slice(out.Identifiers, func(i, j int) bool { return out.Identifiers[i].Kind < out.Identifiers[j].Kind })
	return out
}

// Canonical returns deterministic JSON for the vocabulary, excluding Digest.
func (v IdentifierVocabulary) Canonical() []byte {
	s := v.sorted()
	b, err := json.Marshal(struct {
		Schema        string                 `json:"schema"`
		SchemaVersion int                    `json:"schema_version"`
		Identifiers   []IdentifierDefinition `json:"identifiers"`
	}{s.Schema, s.SchemaVersion, s.Identifiers})
	if err != nil {
		return nil
	}
	return b
}

// Digest returns the SHA-256 identity of the canonical vocabulary.
func (v IdentifierVocabulary) DigestValue() string {
	sum := sha256.Sum256(v.Canonical())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyDigest reports whether the checked-in digest matches the rows.
func (v IdentifierVocabulary) VerifyDigest() error {
	want := v.DigestValue()
	if v.Digest != want {
		return fmt.Errorf("productslice: identifier vocabulary digest=%q, want %q", v.Digest, want)
	}
	return nil
}

// Resolve returns exactly one row for kind. A missing or duplicate kind is a
// typed refusal naming the requested kind and the registry layer.
func (v IdentifierVocabulary) Resolve(kind string) (IdentifierDefinition, error) {
	var found IdentifierDefinition
	count := 0
	for _, row := range v.Identifiers {
		if row.Kind == kind {
			found = row
			count++
		}
	}
	if count == 0 {
		return IdentifierDefinition{}, refusal(kind, "governance", "no vocabulary row resolves this kind", ErrIdentifierUnknown)
	}
	if count != 1 {
		return IdentifierDefinition{}, refusal(kind, "governance", fmt.Sprintf("%d vocabulary rows resolve this kind", count), ErrIdentifierDoublyOwned)
	}
	return found, nil
}

// Explain returns bounded, audit-safe structure: it exposes no identifier
// values, canonical examples, or payload data that could become a disclosure
// channel in logs or UI summaries.
func (v IdentifierVocabulary) Explain() string {
	digest := v.Digest
	if digest == "" {
		digest = v.DigestValue()
	}
	return fmt.Sprintf("identifier vocabulary schema %d with %d kinds (%s)", v.SchemaVersion, len(v.Identifiers), digest)
}

// NewIdentifierVocabulary builds a canonical vocabulary from rows.
func NewIdentifierVocabulary(rows ...IdentifierDefinition) IdentifierVocabulary {
	v := IdentifierVocabulary{
		Schema:        IdentifierVocabularySchema,
		SchemaVersion: IdentifierVocabularySchemaVersion,
		Identifiers:   append([]IdentifierDefinition(nil), rows...),
	}
	v.Digest = v.DigestValue()
	return v
}

// LoadIdentifierVocabularyYAML loads a vocabulary fixture or checked-in
// registry. Validation and digest verification remain explicit operations.
func LoadIdentifierVocabularyYAML(path string) (IdentifierVocabulary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return IdentifierVocabulary{}, fmt.Errorf("productslice: reading identifier vocabulary %s: %w", path, err)
	}
	var v IdentifierVocabulary
	if err := yaml.Unmarshal(data, &v); err != nil {
		return IdentifierVocabulary{}, fmt.Errorf("productslice: parsing identifier vocabulary %s: %w", path, err)
	}
	return v, nil
}

// MarshalIdentifierVocabularyYAML recomputes the digest and renders the
// canonical YAML representation used by the fixture generator.
func MarshalIdentifierVocabularyYAML(v IdentifierVocabulary) ([]byte, error) {
	v.Digest = v.DigestValue()
	data, err := yaml.Marshal(v.sorted())
	if err != nil {
		return nil, fmt.Errorf("productslice: marshaling identifier vocabulary: %w", err)
	}
	return data, nil
}

// IdentifierKindsForRegistry returns the identifier-kind names represented by
// the real product-slice registry. Field names are the vocabulary join points;
// values remain references owned by their respective registries.
func IdentifierKindsForRegistry(r Registry) []string {
	seen := make(map[string]bool)
	var kinds []string
	add := func(kind string, present bool) {
		if present && !seen[kind] {
			seen[kind] = true
			kinds = append(kinds, kind)
		}
	}
	for _, slice := range r.Slices {
		add("slice_id", slice.SliceID != "")
		add("business_intent_ref", len(slice.BusinessIntents) > 0)
		add("feature_id", len(slice.Features) > 0)
		add("page_ref", len(slice.Pages) > 0)
		add("widget_ref", len(slice.Widgets) > 0)
		add("capability_id", len(slice.Capabilities) > 0)
		add("package_path", len(slice.Packages) > 0)
		add("todo_id", len(slice.Todos) > 0)
	}
	sort.Strings(kinds)
	return kinds
}

// ProductSliceIdentifierKinds is a descriptive alias for callers that read
// the conformance rule in product-slice language.
func ProductSliceIdentifierKinds(r Registry) []string { return IdentifierKindsForRegistry(r) }
