package transaction

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// IsolationLevel is the isolation the boundary's single local ACID commit
// protects the admitted participants with.
type IsolationLevel uint8

// Isolation levels. IsolationUnspecified is the zero value and never legal
// on a resolvable boundary.
const (
	IsolationUnspecified IsolationLevel = iota
	IsolationReadCommitted
	IsolationSnapshot
	IsolationSerializable
)

var isolationWire = map[IsolationLevel]string{
	IsolationReadCommitted: "READ_COMMITTED",
	IsolationSnapshot:      "SNAPSHOT",
	IsolationSerializable:  "SERIALIZABLE",
}

// String returns the stable wire token, or "ISOLATION_UNSPECIFIED".
func (l IsolationLevel) String() string {
	if s, ok := isolationWire[l]; ok {
		return s
	}
	return "ISOLATION_UNSPECIFIED"
}

// Valid reports whether l is a declared isolation level.
func (l IsolationLevel) Valid() bool { _, ok := isolationWire[l]; return ok }

// CommitProtocol names the commit mechanism a boundary uses to make its
// admitted participants durable together.
type CommitProtocol uint8

// Commit protocols. CommitProtocolUnspecified is the zero value and never
// legal on a resolvable boundary.
const (
	CommitProtocolUnspecified CommitProtocol = iota
	CommitProtocolSingleDatabaseACID
	CommitProtocolTwoPhaseCommit
)

var protocolWire = map[CommitProtocol]string{
	CommitProtocolSingleDatabaseACID: "SINGLE_DATABASE_ACID",
	CommitProtocolTwoPhaseCommit:     "TWO_PHASE_COMMIT",
}

// String returns the stable wire token, or "COMMIT_PROTOCOL_UNSPECIFIED".
func (p CommitProtocol) String() string {
	if s, ok := protocolWire[p]; ok {
		return s
	}
	return "COMMIT_PROTOCOL_UNSPECIFIED"
}

// Valid reports whether p is a declared commit protocol.
func (p CommitProtocol) Valid() bool { _, ok := protocolWire[p]; return ok }

// CrossBoundaryDisposition names what a boundary declares for a participant
// it does not admit into its local ACID set: the plan's own Local=false
// flag already routes such a participant to Effects, and this value is
// carried onto the [Resolution] as the disposition downstream consumers
// (workflow, saga dispatch) must honor for it.
type CrossBoundaryDisposition uint8

// Cross-boundary dispositions. CrossBoundaryDispositionUnspecified is the
// zero value and never legal on a resolvable boundary.
const (
	CrossBoundaryDispositionUnspecified CrossBoundaryDisposition = iota
	CrossBoundaryDispositionReject
	CrossBoundaryDispositionSplitIntoEffects
	CrossBoundaryDispositionChildTransaction
)

var dispositionWire = map[CrossBoundaryDisposition]string{
	CrossBoundaryDispositionReject:           "REJECT",
	CrossBoundaryDispositionSplitIntoEffects: "SPLIT_INTO_EFFECTS",
	CrossBoundaryDispositionChildTransaction: "CHILD_TRANSACTION",
}

// String returns the stable wire token, or
// "CROSS_BOUNDARY_DISPOSITION_UNSPECIFIED".
func (d CrossBoundaryDisposition) String() string {
	if s, ok := dispositionWire[d]; ok {
		return s
	}
	return "CROSS_BOUNDARY_DISPOSITION_UNSPECIFIED"
}

// Valid reports whether d is a declared disposition.
func (d CrossBoundaryDisposition) Valid() bool { _, ok := dispositionWire[d]; return ok }

// AdmissionSelector names one storage-class/stream-prefix pair a
// ConsistencyBoundary admits into its local ACID set. An empty StreamPrefix
// matches every stream of the storage class.
type AdmissionSelector struct {
	StorageClass string
	StreamPrefix string
}

func (s AdmissionSelector) matches(storageClass, streamID string) bool {
	return s.StorageClass == storageClass && strings.HasPrefix(streamID, s.StreamPrefix)
}

// ConsistencyBoundary is explicit data naming one local ACID commit
// boundary: which tenant, cell and storage a coordinator's single database
// transaction may admit, under which isolation level, commit protocol and
// coordinator fence epoch, and what a participant outside the boundary
// becomes.
//
// It is deliberately a plain value, never inferred from deployment topology
// at resolution time -- the TX-002 REFACTOR rule. Two callers holding the
// same ConsistencyBoundary value and the same plan always resolve it
// identically.
type ConsistencyBoundary struct {
	BoundaryID    string
	Tenant        values.TenantId
	CellID        string
	CoordinatorID string

	Admitted  []AdmissionSelector
	Isolation IsolationLevel
	Protocol  CommitProtocol

	// CoordinatorEpoch is the fence epoch this boundary is currently
	// registered under. It increments on every coordinator failover; a
	// resolution attempted under any other epoch is rejected outright
	// rather than admitted under a coordinator that no longer holds the
	// fence.
	CoordinatorEpoch uint64

	// MaxParticipants bounds how many participants one local transaction may
	// admit. Zero means unbounded.
	MaxParticipants int

	CrossBoundaryDisposition CrossBoundaryDisposition

	// Version identifies this boundary declaration's own revision, so a
	// republished boundary (a new cell, a widened admission set) is
	// distinguishable from the one an earlier resolution was fenced to.
	Version uint64
}

