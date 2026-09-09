package blockshared

import "human-capital-management-suite-executor/internal/executor"

const (
	// RiskLow marks a valid preflight with no material warning context.
	RiskLow = "low"
	// RiskMedium marks a valid preflight that still needs warning review.
	RiskMedium = "medium"
	// RiskHigh marks an invalid preflight.
	RiskHigh = "high"
)

// RiskLevelForValidation maps deterministic validation state to the shared risk vocabulary.
func RiskLevelForValidation(isValid bool, warningCount int) string {
	if !isValid {
		return RiskHigh
	}

	if warningCount > 0 {
		return RiskMedium
	}

	return RiskLow
}

// CorePreflightFacts returns the shared validation facts emitted by every preflight block.
func CorePreflightFacts(blockName string, isValid bool, riskLevel string, requiresEvidence bool, requiresApproval bool) []executor.Fact {
	return []executor.Fact{
		{Key: "valid", Value: isValid, Source: blockName},
		{Key: "riskLevel", Value: riskLevel, Source: blockName},
		{Key: "requiresEvidence", Value: requiresEvidence, Source: blockName},
		{Key: "requiresApproval", Value: requiresApproval, Source: blockName},
	}
}

// PreflightFacts returns shared validation facts plus warning count and block-specific facts.
func PreflightFacts(blockName string, isValid bool, riskLevel string, requiresEvidence bool, requiresApproval bool, warningCount int, extraFacts ...executor.Fact) []executor.Fact {
	facts := CorePreflightFacts(blockName, isValid, riskLevel, requiresEvidence, requiresApproval)
	facts = append(facts, executor.Fact{Key: "warningCount", Value: warningCount, Source: blockName})
	facts = append(facts, extraFacts...)

	return facts
}

// TransactionFacts returns the shared transaction-plan facts plus block-specific facts.
func TransactionFacts(blockName string, ledgerFactCount int, externalCallCount int, projectionPatchCount int, extraFacts ...executor.Fact) []executor.Fact {
	facts := []executor.Fact{
		{Key: "ledgerFactCount", Value: ledgerFactCount, Source: blockName},
		{Key: "externalCallCount", Value: externalCallCount, Source: blockName},
		{Key: "projectionPatchCount", Value: projectionPatchCount, Source: blockName},
	}
	facts = append(facts, extraFacts...)

	return facts
}
