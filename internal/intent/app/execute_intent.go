package app

import (
	"context"
	"reflect"

	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/engines/wire/digest"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/transport/envelope"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// OBS-024's execution-evidence vocabulary entries [IntentService.ExecuteIntent]
// itself records. This is a deliberate, documented duplication of
// internal/workflow/execute's own EvidenceKindGateRefused/
// EvidenceKindGateAdmitted constants: this package must not import
// internal/workflow/execute (see execution.go's ProposalExecutor doc for
// why), so it carries its own copy of the two kinds it is the sole recorder
// of. A later phase that centralizes the vocabulary for the inspector
// (execute.EvidenceKind's own REFACTOR note) replaces both copies with one
// shared definition neither package currently depends on.
const (
	EvidenceKindGateRefused  = "GATE_REFUSED"
	EvidenceKindGateAdmitted = "GATE_ADMITTED"
)

// Reason references [IntentService.ExecuteIntent] owns.
const (
	// reasonExecutionRoleRequired reports a cell whose ExecutionAuthority is
	// configured and admits the intent type, but whose caller does not carry
	// the role the authority names.
	reasonExecutionRoleRequired = "p1b.execution_role_required"
	// reasonNoExecutablePlan reports an intent whose current preflight/
	// simulation produced no executable plan to run (the same condition
	// SimulateIntent expresses by minting no proposal revision at all).
	reasonNoExecutablePlan = "p1b.no_executable_plan"
	// reasonStaleProposal reports a caller-presented proposal revision or
	// material digest that does not match what this cell would itself
	// (re)compute for the named intent right now.
	reasonStaleProposal = "p1b.stale_proposal"
	// reasonUnapprovedProposal reports a proposal revision presented with no
	// recorded approval.
	reasonUnapprovedProposal = "p1b.unapproved_proposal"
	// reasonExecutionUnavailable reports an authorized, approved call this
	// cell still cannot run because it was not composed with the
	// driver-side wiring EXECUTE needs.
	reasonExecutionUnavailable = "workflow.execution_unavailable"

	ruleExecutionAuthorityGate = "release.p1b_execution_authority_gate"
)

// ExecuteIntent runs the caller-driven workflow driver for an intent whose
// current proposal revision is approved and immutable, and returns the
// resulting execution receipt.
//
// It is refused under the exact envelope every other governed write in this
// release already uses ([p1aRefusal]) unless three things are all true:
// this cell was composed with a non-nil [ExecutionAuthority] that admits the
// intent's own type, the authenticated caller carries the role that
// authority names, and the presented [intentsv1.ProposalApproval] names the
// exact proposal revision and material digest this cell would itself
// (re)compute for the intent right now. A cell composed with no
// ExecutionAuthority answers identically regardless of the caller, the
// approval presented, or whether a [ProposalExecutor] happens to be wired:
// P1A cells never execute.
func (s *IntentService) ExecuteIntent(ctx context.Context, req *intentsv1.ExecuteIntentRequest) (*intentsv1.ExecuteIntentResponse, error) {
	principal, inv, ownedErr := caller(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}

	inst, rec, ownedErr := s.loadInstance(ctx, principal.Tenant().String(), req.GetIntentId())
	if ownedErr != nil {
		return nil, ownedErr
	}
	if want := req.GetExpectedInstanceVersion(); want != 0 && want != rec.InstanceVersion {
		return nil, envelope.New(envelope.CodeFailedPrecondition, reasonStaleRevision,
			"a precondition for the operation is not met").
			WithViolation("expected_instance_version", "the expected instance version is stale", "intent.expected_revision")
	}
	def, err := s.defs.Resolve(inst.Definition)
	if err != nil {
		return nil, envelope.New(envelope.CodeNotFound, reasonDefinitionUnknown,
			"the resource does not exist or is not visible").WithDiagnostic(err)
	}

	// The authority gate runs before this cell does anything else observable
	// (before it even re-simulates), so a caller who is refused here learns
	// nothing about whether the intent has an executable plan at all.
	//
	// OBS-024: the gate's own decision is recorded as evidence right here —
	// including a refusal, which is the whole point of this todo (RED:
	// "ExecuteIntent records evidence only for its re-simulation, never for
	// gate refusals, approvals, submissions or the terminal write") — before
	// this cell has looked at the presented approval or run a single node.
	if ownedErr := s.authorizeExecution(principal, def); ownedErr != nil {
		s.recordGateEvidence(ctx, EvidenceKindGateRefused, inst.IntentID, ownedErr.ReasonRef())
		return nil, ownedErr
	}
	gateEvidenceID, evErr := s.recordGateEvidence(ctx, EvidenceKindGateAdmitted, inst.IntentID, "")
	if evErr != nil {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(evErr)
	}

	// Re-running the exact SimulateIntent path is what proves the presented
	// proposal_revision_id/material_proposal_digest name real, current
	// content: P1A mints a proposal revision id deterministically from the
	// intent's own identity and canonical request digest
	// ([derivedIDs]/[simulationRevision]), so simulating twice for the same
	// stored intent produces the same revision id and digest, never a
	// caller-invented one.
	artifact, ownedErr := s.simulate(ctx, principal, purposeOf(principal, inv), inst, def)
	if ownedErr != nil {
		return nil, ownedErr
	}
	if artifact.GetProposalRevisionId() == "" {
		return nil, envelope.New(envelope.CodeFailedPrecondition, reasonNoExecutablePlan,
			"a precondition for the operation is not met").
			WithViolation("intent_id", "the intent has no executable proposal to run", ruleExecutionAuthorityGate)
	}

	if ownedErr := checkApproval(req.GetApproval(), artifact); ownedErr != nil {
		return nil, ownedErr
	}

	if s.executor == nil || s.executionResolver == nil || s.executionVersions == nil || s.tenantUUID == nil {
		return nil, executionUnavailable()
	}

	materialDigest, digestErr := digest.FromProto(artifact.GetMaterialProposalDigest())
	if digestErr != nil {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(digestErr)
	}
	subjectRefs := make([]string, 0, len(inst.Subjects))
	for _, subj := range inst.Subjects {
		subjectRefs = append(subjectRefs, subj.SubjectID)
	}
	cellID := s.executionCellID
	if cellID == "" {
		cellID = "cell-local"
	}

	start := runtime.StartRequest{
		TenantID:            s.tenantUUID(inst.Tenant),
		CellID:              cellID,
		StartIdempotencyKey: "execute:" + inst.IntentID + ":" + artifact.GetProposalRevisionId(),
		Resolver:            s.executionResolver,
		Versions:            s.executionVersions,
		Proposal: runtime.ProposalBinding{
			// This reconstructs only the fields runtime.Start itself
			// inspects (identity, tenant, subjects and material digest), not
			// a persisted proposal: P1A mints a ProposalRevision as an
			// in-memory simulation artifact and never stores it (see
			// [IntentService.simulatePromotion]), so there is no stored
			// revision to load back here.
			Revision: intent.ProposalRevision{
				ProposalRevisionID:  artifact.GetProposalRevisionId(),
				IntentID:            inst.IntentID,
				Tenant:              inst.Tenant,
				OrganizationScopeID: inst.OrganizationScopeID,
				Subjects:            inst.Subjects,
				MaterialDigest:      materialDigest,
			},
			Approved:    true,
			ApprovalRef: req.GetApproval().GetApprovalRef(),
		},
		ExpectedIntentID:    inst.IntentID,
		ExpectedTenant:      inst.Tenant,
		BusinessSubjectRefs: subjectRefs,
		ExecutionMode:       workflow.ModeExecute,
		CorrelationID:       inst.CorrelationID,
		CreatedAt:           s.clock().Time(),
	}

	result, err := s.executor.Execute(ctx, start)
	if err != nil {
		return nil, executionError(err)
	}
	// The GATE_ADMITTED entry recorded above precedes every evidence id the
	// driver itself records (APPROVAL_COMPLETED/TASK_SUBMITTED/
	// TERMINAL_WRITTEN), so it leads result.EvidenceIDs rather than trailing it.
	if gateEvidenceID != "" {
		result.EvidenceIDs = append([]string{gateEvidenceID}, result.EvidenceIDs...)
	}
	return &intentsv1.ExecuteIntentResponse{Execution: executionReceiptProto(result)}, nil
}

// recordGateEvidence records one OBS-024 GATE_REFUSED/GATE_ADMITTED entry
// through this service's own evidence sink — the same capability evidence
// sink mechanism CAP-002's gateway already writes invocation/refusal
// evidence through (a fresh [MemoryEvidenceSink] private to this service
// when no cell-wide sink was configured; [NewCell] wires the cell's own
// gateway sink instead, so [Cell.Evidence] reads both back from one place).
// No workflow instance exists yet at this call: nodeID is always empty.
func (s *IntentService) recordGateEvidence(ctx context.Context, kind, intentID, reason string) (string, error) {
	return s.evidence.RecordInvocation(ctx, capability.InvocationEvidence{
		CapabilityID:      "workflow.execution_authority_gate",
		CapabilityVersion: 1,
		SubjectRef:        intentID,
		Decision:          kind,
		ReasonCode:        reason,
		OccurredAt:        s.clock().Time(),
	})
}

// authorizeExecution is the P1B execution authority gate. It returns nil only
// when this cell was composed with an [ExecutionAuthority] that admits def's
// own intent type and principal carries the role that authority names.
func (s *IntentService) authorizeExecution(principal *trust.Principal, def intent.Definition) *envelope.Error {
	if !s.executionAuthority.admitsType(def.Ref.TypeID) {
		return p1aRefusal("ExecuteIntent", "executing an approved promotion proposal")
	}
	if !s.executionAuthority.admitsCaller(principal) {
		return envelope.New(envelope.CodePermissionDenied, reasonExecutionRoleRequired,
			"the caller is not authorized to execute this proposal").
			WithViolation("(caller)",
				"the caller does not carry the role this cell's execution authority requires",
				ruleExecutionAuthorityGate)
	}
	return nil
}

// checkApproval refuses an [intentsv1.ProposalApproval] that does not name
// the exact revision and digest artifact carries, or that asserts no
// recorded approval.
func checkApproval(approval *intentsv1.ProposalApproval, artifact *intentsv1.SimulationArtifact) *envelope.Error {
	if approval.GetProposalRevisionId() == "" || approval.GetProposalRevisionId() != artifact.GetProposalRevisionId() {
		return envelope.New(envelope.CodeFailedPrecondition, reasonStaleProposal,
			"a precondition for the operation is not met").
			WithViolation("approval.proposal_revision_id",
				"the presented proposal revision does not match the revision this cell would simulate for this intent right now",
				ruleExecutionAuthorityGate)
	}
	presentedDigest, presentedErr := digest.FromProto(approval.GetMaterialProposalDigest())
	simulatedDigest, simulatedErr := digest.FromProto(artifact.GetMaterialProposalDigest())
	if presentedErr != nil || simulatedErr != nil || !reflect.DeepEqual(presentedDigest, simulatedDigest) {
		return envelope.New(envelope.CodeFailedPrecondition, reasonStaleProposal,
			"a precondition for the operation is not met").
			WithViolation("approval.material_proposal_digest",
				"the presented material proposal digest does not match the revision this cell would simulate for this intent right now",
				ruleExecutionAuthorityGate)
	}
	if !approval.GetApproved() || approval.GetApprovalRef() == "" {
		return envelope.New(envelope.CodeFailedPrecondition, reasonUnapprovedProposal,
			"a precondition for the operation is not met").
			WithViolation("approval.approved",
				"the presented proposal revision carries no recorded approval", ruleExecutionAuthorityGate)
	}
	return nil
}

// executionUnavailable is returned when the authority gate and the presented
// approval both admit the call, but this cell was not composed with the
// driver-side wiring ([ProposalExecutor] plus its workflow resolver, version
// store and tenant-identity mapper) EXECUTE needs to actually run.
func executionUnavailable() *envelope.Error {
	return envelope.New(
		envelope.CodeFailedPrecondition,
		reasonExecutionUnavailable,
		"workflow execution is not configured for this service",
	)
}

// executionError projects a caller-driven driver refusal onto the owned
// error model. [runtime.Error] is the one typed refusal shape that package
// exposes; anything else is reported as this cell's own fault.
func executionError(err error) *envelope.Error {
	code := runtime.CodeOf(err)
	if code == "" {
		return envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(err)
	}
	switch code {
	case runtime.CodeInstanceNotFound, runtime.CodeNodeExecutionNotFound:
		return envelope.New(envelope.CodeNotFound, reasonIntentNotFound,
			"the resource does not exist or is not visible").WithDiagnostic(err)
	case runtime.CodeStaleInstance:
		return envelope.New(envelope.CodeFailedPrecondition, reasonStaleRevision,
			"a precondition for the operation is not met").
			WithViolation("expected_instance_version", "the workflow instance version is stale", ruleExecutionAuthorityGate).
			WithDiagnostic(err)
	case runtime.CodeMutableProposal, runtime.CodeUnapprovedProposal, runtime.CodeSupersededProposal:
		return envelope.New(envelope.CodeFailedPrecondition, reasonStaleProposal,
			"a precondition for the operation is not met").
			WithViolation("approval", "the proposal revision is no longer executable", ruleExecutionAuthorityGate).
			WithDiagnostic(err)
	default:
		return envelope.New(envelope.CodeFailedPrecondition, reasonDomainUnavailable,
			"a precondition for the operation is not met").WithDiagnostic(err)
	}
}

// executionReceiptProto projects one [ExecutionResult] onto the wire receipt.
//
// WF-RUN-032: parked_continuation_refs and work_items are rendered as
// separate typed lists from result.ParkedContinuationRefs and
// result.ParkedWorkItems respectively -- a continuation is never named as a
// work item here, and a work item never as a continuation. The deprecated
// parked_continuations string field is still populated, from the same
// ParkedContinuations the port has always carried, for a caller that has not
// migrated off it yet.
func executionReceiptProto(result ExecutionResult) *intentsv1.ExecutionReceipt {
	continuations := make([]*intentsv1.ParkedContinuation, 0, len(result.ParkedContinuationRefs))
	for _, c := range result.ParkedContinuationRefs {
		continuations = append(continuations, &intentsv1.ParkedContinuation{
			ContinuationId: c.ContinuationID, Kind: c.Kind, TargetNodeId: c.TargetNodeID,
		})
	}
	workItems := make([]*intentsv1.ParkedWorkItem, 0, len(result.ParkedWorkItems))
	for _, w := range result.ParkedWorkItems {
		workItems = append(workItems, &intentsv1.ParkedWorkItem{
			WorkItemId: w.WorkItemID, Kind: w.Kind, NodeId: w.NodeID,
		})
	}
	return &intentsv1.ExecutionReceipt{
		InstanceId:             result.InstanceID,
		VisitedNodes:           append([]string(nil), result.VisitedNodes...),
		ParkedContinuations:    append([]string(nil), result.ParkedContinuations...),
		InstanceVersion:        uint64(result.InstanceVersion),
		ReceiptDigest:          receiptDigestFor(result),
		ParkedContinuationRefs: continuations,
		WorkItems:              workItems,
	}
}

// receiptDigestFor mints a stable, human-inspectable tag binding an
// execution receipt's identity together. It is not a cryptographic digest
// over canonicalized content the way [intent.ProposalRevision.MaterialDigest]
// is; nothing here is approval-bound material, only a receipt of what
// already ran.
func receiptDigestFor(result ExecutionResult) string {
	status := "PARKED"
	if !result.Parked {
		status = "COMPLETE"
	}
	return "execution:" + result.InstanceID + ":" + status
}
