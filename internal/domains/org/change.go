// Manager change is the minimal pure-HRIS workflow (CONF-007): one PRIMARY
// manager edge ends and another begins through the closed story
// proposed/validated/approved/scheduled/revalidated/changed/observed/
// reconciled. The derived org hierarchy stays a projection: this file owns
// the edge lifecycle, never an extra authority.
package org

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Sentinel causes. Classify with errors.Is.
var (
	// ErrInactiveManager reports a worker or manager who is not active
	// and eligible on the effective date.
	ErrInactiveManager = errors.New("org: manager change actor is not active")

	// ErrSelfManager reports a proposal naming the worker as their own
	// manager.
	ErrSelfManager = errors.New("org: worker cannot manage themselves")

	// ErrManagerCycle reports a proposal that would close a management
	// loop.
	ErrManagerCycle = errors.New("org: manager change would create a cycle")

	// ErrOverlappingPrimary reports a second PRIMARY edge overlapping the
	// effective date.
	ErrOverlappingPrimary = errors.New("org: overlapping primary manager")

	// ErrFutureConflict reports a transfer, termination, leave, org move
	// or future manager change colliding with the effective date.
	ErrFutureConflict = errors.New("org: future conflict blocks manager change")

	// ErrStaleAuthority reports approvals bound to an authority version
	// that has since moved.
	ErrStaleAuthority = errors.New("org: approval authority is stale")

	// ErrProposalTampered reports a commit-time proposal that no longer
	// matches the approved digest.
	ErrProposalTampered = errors.New("org: proposal changed after approval")

	// ErrChangeState reports a lifecycle step taken out of order.
	ErrChangeState = errors.New("org: manager change in wrong state")

	// ErrReconcileDrift reports an authoritative observation that does
	// not match the applied edge.
	ErrReconcileDrift = errors.New("org: observed manager does not match applied change")
)

// ChangeState is the closed manager-change story vocabulary.
type ChangeState string

const (
	ChangeProposed    ChangeState = "PROPOSED"
	ChangeValidated   ChangeState = "VALIDATED"
	ChangeApproved    ChangeState = "APPROVED"
	ChangeScheduled   ChangeState = "SCHEDULED"
	ChangeRevalidated ChangeState = "REVALIDATED"
	ChangeChanged     ChangeState = "CHANGED"
	ChangeObserved    ChangeState = "OBSERVED"
	ChangeReconciled  ChangeState = "RECONCILED"
)

// Person is one directory actor with an activity flag.
type Person struct {
	ID     string
	Active bool
}

// Edge is one PRIMARY manager edge revision.
type Edge struct {
	EdgeID    string
	WorkerID  string
	ManagerID string
	StartDate string
	EndDate   string
	Ended     bool
	Revision  uint64
}

// FutureEvent is one transfer, termination, leave, org move or scheduled
// manager change that can collide with a change.
type FutureEvent struct {
	WorkerID      string
	Kind          string
	EffectiveDate string
}

// Directory is the deterministic fixture authority: people, edges, future
// events, the graph version and the approval-authority version.
type Directory struct {
	People           map[string]Person
	Edges            map[string]Edge
	Future           []FutureEvent
	GraphVersion     uint64
	AuthorityVersion uint64
}

// Approval is one governed decision bound to an authority version.
type Approval struct {
	ApproverID       string
	Role             string
	AuthorityVersion uint64
}

// ManagerChange is one in-flight change with its appended story.
type ManagerChange struct {
	ID              string
	WorkerID        string
	OldEdgeID       string
	OldManagerID    string
	NewManagerID    string
	EffectiveDate   string
	Reason          string
	ProposalDigest  string
	ApprovedDigest  string
	Approvals       []Approval
	GraphVersion    uint64
	AppliedEdgeID   string
	ObservedManager string
	Story           []ChangeState
}

func changeDigest(c ManagerChange) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"manager-change", c.WorkerID, c.OldEdgeID, c.OldManagerID,
		c.NewManagerID, c.EffectiveDate, c.Reason,
	}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (c *ManagerChange) append(state ChangeState) {
	c.Story = append(c.Story, state)
}

// Propose binds one immutable proposal to the exact old edge.
func Propose(id, workerID, oldEdgeID, newManagerID, effectiveDate, reason string, dir Directory) (ManagerChange, error) {
	old, ok := dir.Edges[oldEdgeID]
	if !ok || old.WorkerID != workerID || old.Ended {
		return ManagerChange{}, fmt.Errorf("org: Propose %s: %w", id, ErrOverlappingPrimary)
	}
	change := ManagerChange{
		ID: id, WorkerID: workerID, OldEdgeID: oldEdgeID, OldManagerID: old.ManagerID,
		NewManagerID: newManagerID, EffectiveDate: effectiveDate, Reason: reason,
		GraphVersion: dir.GraphVersion,
	}
	change.ProposalDigest = changeDigest(change)
	change.append(ChangeProposed)
	return change, nil
}