// Validate rejects a boundary that cannot resolve anything.
func (b ConsistencyBoundary) Validate() error {
	switch {
	case b.BoundaryID == "":
		return fmt.Errorf("%w: boundary has no id", ErrInvalidBoundary)
	case b.Tenant.Validate() != nil:
		return fmt.Errorf("%w: %v", ErrInvalidBoundary, b.Tenant.Validate())
	case b.CellID == "":
		return fmt.Errorf("%w: boundary %s names no cell", ErrInvalidBoundary, b.BoundaryID)
	case b.CoordinatorID == "":
		return fmt.Errorf("%w: boundary %s names no coordinator", ErrInvalidBoundary, b.BoundaryID)
	case len(b.Admitted) == 0:
		return fmt.Errorf("%w: boundary %s admits no storage selector", ErrInvalidBoundary, b.BoundaryID)
	case !b.Isolation.Valid():
		return fmt.Errorf("%w: boundary %s declares no isolation level", ErrInvalidBoundary, b.BoundaryID)
	case !b.Protocol.Valid():
		return fmt.Errorf("%w: boundary %s declares no commit protocol", ErrInvalidBoundary, b.BoundaryID)
	case !b.CrossBoundaryDisposition.Valid():
		return fmt.Errorf("%w: boundary %s declares no cross-boundary disposition", ErrInvalidBoundary, b.BoundaryID)
	case b.MaxParticipants < 0:
		return fmt.Errorf("%w: boundary %s declares a negative participant cap", ErrInvalidBoundary, b.BoundaryID)
	}
	for i, sel := range b.Admitted {
		if sel.StorageClass == "" {
			return fmt.Errorf("%w: boundary %s admission selector %d names no storage class",
				ErrInvalidBoundary, b.BoundaryID, i)
		}
	}
	return nil
}

func (b ConsistencyBoundary) admits(p intent.PlanParticipant) bool {
	for _, sel := range b.Admitted {
		if sel.matches(p.StorageClass, p.StreamID) {
			return true
		}
	}
	return false
}

// BoundaryFence is the coordinator fence a [Resolution] was produced under.
// It binds the exact boundary, coordinator and epoch so a resolution can
// never be replayed as if it were produced under a different or later
// coordinator term.
type BoundaryFence struct {
	BoundaryID    string
	CoordinatorID string
	Epoch         uint64
}

// Token returns the fence's stable text form:
// "<boundary_id>@<coordinator_id>#<epoch>".
func (f BoundaryFence) Token() string {
	return f.BoundaryID + "@" + f.CoordinatorID + "#" + strconv.FormatUint(f.Epoch, 10)
}

// Resolution is the result of resolving a compiled plan's participants
// against a ConsistencyBoundary: which participants are admitted into the
// single local ACID commit, which become durable effects outside it, the
// coordinator fence the resolution was produced under, and the canonical
// lock order the admitted streams must be acquired in.
//
// It is an explicit value with its own digest, never a live decision
// recomputed silently from ambient state: two resolutions that admit
// different participants, disagree on lock order, or bind a different fence
// always produce different digests (see [Resolution.VerifyDigest]).
type Resolution struct {
	PlanID string

	Fence    BoundaryFence
	Admitted []intent.PlanParticipant
	Effects  []intent.PlanParticipant

	// LockOrder is the admitted participants' distinct stream ids in
	// LEDGER-003's canonical lock order: ascending byte order of the stream
	// key. Two coordinators resolving the same admitted set always compute
	// the same acquisition order, which is what makes "no process-local
	// worker lock is treated as business conflict control" safe -- the
	// order itself is a reproducible function of the data, not of
	// whichever goroutine got there first.
	LockOrder []string

	CrossBoundaryDisposition CrossBoundaryDisposition

	// Digest derives from the resolution's canonical semantic content, not
	// from field layout or slice construction order.
	Digest string
}

