package execute_test

import (
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
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
