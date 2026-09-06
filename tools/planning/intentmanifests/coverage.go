package intentmanifests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// CoverageStatus records catalog coverage independently from implementation
// phase. A DEFINED row has a binding to one drafted intent; DEFERRED is an
// explicit intake sentinel; the other states describe the quality of the
// available semantic evidence.
type CoverageStatus string

const (
	CoverageDefined  CoverageStatus = "DEFINED"
	CoveragePartial  CoverageStatus = "PARTIAL"
	CoverageImplied  CoverageStatus = "IMPLIED"
	CoverageMissing  CoverageStatus = "MISSING"
	CoverageDeferred CoverageStatus = "DEFERRED"
)

var validCoverageStatuses = map[CoverageStatus]bool{
	CoverageDefined:  true,
	CoveragePartial:  true,
	CoverageImplied:  true,
	CoverageMissing:  true,
	CoverageDeferred: true,
}

// FeatureDisposition is the review decision for an intake feature.
type FeatureDisposition string

const (
	DispositionDeferredToIntent FeatureDisposition = "DEFERRED_TO_INTENT"
	DispositionNonMaterial      FeatureDisposition = "NON_MATERIAL"
	DispositionMergedInto       FeatureDisposition = "MERGED_INTO"
	DispositionReview           FeatureDisposition = "REVIEW"
)

var validFeatureDispositions = map[FeatureDisposition]bool{
	DispositionDeferredToIntent: true,
	DispositionNonMaterial:      true,
	DispositionMergedInto:       true,
	DispositionReview:           true,
}

// FeatureIntentCoverage is the lossless, machine-readable record for one
// intake feature. Derived fields make the fourteen-dimensional contract
// inspectable without claiming that an unbound feature is implemented.
type FeatureIntentCoverage struct {
	FeatureID              string                 `yaml:"feature_id" json:"feature_id"`
	Group                  int                    `yaml:"group" json:"group"`
	GroupName              string                 `yaml:"group_name" json:"group_name"`
	CanonicalIdentity      string                 `yaml:"canonical_identity" json:"canonical_identity"`
	Classification         SemanticClassification `yaml:"classification" json:"classification"`
	Role                   FeatureIntentRole      `yaml:"role" json:"role"`
	CoverageStatus         CoverageStatus         `yaml:"coverage_status" json:"coverage_status"`
	BoundIntentID          string                 `yaml:"bound_intent" json:"bound_intent"`
	DeclaredIntentIDs      []string               `yaml:"declared_intents,omitempty" json:"declared_intents,omitempty"`
	MappingIssue           string                 `yaml:"mapping_issue,omitempty" json:"mapping_issue,omitempty"`
	Disposition            FeatureDisposition     `yaml:"disposition" json:"disposition"`
	DispositionTarget      string                 `yaml:"disposition_target,omitempty" json:"disposition_target,omitempty"`
	DispositionRationale   string                 `yaml:"disposition_rationale" json:"disposition_rationale"`
	Owner                  string                 `yaml:"owner" json:"owner"`
	Phase                  string                 `yaml:"phase" json:"phase"`
	Depth                  string                 `yaml:"depth" json:"depth"`
	Actor                  string                 `yaml:"actor" json:"actor"`
	Channel                string                 `yaml:"channel" json:"channel"`
	Subject                string                 `yaml:"subject" json:"subject"`
	Resource               string                 `yaml:"resource" json:"resource"`
	Capability             string                 `yaml:"capability" json:"capability"`
	CapabilityVersion      string                 `yaml:"capability_version" json:"capability_version"`
	InputSchema            string                 `yaml:"input_schema" json:"input_schema"`
	ResultSchema           string                 `yaml:"result_schema" json:"result_schema"`
	ParentChildBehavior    string                 `yaml:"parent_child_behavior" json:"parent_child_behavior"`
	GovernanceProfile      string                 `yaml:"governance_profile" json:"governance_profile"`
	EvidenceExpectation    string                 `yaml:"evidence_expectation" json:"evidence_expectation"`
	SourceGroupProvenance  string                 `yaml:"source_group_provenance" json:"source_group_provenance"`
	SourceFeatureCanonical string                 `yaml:"source_feature_canonical" json:"source_feature_canonical"`
}