func active(dir Directory, id string) bool {
	person, ok := dir.People[id]
	return ok && person.Active
}

// chain reports whether target appears in the management chain above start.
func chain(dir Directory, start, target, date string) bool {
	seen := make(map[string]bool)
	current := start
	for i := 0; i < 64; i++ {
		if seen[current] {
			return false
		}
		seen[current] = true
		next := ""
		for _, edge := range dir.Edges {
			if edge.Ended || edge.WorkerID != current {
				continue
			}
			if edge.StartDate <= date && (edge.EndDate == "" || date <= edge.EndDate) {
				next = edge.ManagerID
				break
			}
		}
		if next == "" {
			return false
		}
		if next == target {
			return true
		}
		current = next
	}
	return false
}

// Validate proves both relationships eligible, refuses self-management,
// cycles and overlapping primaries.
func Validate(change ManagerChange, dir Directory) (ManagerChange, error) {
	if len(change.Story) == 0 || change.Story[len(change.Story)-1] != ChangeProposed {
		return ManagerChange{}, fmt.Errorf("org: Validate %s: %w", change.ID, ErrChangeState)
	}
	if !active(dir, change.WorkerID) || !active(dir, change.OldManagerID) || !active(dir, change.NewManagerID) {
		return ManagerChange{}, fmt.Errorf("org: Validate %s: %w", change.ID, ErrInactiveManager)
	}
	if change.NewManagerID == change.WorkerID {
		return ManagerChange{}, fmt.Errorf("org: Validate %s: %w", change.ID, ErrSelfManager)
	}
	if chain(dir, change.NewManagerID, change.WorkerID, change.EffectiveDate) {
		return ManagerChange{}, fmt.Errorf("org: Validate %s: %w", change.ID, ErrManagerCycle)
	}
	for _, edge := range dir.Edges {
		if edge.EdgeID == change.OldEdgeID || edge.Ended || edge.WorkerID != change.WorkerID {
			continue
		}
		if edge.StartDate <= change.EffectiveDate && (edge.EndDate == "" || change.EffectiveDate <= edge.EndDate) {
			return ManagerChange{}, fmt.Errorf("org: Validate %s: %w", change.ID, ErrOverlappingPrimary)
		}
	}
	change.append(ChangeValidated)
	return change, nil
}

// Approve records governed decisions: approvers are active, never the
// worker, and bound to the current authority version.
func Approve(change ManagerChange, dir Directory, approvals ...Approval) (ManagerChange, error) {
	if len(change.Story) == 0 || change.Story[len(change.Story)-1] != ChangeValidated {
		return ManagerChange{}, fmt.Errorf("org: Approve %s: %w", change.ID, ErrChangeState)
	}
	if len(approvals) == 0 {
		return ManagerChange{}, fmt.Errorf("org: Approve %s: %w", change.ID, ErrChangeState)
	}
	for _, approval := range approvals {
		if approval.ApproverID == change.WorkerID || !active(dir, approval.ApproverID) {
			return ManagerChange{}, fmt.Errorf("org: Approve %s: %w", change.ID, ErrInactiveManager)
		}
		if approval.AuthorityVersion != dir.AuthorityVersion {
			return ManagerChange{}, fmt.Errorf("org: Approve %s: %w", change.ID, ErrStaleAuthority)
		}
	}
	change.Approvals = append([]Approval(nil), approvals...)
	change.ApprovedDigest = changeDigest(change)
	change.append(ChangeApproved)
	return change, nil
}

// Schedule waits durably until the effective date.
func Schedule(change ManagerChange, now time.Time) (ManagerChange, error) {
	if len(change.Story) == 0 || change.Story[len(change.Story)-1] != ChangeApproved {
		return ManagerChange{}, fmt.Errorf("org: Schedule %s: %w", change.ID, ErrChangeState)
	}
	effective, err := time.Parse("2006-01-02", change.EffectiveDate)
	if err != nil {
		return ManagerChange{}, fmt.Errorf("org: Schedule %s: %w", change.ID, err)
	}
	if now.UTC().Truncate(24 * time.Hour).Before(effective) {
		return ManagerChange{}, fmt.Errorf("org: Schedule %s: %w", change.ID, ErrChangeState)
	}
	change.append(ChangeScheduled)
	return change, nil
}

