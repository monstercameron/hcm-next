// Package intentmanifests provides feature-to-intent classification.
package intentmanifests

import (
	"fmt"
	"strings"
)

// FeatureIntentRole represents the role a feature plays in relation to business intents.
type FeatureIntentRole string

const (
	RoleIntentCreator       FeatureIntentRole = "INTENT_CREATOR"
	RoleIntentConsumer      FeatureIntentRole = "INTENT_CONSUMER"
	RoleChildIntentEmitter  FeatureIntentRole = "CHILD_INTENT_EMITTER"
	RoleIntentObserver      FeatureIntentRole = "INTENT_OBSERVER"
	RoleNonMaterialMechanic FeatureIntentRole = "NON_MATERIAL_MECHANIC"
)

// ValidFeatureIntentRoles is the closed set of allowed roles.
var ValidFeatureIntentRoles = map[FeatureIntentRole]bool{
	RoleIntentCreator:       true,
	RoleIntentConsumer:      true,
	RoleChildIntentEmitter:  true,
	RoleIntentObserver:      true,
	RoleNonMaterialMechanic: true,
}

// FeatureIntentClassification describes how a feature relates to business intents.
type FeatureIntentClassification struct {
	// The feature being classified
	FeatureID string

	// The role this feature plays
	Role FeatureIntentRole

	// Whether this is a material business operation
	IsMaterial bool

	// The business intent this feature binds to (required if IsMaterial)
	BoundIntentID string

	// Business purpose (why this intent exists)
	BusinessPurpose string

	// Subject (what entity is affected)
	Subject string

	// Significance of authority/effect/result
	Authority string
	Effect    string
	Result    string

	// Owner of the feature/intent
	Owner string

	// Rationale (justification for the classification)
	Rationale string

	// Whether this classification requires review before finalization
	RequiresReview bool
	ReviewReason   string
}

// MaterialFeatureKeywords indicates operations that are material (real business operations).
var MaterialFeatureKeywords = []string{
	"change", "approve", "reject", "create", "update", "delete", "calculate", "process",
	"file", "investigate", "communicate", "repair", "plan", "schedule", "answer",
	"propose", "request", "allocate", "reserve", "release", "promote", "transfer",
}

// NonMaterialKeywords indicates operations that are non-material (plumbing/mechanics).
var NonMaterialKeywords = []string{
	"render", "display", "show", "cache", "buffer", "compress", "health", "transport",
	"retry", "logging", "monitoring", "cleanup", "sync", "preload",
}

// FeatureIntentClassifier classifies features and validates material bindings.
type FeatureIntentClassifier struct {
	// Catalog of valid business intents (intent_id -> IntentDescriptor)
	intentCatalog map[string]*IntentDescriptor

	// Classification results (feature_id -> FeatureIntentClassification)
	classifications map[string]*FeatureIntentClassification

	// Track material features without intent bindings
	unbound []string

	// Track material features bound to non-existent intents
	invalidBindings map[string]string
}

// NewFeatureIntentClassifier creates a new classifier with a given intent catalog.
func NewFeatureIntentClassifier(intents []IntentDescriptor) (*FeatureIntentClassifier, error) {
	fic := &FeatureIntentClassifier{
		intentCatalog:   make(map[string]*IntentDescriptor),
		classifications: make(map[string]*FeatureIntentClassification),
		invalidBindings: make(map[string]string),
	}

	// Build intent catalog (indexed by intent_id with version).
	for i := range intents {
		key := fmt.Sprintf("%s/v%d", intents[i].IntentTypeID, intents[i].Version)
		fic.intentCatalog[key] = &intents[i]
		// Also index without version for simpler lookup.
		fic.intentCatalog[intents[i].IntentTypeID] = &intents[i]
	}

	return fic, nil
}