// FeatureIntentCoverageRegistry is the generated registry and its source
// attestations. Digest excludes Digest itself and is stable across ordering.
type FeatureIntentCoverageRegistry struct {
	Version             string                     `yaml:"version" json:"version"`
	SourceFeatureDigest string                     `yaml:"source_feature_digest" json:"source_feature_digest"`
	SourceIntentDigest  string                     `yaml:"source_intent_digest" json:"source_intent_digest"`
	FeatureGroups       int                        `yaml:"feature_groups" json:"feature_groups"`
	FeatureCount        int                        `yaml:"feature_count" json:"feature_count"`
	DispositionCounts   map[FeatureDisposition]int `yaml:"disposition_counts" json:"disposition_counts"`
	Features            []FeatureIntentCoverage    `yaml:"features" json:"features"`
	Digest              string                     `yaml:"digest" json:"digest"`
}

// BuildFeatureIntentCoverage compiles the intake and intent catalog into a
// deterministic registry. It is pure: it reads only the supplied values and
// performs no database, network, or implementation-phase lookup.
func BuildFeatureIntentCoverage(groups []FeatureGroup, intents []IntentDescriptor) (FeatureIntentCoverageRegistry, error) {
	classifier, err := NewFeatureIntentClassifier(intents)
	if err != nil {
		return FeatureIntentCoverageRegistry{}, fmt.Errorf("create classifier: %w", err)
	}
	normalizer := NewFeatureNormalizer()
	registry := FeatureIntentCoverageRegistry{
		Version:           "1.0",
		FeatureGroups:     len(groups),
		DispositionCounts: make(map[FeatureDisposition]int),
		Features:          make([]FeatureIntentCoverage, 0),
	}
	registry.SourceFeatureDigest, err = ComputeFeatureDigestYAML(groups)
	if err != nil {
		return FeatureIntentCoverageRegistry{}, fmt.Errorf("feature source digest: %w", err)
	}
	registry.SourceIntentDigest, err = ComputeIntentDigestYAML(intents)
	if err != nil {
		return FeatureIntentCoverageRegistry{}, fmt.Errorf("intent source digest: %w", err)
	}

	descriptors := make(map[string]IntentDescriptor, len(intents))
	for _, descriptor := range intents {
		descriptors[intentKey(descriptor.IntentTypeID, descriptor.Version)] = descriptor
		descriptors[descriptor.IntentTypeID] = descriptor
	}

	for _, group := range groups {
		groupDomain := group.Domain
		if groupDomain == "" {
			groupDomain = sanitizeIdentifier(group.Name)
		}
		for _, feature := range group.Features {
			classification := intakeClassification(feature.Category, feature.Label)
			normalized := &NormalizedFeature{
				FeatureID:      feature.FeatureID,
				Label:          feature.Label,
				Classification: classification,
				Domain:         groupDomain,
				IntakeLabel:    feature.Label,
				IntakeGroup:    group.GroupID,
				SourceGroupID:  group.GroupID,
				SourceRef:      "definitions/governance/feature-intent-intake.yaml",
				IsMaterial:     isMaterialOperation(feature.Label),
			}
			if err := normalizer.AddNormalizedFeature(normalized); err != nil {
				return FeatureIntentCoverageRegistry{}, fmt.Errorf("group %d feature %s: %w", group.GroupID, feature.FeatureID, err)
			}
			classified, err := classifier.ClassifyFeature(normalized, feature.MappedIntentID)
			if err != nil && feature.MappedIntentID != "MISSING" && feature.MappedIntentID != "" {
				// A comma-separated binding is intentionally retained as an explicit
				// ambiguity so the generated registry can assign REVIEW.
				if len(splitIntentIDs(feature.MappedIntentID)) <= 1 {
					return FeatureIntentCoverageRegistry{}, fmt.Errorf("group %d feature %s: %w", group.GroupID, feature.FeatureID, err)
				}
			}
			if classified == nil {
				classified = &FeatureIntentClassification{FeatureID: feature.FeatureID, Role: RoleIntentCreator, IsMaterial: normalized.IsMaterial}
			}
			if len(splitIntentIDs(feature.MappedIntentID)) == 1 {
				if _, drafted := descriptors[splitIntentIDs(feature.MappedIntentID)[0]]; drafted {
					classified.IsMaterial = true
					switch normalized.Classification {
					case ClassCreate:
						classified.Role = RoleIntentCreator
					case ClassConsume:
						classified.Role = RoleIntentConsumer
					case ClassEmitChild:
						classified.Role = RoleChildIntentEmitter
					case ClassObserve:
						classified.Role = RoleIntentObserver
					}
				}
			}
			record := makeCoverageRecord(group, feature, normalized, classified, descriptors)
			registry.Features = append(registry.Features, record)
			registry.DispositionCounts[record.Disposition]++
		}
	}
	sort.Slice(registry.Features, func(i, j int) bool { return registry.Features[i].FeatureID < registry.Features[j].FeatureID })
	registry.FeatureCount = len(registry.Features)
	if err := registry.refreshDigest(); err != nil {
		return FeatureIntentCoverageRegistry{}, err
	}
	return registry, nil
}

