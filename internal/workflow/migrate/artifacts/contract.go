package artifacts

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/workflow/lease"
	"github.com/monstercameron/hcm-next/internal/workflow/steps/wait"
)

// ContractVersion is the identity of the artifact-migration contract every
// handler in this package answers, and the version stamped into every
// [Receipt]. A change to what a handler is allowed to do to an artifact --
// which dispositions exist, what "preserved" means, what refuses -- is a new
// version, because a receipt digested under the old one no longer describes
// the same promise.
const ContractVersion = "hcmnext.workflow.migrate.artifacts/v1"

// Kind names one kind of pending runtime artifact.
type Kind string

// The declared artifact kinds, in the order [Handlers] runs them.
const (
	// KindLease is the instance's own ownership: the live workflow_lease row
	// and the fence its holder presents. It runs first because every later
	// write is made under it.
	KindLease Kind = "LEASE"
	// KindTimer is a durable wake promise (workflow_timer), keyed on the wait
	// requirement's own content digest per WF-RUN-004.
	KindTimer Kind = "TIMER"
	// KindSignal is an open signal subscription
	// (workflow_signal_subscription).
	KindSignal Kind = "SIGNAL_SUBSCRIPTION"
	// KindReadyWork is an unsettled unit of ready work
	// (workflow_ready_work).
	KindReadyWork Kind = "READY_WORK"
	// KindApproval is a live approval work item (work_item) the instance is
	// waiting on.
	KindApproval Kind = "APPROVAL"
	// KindChildContinuation is a child instance the parent still owes a
	// continuation for (workflow_child_link), plus the one continuation
	// ledger row that records the parent still awaiting under the new epoch.
	KindChildContinuation Kind = "CHILD_CONTINUATION"
)

// Kinds returns every declared artifact kind, in handler order.
func Kinds() []Kind {
	return []Kind{KindLease, KindTimer, KindSignal, KindReadyWork, KindApproval, KindChildContinuation}
}

// Valid reports whether k is a declared artifact kind.
func (k Kind) Valid() bool {
	for _, declared := range Kinds() {
		if k == declared {
			return true
		}
	}
	return false
}

// order is the kind's position in handler order, used to sort a receipt's
// entries canonically.
func (k Kind) order() int {
	for i, declared := range Kinds() {
		if k == declared {
			return i
		}
	}
	return len(Kinds())
}

// Disposition is what one handler did with one artifact. There are exactly
// three: a refusal is an error, never a disposition, so a receipt can never
// record that something was refused and the migration went ahead anyway.
type Disposition string

// The declared dispositions.
const (
	// Carried reports an artifact whose durable identity is unchanged under
	// the new epoch. Nothing was written: the artifact already is what the
	// new epoch needs it to be.
	Carried Disposition = "CARRIED"
	// Rekeyed reports an artifact written afresh under the new epoch's key
	// and its predecessor settled, with semantic identity, owner and deadline
	// preserved exactly.
	Rekeyed Disposition = "REKEYED"
	// Deduplicated reports a re-key whose target already existed -- a
	// replayed migration -- so the second write was a no-op and no duplicate
	// artifact was created.
	Deduplicated Disposition = "DEDUPLICATED"
)

// Contract is the declared migration contract for one artifact kind: what
// this package promises about it, in a form a caller can read before it
// commits to a migration rather than discovering at a refusal.
type Contract struct {
	Version string
	Kind    Kind
	// Relocatable reports whether an artifact of this kind can be moved onto
	// a different frontier node. False means the kind's durable identity
	// binds it to the node that raised it, and a migration that moves the
	// frontier off that node is refused as [CodeNotRelocatable].
	Relocatable bool
	// PreservesDeadline reports whether the kind carries an instant that the
	// migration must reproduce exactly.
	PreservesDeadline bool
	// PreservesOwner reports whether the kind carries an owner the migration
	// must reproduce exactly.
	PreservesOwner bool
}

// Contracts returns the declared contract for every artifact kind, in handler
// order.
func Contracts() []Contract {
	return []Contract{
		{Version: ContractVersion, Kind: KindLease, Relocatable: true, PreservesDeadline: true, PreservesOwner: true},
		{Version: ContractVersion, Kind: KindTimer, Relocatable: true, PreservesDeadline: true},
		{Version: ContractVersion, Kind: KindSignal, Relocatable: true},
		{Version: ContractVersion, Kind: KindReadyWork, Relocatable: true, PreservesDeadline: true},
		{Version: ContractVersion, Kind: KindApproval, Relocatable: false, PreservesDeadline: true, PreservesOwner: true},
		{Version: ContractVersion, Kind: KindChildContinuation, Relocatable: false},
	}
}

// ContractFor returns the declared contract for one kind.
func ContractFor(k Kind) (Contract, bool) {
	for _, c := range Contracts() {
		if c.Kind == k {
			return c, true
		}
	}
	return Contract{}, false
}

