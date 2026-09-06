// Package intentmanifests provides feature normalization and intent classification.
package intentmanifests

import (
	"fmt"
	"sort"
	"strings"
)

// SemanticClassification represents the semantic role of a feature.
type SemanticClassification string

const (
	ClassCreate       SemanticClassification = "CREATE"
	ClassConsume      SemanticClassification = "CONSUME"
	ClassEmitChild    SemanticClassification = "EMIT_CHILD"
	ClassObserve      SemanticClassification = "OBSERVE"
	ClassNonMaterial  SemanticClassification = "NON_MATERIAL"
	ClassReviewNeeded SemanticClassification = "REVIEW_REQUIRED"
)

// ValidSemanticClassifications is the closed set of allowed classifications.
var ValidSemanticClassifications = map[SemanticClassification]bool{
	ClassCreate:       true,
	ClassConsume:      true,
	ClassEmitChild:    true,
	ClassObserve:      true,
	ClassNonMaterial:  true,
	ClassReviewNeeded: true,
}

// NormalizedFeature represents a normalized feature identity with aliases and classification.
type NormalizedFeature struct {
	// Canonical identity
	FeatureID string
	Label     string

	// Semantic classification
	Classification SemanticClassification

	// Domain and actor context
	Domain    string
	ActorRole string
	Channel   string

	// Aliases that map to this normalized identity
	Aliases []string

	// Source provenance: must preserve exact intake entry and source reference
	IntakeLabel   string
	IntakeGroup   int
	SourceGroupID int
	SourceRef     string

	// Material operation check
	IsMaterial bool
}

// FeatureNormalizer normalizes feature identities and manages aliases.
type FeatureNormalizer struct {
	// Map from intake label (with group context) to normalized identity
	labelToNormalized map[string]*NormalizedFeature

	// Map from feature_id to normalized feature (for duplicate detection)
	featureIDMap map[string]*NormalizedFeature

	// Reverse alias map: alias -> canonical feature_id (for ambiguity detection)
	aliasIndex map[string]string

	// Track which aliases appear in multiple domains (ambiguous)
	aliasConflicts map[string][]string
}

// NewFeatureNormalizer creates a fresh normalizer.
func NewFeatureNormalizer() *FeatureNormalizer {
	return &FeatureNormalizer{
		labelToNormalized: make(map[string]*NormalizedFeature),
		featureIDMap:      make(map[string]*NormalizedFeature),
		aliasIndex:        make(map[string]string),
		aliasConflicts:    make(map[string][]string),
	}
}

// AddNormalizedFeature registers a normalized feature, rejecting:
// - duplicate feature_id
// - ambiguous aliases (same alias in different domains without context)
// - unknown classifications
// - material operations classified as non-material mechanics without justification
func (fn *FeatureNormalizer) AddNormalizedFeature(nf *NormalizedFeature) error {
	if nf == nil {
		return fmt.Errorf("normalized feature is nil")
	}

	// Validate semantic classification.
	if !ValidSemanticClassifications[nf.Classification] {
		return fmt.Errorf("feature %s: unknown classification %q", nf.FeatureID, nf.Classification)
	}

	// Reject duplicate feature_id.
	if existing, ok := fn.featureIDMap[nf.FeatureID]; ok {
		return fmt.Errorf("feature_id %s already registered (intake group %d vs %d)",
			nf.FeatureID, existing.IntakeGroup, nf.IntakeGroup)
	}

	// Check for alias conflicts across domains.
	for _, alias := range nf.Aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}

		if existing, ok := fn.aliasIndex[alias]; ok {
			// Same alias in different domains is ambiguous unless explicitly qualified.
			existingFeature := fn.featureIDMap[existing]
			if existingFeature.Domain != nf.Domain {
				fn.aliasConflicts[alias] = append(fn.aliasConflicts[alias], existing, nf.FeatureID)
				return fmt.Errorf("alias %q maps to multiple domains: %s (domain=%s) and %s (domain=%s)",
					alias, existing, existingFeature.Domain, nf.FeatureID, nf.Domain)
			}
		}

		fn.aliasIndex[alias] = nf.FeatureID
	}

	// Register the feature.
	fn.featureIDMap[nf.FeatureID] = nf

	// Register by intake label (group + label for source traceability).
	labelKey := fmt.Sprintf("g%d:%s", nf.IntakeGroup, nf.IntakeLabel)
	fn.labelToNormalized[labelKey] = nf

	return nil
}

// RejectFalseMerge detects when the same label in different domains is incorrectly merged.
func (fn *FeatureNormalizer) RejectFalseMerge(label string, domain1, domain2 string) error {
	if domain1 == domain2 {
		return nil // Same domain, no merge issue.
	}

	// Check if features with this label exist in both domains.
	for _, nf := range fn.featureIDMap {
		if nf.IntakeLabel == label && (nf.Domain == domain1 || nf.Domain == domain2) {
			// If both domains are represented, this could be a false merge.
			var domains []string
			for _, nf2 := range fn.featureIDMap {
				if nf2.IntakeLabel == label && nf2.Domain != "" {
					domains = append(domains, nf2.Domain)
				}
			}
			if len(domains) > 1 {
				return fmt.Errorf("label %q appears in multiple domains %v without qualified domain context",
					label, domains)
			}
		}
	}
	return nil
}

