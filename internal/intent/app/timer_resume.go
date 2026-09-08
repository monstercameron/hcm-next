package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/hcm-next/internal/data/intentcontrol"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

type intentCorrelationReader interface {
	LoadIntentByCorrelation(context.Context, string, string) (IntentRecord, error)
}

type resumeTenantContextKey struct{}

// WithResumeTenant supplies the tenant a scheduler workload is allowed to
// dispatch. The tenant is deliberately explicit at this boundary: a ready
// work row is not permission to infer or cross a tenant.
func WithResumeTenant(ctx context.Context, tenant string) context.Context {
	return context.WithValue(ctx, resumeTenantContextKey{}, tenant)
}

func resumeTenant(ctx context.Context) (string, bool) {
	tenant, ok := ctx.Value(resumeTenantContextKey{}).(string)
	return tenant, ok && tenant != ""
}

// ResumeFiredTimer reconstructs the same approved StartRequest used by
// ExecuteIntent, then resumes the parked WAIT through the executor's durable
// timer path. Intent lookup is correlation-based because a timer parked
// instance has no work item to carry the intent identity.
func (c *Cell) ResumeFiredTimer(ctx context.Context, instanceID, nodeID string, attempt int) (ExecutionResult, error) {
	if c == nil || c.Service == nil {
		return ExecutionResult{}, fmt.Errorf("app: timer resume has no intent service")
	}
	engine, ok := c.Journey.(*journeyEngine)
	if !ok || engine == nil || engine.db == nil {
		return ExecutionResult{}, fmt.Errorf("app: timer resume has no execution journey")
	}
	if c.Service.executor == nil || c.Service.tenantUUID == nil || c.Service.executionResolver == nil || c.Service.executionVersions == nil {
		return ExecutionResult{}, fmt.Errorf("app: timer resume has incomplete execution authority")
	}
	if nodeID == "" || attempt < 1 {
		return ExecutionResult{}, fmt.Errorf("app: timer resume needs a node and positive attempt")
	}
	parsedInstanceID, err := uuid.Parse(instanceID)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("app: timer resume instance id: %w", err)
	}
	tenant, ok := resumeTenant(ctx)
	if !ok {
		return ExecutionResult{}, fmt.Errorf("app: timer resume needs an explicit tenant")
	}
	tenantID := c.Service.tenantUUID(values.TenantId(tenant))
	if tenantID == uuid.Nil {
		return ExecutionResult{}, fmt.Errorf("app: timer resume resolved a nil tenant")
	}

	tx, err := engine.db.Begin(ctx)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("app: begin timer resume: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return ExecutionResult{}, err
	}
	instance, err := (runtime.Store{}).LoadInstance(ctx, tx, tenantID, parsedInstanceID)
	if err != nil {
		return ExecutionResult{}, err
	}
	// A timer-parked instance is RUNNING (the driver records WAITING only for
	// human work); either is a live instance whose frontier the fired timer's
	// node must still be on. Every other status (paused, quarantined,
	// terminal, repair) refuses: a fired promise never revives an
	// intervention or a finished run.
	if instance.RuntimeStatus != runtime.InstanceRunning && instance.RuntimeStatus != runtime.InstanceWaiting {
		return ExecutionResult{}, fmt.Errorf("app: timer resume instance %s is %s, not RUNNING or WAITING", instanceID, instance.RuntimeStatus)
	}
	foundNode := false
	for _, current := range instance.CurrentNodeIDs {
		if current == nodeID {
			foundNode = true
			break
		}
	}
	if !foundNode {
		return ExecutionResult{}, fmt.Errorf("app: timer resume node %q is not on instance frontier", nodeID)
	}
	var timerID uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT timer_id FROM workflow_timer
		WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND timer_state = $4
		ORDER BY fired_at DESC NULLS LAST, timer_id DESC
		LIMIT 1`, tenantID, parsedInstanceID, nodeID, "FIRED").Scan(&timerID); err != nil {
		return ExecutionResult{}, fmt.Errorf("app: load fired timer for %s/%s: %w", instanceID, nodeID, err)
	}

	reader, ok := c.Service.store.(intentCorrelationReader)
	if !ok {
		return ExecutionResult{}, fmt.Errorf("app: intent store cannot resolve workflow correlation")
	}
	intentRecord, err := reader.LoadIntentByCorrelation(ctx, tenant, instance.CorrelationID)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("app: resolve timer instance intent: %w", err)
	}
	intentInstance, err := decodeEnvelope(intentRecord.Envelope)
	if err != nil {
		return ExecutionResult{}, err
	}
	intentID, err := executionIntentUUID(intentInstance.IntentID)
	if err != nil {
		return ExecutionResult{}, err
	}
	stored, err := (intentcontrol.RevisionStore{}).Load(ctx, tx, tenantID, intentID, simulationRevision)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("app: load stored proposal revision: %w", err)
	}
	revision, err := intentcontrol.DecodeFullProposal(stored.Payload, fullProposalVerifier{c.Service.digester})
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("app: timer resume legacy/tampered proposal: %w", err)
	}
	if stored.TenantID != tenantID || stored.IntentID != intentID || stored.Revision != simulationRevision ||
		stored.SchemaRef != executionProposalSchemaRef || stored.ProposalDigest != stored.MaterialDigest ||
		revision.Tenant != values.TenantId(tenant) || revision.IntentID != intentInstance.IntentID || revision.Revision != simulationRevision ||
		revision.MaterialDigest.Digest != stored.MaterialDigest {
		return ExecutionResult{}, fmt.Errorf("app: timer resume proposal identity mismatch")
	}
	artifact := &intentsv1.SimulationArtifact{
		IntentId: intentInstance.IntentID, ProposalRevisionId: revision.ProposalRevisionID,
		MaterialProposalDigest: revision.MaterialDigest.ToProto(),
	}
	start, startErr := c.Service.executionStart(intentInstance, artifact, "timer:"+timerID.String(), revision)
	if startErr != nil {
		return ExecutionResult{}, startErr
	}
	if err := tx.Commit(ctx); err != nil {
		return ExecutionResult{}, fmt.Errorf("app: commit timer resume preparation: %w", err)
	}
	result, resumeErr := c.Service.executor.ResumeTimer(ctx, ExecutionTimerResumeRequest{
		Start: start, InstanceID: parsedInstanceID,
		ExpectedInstanceVersion: instance.InstanceVersion, TimerID: timerID,
		Outcome: frontier.NodeOutcome{NodeID: nodeID, Outcome: workflow.OutcomeSucceeded},
	})
	if resumeErr != nil {
		return ExecutionResult{}, resumeErr
	}
	def, err := c.Service.defs.Resolve(intentInstance.Definition)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("app: timer resume definition: %w", err)
	}
	if err := c.Service.consumeExecutionResult(ctx, intentInstance, def, intentRecord, result); err != nil {
		return ExecutionResult{}, err
	}
	return result, nil
}