// Epoch is one side of a migration: which compiled plan the instance is
// pinned to and which single node its frontier carries under that pin.
//
// It is deliberately the same shape on both sides. "The new epoch" is not a
// counter this package mints; it is the target plan's own identity plus the
// frontier [migrate.Migrate] left the instance on, which is what makes an
// artifact's re-keyed identity reproducible by anyone holding the same two
// plans.
type Epoch struct {
	WorkflowVersion    uint32
	CompiledPlanDigest string
	NodeID             string
	// Attempt is the node execution attempt the frontier node sits at under
	// this epoch. Ready work is keyed on it.
	Attempt int
}

func (e Epoch) validate(side string) error {
	switch {
	case e.CompiledPlanDigest == "":
		return refuse(CodeInvalidRequest, "", side, "%s epoch names no compiled plan digest", side)
	case e.NodeID == "":
		return refuse(CodeInvalidRequest, "", side, "%s epoch names no frontier node", side)
	case e.Attempt < 1:
		return refuse(CodeInvalidRequest, "", side, "%s epoch's frontier attempt starts at 1", side)
	}
	return nil
}

// Scope is the migration frame every handler shares: whose instance, from
// which epoch to which, under whose fence, at whose instant.
type Scope struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID

	From Epoch
	To   Epoch

	// Fence is the lease the migrating caller holds on the instance. The zero
	// value asserts that nobody holds the instance; if a live lease exists
	// anyway the run refuses ([CodeStaleLease]) rather than migrating another
	// holder's artifacts.
	Fence lease.Fence

	// Requirements maps a pending timer's current durable key -- the wake
	// requirement digest WF-RUN-004 keys workflow_timer on -- onto the
	// requirement the target plan computes for the same wait. It is the only
	// input this package cannot derive itself: recomputing a wake requirement
	// needs the target plan's node, its zone, its calendar and the dataset
	// versions in force, all of which internal/workflow/steps/wait already
	// resolves purely for a caller that holds them.
	//
	// A timer whose key is absent from this map is carried unchanged, which
	// is correct whenever the migration does not move the frontier off the
	// node that raised it. A timer that must move and has no replacement
	// requirement is [CodeNotRelocatable].
	Requirements map[string]wait.TimerRequirement

	MigratedBy string
	// MigratedAt is the caller's own instant. This package reads no clock.
	MigratedAt time.Time
}

// Relocating reports whether this migration moves the instance's frontier
// onto a different node, which is what turns a carry into a re-key.
func (s Scope) Relocating() bool { return s.From.NodeID != s.To.NodeID }

func (s Scope) validate() error {
	switch {
	case s.TenantID == uuid.Nil:
		return refuse(CodeInvalidRequest, "", "", "tenant id must not be the nil UUID")
	case s.InstanceID == uuid.Nil:
		return refuse(CodeInvalidRequest, "", "", "instance id must not be the nil UUID")
	case s.MigratedBy == "":
		return refuse(CodeInvalidRequest, "", s.InstanceID.String(), "no principal named as executing this migration")
	case s.MigratedAt.IsZero():
		return refuse(CodeInvalidRequest, "", s.InstanceID.String(),
			"migrated_at must be supplied; this package never reads a wall clock")
	}
	if err := s.From.validate("source"); err != nil {
		return err
	}
	if err := s.To.validate("target"); err != nil {
		return err
	}
	if s.From.CompiledPlanDigest == s.To.CompiledPlanDigest && s.From.NodeID == s.To.NodeID {
		return refuse(CodeInvalidRequest, "", s.InstanceID.String(),
			"source and target epochs are identical; there is no migration to apply")
	}
	return nil
}

// Handler migrates every pending artifact of one kind.
//
// Handlers are the only thing that touches durable artifact state here, they
// all answer [ContractVersion], and they all obey the same three rules: read
// through the supplied [Ports] and nowhere else, write only what the scope's
// target epoch requires, and return an error rather than a disposition
// whenever an artifact cannot be moved faithfully.
type Handler interface {
	Kind() Kind
	Migrate(ctx context.Context, ex Executor, scope Scope, ports Ports) ([]Entry, error)
}

// Handlers returns one handler per declared kind, in the fixed order
// [Migrate] runs them: lease first, because every later write is made under
// the ownership it establishes, then the three machine-owned waits, then the
// two human- and child-owned artifacts whose identity is immutable.
func Handlers() []Handler {
	return []Handler{
		leaseHandler{},
		timerHandler{},
		signalHandler{},
		readyWorkHandler{},
		approvalHandler{},
		childHandler{},
	}
}

// Barrier is the seam between two handlers.
//
// [Migrate] calls Enter with the kind it is about to run, before that handler
// touches anything. A Barrier that returns an error stops the run there with
// [CodeBarrierFailed], leaving exactly the writes the earlier handlers made
// for the caller to roll back -- which is how this ticket's fault case proves
// a partial artifact migration is never committed. A nil Barrier enters every
// handler.
type Barrier interface {
	Enter(kind Kind) error
}
