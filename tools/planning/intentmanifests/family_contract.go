package intentmanifests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// IntentFamilyResultContract is the declared result contract shared by every
// intent in a kernel family. Feature rows bind to intent definitions, never to
// a feature-specific result shape, so adding a feature does not create a new
// semantic kernel.
type IntentFamilyResultContract struct {
	Family                  string   `json:"family"`
	RequestContract         string   `json:"request_contract"`
	ResultContract          string   `json:"result_contract"`
	ResultKinds             []string `json:"result_kinds"`
	EvidenceKinds           []string `json:"evidence_kinds"`
	TerminalStates          []string `json:"terminal_states"`
	ExecutionPolicy         string   `json:"execution_policy"`
	EffectPolicy            string   `json:"effect_policy"`
	AllowedRelationships    []string `json:"allowed_relationships"`
	TerminalDimensions      []string `json:"terminal_dimensions"`
	TransactionEffectPolicy string   `json:"transaction_effect_policy"`
}

// Declared family names are the same canonical names used by
// internal/intent.Family and the generated governance manifest.
const (
	FamilyChangeRequest      = "CHANGE_REQUEST"
	FamilyCalculationRequest = "CALCULATION_REQUEST"
	FamilyAnalyticalRequest  = "ANALYTICAL_REQUEST"
)

var declaredIntentFamilyResultContracts = map[string]IntentFamilyResultContract{
	FamilyChangeRequest: {
		Family:                  FamilyChangeRequest,
		RequestContract:         "typed_change_request/v1",
		ResultContract:          "typed_change_result/v1",
		ResultKinds:             []string{"MUTATION_APPLIED", "EFFECT_REQUESTED", "REJECTED", "FAILED", "CANCELLED", "REPAIR_REQUIRED"},
		EvidenceKinds:           []string{"request_digest", "decision_or_result", "provenance", "conflict_resolution", "correction", "terminal_observation"},
		TerminalStates:          []string{"COMPLETED", "REJECTED", "FAILED", "CANCELLED", "REPAIR_REQUIRED"},
		ExecutionPolicy:         "preflight_then_execute_or_repair",
		EffectPolicy:            "authorized_mutation_or_external_effect_only",
		AllowedRelationships:    []string{"proposal_approval", "parent_child_intent", "transaction_effect", "evidence_lineage"},
		TerminalDimensions:      []string{"request", "execution", "business", "consistency", "obligation"},
		TransactionEffectPolicy: "MUTATION_OR_EFFECT_ALLOWED",
	},
	FamilyCalculationRequest: {
		Family:                  FamilyCalculationRequest,
		RequestContract:         "typed_calculation_request/v1",
		ResultContract:          "typed_calculation_result/v1",
		ResultKinds:             []string{"CALCULATION_RESULT", "INSUFFICIENT_DATA", "UNCERTAIN", "DENIED"},
		EvidenceKinds:           []string{"request_digest", "decision_or_result", "provenance", "input_snapshot"},
		TerminalStates:          []string{"COMPLETED", "INSUFFICIENT_DATA", "UNCERTAIN", "DENIED"},
		ExecutionPolicy:         "evaluate_from_pinned_inputs",
		EffectPolicy:            "pure_computation_only",
		AllowedRelationships:    []string{"evidence_lineage", "input_snapshot"},
		TerminalDimensions:      []string{"request", "execution", "business", "consistency", "obligation"},
		TransactionEffectPolicy: "NO_BUSINESS_TRANSACTION_OR_EFFECT",
	},
	FamilyAnalyticalRequest: {
		Family:                  FamilyAnalyticalRequest,
		RequestContract:         "typed_analytical_request/v1",
		ResultContract:          "typed_analytical_result/v1",
		ResultKinds:             []string{"ANSWER", "NO_DATA", "UNCERTAIN", "DENIED"},
		EvidenceKinds:           []string{"request_digest", "decision_or_result", "provenance", "source_revision"},
		TerminalStates:          []string{"COMPLETED", "NO_DATA", "UNCERTAIN", "DENIED"},
		ExecutionPolicy:         "read_authorized_source_revisions",
		EffectPolicy:            "read_only_observation",
		AllowedRelationships:    []string{"evidence_lineage", "source_revision"},
		TerminalDimensions:      []string{"request", "execution", "business", "consistency", "obligation"},
		TransactionEffectPolicy: "NO_BUSINESS_TRANSACTION_OR_EFFECT",
	},
}

