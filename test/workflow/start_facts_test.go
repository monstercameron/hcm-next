package workflow_test

import (
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// approvedStartFacts is the explicit in-memory fact source used by the
// end-to-end fixtures. It mirrors the durable decision shape without allowing
// ProposalBinding's deprecated caller fields to authorize a start.
func approvedStartFacts(rev intent.ProposalRevision) runtime.ApprovalFacts {
	return runtime.MemoryApprovalFacts{ByRevisionID: map[string][]runtime.ApprovalDecisionFact{
		rev.ProposalRevisionID: {{
			DecisionID:     "decision:test-workflow-start",
			Outcome:        runtime.ApprovalOutcomeApproved,
			ProposalDigest: rev.MaterialDigest.Digest,
		}},
	}}
}
