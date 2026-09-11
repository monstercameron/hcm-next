package intent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/governance"
)

// RiskLevel is one declared preflight risk tier.
type RiskLevel string

// Declared risk tiers.
const (
	RiskLow      RiskLevel = "LOW"
	RiskMedium   RiskLevel = "MEDIUM"
	RiskHigh     RiskLevel = "HIGH"
	RiskCritical RiskLevel = "CRITICAL"
)

// Valid reports whether l is declared.
func (l RiskLevel) Valid() bool {
	switch l {
	case RiskLow, RiskMedium, RiskHigh, RiskCritical:
		return true
	}
	return false
}

// Assumption is one named preflight input. Unknown marks a redacted
// material input: resolving one to a concrete value without a resolver is
// an unsafe affirmation.
type Assumption struct {
	Name          string
	Value         string
	Unknown       bool
	ResolvedValue string
}

// CostBreakdown is one calculated cost with its price basis.
type CostBreakdown struct {
	Units int64
	Basis string
}

// RiskAssessment is one calculated risk tier with its factors.
type RiskAssessment struct {
	Level   RiskLevel
	Factors []string
}

// ConflictReservation cites one fenced write intent from the conflict
// registry: the fence proves the reservation, never a local guess.
type ConflictReservation struct {
	IntentID string
	Fence    uint64
}

// ApprovalKind is APPROVAL or TASK.
type ApprovalKind string

// Approval kinds.
const (
	KindApproval ApprovalKind = "APPROVAL"
	KindTask     ApprovalKind = "TASK"
)

// ApprovalTask is one required approval or task.
type ApprovalTask struct {
	Kind ApprovalKind
	Ref  string
}

// IntendedWrite names one intended write and its effect. Guaranteed must
// stay false: preflight simulates, it never promises external outcomes.
type IntendedWrite struct {
	Target     string
	Effect     string
	Guaranteed bool
}

// EffectEdge orders two declared writes in the effect DAG.
type EffectEdge struct {
	From string
	To   string
}

// PreflightPlanRequest asks for one universal preflight plan. Domain
// simulations supply business meaning through the values carried here;
// the plan owns composition and comparable evidence, never domain math.
type PreflightPlanRequest struct {
	Definition           Ref
	ProposalRevision     string
	Snapshot             snapshot.ReadSnapshot
	Governance           governance.ComposeRequest
	Assumptions          []Assumption
	Cost                 CostBreakdown
	Risk                 RiskAssessment
	Reservations         []ConflictReservation
	Approvals            []ApprovalTask
	Obligations          []string
	Writes               []IntendedWrite
	EffectEdges          []EffectEdge
	Observations         []string
	RevalidationTriggers []string
	Completion           ChildCompletionBehavior
	RepairExpectation    string
	EngineVersions       map[string]string
	ControlVersions      map[string]string
}

// PreflightPlan is one side-effect-free universal plan: the whole
// evidence bundle under a canonical digest.
type PreflightPlan struct {
	Request    PreflightPlanRequest
	Governance governance.ComposeResult
	Digest     string
}