// IntentFamilyResultContracts returns the declared table in stable family
// order. Returned slices are copied so callers cannot mutate the declaration.
func IntentFamilyResultContracts() []IntentFamilyResultContract {
	contracts := make([]IntentFamilyResultContract, 0, len(declaredIntentFamilyResultContracts))
	for _, family := range []string{FamilyChangeRequest, FamilyCalculationRequest, FamilyAnalyticalRequest} {
		contracts = append(contracts, cloneIntentFamilyResultContract(declaredIntentFamilyResultContracts[family]))
	}
	return contracts
}

// IntentFamilyResultContractFor resolves one canonical family name.
func IntentFamilyResultContractFor(family string) (IntentFamilyResultContract, bool) {
	contract, ok := declaredIntentFamilyResultContracts[family]
	if !ok {
		return IntentFamilyResultContract{}, false
	}
	return cloneIntentFamilyResultContract(contract), true
}

// ValidateIntentFamilyResultContract checks the completeness dimensions that
// every declared family must provide.
func (c IntentFamilyResultContract) Validate() error {
	if c.Family == "" || c.RequestContract == "" || c.ResultContract == "" ||
		c.ExecutionPolicy == "" || c.EffectPolicy == "" || c.TransactionEffectPolicy == "" {
		return fmt.Errorf("family contract %q is missing a required scalar", c.Family)
	}
	for name, values := range map[string][]string{
		"result_kinds":          c.ResultKinds,
		"evidence_kinds":        c.EvidenceKinds,
		"terminal_states":       c.TerminalStates,
		"allowed_relationships": c.AllowedRelationships,
		"terminal_dimensions":   c.TerminalDimensions,
	} {
		if err := validateContractVocabulary(c.Family, name, values); err != nil {
			return err
		}
	}
	if len(c.TerminalDimensions) != 5 || !containsAll(c.TerminalDimensions, []string{"request", "execution", "business", "consistency", "obligation"}) {
		return fmt.Errorf("family contract %q must cover all five terminal dimensions", c.Family)
	}
	switch c.Family {
	case FamilyChangeRequest:
		if c.TransactionEffectPolicy != "MUTATION_OR_EFFECT_ALLOWED" {
			return fmt.Errorf("family contract %q has an invalid transaction/effect policy", c.Family)
		}
	case FamilyCalculationRequest, FamilyAnalyticalRequest:
		if c.TransactionEffectPolicy != "NO_BUSINESS_TRANSACTION_OR_EFFECT" {
			return fmt.Errorf("family contract %q must prohibit business transactions and effects", c.Family)
		}
	default:
		return fmt.Errorf("family contract %q is not a declared kernel family", c.Family)
	}
	return nil
}

// ContractCompletenessGap identifies a coverage row that cannot resolve to a
// complete family/result contract.
type ContractCompletenessGap struct {
	FeatureClass FeatureIntentRole
	FeatureID    string
	BoundIntent  string
	Family       string
	Reason       string
}

// ContractCompletenessReport is the deterministic audit output for the
// coverage registry. GapCounts is keyed by feature class so aggregate holes
// remain visible even when many features share one missing contract.
type ContractCompletenessReport struct {
	FeatureClassCounts map[FeatureIntentRole]int
	GapCounts          map[FeatureIntentRole]int
	CheckedBound       int
	Gaps               []ContractCompletenessGap
}

// Complete reports whether every mapped coverage row has a complete family
// contract and every declared family table entry is itself complete.
func (r ContractCompletenessReport) Complete() bool { return len(r.Gaps) == 0 }