// Revalidate rechecks actors, graph version, approval authority and future
// conflicts at execution time.
func Revalidate(change ManagerChange, dir Directory) (ManagerChange, error) {
	if len(change.Story) == 0 || change.Story[len(change.Story)-1] != ChangeScheduled {
		return ManagerChange{}, fmt.Errorf("org: Revalidate %s: %w", change.ID, ErrChangeState)
	}
	if !active(dir, change.WorkerID) || !active(dir, change.OldManagerID) || !active(dir, change.NewManagerID) {
		return ManagerChange{}, fmt.Errorf("org: Revalidate %s: %w", change.ID, ErrInactiveManager)
	}
	if dir.GraphVersion != change.GraphVersion {
		return ManagerChange{}, fmt.Errorf("org: Revalidate %s: %w", change.ID, ErrStaleAuthority)
	}
	for _, approval := range change.Approvals {
		if approval.AuthorityVersion != dir.AuthorityVersion || !active(dir, approval.ApproverID) {
			return ManagerChange{}, fmt.Errorf("org: Revalidate %s: %w", change.ID, ErrStaleAuthority)
		}
	}
	for _, event := range dir.Future {
		if event.WorkerID == change.WorkerID && event.EffectiveDate <= change.EffectiveDate {
			return ManagerChange{}, fmt.Errorf("org: Revalidate %s %s: %w", change.ID, event.Kind, ErrFutureConflict)
		}
	}
	change.append(ChangeRevalidated)
	return change, nil
}

// ApplyChange ends the old edge and creates the new edge atomically: the
// proposal must still match the approved digest, and both writes land
// together under a bumped graph version.
func ApplyChange(change ManagerChange, dir *Directory) (ManagerChange, error) {
	if len(change.Story) == 0 || change.Story[len(change.Story)-1] != ChangeRevalidated {
		return ManagerChange{}, fmt.Errorf("org: ApplyChange %s: %w", change.ID, ErrChangeState)
	}
	if changeDigest(change) != change.ApprovedDigest {
		return ManagerChange{}, fmt.Errorf("org: ApplyChange %s: %w", change.ID, ErrProposalTampered)
	}
	old := dir.Edges[change.OldEdgeID]
	old.Ended = true
	old.EndDate = change.EffectiveDate
	old.Revision++
	dir.Edges[change.OldEdgeID] = old
	applied := Edge{
		EdgeID: "edge/" + change.ID, WorkerID: change.WorkerID, ManagerID: change.NewManagerID,
		StartDate: change.EffectiveDate, Revision: 1,
	}
	dir.Edges[applied.EdgeID] = applied
	dir.GraphVersion++
	change.AppliedEdgeID = applied.EdgeID
	change.append(ChangeChanged)
	return change, nil
}

// Observe reads the authoritative manager relationship after commit.
func Observe(change ManagerChange, dir Directory) (ManagerChange, error) {
	if len(change.Story) == 0 || change.Story[len(change.Story)-1] != ChangeChanged {
		return ManagerChange{}, fmt.Errorf("org: Observe %s: %w", change.ID, ErrChangeState)
	}
	manager, ok := CurrentManager(dir, change.WorkerID, change.EffectiveDate)
	if !ok {
		return ManagerChange{}, fmt.Errorf("org: Observe %s: %w", change.ID, ErrReconcileDrift)
	}
	change.ObservedManager = manager
	change.append(ChangeObserved)
	return change, nil
}

// Reconcile confirms the observation matches the applied change.
func Reconcile(change ManagerChange) (ManagerChange, error) {
	if len(change.Story) == 0 || change.Story[len(change.Story)-1] != ChangeObserved {
		return ManagerChange{}, fmt.Errorf("org: Reconcile %s: %w", change.ID, ErrChangeState)
	}
	if change.ObservedManager != change.NewManagerID {
		return ManagerChange{}, fmt.Errorf("org: Reconcile %s: %w", change.ID, ErrReconcileDrift)
	}
	change.append(ChangeReconciled)
	return change, nil
}

// CurrentManager resolves the active PRIMARY manager projection for one
// worker on one date. Ended edges still project over the dates they
// covered: history is a projection, not a deletion.
func CurrentManager(dir Directory, workerID, date string) (string, bool) {
	for _, edge := range dir.Edges {
		if edge.WorkerID != workerID {
			continue
		}
		if edge.StartDate <= date && (edge.EndDate == "" || date < edge.EndDate) {
			return edge.ManagerID, true
		}
	}
	return "", false
}
