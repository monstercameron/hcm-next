package execute

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// Beginner opens the transactions that fence Start and each Advance. A pool
// or a dedicated connection can satisfy it.
type Beginner interface {
	Begin(ctx context.Context) (dbport.Tx, error)
}

// AdvanceFunc is the runtime advancement boundary. Production wiring uses
// runtime.Advance; naming the function lets the driver be tested without
// pretending an in-memory transaction is PostgreSQL.
type AdvanceFunc func(context.Context, runtime.Executor, runtime.AdvanceRequest) (runtime.AdvanceReceipt, error)

// StepRequest is the complete runtime context handed to one synchronous READY
// step. Proposal content is immutable and the compiled node is from the exact
// plan Start pinned.
type StepRequest struct {
	TenantID        uuid.UUID
	InstanceID      uuid.UUID
	InstanceVersion int64
	Attempt         int
	Node            workflow.CompiledNode
	Plan            *workflow.CompiledWorkflow
	Proposal        runtime.ProposalBinding
	CorrelationID   string
	RecordedAt      time.Time
	// TraceID is the ambient trace id the driver read off the incoming span
	// context (OBS-023). Empty when the caller carried no trace context.
	TraceID string
}

// StepRunner executes one READY node and returns only its typed outcome and
// governance references. It must not schedule a successor or create human
// work; runtime.Advance derives those continuations from the pinned plan.
type StepRunner interface {
	Run(ctx context.Context, req StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error)
}

// WorkItemRequest asks the human-work adapter to create and route the item a
// WORK_ITEM_REQUIRED continuation describes. WorkItemID is deterministic and
// must be used as supplied.
type WorkItemRequest struct {
	WorkItemID    uuid.UUID
	Continuation  runtime.ContinuationRecord
	Proposal      runtime.ProposalBinding
	CellID        string
	CorrelationID string
	SubjectRefs   []string
	CreatedAt     time.Time
}

// WorkItemFactory owns the policy-specific fields and assignment resolution a
// real WorkItem requires. Its writes run through ex, the same transaction as
// the runtime advancement that raised the continuation.
type WorkItemFactory interface {
	CreateAndRoute(ctx context.Context, ex workitem.Executor, req WorkItemRequest) (workitem.WorkItem, error)
}

// TerminalWriteRequest is the governed business-terminal write. The writer
// must append its ledger event, apply its projection and enqueue its outbox
// message through tx, and return the identities TX-006 stores for replay.
type TerminalWriteRequest struct {
	TenantID       uuid.UUID
	InstanceID     uuid.UUID
	WorkflowID     string
	PlanDigest     string
	Proposal       runtime.ProposalBinding
	TerminalCode   string
	CorrelationID  string
	IdempotencyKey string
	RecordedAt     time.Time
	// EndNodeID and EndOutputDigest are the completed END node and its typed
	// output digest (WF-RUN-030), recorded verbatim from the outcome that
	// reached this terminal rather than discarded before the write.
	EndNodeID       string
	EndOutputDigest string
}

// TerminalWriter performs the single governed terminal write inside the
// transaction supplied by the driver. It must have no out-of-transaction side
// channel.
type TerminalWriter interface {
	Write(ctx context.Context, tx dbport.Tx, req TerminalWriteRequest) (idempotency.ResultIdentity, error)
}

// WorkItemReader loads the durable WorkItem a [Driver.Resume] advances from,
// inside the same transaction as the advancement it feeds -- WF-RUN-028's
// replacement for a [ResumeRequest] that carried a caller-assembled
// [workitem.WorkItem] struct. internal/platform/execution's thin adapter over
// internal/humanwork/workitem.Store is the production implementation; a test
// composes its own double.
type WorkItemReader interface {
	Load(ctx context.Context, ex workitem.Executor, tenantID, workItemID uuid.UUID) (workitem.WorkItem, error)
}