// RejectUIWording detects when UI wording becomes a new semantic identity incorrectly.
// This checks that the normalized identity reflects business semantics, not UI text variations.
func (fn *FeatureNormalizer) RejectUIWording(intakeLabel string, normalizedLabel string) error {
	// UI wording variations (e.g., "Approve the proposal" vs "Approve Proposal") should
	// map to the same semantic identity. If they don't, the normalization has incorrectly
	// created multiple semantic identities from UI text.
	if intakeLabel != normalizedLabel {
		// Allow minor normalization (trim, case, punctuation).
		normalized := strings.ToLower(strings.TrimSpace(intakeLabel))
		normalizedRef := strings.ToLower(strings.TrimSpace(normalizedLabel))
		if normalized == normalizedRef {
			return nil // Just case/whitespace difference, acceptable.
		}

		// Check if the meaning has materially changed.
		intakeParts := strings.Fields(normalized)
		refParts := strings.Fields(normalizedRef)
		if len(intakeParts) != len(refParts) {
			return fmt.Errorf("intake label %q becomes new semantic identity %q (word count changed)",
				intakeLabel, normalizedLabel)
		}
	}
	return nil
}

// RejectMultipleUnqualifiedActions detects when one feature maps to multiple unqualified actions.
func (fn *FeatureNormalizer) RejectMultipleUnqualifiedActions(featureID string, actions []string) error {
	if len(actions) <= 1 {
		return nil // Single action is fine.
	}

	// If multiple actions are mapped without qualification (domain/role/channel context),
	// this suggests incomplete normalization.
	if actions[0] == "" || actions[1] == "" {
		return fmt.Errorf("feature %s maps to multiple unqualified actions: %v", featureID, actions)
	}

	return nil
}

// RejectMismatchedClassification detects when material operations are classified as non-material.
func (fn *FeatureNormalizer) RejectMismatchedClassification(nf *NormalizedFeature) error {
	// Material operations must be classified as CREATE, CONSUME, EMIT_CHILD, OBSERVE, or REVIEW_REQUIRED.
	// Static rendering, transport health, or internal retry mechanics should be NON_MATERIAL.
	materialKeywords := []string{"change", "approve", "reject", "create", "calculate", "process",
		"file", "investigate", "communicate", "repair", "plan", "schedule", "answer"}

	isMaterialLabel := false
	for _, kw := range materialKeywords {
		if strings.Contains(strings.ToLower(nf.Label), kw) {
			isMaterialLabel = true
			break
		}
	}

	nonMaterialKeywords := []string{"render", "display", "show", "health", "transport", "retry",
		"cache", "buffer", "compress"}

	isNonMaterialLabel := false
	for _, kw := range nonMaterialKeywords {
		if strings.Contains(strings.ToLower(nf.Label), kw) {
			isNonMaterialLabel = true
			break
		}
	}

	if isMaterialLabel && nf.Classification == ClassNonMaterial {
		return fmt.Errorf("feature %s has material label %q but classified as NON_MATERIAL",
			nf.FeatureID, nf.Label)
	}

	if isNonMaterialLabel && nf.Classification != ClassNonMaterial && nf.Classification != ClassReviewNeeded {
		return fmt.Errorf("feature %s has non-material label %q but classified as %s",
			nf.FeatureID, nf.Label, nf.Classification)
	}

	return nil
}

// NormalizedFeatures returns all registered normalized features sorted by feature_id.
func (fn *FeatureNormalizer) NormalizedFeatures() []*NormalizedFeature {
	var result []*NormalizedFeature
	for _, nf := range fn.featureIDMap {
		result = append(result, nf)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].FeatureID < result[j].FeatureID
	})
	return result
}

// VerifySourceTraceable ensures every normalized record can trace back to its exact intake entry.
func (fn *FeatureNormalizer) VerifySourceTraceable(nf *NormalizedFeature) error {
	if nf.SourceRef == "" {
		return fmt.Errorf("feature %s: missing source reference (cannot trace to intake)", nf.FeatureID)
	}
	if nf.IntakeLabel == "" {
		return fmt.Errorf("feature %s: missing intake label", nf.FeatureID)
	}
	if nf.SourceGroupID == 0 {
		return fmt.Errorf("feature %s: missing source group id", nf.FeatureID)
	}
	return nil
}

// ValidateNormalization checks the entire normalized set for consistency.
// Returns nil if valid, or an error describing the violation.
func (fn *FeatureNormalizer) ValidateNormalization() error {
	for _, nf := range fn.featureIDMap {
		// Verify source traceable.
		if err := fn.VerifySourceTraceable(nf); err != nil {
			return err
		}

		// Verify classification is valid.
		if !ValidSemanticClassifications[nf.Classification] {
			return fmt.Errorf("feature %s: invalid classification %q", nf.FeatureID, nf.Classification)
		}

		// Verify no material/non-material mismatches.
		if err := fn.RejectMismatchedClassification(nf); err != nil {
			return err
		}
	}

	// Verify no ambiguous aliases across domains.
	if len(fn.aliasConflicts) > 0 {
		var conflicts []string
		for alias, features := range fn.aliasConflicts {
			conflicts = append(conflicts, fmt.Sprintf("%q in %v", alias, features))
		}
		sort.Strings(conflicts)
		return fmt.Errorf("ambiguous aliases detected: %s", strings.Join(conflicts, "; "))
	}

	return nil
}
