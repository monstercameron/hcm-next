package leave

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Leave-start streams: every one of them must ride the local boundary.
var leaveLocalStreams = []string{"leave.record", "leave.absence", "leave.availability", "leave.balance"}

// Leave-start external systems: ordered effects, never local ACID.
var leaveExternalSystems = []string{"payroll", "benefits", "wfm"}

// LeaveStartInput carries the leave material plus the intent plumbing the
// transaction compiles from. Employment stays ACTIVE: leave start never
// terminates or inactivates employment.
type LeaveStartInput struct {
	Proposal          intent.ProposalRevision
	Definition        intent.Definition
	EmploymentStatus  string
	BalanceHead       uint64
	AvailabilityHead  uint64
	OverlappingLeaves []string
	Governance        intent.GovernanceSnapshot
	Conflict          intent.ConflictSnapshot
	ExpiresAt         values.Instant
	IDs               intent.IDSource
}

func leaveResourceKey(tenant, kind string, segments ...string) (values.ResourceKey, error) {
	return values.NewResourceKey(values.TenantId(tenant), values.Kind(kind), segments...)
}

// CompileLeaveStart compiles the bounded Leave-start TransactionPlan
// through the real transaction compiler. Local LeaveRecord, absence
// relationship, availability and balance entries sit inside one admitted
// boundary; payroll, benefits and schedule/WFM stay ordered external
// effects with observation and reconciliation policies.
func CompileLeaveStart(input LeaveStartInput) (intent.TransactionPlan, error) {
	if input.EmploymentStatus != "ACTIVE" {
		return intent.TransactionPlan{}, fmt.Errorf("leave: leave start requires ACTIVE employment, not %q", input.EmploymentStatus)
	}
	if len(input.OverlappingLeaves) > 0 {
		return intent.TransactionPlan{}, fmt.Errorf("leave: overlapping incompatible leave %q blocks leave start", input.OverlappingLeaves[0])
	}
	if input.BalanceHead == 0 || input.AvailabilityHead == 0 {
		return intent.TransactionPlan{}, fmt.Errorf("leave: leave start pins expected balance and availability heads")
	}
	tenant := string(input.Proposal.Tenant)
	if strings.TrimSpace(tenant) == "" {
		tenant = "acme"
	}
	participants := make([]intent.PlanParticipant, 0, len(leaveLocalStreams)+len(leaveExternalSystems))
	streams := make(map[string]bool, len(leaveLocalStreams))
	for _, stream := range leaveLocalStreams {
		participants = append(participants, intent.PlanParticipant{ParticipantID: "participant:" + stream, StreamID: stream + ".w1", StorageClass: "LOCAL_POSTGRES", Local: true})
		streams[stream] = true
	}
	var effects []intent.OutboxEffect
	var compensations []intent.CompensationBinding
	var observations []intent.PostCommitObservation
	for _, system := range leaveExternalSystems {
		effectID := "effect:" + system
		effects = append(effects, intent.OutboxEffect{EffectID: effectID, DestinationRef: system + ".outbox/v1", IdempotencyKey: "leave-start:w1:" + system, Reversibility: "COMPENSATABLE"})
		compensations = append(compensations, intent.CompensationBinding{EffectID: effectID, Strategy: "reverse-" + system, RepairPlanID: "repair:" + system})
		observations = append(observations, intent.PostCommitObservation{EffectID: effectID, ObservationRef: "observe:" + system + "/v1", Deadline: input.ExpiresAt})
		participants = append(participants, intent.PlanParticipant{ParticipantID: "participant:" + system, StreamID: system + ".outbox.w1", StorageClass: "REMOTE_OUTBOX", Local: false})
	}
	balanceKey, err := leaveResourceKey(tenant, "balance", "w1", "pto")
	if err != nil {
		return intent.TransactionPlan{}, fmt.Errorf("leave: balance key: %v", err)
	}
	availabilityKey, err := leaveResourceKey(tenant, "availability", "w1")
	if err != nil {
		return intent.TransactionPlan{}, fmt.Errorf("leave: availability key: %v", err)
	}
	balanceRevision, err := values.NewSequenceRevision("leave.balance.w1", input.BalanceHead)
	if err != nil {
		return intent.TransactionPlan{}, fmt.Errorf("leave: balance head: %v", err)
	}
	availabilityRevision, err := values.NewSequenceRevision("leave.availability.w1", input.AvailabilityHead)
	if err != nil {
		return intent.TransactionPlan{}, fmt.Errorf("leave: availability head: %v", err)
	}
	compiled, err := intent.CompilePlan(intent.PlanInput{
		Proposal: input.Proposal, Definition: input.Definition, Mode: intent.ModeSimulate,
		Governance: input.Governance, Conflict: input.Conflict,
		Participants: participants,
		Reads: []intent.PlannedRead{
			{ResourceKey: balanceKey, ExpectedRevision: balanceRevision},
			{ResourceKey: availabilityKey, ExpectedRevision: availabilityRevision},
		},
		Appends: []intent.PlannedAppend{
			{StreamID: "leave.record.w1", ExpectedSequence: 1, EventType: "leave.record_opened/v1", PayloadDigest: "leave-record:w1"},
			{StreamID: "leave.absence.w1", ExpectedSequence: 1, EventType: "leave.absence_related/v1", PayloadDigest: "leave-absence:w1"},
		},
		Effects:       effects,
		Compensations: compensations,
		Observations:  observations,
		Preconditions: []intent.CommitPrecondition{
			{Kind: "EMPLOYMENT_ACTIVE", Ref: "employment:w1"},
			{Kind: "NO_OVERLAPPING_LEAVE", Ref: "leave.overlap_check/v1"},
		},
		IdempotencyRecordRef:   "idempotency:leave-start:w1",
		ApprovalRequirementIDs: []string{"req.leave_manager/v1"},
		RevalidationRuleRefs:   []string{"leave_start_revalidation/v1"},
		ExpiresAt:              input.ExpiresAt,
	}, input.IDs)
	if err != nil {
		return intent.TransactionPlan{}, err
	}
	for _, participant := range compiled.Participants {
		if !participant.Local {
			continue
		}
		known := false
		for _, stream := range leaveLocalStreams {
			if participant.StreamID == stream+".w1" {
				known = true
			}
		}
		if !known {
			return intent.TransactionPlan{}, fmt.Errorf("leave: local participant %q is outside the admitted boundary", participant.StreamID)
		}
	}
	return compiled, nil
}
