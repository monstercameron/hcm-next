package execute_test

import (
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func approvedStartFacts(rev intent.ProposalRevision) runtime.ApprovalFacts {
	return runtime.MemoryApprovalFacts{ByRevisionID: map[string][]runtime.ApprovalDecisionFact{
		rev.ProposalRevisionID: {{
			DecisionID:     "decision:execute-test-start",
			Outcome:        runtime.ApprovalOutcomeApproved,
			ProposalDigest: rev.MaterialDigest.Digest,
		}},
	}}
}