// ValidateIntentFamilyResultExhaustiveness checks the real coverage shape
// against the declared family/result table. DEFERRED rows and explicitly
// non-material mechanics have no bound intent and are intentionally not
// treated as missing family contracts.
func ValidateIntentFamilyResultExhaustiveness(registry FeatureIntentCoverageRegistry, descriptors []IntentDescriptor) (ContractCompletenessReport, error) {
	report := ContractCompletenessReport{
		FeatureClassCounts: make(map[FeatureIntentRole]int),
		GapCounts:          make(map[FeatureIntentRole]int),
	}
	for _, contract := range IntentFamilyResultContracts() {
		if err := contract.Validate(); err != nil {
			return report, fmt.Errorf("declared family/result table: %w", err)
		}
	}

	descriptorFamilies := make(map[string]string, len(descriptors)*2)
	descriptorByReference := make(map[string]IntentDescriptor, len(descriptors)*2)
	for _, descriptor := range descriptors {
		descriptorFamilies[descriptor.IntentTypeID] = descriptor.Family
		descriptorFamilies[fmt.Sprintf("%s/v%d", descriptor.IntentTypeID, descriptor.Version)] = descriptor.Family
		descriptorByReference[descriptor.IntentTypeID] = descriptor
		descriptorByReference[fmt.Sprintf("%s/v%d", descriptor.IntentTypeID, descriptor.Version)] = descriptor
	}
	for _, feature := range registry.Features {
		report.FeatureClassCounts[feature.Role]++
		if feature.BoundIntentID == "" || feature.BoundIntentID == DeferredIntentBinding {
			continue
		}
		report.CheckedBound++
		family, ok := descriptorFamilies[feature.BoundIntentID]
		if !ok {
			report.addGap(feature, "bound intent is absent from the intent catalog", "")
			continue
		}
		contract, ok := IntentFamilyResultContractFor(family)
		if !ok {
			report.addGap(feature, "intent family has no declared result contract", family)
			continue
		}
		if err := contract.Validate(); err != nil {
			report.addGap(feature, "intent family result contract is incomplete: "+err.Error(), family)
			continue
		}
		descriptor := descriptorByReference[feature.BoundIntentID]
		if family == FamilyCalculationRequest || family == FamilyAnalyticalRequest {
			if !isNoneOnly(descriptor.Writes) || !isNoneOnly(descriptor.Effects) {
				report.addGap(feature, "read/calculation intent declares a write or effect and could fabricate a business transaction", family)
			}
		}
	}
	if len(report.Gaps) != 0 {
		return report, fmt.Errorf("intent family/result contract gaps by feature class: %s", formatGapCounts(report.GapCounts))
	}
	return report, nil
}

// IntentFamilyResultContractDigest is the stable digest of the declared
// contract table, excluding no mutable runtime state.
func IntentFamilyResultContractDigest() string {
	sum := sha256.Sum256(intentFamilyResultContractCanonical())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// IntentFamilyResultContractCanonical returns the deterministic table encoding
// used by the digest and golden test.
func IntentFamilyResultContractCanonical() []byte {
	return append([]byte(nil), intentFamilyResultContractCanonical()...)
}

func intentFamilyResultContractCanonical() []byte {
	contracts := IntentFamilyResultContracts()
	b, _ := json.Marshal(struct {
		Schema        string                       `json:"schema"`
		SchemaVersion int                          `json:"schema_version"`
		Contracts     []IntentFamilyResultContract `json:"contracts"`
	}{"hcmnext.planning.intent_family_result_contract", 1, contracts})
	return b
}

func (r *ContractCompletenessReport) addGap(feature FeatureIntentCoverage, reason, family string) {
	r.Gaps = append(r.Gaps, ContractCompletenessGap{
		FeatureClass: feature.Role,
		FeatureID:    feature.FeatureID,
		BoundIntent:  feature.BoundIntentID,
		Family:       family,
		Reason:       reason,
	})
	r.GapCounts[feature.Role]++
}

func validateContractVocabulary(family, name string, values []string) error {
	if len(values) == 0 {
		return fmt.Errorf("family contract %q has no %s", family, name)
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" {
			return fmt.Errorf("family contract %q has an empty %s entry", family, name)
		}
		if seen[value] {
			return fmt.Errorf("family contract %q repeats %s entry %q", family, name, value)
		}
		seen[value] = true
	}
	return nil
}

func containsAll(values, required []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range required {
		if !seen[value] {
			return false
		}
	}
	return true
}

func isNoneOnly(values []string) bool { return len(values) == 1 && values[0] == "none" }

func formatGapCounts(counts map[FeatureIntentRole]int) string {
	roles := make([]string, 0, len(counts))
	for role := range counts {
		roles = append(roles, string(role))
	}
	sort.Strings(roles)
	parts := make([]string, 0, len(roles))
	for _, role := range roles {
		parts = append(parts, fmt.Sprintf("%s=%d", role, counts[FeatureIntentRole(role)]))
	}
	return fmt.Sprintf("%v", parts)
}

func cloneIntentFamilyResultContract(c IntentFamilyResultContract) IntentFamilyResultContract {
	c.ResultKinds = append([]string(nil), c.ResultKinds...)
	c.EvidenceKinds = append([]string(nil), c.EvidenceKinds...)
	c.TerminalStates = append([]string(nil), c.TerminalStates...)
	c.AllowedRelationships = append([]string(nil), c.AllowedRelationships...)
	c.TerminalDimensions = append([]string(nil), c.TerminalDimensions...)
	return c
}
