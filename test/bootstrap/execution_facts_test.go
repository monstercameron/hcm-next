package bootstrap_test

import (
	"context"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// approvedExecutionFacts is a composition-time double for the direct
// ExecuteIntent driver tests. The journey tests intentionally omit it so they
// exercise DurableProposalFacts and prove that caller flags cannot authorize a
// start without a recorded decision.
type approvedExecutionFacts struct{}

func (approvedExecutionFacts) Supersession(context.Context, runtime.Executor, uuid.UUID, intent.ProposalRevision) (runtime.ProposalSupersessionFact, error) {
	return runtime.ProposalSupersessionFact{}, nil
}

func (approvedExecutionFacts) Decisions(_ context.Context, _ runtime.Executor, _ uuid.UUID, rev intent.ProposalRevision) ([]runtime.ApprovalDecisionFact, error) {
	return []runtime.ApprovalDecisionFact{{
		DecisionID:     "decision:bootstrap-execution-fixture",
		Outcome:        runtime.ApprovalOutcomeApproved,
		ProposalDigest: rev.MaterialDigest.Digest,
	}}, nil
}

var _ app.ExecutionFacts = approvedExecutionFacts{}
