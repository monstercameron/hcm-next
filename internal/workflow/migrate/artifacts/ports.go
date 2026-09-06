package artifacts

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// Executor is the minimal database capability this package needs -- the same
// shape internal/workflow/migrate, internal/workflow/runtime and
// internal/data/runtimestate each declare, so the one transaction a
// WF-RUN-018 migration already runs in satisfies every port below.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// TimerRow is one pending wake promise, in the shape this package reads and
// writes it. Key is WF-RUN-004's durable timer key: the wake requirement's
// own content digest.
type TimerRow struct {
	TimerID uuid.UUID
	NodeID  string
	Key     string
	Kind    string
	FiresAt time.Time
	Version uint64
}

// TimerPort is the durable timer state one migration reads and re-keys.
type TimerPort interface {
	Pending(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]TimerRow, error)
	// Schedule writes one timer under the new epoch. It reports true when the
	// promise was already on the table, in which case nothing was written.
	Schedule(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, row TimerRow, at time.Time) (bool, error)
	// Cancel settles the predecessor promise under its own version.
	Cancel(ctx context.Context, ex Executor, tenantID, timerID uuid.UUID, version uint64, at time.Time, reason string) error
}

// SubscriptionRow is one open signal subscription.
type SubscriptionRow struct {
	SubscriptionID uuid.UUID
	NodeID         string
	SignalName     string
	CorrelationKey string
	Version        uint64
}

// SignalPort is the durable subscription state one migration reads and
// re-keys.
type SignalPort interface {
	Open(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]SubscriptionRow, error)
	// Subscribe opens one subscription under the new epoch, reporting true
	// when it already existed.
	Subscribe(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, row SubscriptionRow, at time.Time) (bool, error)
	Close(ctx context.Context, ex Executor, tenantID, subscriptionID uuid.UUID, version uint64, at time.Time) error
}

// ReadyRow is one unsettled unit of ready work.
type ReadyRow struct {
	ReadyWorkID uuid.UUID
	NodeID      string
	Attempt     int
	Priority    int
	State       string
	EligibleAt  time.Time
	Version     uint64
}

// ReadyWorkPort is the durable ready-work state one migration reads and
// re-keys.
type ReadyWorkPort interface {
	Pending(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]ReadyRow, error)
	Enqueue(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, row ReadyRow, at time.Time) (bool, error)
	Cancel(ctx context.Context, ex Executor, tenantID, readyWorkID uuid.UUID, version uint64, at time.Time) error
}

// ApprovalRow is one live approval work item the instance is waiting on.
// Owner is the canonical "kind:ref" rendering of where responsibility
// currently sits, which is exactly what a migration must not change.
type ApprovalRow struct {
	WorkItemID uuid.UUID
	NodeID     string
	Owner      string
	Status     string
	DeadlineAt time.Time
}

// ApprovalPort reads the approval work items an instance still owes. It is
// read-only on purpose: a routed work item's node is immutable through
// internal/humanwork/workitem's exported surface, and this package refuses to
// relocate one rather than minting a second unit of human responsibility for
// one decision.
type ApprovalPort interface {
	Pending(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]ApprovalRow, error)
}

// ChildRow is one parent/child edge the instance carries.
type ChildRow struct {
	Child        uuid.UUID
	ParentNodeID string
	Ordinal      int
	Mode         string
}

// Owed reports whether the parent still owes a continuation for this child.
// A DETACH child reports its own completion nowhere, so it is carried but
// never binds the parent's frontier node.
func (c ChildRow) Owed() bool { return c.Mode != childModeDetach }

// ChildPort reads the child links an instance carries.
type ChildPort interface {
	Links(ctx context.Context, ex Executor, tenantID, parent uuid.UUID) ([]ChildRow, error)
}

// LeaseRow is the live lease on one instance.
type LeaseRow struct {
	LeaseID    uuid.UUID
	Holder     string
	FenceToken uint64
	ExpiresAt  time.Time
}

// LeasePort reads the live lease on an instance, reporting false when nobody
// holds it. It is read-only: this package verifies ownership, it never
// acquires, extends or takes over a lease on a caller's behalf.
type LeasePort interface {
	Current(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) (LeaseRow, bool, error)
}

// ContinuationIntent is the one ledger row a migrated parent leaves behind:
// under the new epoch, this node is still awaiting its children.
//
// Its durable identity is derived from (instance, source node, attempt,
// target node, kind), so a parent awaiting five children records exactly one
// row and a replayed migration records none -- which is the whole of this
// ticket's "zero duplicate continuation".
type ContinuationIntent struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	NodeID     string
	Attempt    int
	// Ref names the awaited children, canonically joined, as evidence rather
	// than as identity.
	Ref        string
	RecordedAt time.Time
}

// ContinuationPort records the continuation ledger row a migrated parent
// leaves behind.
type ContinuationPort interface {
	Record(ctx context.Context, ex Executor, in ContinuationIntent) error
}

// Ports is the durable state one artifact migration reads and writes. Every
// port is required: a nil port would silently migrate zero artifacts of its
// kind, which is precisely the loss WF-RUN-026 exists to prevent, so
// [Migrate] refuses a set with a hole in it ([CodeMissingPort]).
type Ports struct {
	Leases        LeasePort
	Timers        TimerPort
	Signals       SignalPort
	ReadyWork     ReadyWorkPort
	Approvals     ApprovalPort
	Children      ChildPort
	Continuations ContinuationPort
}

func (p Ports) validate() error {
	missing := ""
	switch {
	case p.Leases == nil:
		missing = "Leases"
	case p.Timers == nil:
		missing = "Timers"
	case p.Signals == nil:
		missing = "Signals"
	case p.ReadyWork == nil:
		missing = "ReadyWork"
	case p.Approvals == nil:
		missing = "Approvals"
	case p.Children == nil:
		missing = "Children"
	case p.Continuations == nil:
		missing = "Continuations"
	}
	if missing != "" {
		return refuse(CodeMissingPort, "", missing,
			"no %s port supplied; a missing port would migrate zero artifacts of its kind silently", missing)
	}
	return nil
}