// ValidateFeatureIntentCoverage rejects implicit or ambiguous records that
// could otherwise enter the generated registry without a review decision.
func ValidateFeatureIntentCoverage(registry FeatureIntentCoverageRegistry) error {
	if registry.Version == "" || registry.FeatureGroups < 1 || registry.FeatureCount != len(registry.Features) {
		return fmt.Errorf("registry metadata is incomplete")
	}
	seen := make(map[string]bool, len(registry.Features))
	counts := make(map[FeatureDisposition]int)
	for i, record := range registry.Features {
		if record.FeatureID == "" || record.Group < 1 || record.GroupName == "" || record.CanonicalIdentity == "" ||
			record.Classification == "" || record.Role == "" || record.CoverageStatus == "" || record.BoundIntentID == "" ||
			record.Disposition == "" || record.DispositionRationale == "" || record.Owner == "" || record.Phase == "" ||
			record.Depth == "" || record.Actor == "" || record.Channel == "" || record.Subject == "" || record.Resource == "" ||
			record.Capability == "" || record.CapabilityVersion == "" || record.InputSchema == "" || record.ResultSchema == "" ||
			record.ParentChildBehavior == "" || record.GovernanceProfile == "" || record.EvidenceExpectation == "" {
			return fmt.Errorf("feature row %d (%s) has an implicit or incomplete dimension", i, record.FeatureID)
		}
		if seen[record.FeatureID] {
			return fmt.Errorf("duplicate feature_id %q", record.FeatureID)
		}
		seen[record.FeatureID] = true
		if !validSemanticClassification(record.Classification) || !ValidFeatureIntentRoles[record.Role] || !validCoverageStatuses[record.CoverageStatus] || !validFeatureDispositions[record.Disposition] {
			return fmt.Errorf("feature %s has an unknown classification, role, coverage status, or disposition", record.FeatureID)
		}
		if record.BoundIntentID != DeferredIntentBinding && len(splitIntentIDs(record.BoundIntentID)) != 1 {
			return fmt.Errorf("feature %s has multiple effective bound intents", record.FeatureID)
		}
		switch record.Disposition {
		case DispositionMergedInto:
			if record.DispositionTarget == "" || record.BoundIntentID == DeferredIntentBinding {
				return fmt.Errorf("feature %s: MERGED_INTO requires one bound intent and target", record.FeatureID)
			}
		case DispositionDeferredToIntent:
			if record.DispositionTarget == "" || !strings.HasPrefix(record.DispositionTarget, "hcmnext.") {
				return fmt.Errorf("feature %s: DEFERRED_TO_INTENT requires a future intent id", record.FeatureID)
			}
		case DispositionNonMaterial:
			if record.Role != RoleNonMaterialMechanic {
				return fmt.Errorf("feature %s: NON_MATERIAL requires NON_MATERIAL_MECHANIC role", record.FeatureID)
			}
		case DispositionReview:
			if record.MappingIssue == "" && record.BoundIntentID == DeferredIntentBinding {
				return fmt.Errorf("feature %s: REVIEW requires a recorded issue", record.FeatureID)
			}
		}
		counts[record.Disposition]++
	}
	if !equalDispositionCounts(counts, registry.DispositionCounts) {
		return fmt.Errorf("disposition counts do not match feature rows")
	}
	if registry.Digest != "" {
		copy := registry
		copy.Digest = ""
		digest, err := copy.canonicalDigest()
		if err != nil {
			return fmt.Errorf("registry digest: %w", err)
		}
		if digest != registry.Digest {
			return fmt.Errorf("registry digest=%q, want %q", registry.Digest, digest)
		}
	}
	return nil
}

