package pagedef

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// SemanticRegion describes one member of the page anatomy vocabulary. The
// renderer owns the HTML implementation; this registry owns the semantic
// constraints a page author can rely on.
type SemanticRegion struct {
	Kind                            RegionKind   `json:"kind"`
	LandmarkRole                    string       `json:"landmark_role"`
	AllowedChildRegionKinds         []RegionKind `json:"allowed_child_region_kinds"`
	RequiredAccessibilityAttributes []string     `json:"required_accessibility_attributes"`
	MayHostLiveRegion               bool         `json:"may_host_live_region"`
}

// SemanticRegionRegistry is the versioned, renderer-independent semantic
// region vocabulary. It is deliberately separate from PageDefinition: pages
// select regions, while this registry defines what each selection means.
type SemanticRegionRegistry struct {
	Version string           `json:"version"`
	Regions []SemanticRegion `json:"regions"`
}

// SemanticRegionVocabulary returns the canonical WEB-004 registry.
func SemanticRegionVocabulary() SemanticRegionRegistry {
	return DefaultSemanticRegionRegistry()
}

// DefaultSemanticRegionRegistry returns a fresh copy of the canonical
// vocabulary. The returned slices may be safely changed by a caller.
func DefaultSemanticRegionRegistry() SemanticRegionRegistry {
	return SemanticRegionRegistry{
		Version: "1.0",
		Regions: []SemanticRegion{
			{Kind: RegionShell, LandmarkRole: "banner", AllowedChildRegionKinds: []RegionKind{RegionPageIdentity, RegionAuthorityContext, RegionLocalNavigation, RegionPrimary, RegionSupporting, RegionUtility, RegionCompletion}, RequiredAccessibilityAttributes: []string{"aria-label"}, MayHostLiveRegion: false},
			{Kind: RegionPageIdentity, LandmarkRole: "region", AllowedChildRegionKinds: []RegionKind{RegionUtility}, RequiredAccessibilityAttributes: []string{"aria-labelledby"}, MayHostLiveRegion: false},
			{Kind: RegionAuthorityContext, LandmarkRole: "region", AllowedChildRegionKinds: []RegionKind{RegionUtility}, RequiredAccessibilityAttributes: []string{"aria-label"}, MayHostLiveRegion: false},
			{Kind: RegionLocalNavigation, LandmarkRole: "navigation", AllowedChildRegionKinds: nil, RequiredAccessibilityAttributes: []string{"aria-label"}, MayHostLiveRegion: false},
			{Kind: RegionPrimary, LandmarkRole: "main", AllowedChildRegionKinds: []RegionKind{RegionSupporting, RegionUtility, RegionCompletion}, RequiredAccessibilityAttributes: []string{"aria-labelledby"}, MayHostLiveRegion: true},
			{Kind: RegionSupporting, LandmarkRole: "complementary", AllowedChildRegionKinds: []RegionKind{RegionUtility}, RequiredAccessibilityAttributes: []string{"aria-label"}, MayHostLiveRegion: true},
			{Kind: RegionUtility, LandmarkRole: "complementary", AllowedChildRegionKinds: nil, RequiredAccessibilityAttributes: []string{"aria-label"}, MayHostLiveRegion: true},
			{Kind: RegionCompletion, LandmarkRole: "contentinfo", AllowedChildRegionKinds: []RegionKind{RegionUtility}, RequiredAccessibilityAttributes: []string{"aria-label"}, MayHostLiveRegion: true},
		},
	}
}

// Validate checks the vocabulary's version, closed membership, semantic
// landmark roles, accessibility requirements, and child references.
func (r SemanticRegionRegistry) Validate() error {
	if r.Version == "" {
		return fmt.Errorf("semantic region registry has no version")
	}
	if len(r.Regions) != len(RegionKinds()) {
		return fmt.Errorf("semantic region registry has %d regions, want %d", len(r.Regions), len(RegionKinds()))
	}
	known := make(map[RegionKind]bool, len(RegionKinds()))
	for _, kind := range RegionKinds() {
		known[kind] = true
	}
	seen := make(map[RegionKind]bool, len(r.Regions))
	for _, region := range r.Regions {
		if !known[region.Kind] {
			return fmt.Errorf("semantic region %q is outside the closed RegionKind vocabulary", region.Kind)
		}
		if seen[region.Kind] {
			return fmt.Errorf("semantic region %q is declared more than once", region.Kind)
		}
		seen[region.Kind] = true
		if region.LandmarkRole == "" {
			return fmt.Errorf("semantic region %q has no landmark role", region.Kind)
		}
		if len(region.RequiredAccessibilityAttributes) == 0 {
			return fmt.Errorf("semantic region %q has no required accessibility attributes", region.Kind)
		}
		if err := validateUniqueStrings(region.Kind, "required accessibility attribute", region.RequiredAccessibilityAttributes); err != nil {
			return err
		}
		seenChildren := make(map[RegionKind]bool, len(region.AllowedChildRegionKinds))
		for _, child := range region.AllowedChildRegionKinds {
			if !known[child] {
				return fmt.Errorf("semantic region %q allows unknown child region %q", region.Kind, child)
			}
			if child == region.Kind {
				return fmt.Errorf("semantic region %q cannot contain itself", region.Kind)
			}
			if seenChildren[child] {
				return fmt.Errorf("semantic region %q repeats child region %q", region.Kind, child)
			}
			seenChildren[child] = true
		}
	}
	for _, kind := range RegionKinds() {
		if !seen[kind] {
			return fmt.Errorf("semantic region registry omits %q", kind)
		}
	}
	return nil
}

// Lookup returns one semantic region definition by kind.
func (r SemanticRegionRegistry) Lookup(kind RegionKind) (SemanticRegion, bool) {
	for _, region := range r.Regions {
		if region.Kind == kind {
			return cloneSemanticRegion(region), true
		}
	}
	return SemanticRegion{}, false
}

// Canonical returns the deterministic JSON encoding used by Digest.
func (r SemanticRegionRegistry) Canonical() []byte {
	copy := SemanticRegionRegistry{Version: r.Version, Regions: make([]SemanticRegion, len(r.Regions))}
	for i, region := range r.Regions {
		copy.Regions[i] = cloneSemanticRegion(region)
	}
	b, _ := json.Marshal(struct {
		Schema        string           `json:"schema"`
		SchemaVersion int              `json:"schema_version"`
		Version       string           `json:"version"`
		Regions       []SemanticRegion `json:"regions"`
	}{"hcmnext.uxqual.pagedef.semantic_region_registry", 1, copy.Version, copy.Regions})
	return b
}

// Digest returns the stable vocabulary digest.
func (r SemanticRegionRegistry) Digest() string {
	sum := sha256.Sum256(r.Canonical())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// SemanticRegionVocabularyDigest returns the digest pinned for the canonical
// WEB-004 registry.
func SemanticRegionVocabularyDigest() string { return SemanticRegionVocabulary().Digest() }

func validateUniqueStrings(kind RegionKind, label string, values []string) error {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" {
			return fmt.Errorf("semantic region %q has an empty %s", kind, label)
		}
		if seen[value] {
			return fmt.Errorf("semantic region %q repeats %s %q", kind, label, value)
		}
		seen[value] = true
	}
	return nil
}

func cloneSemanticRegion(region SemanticRegion) SemanticRegion {
	region.AllowedChildRegionKinds = append([]RegionKind(nil), region.AllowedChildRegionKinds...)
	region.RequiredAccessibilityAttributes = append([]string(nil), region.RequiredAccessibilityAttributes...)
	return region
}