// CompilePreflightPlan validates one request and binds its digest. It is
// pure: the request is never mutated and no domain, external or human
// state moves.
func CompilePreflightPlan(req PreflightPlanRequest) (PreflightPlan, error) {
	if strings.TrimSpace(req.Definition.TypeID) == "" || req.Definition.Version == 0 ||
		strings.TrimSpace(req.ProposalRevision) == "" {
		return PreflightPlan{}, newError("CompilePreflightPlan", "request", ErrInvalidPreflight,
			"definition and proposal revision are required")
	}
	if strings.TrimSpace(req.Snapshot.Digest) == "" {
		return PreflightPlan{}, newError("CompilePreflightPlan", "snapshot", ErrMismatchedSnapshot,
			"snapshot digest is required")
	}
	for engine, version := range req.EngineVersions {
		if strings.TrimSpace(version) == "" {
			return PreflightPlan{}, newError("CompilePreflightPlan", "engine_versions", ErrMismatchedSnapshot,
				"engine %q pins no version", engine)
		}
	}
	for control, version := range req.ControlVersions {
		if strings.TrimSpace(version) == "" {
			return PreflightPlan{}, newError("CompilePreflightPlan", "control_versions", ErrMismatchedSnapshot,
				"control %q pins no version", control)
		}
	}
	if req.Cost.Units < 0 || strings.TrimSpace(req.Cost.Basis) == "" {
		return PreflightPlan{}, newError("CompilePreflightPlan", "cost", ErrMissingEstimate,
			"calculated cost needs units and a price basis")
	}
	if !req.Risk.Level.Valid() {
		return PreflightPlan{}, newError("CompilePreflightPlan", "risk", ErrMissingEstimate,
			"calculated risk needs a declared tier")
	}
	declared := make(map[string]bool, len(req.Writes))
	for i, write := range req.Writes {
		if strings.TrimSpace(write.Target) == "" || strings.TrimSpace(write.Effect) == "" {
			return PreflightPlan{}, newError("CompilePreflightPlan", fmt.Sprintf("writes[%d]", i),
				ErrInvalidPreflight, "intended write needs a target and an effect")
		}
		if write.Guaranteed {
			return PreflightPlan{}, newError("CompilePreflightPlan", fmt.Sprintf("writes[%d]", i),
				ErrGuaranteedOutcome, "preflight never guarantees external outcomes")
		}
		declared[write.Target] = true
	}
	for i, edge := range req.EffectEdges {
		if !declared[edge.From] || !declared[edge.To] {
			return PreflightPlan{}, newError("CompilePreflightPlan", fmt.Sprintf("effect_edges[%d]", i),
				ErrHiddenEffect, "edge names an undeclared write")
		}
	}
	for i, assumption := range req.Assumptions {
		if assumption.Unknown && strings.TrimSpace(assumption.ResolvedValue) != "" {
			return PreflightPlan{}, newError("CompilePreflightPlan", fmt.Sprintf("assumptions[%d]", i),
				ErrUnsafeAffirmation, "redacted assumption %q resolved without a resolver", assumption.Name)
		}
	}
	for i, reservation := range req.Reservations {
		if strings.TrimSpace(reservation.IntentID) == "" || reservation.Fence == 0 {
			return PreflightPlan{}, newError("CompilePreflightPlan", fmt.Sprintf("reservations[%d]", i),
				ErrUnfencedReservation, "reservation needs a fenced write intent")
		}
	}
	for i, approval := range req.Approvals {
		if approval.Kind != KindApproval && approval.Kind != KindTask || strings.TrimSpace(approval.Ref) == "" {
			return PreflightPlan{}, newError("CompilePreflightPlan", fmt.Sprintf("approvals[%d]", i),
				ErrInvalidPreflight, "approval needs a kind and a ref")
		}
	}
	if !req.Completion.Valid() || strings.TrimSpace(req.RepairExpectation) == "" ||
		len(req.RevalidationTriggers) == 0 {
		return PreflightPlan{}, newError("CompilePreflightPlan", "repair", ErrMissingRepairPolicy,
			"completion behavior, repair expectation and revalidation triggers are required")
	}
	composed := governance.Compose(req.Governance)
	return PreflightPlan{Request: req, Governance: composed, Digest: preflightDigest(req)}, nil
}

// preflightDigest binds one deterministic identity over the whole request.
func preflightDigest(req PreflightPlanRequest) string {
	parts := []string{"preflight", req.Definition.TypeID, fmt.Sprintf("v%d", req.Definition.Version),
		req.ProposalRevision, req.Snapshot.Digest, string(req.Completion), req.RepairExpectation}
	for _, assumption := range req.Assumptions {
		parts = append(parts, "assumption:"+assumption.Name+"\x01"+assumption.Value)
	}
	parts = append(parts, fmt.Sprintf("cost=%d@%s", req.Cost.Units, req.Cost.Basis))
	parts = append(parts, "risk:"+string(req.Risk.Level)+"\x01"+strings.Join(req.Risk.Factors, ","))
	for _, reservation := range req.Reservations {
		parts = append(parts, fmt.Sprintf("reservation:%s#%d", reservation.IntentID, reservation.Fence))
	}
	for _, approval := range req.Approvals {
		parts = append(parts, "approval:"+string(approval.Kind)+"\x01"+approval.Ref)
	}
	obligations := append([]string(nil), req.Obligations...)
	sort.Strings(obligations)
	parts = append(parts, "obligations:"+strings.Join(obligations, ","))
	for _, write := range req.Writes {
		parts = append(parts, "write:"+write.Target+"\x01"+write.Effect)
	}
	edges := make([]string, 0, len(req.EffectEdges))
	for _, edge := range req.EffectEdges {
		edges = append(edges, edge.From+"\x01"+edge.To)
	}
	sort.Strings(edges)
	parts = append(parts, edges...)
	observations := append([]string(nil), req.Observations...)
	sort.Strings(observations)
	parts = append(parts, "observations:"+strings.Join(observations, ","))
	triggers := append([]string(nil), req.RevalidationTriggers...)
	sort.Strings(triggers)
	parts = append(parts, "triggers:"+strings.Join(triggers, ","))
	engines := make([]string, 0, len(req.EngineVersions))
	for engine, version := range req.EngineVersions {
		engines = append(engines, engine+"\x01"+version)
	}
	sort.Strings(engines)
	parts = append(parts, engines...)
	controls := make([]string, 0, len(req.ControlVersions))
	for control, version := range req.ControlVersions {
		controls = append(controls, control+"\x01"+version)
	}
	sort.Strings(controls)
	parts = append(parts, controls...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