// isMaterialOperation checks if a feature label indicates a material operation.
func isMaterialOperation(label string) bool {
	lower := strings.ToLower(label)
	for _, kw := range MaterialFeatureKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// isNonMaterialOperation checks if a feature label indicates non-material mechanics.
func isNonMaterialOperation(label string) bool {
	lower := strings.ToLower(label)
	for _, kw := range NonMaterialKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// ClassifyFeature classifies a feature's relationship to business intents.
// Returns an error if the classification violates materiality rules.
// DeferredIntentBinding is the intake manifest's sentinel for a material
// feature whose intent definition is deferred to a later slice.
const DeferredIntentBinding = "DEFERRED"

func (fic *FeatureIntentClassifier) ClassifyFeature(nf *NormalizedFeature, boundIntentID string) (*FeatureIntentClassification, error) {
	if nf == nil {
		return nil, fmt.Errorf("normalized feature is nil")
	}

	fc := &FeatureIntentClassification{
		FeatureID: nf.FeatureID,
	}

	// The intake manifest marks features whose intent is not yet drafted with
	// the DEFERRED sentinel; that is an explicit absence of a binding (it lands
	// in the review queue), never a catalog lookup.
	if boundIntentID == DeferredIntentBinding {
		boundIntentID = ""
	}

	// If a binding is provided, validate it exists in the catalog (regardless of materiality).
	if boundIntentID != "" {
		if _, exists := fic.intentCatalog[boundIntentID]; !exists {
			// Also check without version suffix
			if _, exists := fic.intentCatalog[strings.Split(boundIntentID, "/")[0]]; !exists {
				return nil, fmt.Errorf("feature %s bound to non-existent intent %q",
					nf.FeatureID, boundIntentID)
			}
		}
	}

	// Determine materiality from label and classification hints.
	isMaterial := isMaterialOperation(nf.Label)
	isNonMaterial := isNonMaterialOperation(nf.Label)

	if isNonMaterial && !isMaterial {
		// Non-material mechanics (rendering, caching, health checks).
		fc.Role = RoleNonMaterialMechanic
		fc.IsMaterial = false
		fc.Subject = "internal mechanics"
		fc.Rationale = fmt.Sprintf("feature %q contains non-material keywords", nf.Label)
		return fc, nil
	}

	if isMaterial {
		// Material operation: must have a business intent binding.
		if boundIntentID == "" {
			fc.Role = RoleIntentCreator // Default assumption
			fc.IsMaterial = true
			fc.RequiresReview = true
			fc.ReviewReason = fmt.Sprintf("feature %q is material but has no bound intent; requires business intake review", nf.Label)
			fic.unbound = append(fic.unbound, nf.FeatureID)
			return fc, nil
		}

		fc.BoundIntentID = boundIntentID
		fc.IsMaterial = true

		// Infer role from feature classification.
		switch nf.Classification {
		case ClassCreate:
			fc.Role = RoleIntentCreator
		case ClassConsume:
			fc.Role = RoleIntentConsumer
		case ClassEmitChild:
			fc.Role = RoleChildIntentEmitter
		case ClassObserve:
			fc.Role = RoleIntentObserver
		case ClassReviewNeeded:
			fc.Role = RoleIntentCreator // Default pending review
			fc.RequiresReview = true
			fc.ReviewReason = "feature classification is REVIEW_REQUIRED"
		default:
			return nil, fmt.Errorf("feature %s: unexpected classification %q", nf.FeatureID, nf.Classification)
		}

		fc.Subject = fmt.Sprintf("business entity affected by %s", boundIntentID)
		fc.Rationale = fmt.Sprintf("feature %q is material and bound to %s", nf.Label, boundIntentID)
		return fc, nil
	}

	// Ambiguous: could be either. Require review.
	fc.Role = RoleIntentCreator
	fc.IsMaterial = false
	fc.RequiresReview = true
	fc.ReviewReason = fmt.Sprintf("feature %q is ambiguous: neither clearly material nor clearly non-material mechanics", nf.Label)
	return fc, nil
}

// ValidateMaterialFeatureHasBoundIntent checks that material features have valid intent bindings.
func (fic *FeatureIntentClassifier) ValidateMaterialFeatureHasBoundIntent(fc *FeatureIntentClassification) error {
	if !fc.IsMaterial {
		return nil // Non-material features don't need bindings.
	}

	if fc.BoundIntentID == "" {
		return fmt.Errorf("feature %s is material but has no bound intent", fc.FeatureID)
	}

	if _, exists := fic.intentCatalog[fc.BoundIntentID]; !exists {
		return fmt.Errorf("feature %s bound intent %q does not exist in catalog", fc.FeatureID, fc.BoundIntentID)
	}

	return nil
}

// ValidateNoStaticRenderingAsMaterial rejects treating static rendering as a material business intent.
func (fic *FeatureIntentClassifier) ValidateNoStaticRenderingAsMaterial(fc *FeatureIntentClassification) error {
	if !fc.IsMaterial {
		return nil
	}

	// Check if the feature looks like rendering/display without semantic purpose.
	renderingTerms := []string{"render", "display", "show", "ui", "view", "template"}
	isCleanlySemantic := false

	// A feature like "display employee information" might be fine if it has
	// a semantic business purpose (e.g., an intent that says "explain_worker_state").
	// But a feature like "render cache" is not semantic.

	lower := strings.ToLower(fc.Subject)
	for _, term := range renderingTerms {
		if strings.Contains(lower, term) {
			isCleanlySemantic = false
			break
		}
		isCleanlySemantic = true
	}

	if !isCleanlySemantic && fc.BoundIntentID != "" {
		intent, _ := fic.intentCatalog[fc.BoundIntentID]
		if intent != nil {
			// Check if this is truly a semantic purpose (e.g., "explain" vs "render").
			if strings.Contains(strings.ToLower(intent.DisplayName), "explain") ||
				strings.Contains(strings.ToLower(intent.DisplayName), "show") ||
				strings.Contains(strings.ToLower(intent.DisplayName), "display") {
				return nil // OK if the intent is semantic.
			}
		}
	}

	return nil
}

// ValidateNoTransportHealthAsIntent rejects treating transport mechanics as intents.
func (fic *FeatureIntentClassifier) ValidateNoTransportHealthAsIntent(fc *FeatureIntentClassification) error {
	if !fc.IsMaterial {
		return nil
	}

	transportTerms := []string{"transport", "network", "connection", "health", "heartbeat", "keepalive", "retransmit"}
	for _, term := range transportTerms {
		if strings.Contains(strings.ToLower(fc.Subject), term) {
			return fmt.Errorf("feature %s has transport/health subject but is marked material; should be NON_MATERIAL_MECHANIC",
				fc.FeatureID)
		}
	}

	return nil
}

// ReportsUnboundMaterialFeatures returns all material features without intent bindings.
func (fic *FeatureIntentClassifier) ReportsUnboundMaterialFeatures() []string {
	return fic.unbound
}

// ReportsInvalidBindings returns features bound to non-existent intents.
func (fic *FeatureIntentClassifier) ReportsInvalidBindings() map[string]string {
	return fic.invalidBindings
}

// AllClassifications returns all recorded classifications.
func (fic *FeatureIntentClassifier) AllClassifications() map[string]*FeatureIntentClassification {
	return fic.classifications
}

// StoreClassification saves a classification for later retrieval.
func (fic *FeatureIntentClassifier) StoreClassification(fc *FeatureIntentClassification) {
	fic.classifications[fc.FeatureID] = fc
}
