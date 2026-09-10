// ChangeManager intent compilation (CONF-025) proves the standalone
// hcmnext.people.change_manager/v1 intent at conformance-only depth: the
// exact typed intent compiles through snapshot, validation, AuthZ,
// conflict, simulation, approval, wait, revalidation and one relationship
// write plan. Every negative case returns its exact block/replan state,
// and production registry exposure remains absent: the plan carries
// relationship operations only, never production capabilities.
package org

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// BlockCode names the exact refusal for one negative case.
type BlockCode string

const (
	BlockInactiveActor        BlockCode = "INACTIVE_ACTOR"
	BlockSelfManagement       BlockCode = "SELF_MANAGEMENT"
	BlockCycle                BlockCode = "GRAPH_CYCLE"
	BlockStaleRelationship    BlockCode = "STALE_RELATIONSHIP"
	BlockConflict             BlockCode = "FUTURE_CONFLICT"
	BlockScope                BlockCode = "UNAUTHORIZED_SCOPE"
	BlockApproval             BlockCode = "UNBOUND_APPROVAL"
	BlockDateDrift            BlockCode = "EFFECTIVE_DATE_DRIFT"
	BlockObservationAuthority BlockCode = "OBSERVATION_ASSERTED_AS_AUTHORITY"
)

// Block is the typed negative outcome: what refused and whether the
// caller replans or stays blocked.
type Block struct {
	Code   BlockCode
	Replan bool
	Detail string
}

func (b *Block) Error() string {
	return fmt.Sprintf("org: change-manager intent blocked: %s (replan=%v): %s", b.Code, b.Replan, b.Detail)
}

// IntentBasis fixes what the intent may stand on. Snapshots are authority;
// external observations never are.
type IntentBasis string

const (
	BasisSnapshot    IntentBasis = "SNAPSHOT"
	BasisObservation IntentBasis = "OBSERVATION"
)

// ChangeManagerIntent is the exact typed standalone intent.
type ChangeManagerIntent struct {
	WorkerID           string
	CurrentManagerRef  string
	ProposedManagerRef string
	EffectiveDate      string
	Reason             string
	OrgScope           []string
	Basis              IntentBasis
}

// WriteOp is one relationship write in the single permitted write plan.
type WriteOp struct {
	Op      string
	EdgeID  string
	Worker  string
	Manager string
	Start   string
	End     string
}

// CompiledPlan is the zero-production-exposure compilation result.
type CompiledPlan struct {
	IntentRef        string
	Stages           []string
	RequiredApprover []string
	Impacted         []string
	WaitUntil        string
	Revalidate       []string
	WritePlan        []WriteOp
	ProposalDigest   string
	PlanDigest       string
}

// ProductionEffects reports the production capabilities the plan would
// publish. Conformance-only depth publishes none: always empty.
func (p CompiledPlan) ProductionEffects() []string { return nil }

func inScope(scope []string, id string) bool {
	for _, member := range scope {
		if member == id {
			return true
		}
	}
	return false
}

func mapValidationError(err error) *Block {
	switch {
	case errors.Is(err, ErrInactiveManager):
		return &Block{Code: BlockInactiveActor, Detail: err.Error()}
	case errors.Is(err, ErrSelfManager):
		return &Block{Code: BlockSelfManagement, Detail: err.Error()}
	case errors.Is(err, ErrManagerCycle):
		return &Block{Code: BlockCycle, Detail: err.Error()}
	case errors.Is(err, ErrOverlappingPrimary):
		return &Block{Code: BlockStaleRelationship, Replan: true, Detail: err.Error()}
	default:
		return &Block{Code: BlockStaleRelationship, Replan: true, Detail: err.Error()}
	}
}

