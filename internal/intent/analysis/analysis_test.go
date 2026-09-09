package analysis_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/analysis"
)

func fixture(now time.Time) (analysis.AnalyticalRequest, analysis.AnalyticalResult, analysis.RecommendationRequest) {
	auth := analysis.Authorization{
		Allowed: true, TenantID: "tenant-a", OrganizationID: "org-a", PrincipalRef: "principal:hr",
		DecisionRef: "decision:analysis", DecisionDigest: "sha256:decision", Purpose: "workforce-planning",
		AllowedFields: []string{"worker.status", "worker.level"}, AllowedEvidence: []string{"evidence:status"},
		ValidUntil: now.Add(30 * time.Minute),
	}
	req := analysis.AnalyticalRequest{
		RequestID: "analysis-request:1", TenantID: "tenant-a", OrganizationID: "org-a", Purpose: "workforce-planning",
		DefinitionRef: "hcmnext.people.analyze_workforce/v1", QueryRef: "query:headcount", QueryVersion: "v3", QueryDigest: "sha256:query",
		CohortRef: "cohort:active-workers", CohortVersion: "v4", CohortDigest: "sha256:cohort", ModelRef: "model:attrition", ModelVersion: "v2", ModelDigest: "sha256:model",
		RequestedFields: []string{"worker.status"}, Authorization: auth, RequestedAt: now,
	}
	evidence := analysis.EvidenceRef{ID: "evidence:status", SourceRef: "source:hris", TransformationRef: "transform:normalize", AuthorityRef: "authority:hris", FieldPath: "worker.status", Digest: "sha256:evidence", Observed: now.Add(-time.Minute)}
	result := analysis.AnalyticalResult{
		ResultID: "result:1", RequestID: req.RequestID, TenantID: req.TenantID, OrganizationID: req.OrganizationID, Purpose: req.Purpose, DefinitionRef: req.DefinitionRef,
		QueryRef: req.QueryRef, QueryVersion: req.QueryVersion, QueryDigest: req.QueryDigest, CohortRef: req.CohortRef, CohortVersion: req.CohortVersion, CohortDigest: req.CohortDigest,
		ModelRef: req.ModelRef, ModelVersion: req.ModelVersion, ModelDigest: req.ModelDigest, ArtifactRef: "artifact:analysis:1",
		SourceWatermarks: []analysis.Watermark{{SourceRef: "source:hris", Version: "v91", Digest: "sha256:watermark", Observed: now.Add(-time.Minute)}},
		Evidence:         []analysis.EvidenceRef{evidence}, Uncertainty: analysis.Uncertainty{Class: "LIMITED", Confidence: "0.82", Limitations: []string{"descriptive evidence only"}},
		GeneratedAt: now.Add(-time.Second), ValidUntil: now.Add(10 * time.Minute),
	}
	sourced, err := analysis.SourceResult(req, result)
	if err != nil {
		panic(err)
	}
	recommendation := analysis.RecommendationRequest{
		Analysis: sourced, Authorization: auth,
		Action:           analysis.ActionSpec{DefinitionRef: "hcmnext.people.open_review/v1", CapabilityRef: "people.review.open/v1", InputDigest: "sha256:action-input"},
		SelectedEvidence: []string{"evidence:status"},
		Population:       analysis.Population{TenantID: "tenant-a", Ref: "population:active-workers", Version: "v2", Digest: "sha256:population", Authorized: true},
		CausalLink:       analysis.CausalLink{Kind: analysis.LinkAssociational, Basis: "role and level association", EvidenceIDs: []string{"evidence:status"}, Limitations: []string{"association is not causation"}},
		Governance:       analysis.GovernanceEvidence{DecisionRef: "decision:action", DecisionDigest: "sha256:action-decision", State: "ALLOW_WITH_OBLIGATIONS", ScopeDigest: "sha256:population", Purpose: "workforce-planning", EvaluatedAt: now.Add(-time.Second), ValidUntil: now.Add(5 * time.Minute)},
		Simulation:       analysis.SimulationEvidence{SimulationRef: "simulation:action", SimulationDigest: "sha256:simulation", ActionInputDigest: "sha256:action-input", Status: analysis.SimulationPass, EvaluatedAt: now.Add(-time.Second), ValidUntil: now.Add(5 * time.Minute)},
		Now:              now,
	}
	return req, result, recommendation
}