// ResolveConsistencyBoundary resolves plan's participants against boundary
// under observedEpoch, the coordinator epoch the caller captured when it
// began resolving.
//
// It admits into the local ACID set exactly the participants that are both
// declared Local by the plan and matched by one of the boundary's admission
// selectors. Every participant the plan itself declared non-local
// (Local == false) always becomes a durable effect, regardless of whether
// its storage class happens to match an admission selector: a remote
// effect is never promoted into the local ACID set by this function.
//
// A participant that declares itself Local but that the boundary does not
// admit fails resolution outright with [ErrUnsupportedStream] rather than
// being silently promoted to an effect or silently dropped: a local write
// the coordinator cannot actually commit locally is a compilation defect,
// not a routing decision. A plan whose tenant is outside the boundary's own
// tenant, or that was resolved under any epoch but the boundary's current
// one, fails closed the same way.
func ResolveConsistencyBoundary(boundary ConsistencyBoundary, plan intent.TransactionPlan, observedEpoch uint64) (Resolution, error) {
	if err := boundary.Validate(); err != nil {
		return Resolution{}, err
	}
	if plan.PlanID == "" {
		return Resolution{}, fmt.Errorf("%w: plan has no id", ErrInvalidResolution)
	}
	if plan.Tenant != boundary.Tenant {
		return Resolution{}, fmt.Errorf("%w: plan %s tenant %q, boundary %s tenant %q",
			ErrTenantMismatch, plan.PlanID, plan.Tenant, boundary.BoundaryID, boundary.Tenant)
	}
	if observedEpoch != boundary.CoordinatorEpoch {
		return Resolution{}, fmt.Errorf("%w: plan %s resolved under epoch %d, boundary %s is fenced at epoch %d",
			ErrStaleCoordinatorEpoch, plan.PlanID, observedEpoch, boundary.BoundaryID, boundary.CoordinatorEpoch)
	}
	if len(plan.Participants) == 0 {
		return Resolution{}, fmt.Errorf("%w: plan %s declares no participant", ErrInvalidResolution, plan.PlanID)
	}
	if boundary.MaxParticipants > 0 && len(plan.Participants) > boundary.MaxParticipants {
		return Resolution{}, fmt.Errorf("%w: plan %s has %d participant(s), boundary %s admits at most %d",
			ErrBoundaryOverCapacity, plan.PlanID, len(plan.Participants), boundary.BoundaryID, boundary.MaxParticipants)
	}

	var admitted, effects []intent.PlanParticipant
	seen := make(map[string]bool, len(plan.Participants))
	for _, p := range plan.Participants {
		if p.ParticipantID == "" || p.StreamID == "" || p.StorageClass == "" {
			return Resolution{}, fmt.Errorf("%w: plan %s carries a participant with no id, stream or storage class",
				ErrInvalidResolution, plan.PlanID)
		}
		if seen[p.ParticipantID] {
			return Resolution{}, fmt.Errorf("%w: plan %s names participant %q twice",
				ErrInvalidResolution, plan.PlanID, p.ParticipantID)
		}
		seen[p.ParticipantID] = true

		if !p.Local {
			// A participant the plan itself declared non-local is never
			// admitted, regardless of whether it would otherwise match a
			// selector.
			effects = append(effects, p)
			continue
		}
		if !boundary.admits(p) {
			return Resolution{}, fmt.Errorf(
				"%w: plan %s participant %q (stream %q, storage %q) matches no admission selector of boundary %s",
				ErrUnsupportedStream, plan.PlanID, p.ParticipantID, p.StreamID, p.StorageClass, boundary.BoundaryID)
		}
		admitted = append(admitted, p)
	}
	if len(admitted) == 0 {
		return Resolution{}, fmt.Errorf("%w: plan %s admits no participant into the local ACID set",
			ErrInvalidResolution, plan.PlanID)
	}

	lockOrderSet := make(map[string]bool, len(admitted))
	lockOrder := make([]string, 0, len(admitted))
	for _, p := range admitted {
		if lockOrderSet[p.StreamID] {
			continue
		}
		lockOrderSet[p.StreamID] = true
		lockOrder = append(lockOrder, p.StreamID)
	}
	sort.Strings(lockOrder)

	res := Resolution{
		PlanID: plan.PlanID,
		Fence: BoundaryFence{
			BoundaryID:    boundary.BoundaryID,
			CoordinatorID: boundary.CoordinatorID,
			Epoch:         boundary.CoordinatorEpoch,
		},
		Admitted:                 admitted,
		Effects:                  effects,
		LockOrder:                lockOrder,
		CrossBoundaryDisposition: boundary.CrossBoundaryDisposition,
	}
	res.Digest = res.computeDigest()
	return res, nil
}

// VerifyDigest recomputes the resolution digest from its canonical content
// and reports whether the recorded digest matches. A resolution is never
// trusted on the word of the digest it carries.
func (r Resolution) VerifyDigest() error {
	if got := r.computeDigest(); got != r.Digest {
		return fmt.Errorf("%w: resolution for plan %s records digest %q but its content hashes to %q",
			ErrInvalidResolution, r.PlanID, r.Digest, got)
	}
	return nil
}