// CompileIntent compiles one standalone ChangeManager intent. now fixes
// the compile time for the effective-date wait directive.
func CompileIntent(intent ChangeManagerIntent, dir Directory, approvals []Approval, now time.Time) (CompiledPlan, error) {
	var plan CompiledPlan
	if intent.Basis == BasisObservation {
		return CompiledPlan{}, &Block{Code: BlockObservationAuthority, Detail: "external observation asserted as authority"}
	}
	if strings.TrimSpace(intent.WorkerID) == "" || strings.TrimSpace(intent.ProposedManagerRef) == "" ||
		strings.TrimSpace(intent.EffectiveDate) == "" || len(intent.OrgScope) == 0 {
		return CompiledPlan{}, &Block{Code: BlockScope, Detail: "intent identity, scope and effective date are required"}
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"change-manager-intent", intent.WorkerID, intent.CurrentManagerRef,
		intent.ProposedManagerRef, intent.EffectiveDate, intent.Reason,
	}, "\x00")))
	plan.IntentRef = "sha256:" + hex.EncodeToString(sum[:])

	// Snapshot: the current relationship is read, never asserted.
	current, ok := CurrentManager(dir, intent.WorkerID, intent.EffectiveDate)
	if !ok || current != intent.CurrentManagerRef {
		return CompiledPlan{}, &Block{Code: BlockStaleRelationship, Replan: true, Detail: "snapshot does not match the stated current manager"}
	}
	plan.Stages = append(plan.Stages, "snapshot")

	// Validation: eligibility, self-management, cycles and overlaps over
	// a draft that commits nothing.
	oldEdge := ""
	for _, edge := range dir.Edges {
		if edge.WorkerID == intent.WorkerID && edge.ManagerID == current && !edge.Ended {
			oldEdge = edge.EdgeID
			break
		}
	}
	if oldEdge == "" {
		return CompiledPlan{}, &Block{Code: BlockStaleRelationship, Replan: true, Detail: "no live edge for the stated relationship"}
	}
	draft, err := Propose("draft/"+plan.IntentRef[:16], intent.WorkerID, oldEdge, intent.ProposedManagerRef, intent.EffectiveDate, intent.Reason, dir)
	if err != nil {
		return CompiledPlan{}, mapValidationError(err)
	}
	if _, err := Validate(draft, dir); err != nil {
		return CompiledPlan{}, mapValidationError(err)
	}
	plan.ProposalDigest = draft.ProposalDigest
	plan.Stages = append(plan.Stages, "validation")

	// AuthZ: worker and both managers sit inside the authorized scope.
	for _, actor := range []string{intent.WorkerID, current, intent.ProposedManagerRef} {
		if !inScope(intent.OrgScope, actor) {
			return CompiledPlan{}, &Block{Code: BlockScope, Detail: "actor " + actor + " is outside the authorized org scope"}
		}
	}
	plan.Stages = append(plan.Stages, "authz")

	// Conflict: transfer, termination, leave, org move or a future manager
	// change colliding with the effective date replans, never overrides.
	for _, event := range dir.Future {
		if event.WorkerID == intent.WorkerID && event.EffectiveDate <= intent.EffectiveDate {
			return CompiledPlan{}, &Block{Code: BlockConflict, Replan: true, Detail: event.Kind + " collides with the effective date"}
		}
	}
	plan.Stages = append(plan.Stages, "conflict")

	// Simulation: the impacted closure is the worker plus everyone whose
	// approval chain moves with them, sorted and deduplicated.
	impacted := map[string]bool{intent.WorkerID: true}
	for _, edge := range dir.Edges {
		if !edge.Ended && edge.ManagerID == intent.WorkerID {
			impacted[edge.WorkerID] = true
		}
	}
	for id := range impacted {
		plan.Impacted = append(plan.Impacted, id)
	}
	sort.Strings(plan.Impacted)
	plan.Stages = append(plan.Stages, "simulation")

	// Approval: bound, active, non-worker approvers including the current
	// manager; anything else is unbound.
	if len(approvals) == 0 {
		return CompiledPlan{}, &Block{Code: BlockApproval, Detail: "no approval bound"}
	}
	hasCurrentManager := false
	for _, approval := range approvals {
		if approval.AuthorityVersion != dir.AuthorityVersion || !active(dir, approval.ApproverID) || approval.ApproverID == intent.WorkerID {
			return CompiledPlan{}, &Block{Code: BlockApproval, Detail: "unbound approval from " + approval.ApproverID}
		}
		if approval.Role == "current-manager" && approval.ApproverID == current {
			hasCurrentManager = true
		}
		plan.RequiredApprover = append(plan.RequiredApprover, approval.ApproverID)
	}
	if !hasCurrentManager {
		return CompiledPlan{}, &Block{Code: BlockApproval, Detail: "current-manager approval is required"}
	}
	sort.Strings(plan.RequiredApprover)
	plan.Stages = append(plan.Stages, "approval")

	// Wait: the effective date must not already have drifted past.
	effective, err := time.Parse("2006-01-02", intent.EffectiveDate)
	if err != nil {
		return CompiledPlan{}, &Block{Code: BlockDateDrift, Replan: true, Detail: "effective date does not parse"}
	}
	if effective.Before(now.UTC().Truncate(24 * time.Hour)) {
		return CompiledPlan{}, &Block{Code: BlockDateDrift, Replan: true, Detail: "effective date already passed at compile time"}
	}
	plan.WaitUntil = intent.EffectiveDate
	plan.Stages = append(plan.Stages, "wait")

	// Revalidation directive: versions the execution gate must recheck.
	plan.Revalidate = []string{
		fmt.Sprintf("graph-version=%d", dir.GraphVersion),
		fmt.Sprintf("authority-version=%d", dir.AuthorityVersion),
	}
	plan.Stages = append(plan.Stages, "revalidation")

	// One relationship write plan: end the old edge, create the new edge.
	plan.WritePlan = []WriteOp{
		{Op: "end-edge", EdgeID: oldEdge, End: intent.EffectiveDate},
		{Op: "create-edge", Worker: intent.WorkerID, Manager: intent.ProposedManagerRef, Start: intent.EffectiveDate},
	}
	plan.Stages = append(plan.Stages, "write-plan")

	planSum := sha256.Sum256([]byte(plan.IntentRef + "\x00" + plan.ProposalDigest + "\x00" + strings.Join(plan.Stages, ",")))
	plan.PlanDigest = "sha256:" + hex.EncodeToString(planSum[:])
	return plan, nil
}