// TestAnalysisToIntentRequiresFreshGovernedProposal is the PRIMARY test for
// INTENT-020. Analysis must be sourced and current, action authority must be
// separately authorized, and the output remains a non-executable draft.
func TestAnalysisToIntentRequiresFreshGovernedProposal(t *testing.T) {
	now := time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)
	req, result, recommendation := fixture(now)
	if _, err := analysis.SourceResult(req, result); err != nil {
		t.Fatalf("source result: %v", err)
	}
	proposal, err := analysis.RecommendAction(recommendation)
	if err != nil {
		t.Fatalf("recommend action: %v", err)
	}
	if proposal.Status != analysis.ProposalDraft || proposal.Family != analysis.ProposalFamilyChange {
		t.Fatalf("proposal = %+v, want draft change request", proposal)
	}
	if proposal.ExecutionAuthority {
		t.Fatal("analytical recommendation granted execution authority")
	}
	if len(proposal.SelectedEvidence) != 1 || proposal.SelectedEvidence[0].FieldPath != "worker.status" {
		t.Fatalf("selected evidence = %+v, want one redaction-safe reference", proposal.SelectedEvidence)
	}

	stale := recommendation
	stale.Now = now.Add(11 * time.Minute)
	if _, err := analysis.RecommendAction(stale); !errors.Is(err, analysis.ErrStale) {
		t.Fatalf("stale result error = %v, want ErrStale", err)
	}

	unauthorized := recommendation
	unauthorized.SelectedEvidence = []string{"evidence:unlisted"}
	if _, err := analysis.RecommendAction(unauthorized); !errors.Is(err, analysis.ErrUnauthorized) {
		t.Fatalf("unauthorized evidence error = %v, want ErrUnauthorized", err)
	}
}

// TestTodo_INTENT_020_Golden verifies that the proposal digest and explanation
// are deterministic and do not disclose analytical values.
func TestTodo_INTENT_020_Golden(t *testing.T) {
	now := time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)
	_, _, recommendation := fixture(now)
	first, err := analysis.RecommendAction(recommendation)
	if err != nil {
		t.Fatalf("first recommendation: %v", err)
	}
	second, err := analysis.RecommendAction(recommendation)
	if err != nil {
		t.Fatalf("second recommendation: %v", err)
	}
	if first.Digest == "" || first.Digest != second.Digest || first.ProposalID != second.ProposalID {
		t.Fatalf("recommendation is not deterministic: first=%+v second=%+v", first, second)
	}
	if got := analysis.Explain(first); got == "" || containsAny(got, "worker.status", "source:hris", "artifact:analysis") {
		t.Fatalf("explanation disclosed analytical detail: %q", got)
	}
}

// TestTodo_INTENT_020_Mutation proves restricted evidence, stale governance
// and missing simulation cannot cross the analysis-to-action boundary.
func TestTodo_INTENT_020_Mutation(t *testing.T) {
	now := time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)
	_, _, recommendation := fixture(now)
	proposal, err := analysis.RecommendAction(recommendation)
	if err != nil {
		t.Fatalf("baseline recommendation: %v", err)
	}
	proposal.ExecutionAuthority = true
	if err := proposal.Validate(); !errors.Is(err, analysis.ErrExecutionNotGranted) {
		t.Fatalf("execution authority mutation error = %v, want ErrExecutionNotGranted", err)
	}

	restricted := recommendation
	restricted.Analysis.Evidence = append([]analysis.EvidenceRef(nil), recommendation.Analysis.Evidence...)
	restricted.Analysis.Evidence[0].Restricted = true
	if _, err := analysis.RecommendAction(restricted); !errors.Is(err, analysis.ErrRestrictedEvidence) {
		t.Fatalf("restricted evidence error = %v, want ErrRestrictedEvidence", err)
	}

	noGovernance := recommendation
	noGovernance.Governance.State = "UNKNOWN"
	if _, err := analysis.RecommendAction(noGovernance); !errors.Is(err, analysis.ErrGovernanceRequired) {
		t.Fatalf("unknown governance error = %v, want ErrGovernanceRequired", err)
	}

	noSimulation := recommendation
	noSimulation.Simulation.Status = "UNKNOWN"
	if _, err := analysis.RecommendAction(noSimulation); !errors.Is(err, analysis.ErrSimulationRequired) {
		t.Fatalf("unknown simulation error = %v, want ErrSimulationRequired", err)
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if len(needle) > 0 && stringContains(value, needle) {
			return true
		}
	}
	return false
}

func stringContains(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
