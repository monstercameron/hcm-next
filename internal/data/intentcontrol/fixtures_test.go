package intentcontrol_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// fixedInstant is the clock every fixture in this package stamps, so that a
// golden comparison never drifts with wall-clock time.
var fixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

// digestOf mints a well-formed content_digest from a label. Deriving it rather
// than hard-coding sixty-four hex characters is what keeps a test's digests
// distinct from one another without a wall of literals.
func digestOf(label string) string {
	sum := sha256.Sum256([]byte(label))
	return hex.EncodeToString(sum[:])
}

// insertTenant registers one active tenant as the migration/admin role.
func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

// insertIntent creates one intent_instance row (migration 00004) for tenant.
// Every table this package owns hangs off that row, so almost every test needs
// one.
func insertIntent(t *testing.T, db *pgtest.DB, tenant uuid.UUID, idempotencyKey string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO intent_instance (
			tenant_id, intent_id, definition_ref, definition_version,
			request_digest, idempotency_key,
			request_state, execution_state, business_state, consistency_state, obligation_state,
			created_at, last_transition_at)
		VALUES ($1, $2, 'promotion.request', 1, $3, $4,
			'DRAFT', 'NOT_PLANNED', 'NOT_STARTED', 'NOT_APPLICABLE', 'NOT_APPLICABLE',
			$5, $5)`,
		tenant, id, digestOf("request:"+idempotencyKey), idempotencyKey, fixedInstant)
	return id
}

// insertRevision creates one immutable proposal_revision row for intent, which
// is the parent of the proposal set, result, decision, certificate,
// simulation-result and transaction-plan tables.
func insertRevision(t *testing.T, db *pgtest.DB, tenant, intent uuid.UUID, revision uint64) string {
	t.Helper()
	proposalDigest := digestOf(fmt.Sprintf("proposal:%s:%d", intent, revision))
	db.Exec(t, `
		INSERT INTO proposal_revision (
			tenant_id, intent_id, revision, proposal_digest, material_digest,
			schema_ref, payload, produced_by, produced_at)
		VALUES ($1, $2, $3, $4, $5, 'hcmnext.intents.v1.Proposal', $6, 'test-fixture', $7)`,
		tenant, intent, int64(revision), proposalDigest, proposalDigest,
		[]byte(`{"proposal":true}`), fixedInstant)
	return proposalDigest
}

// appConn opens a fresh connection on db's schema and assumes the
// least-privilege hcmnext_app role, which is the only role migration 00024's
// grants and row level security policies actually describe.
func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

// inTenantTx runs fn inside its own transaction on conn, scoped to tenant as
// the first statement, and commits it.
func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

// inTenantTxErr is inTenantTx for a call whose own error the test wants to
// inspect. A failing fn rolls the transaction back, which is what makes
// "and nothing was written" checkable after a refusal.
func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// countRows answers "how many rows does this tenant hold in this table" as the
// admin role, so a refusal's "and nothing was written" claim is checked from
// outside the transaction that was refused.
func countRows(t *testing.T, db *pgtest.DB, table string, tenant uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(),
		`SELECT count(*) FROM `+table+` WHERE tenant_id = $1`, tenant).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// referenceContext is a valid intent_instance_context for intent, with an
// ASSERTED (unattested) origin.
func referenceContext(tenant, intent uuid.UUID) intentcontrol.InstanceContext {
	return intentcontrol.InstanceContext{
		TenantID:              tenant,
		IntentID:              intent,
		IntentFamily:          "CHANGE_REQUEST",
		ExecutionMode:         "SIMULATE",
		FeatureRef:            "feature.promotion.request",
		FeatureCoverageDigest: digestOf("coverage"),
		OriginTrust:           intentcontrol.OriginAsserted,
		OriginKind:            "API",
		CorrelationID:         "corr-" + intent.String(),
		TraceID:               "trace-" + intent.String(),
		InitiatorKind:         "HUMAN",
		InitiatorPrincipalID:  "principal:manager-1",
		IdentityAssuranceRef:  "assurance:aal2",
		Purpose:               "promotion",
		Classification:        "INTERNAL",
		RetentionClass:        "PERMANENT",
		ControlDigest:         digestOf("control"),
		RiskContextDigest:     digestOf("risk"),
	}
}

// referenceChangeRequest is a valid DRAFT hcm_change_request for intent.
func referenceChangeRequest(tenant, intent uuid.UUID) intentcontrol.ChangeRequest {
	return intentcontrol.ChangeRequest{
		TenantID:        tenant,
		ChangeRequestID: uuid.New(),
		IntentID:        intent,
		RequestKind:     "promotion",
		SubjectRef:      "worker:omar-reyes",
		RequestedBy:     "principal:manager-1",
		RequestedAt:     fixedInstant,
		EffectiveFrom:   fixedInstant.AddDate(0, 1, 0),
		RequestStatus:   intentcontrol.RequestDraft,
		IntentDigest:    digestOf("intent:" + intent.String()),
		ControlDigest:   digestOf("control"),
	}
}

// referenceSnapshot is a valid PREFLIGHT input snapshot for intent.
func referenceSnapshot(tenant, intent uuid.UUID, sequence uint64) intentcontrol.InputSnapshot {
	return intentcontrol.InputSnapshot{
		TenantID:   tenant,
		SnapshotID: uuid.New(),
		IntentID:   intent,
		Purpose:    intentcontrol.PurposePreflight,
		Sequence:   sequence,
		ObservedAt: fixedInstant,
		Digest:     digestOf("snapshot"),
		SchemaRef:  "hcmnext.intents.v1.BaselineSnapshot",
		Body:       json.RawMessage(`{"grade":"G7","base_pay":"120000.00"}`),
	}
}

// referenceSets is a valid, complete set of proposal children: one write, one
// effect with both a compensation and an observation, one approval requirement
// and one obligation.
func referenceSets() intentcontrol.ProposalSets {
	return intentcontrol.ProposalSets{
		Writes: []intentcontrol.WriteItem{{
			SubjectKind:             "WORKER",
			SubjectID:               "omar-reyes",
			ResourceKey:             "compensation/base_pay",
			FieldPath:               "base_pay",
			CurrentCanonicalText:    "120000.00",
			ProposedCanonicalText:   "132000.00",
			ExpectedRevision:        "rev-7",
			SourceAuthorityDecision: "ALLOW",
		}},
		Effects: []intentcontrol.EffectItem{{
			EffectID:        "effect-notify-payroll",
			Kind:            "OUTBOUND_MESSAGE",
			DestinationRef:  "connector:payroll",
			Reversibility:   intentcontrol.Compensatable,
			CompensationRef: "compensation:payroll-reverse",
			ObservationRef:  "observation:payroll-ack",
		}},
		Approvals: []intentcontrol.ApprovalRequirementItem{{
			RequirementID:        "req-second-level",
			SeparationConstraint: "NOT_REQUESTER",
			MaterialityClass:     intentcontrol.Material,
		}},
		Obligations: []intentcontrol.ObligationItem{{
			ObligationID: "obl-notify-worker",
			Kind:         "NOTIFY",
			DueAt:        fixedInstant.AddDate(0, 0, 7),
		}},
	}
}

// referencePlan is a valid compiled plan for (intent, revision).
func referencePlan(tenant, intent uuid.UUID, revision uint64, label string) intentcontrol.Plan {
	return intentcontrol.Plan{
		TenantID:                 tenant,
		PlanID:                   uuid.New(),
		IntentID:                 intent,
		Revision:                 revision,
		PlanDigest:               digestOf("plan:" + label),
		IntentDigest:             digestOf("intent:" + intent.String()),
		ProposalDigest:           digestOf("proposal-material:" + label),
		ControlDigest:            digestOf("control"),
		ExecutionMode:            "SIMULATE",
		CompiledStatus:           intentcontrol.PlanGovernanceValidated,
		GovernanceSnapshotDigest: digestOf("governance"),
		ConflictSnapshotDigest:   digestOf("conflict"),
		ConflictFenceToken:       "fence-1",
		IdempotencyRecordRef:     "idem:" + label,
		EffectiveFrom:            fixedInstant.AddDate(0, 1, 0),
		ExpiresAt:                fixedInstant.Add(time.Hour),
		CompiledBy:               "coordinator",
		CompiledAt:               fixedInstant,
	}
}

// referencePlanEffects is one declared effect with everything CompilePlan
// requires: an idempotency key, a compensation, a repair reference, an
// observation and its deadline.
func referencePlanEffects() []intentcontrol.PlanEffect {
	return []intentcontrol.PlanEffect{{
		EffectID:             "effect-notify-payroll",
		DestinationRef:       "connector:payroll",
		IdempotencyKey:       "payroll-1",
		Reversibility:        intentcontrol.Compensatable,
		CompensationStrategy: "REVERSE_POSTING",
		RepairPlanRef:        "repair:payroll",
		ObservationRef:       "observation:payroll-ack",
		ObservationDeadline:  fixedInstant.Add(24 * time.Hour),
	}}
}

// referenceCommitReceipt is a valid commit receipt for plan.
func referenceCommitReceipt(plan intentcontrol.Plan) intentcontrol.CommitReceipt {
	return intentcontrol.CommitReceipt{
		TenantID:                plan.TenantID,
		ReceiptID:               uuid.New(),
		PlanID:                  plan.PlanID,
		IntentDigest:            plan.IntentDigest,
		ProposalDigest:          plan.ProposalDigest,
		ControlDigest:           plan.ControlDigest,
		PlanDigest:              plan.PlanDigest,
		DatabaseTransactionID:   "txid:4711",
		IdempotencyRecordRef:    plan.IdempotencyRecordRef,
		AppendedEventCount:      2,
		ProjectionMutationCount: 1,
		OutboxEffectCount:       1,
		ProducedReferenceDigest: digestOf("produced:" + plan.PlanID.String()),
		CommittedAt:             fixedInstant.Add(time.Minute),
	}
}

// referenceAbortReceipt is a valid abort receipt for plan.
func referenceAbortReceipt(plan intentcontrol.Plan) intentcontrol.AbortReceipt {
	return intentcontrol.AbortReceipt{
		TenantID:             plan.TenantID,
		ReceiptID:            uuid.New(),
		PlanID:               plan.PlanID,
		IntentDigest:         plan.IntentDigest,
		ProposalDigest:       plan.ProposalDigest,
		ControlDigest:        plan.ControlDigest,
		PlanDigest:           plan.PlanDigest,
		ReasonCode:           intentcontrol.AbortPlanStale,
		Detail:               "baseline moved under the plan",
		AbortedBeforeEffects: true,
		AbortedAt:            fixedInstant.Add(time.Minute),
	}
}
