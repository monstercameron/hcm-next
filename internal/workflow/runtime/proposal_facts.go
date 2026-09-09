package runtime

import (
	"context"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

// MemoryProposalFacts is an in-process [ProposalFacts] test double: it
// reports exactly the [ProposalSupersessionFact] it was configured with, one
// per proposal revision id, and the zero value (not superseded) for any
// revision it was not told about.
//
// It is the double the WF-RUN-027 test matrix uses wherever a real proposal
// store is not the point of the test; a caller wanting a durable Postgres
// adapter uses internal/platform/execution's instead.
type MemoryProposalFacts struct {
	Facts map[string]ProposalSupersessionFact
}

var _ ProposalFacts = MemoryProposalFacts{}

// Supersession implements [ProposalFacts].
func (f MemoryProposalFacts) Supersession(
	_ context.Context, _ Executor, _ uuid.UUID, rev intent.ProposalRevision,
) (ProposalSupersessionFact, error) {
	if f.Facts == nil {
		return ProposalSupersessionFact{}, nil
	}
	return f.Facts[rev.ProposalRevisionID], nil
}

// MemoryApprovalFacts is an in-process [ApprovalFacts] test double: it
// reports exactly the [ApprovalDecisionFact] slice it was configured with,
// one per proposal revision id, and no decisions at all for any revision it
// was not told about.
type MemoryApprovalFacts struct {
	ByRevisionID map[string][]ApprovalDecisionFact
}

var _ ApprovalFacts = MemoryApprovalFacts{}

// Decisions implements [ApprovalFacts].
func (f MemoryApprovalFacts) Decisions(
	_ context.Context, _ Executor, _ uuid.UUID, rev intent.ProposalRevision,
) ([]ApprovalDecisionFact, error) {
	if f.ByRevisionID == nil {
		return nil, nil
	}
	return f.ByRevisionID[rev.ProposalRevisionID], nil
}
