package intentmanifests

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestIntentFamilyResultExhaustiveness(t *testing.T) {
	registry, descriptors := loadRealIntentCoverage(t)
	report, err := ValidateIntentFamilyResultExhaustiveness(registry, descriptors)
	if err != nil {
		t.Fatalf("real coverage has family/result contract gaps: %v", err)
	}
	if !report.Complete() || report.CheckedBound == 0 {
		t.Fatalf("report = %+v, want complete report with mapped features checked", report)
	}
	for role, count := range report.FeatureClassCounts {
		if count < 1 {
			t.Errorf("feature class %s has count %d, want a positive count", role, count)
		}
	}
	if len(report.GapCounts) != 0 {
		t.Fatalf("gap counts = %v, want no gaps", report.GapCounts)
	}
}

func TestTodo_INTENT_011_Property(t *testing.T) {
	for _, contract := range IntentFamilyResultContracts() {
		if err := contract.Validate(); err != nil {
			t.Fatalf("%s: %v", contract.Family, err)
		}
		if contract.TransactionEffectPolicy == "" || len(contract.ResultKinds) == 0 || len(contract.EvidenceKinds) == 0 || len(contract.TerminalStates) == 0 {
			t.Fatalf("%s is missing a result/evidence/terminal contract: %+v", contract.Family, contract)
		}
		if contract.Family != FamilyChangeRequest && contract.TransactionEffectPolicy != "NO_BUSINESS_TRANSACTION_OR_EFFECT" {
			t.Fatalf("read family %s can create a transaction or effect", contract.Family)
		}
	}
}

func TestTodo_INTENT_011_Mutation(t *testing.T) {
	registry, descriptors := loadRealIntentCoverage(t)
	mutated := registry
	mutated.Features = append([]FeatureIntentCoverage(nil), registry.Features...)
	mutated.Features[0].BoundIntentID = "hcmnext.missing.not_in_catalog/v1"
	mutated.Features[0].Role = RoleIntentCreator

	report, err := ValidateIntentFamilyResultExhaustiveness(mutated, descriptors)
	if err == nil {
		t.Fatal("unknown bound intent was accepted")
	}
	if report.GapCounts[RoleIntentCreator] != 1 {
		t.Fatalf("gap counts = %v, want INTENT_CREATOR=1", report.GapCounts)
	}
	if !strings.Contains(err.Error(), "INTENT_CREATOR=1") {
		t.Fatalf("error = %q, want feature-class count", err)
	}

	contract, ok := IntentFamilyResultContractFor(FamilyAnalyticalRequest)
	if !ok {
		t.Fatal("analytical family contract missing")
	}
	contract.TransactionEffectPolicy = "MUTATION_OR_EFFECT_ALLOWED"
	if err := contract.Validate(); err == nil {
		t.Fatal("analytical contract accepted a mutation/effect policy")
	}

	readMutation := append([]IntentDescriptor(nil), descriptors...)
	for i := range readMutation {
		if readMutation[i].Family == FamilyAnalyticalRequest {
			readMutation[i].Writes = []string{"fabricated_transaction"}
			break
		}
	}
	if report, err := ValidateIntentFamilyResultExhaustiveness(registry, readMutation); err == nil || len(report.Gaps) == 0 {
		t.Fatalf("read intent with a fabricated write was accepted: report=%+v err=%v", report, err)
	}
}

func TestTodo_INTENT_011_Golden(t *testing.T) {
	const want = "sha256:ba34d39948150b4762d6dcd73e535f4477ae002176822b0129c54e88338752ff"
	if got := IntentFamilyResultContractDigest(); got != want {
		t.Fatalf("family/result contract digest = %q, want %q", got, want)
	}
}

func loadRealIntentCoverage(t *testing.T) (FeatureIntentCoverageRegistry, []IntentDescriptor) {
	t.Helper()
	root := filepath.Join("..", "..", "..", "definitions", "governance")
	registry, err := LoadFeatureIntentCoverageYAML(filepath.Join(root, "feature-intent-coverage.yaml"))
	if err != nil {
		t.Fatalf("load generated feature-intent coverage: %v", err)
	}
	descriptors, err := LoadIntentManifestYAML(filepath.Join(root, "intent-conformance-descriptors.yaml"))
	if err != nil {
		t.Fatalf("load generated intent descriptors: %v", err)
	}
	return registry, descriptors
}