// LoadFeatureIntentCoverageYAML reads a generated coverage registry.
func LoadFeatureIntentCoverageYAML(path string) (FeatureIntentCoverageRegistry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FeatureIntentCoverageRegistry{}, fmt.Errorf("read feature intent coverage: %w", err)
	}
	var registry FeatureIntentCoverageRegistry
	if err := yaml.Unmarshal(data, &registry); err != nil {
		return FeatureIntentCoverageRegistry{}, fmt.Errorf("parse feature intent coverage: %w", err)
	}
	if err := ValidateFeatureIntentCoverage(registry); err != nil {
		return FeatureIntentCoverageRegistry{}, fmt.Errorf("validate feature intent coverage: %w", err)
	}
	return registry, nil
}

// MarshalFeatureIntentCoverageYAML returns the canonical generated YAML.
func MarshalFeatureIntentCoverageYAML(registry FeatureIntentCoverageRegistry) ([]byte, error) {
	if err := ValidateFeatureIntentCoverage(registry); err != nil {
		return nil, err
	}
	return yaml.Marshal(registry)
}

func (r *FeatureIntentCoverageRegistry) refreshDigest() error {
	digest, err := r.canonicalDigest()
	if err != nil {
		return err
	}
	r.Digest = digest
	return nil
}

func (r FeatureIntentCoverageRegistry) canonicalDigest() (string, error) {
	copy := r
	copy.Digest = ""
	copy.Features = append([]FeatureIntentCoverage(nil), r.Features...)
	sort.Slice(copy.Features, func(i, j int) bool { return copy.Features[i].FeatureID < copy.Features[j].FeatureID })
	data, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func makeCoverageRecord(group FeatureGroup, feature Feature, normalized *NormalizedFeature, classified *FeatureIntentClassification, descriptors map[string]IntentDescriptor) FeatureIntentCoverage {
	groupDomain := group.Domain
	if groupDomain == "" {
		groupDomain = sanitizeIdentifier(group.Name)
	}
	declared := splitIntentIDs(feature.MappedIntentID)
	effectiveBinding := DeferredIntentBinding
	if len(declared) == 1 {
		effectiveBinding = declared[0]
	}
	canonical := qualifiedFeatureIdentity(groupDomain, feature.FeatureID)
	descriptor, hasDescriptor := descriptors[effectiveBinding]
	if hasDescriptor {
		canonical = effectiveBinding
	}
	record := FeatureIntentCoverage{
		FeatureID:              feature.FeatureID,
		Group:                  group.GroupID,
		GroupName:              group.Name,
		CanonicalIdentity:      canonical,
		Classification:         normalized.Classification,
		Role:                   classified.Role,
		CoverageStatus:         CoverageDeferred,
		BoundIntentID:          effectiveBinding,
		DeclaredIntentIDs:      declared,
		Owner:                  groupDomain,
		Phase:                  "DESIGN",
		Depth:                  "INTAKE",
		Actor:                  "initiator",
		Channel:                "governed",
		Subject:                groupDomain,
		Resource:               feature.Label,
		Capability:             "intent:" + canonical,
		CapabilityVersion:      "1",
		InputSchema:            "properties:" + feature.Label,
		ResultSchema:           "result:" + feature.Label,
		ParentChildBehavior:    "independent-until-cataloged",
		GovernanceProfile:      "owner:" + groupDomain,
		EvidenceExpectation:    "source_provenance,decision_or_result",
		SourceGroupProvenance:  group.SourceProvenance,
		SourceFeatureCanonical: feature.FeatureID,
	}
	if hasDescriptor {
		record.CoverageStatus = CoverageDefined
		record.Phase = firstOrString(descriptor.Phase, "DESIGN")
		record.Subject = firstOr(descriptor.Entities, groupDomain)
		record.Resource = firstOrString(strings.Join(descriptor.Entities, ","), feature.Label)
		record.Capability = "intent:" + descriptor.IntentTypeID
		record.CapabilityVersion = strconv.Itoa(descriptor.Version)
		record.InputSchema = "properties:" + firstOrString(strings.Join(descriptor.Properties, ","), feature.Label)
		record.ResultSchema = "writes:" + firstOrString(strings.Join(descriptor.Writes, ","), "none") + ";effects:" + firstOrString(strings.Join(descriptor.Effects, ","), "none")
		record.ParentChildBehavior = "catalog-defined:" + descriptor.Family
		record.GovernanceProfile = firstOrString(strings.Join(descriptor.Authority, ","), "owner:"+groupDomain)
		record.EvidenceExpectation = firstOrString(strings.Join(descriptor.Evidence, ","), "source_provenance")
	}

	switch {
	case len(declared) > 1:
		record.MappingIssue = "AMBIGUOUS_MULTIPLE_INTENTS"
		record.Disposition = DispositionReview
		record.DispositionRationale = "intake names multiple drafted intents; a single canonical binding requires review"
	case classified.Role == RoleNonMaterialMechanic:
		record.Disposition = DispositionNonMaterial
		record.DispositionRationale = "feature is an internal display, transport, health, or other non-material mechanic"
	case hasDescriptor:
		record.Disposition = DispositionMergedInto
		record.DispositionTarget = effectiveBinding
		record.DispositionRationale = "feature is covered by the drafted intent with the same canonical semantic identity"
	case feature.MappedIntentID == "MISSING" || feature.MappedIntentID == "":
		record.MappingIssue = "MISSING_BINDING"
		record.Disposition = DispositionReview
		record.DispositionRationale = "intake feature has no explicit DEFERRED or drafted intent binding"
	default:
		record.Disposition = DispositionDeferredToIntent
		record.DispositionTarget = futureIntentID(groupDomain, feature.FeatureID)
		record.DispositionRationale = "feature is explicitly deferred until a future typed intent definition is drafted"
	}
	return record
}

func intakeClassification(category, label string) SemanticClassification {
	if isNonMaterialOperation(label) && !isMaterialOperation(label) {
		return ClassNonMaterial
	}
	switch category {
	case "CREATE", "CHANGE", "CALCULATE":
		return ClassCreate
	case "OBSERVE":
		return ClassObserve
	default:
		return ClassReviewNeeded
	}
}

func validSemanticClassification(classification SemanticClassification) bool {
	return ValidSemanticClassifications[classification]
}

func equalDispositionCounts(a, b map[FeatureDisposition]int) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func intentKey(id string, version int) string { return id + "/v" + strconv.Itoa(version) }

func splitIntentIDs(binding string) []string {
	if binding == "" || binding == DeferredIntentBinding || binding == "MISSING" {
		return nil
	}
	parts := strings.Split(binding, ",")
	ids := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			ids = append(ids, part)
		}
	}
	return ids
}

func qualifiedFeatureIdentity(domain, featureID string) string {
	if strings.HasPrefix(featureID, "hcmnext.") {
		return featureID
	}
	return "hcmnext." + sanitizeIdentifier(domain) + "." + sanitizeIdentifier(featureID)
}

func futureIntentID(domain, featureID string) string {
	return qualifiedFeatureIdentity(domain, featureID) + "/v1"
}

var nonIdentifier = regexp.MustCompile(`[^a-z0-9]+`)

func sanitizeIdentifier(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = nonIdentifier.ReplaceAllString(value, "_")
	return strings.Trim(value, "_")
}

func firstOr(values []string, fallback string) string {
	if len(values) > 0 && values[0] != "" {
		return values[0]
	}
	return fallback
}

func firstOrString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
