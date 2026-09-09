package runtime_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

const promotionConflictSubject = promotionSubjectID

// databaseConflictFacts is a real adapter over workflow_instance. The
// transaction-level advisory lock serializes the current-candidate read with
// the competing Start transaction, while the row query makes the conformance
// race use durable state rather than an in-memory conflict double.
type databaseConflictFacts struct {
	competitor runtime.ConflictObservation
}

func (f databaseConflictFacts) Candidates(ctx context.Context, ex runtime.Executor, req runtime.ConflictCheckRequest) ([]runtime.ConflictObservation, error) {
	if len(req.SubjectRefs) == 0 {
		return nil, errors.New("conflict facts: no subject")
	}
	var locked int
	if err := ex.QueryRow(ctx, `
		SELECT 1 FROM (SELECT pg_advisory_xact_lock(hashtext($1))) AS conflict_lock`, req.SubjectRefs[0]).Scan(&locked); err != nil {
		return nil, err
	}
	var active int
	if err := ex.QueryRow(ctx, `
		SELECT count(*) FROM workflow_instance
		WHERE tenant_id = $1 AND $2 = ANY(business_subject_refs)
		  AND runtime_status NOT IN ('COMPLETED', 'CANCELLED', 'REPAIR_REQUIRED', 'QUARANTINED', 'SUPERSEDED')`,
		req.TenantID, req.SubjectRefs[0]).Scan(&active); err != nil {
		return nil, err
	}
	if active == 0 {
		return nil, nil
	}
	return []runtime.ConflictObservation{f.competitor}, nil
}

func promotionConflictCandidate(t *testing.T, tenant values.TenantId, revisionID string) conflict.Candidate {
	t.Helper()
	resource, err := values.NewResourceKey(tenant, values.Kind("employment"), "promotion-acceptance-worker", "primary")
	if err != nil {
		t.Fatalf("resource key: %v", err)
	}
	revision, err := values.NewSequenceRevision("employment:promotion-acceptance-worker", 1)
	if err != nil {
		t.Fatalf("revision token: %v", err)
	}
	interval, err := values.NewOpenInstantInterval(values.NewInstant(fixedInstant))
	if err != nil {
		t.Fatalf("effective interval: %v", err)
	}
	return conflict.Candidate{
		ProposalRevisionID: revisionID,
		State:              conflict.ProposalApproved,
		RecordedAt:         values.NewInstant(fixedInstant),
		Footprint: conflict.WriteFootprint{
			Resource:         resource,
			Field:            conflict.FieldPath("employment.assignment.position_ref"),
			Interval:         interval,
			Operation:        conflict.OperationUpdate,
			ExpectedRevision: revision,
			Authority:        conflict.AuthorityScope{Domain: "PEOPLE", PolicyRef: "authority.people.assignment/v1"},
		},
	}
}

func startWithConflict(t *testing.T, conn interface {
	Begin(context.Context) (dbport.Tx, error)
}, tenantID uuid.UUID, req runtime.StartRequest) runtime.StartReceipt {
	t.Helper()
	var receipt runtime.StartReceipt
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin start: %v", err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, tenantID); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("set tenant: %v", err)
	}
	if receipt, err = runtime.Start(context.Background(), tx, req); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("Start: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit start: %v", err)
	}
	return receipt
}

func hardConflictFacts(competitor conflict.Candidate, kind runtime.ConflictKind) databaseConflictFacts {
	return databaseConflictFacts{competitor: runtime.ConflictObservation{Candidate: competitor, Kind: kind}}
}

func workflowWriteStatements(sqls []string) []string {
	var writes []string
	for _, sql := range sqls {
		upper := strings.ToUpper(sql)
		if (strings.Contains(upper, "INSERT") || strings.Contains(upper, "UPDATE") || strings.Contains(upper, "DELETE")) && strings.Contains(upper, "WORKFLOW_") {
			writes = append(writes, sql)
		}
	}
	return writes
}

func secondPromotionNodeRequest(start runtime.StartReceipt, plan *workflow.CompiledWorkflow, tenantID uuid.UUID, revalidation *runtime.PromotionRevalidation) runtime.AdvanceRequest {
	return runtime.AdvanceRequest{
		TenantID: tenantID, InstanceID: start.InstanceID, ExpectedInstanceVersion: start.InstanceVersion,
		Attempt: 1, Plan: plan, Revalidation: revalidation,
		Outcome:    frontier.NodeOutcome{NodeID: promotionexec.NodeSimulateCompensation, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:promotion-revalidation-not-run"},
		RecordedAt: fixedInstant, Sink: runtime.NewMemorySink(),
	}
}

// TestPromotionLosesTheConflictRaceToACompetingTransfer proves a committed
// transfer wins an overlapping write-footprint race and a promotion is
// refused before Start writes its workflow instance.
func TestPromotionLosesTheConflictRaceToACompetingTransfer(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "promotion-conflict-transfer")
	tenantSlug := values.TenantId("promotion-conflict-transfer")
	transfer := newPromotionFixture(t, tenantSlug, "intent:transfer-wins")
	promotion := newPromotionFixture(t, tenantSlug, "intent:promotion-loses")
	transferCandidate := promotionConflictCandidate(t, tenantSlug, transfer.Proposal.ProposalRevisionID)
	promotionCandidate := promotionConflictCandidate(t, tenantSlug, promotion.Proposal.ProposalRevisionID)
	facts := hardConflictFacts(transferCandidate, runtime.ConflictKindTransfer)

	transferReq := transfer.baseStartRequest(tenantID, "start:transfer-wins")
	transferReq.BusinessSubjectRefs = []string{promotionConflictSubject}
	transferReq.ConflictFacts = facts
	transferReq.ConflictCandidate = &transferCandidate
	startWithConflict(t, conn, tenantID, transferReq)

	promotionReq := promotion.baseStartRequest(tenantID, "start:promotion-loses-transfer")
	promotionReq.BusinessSubjectRefs = []string{promotionConflictSubject}
	promotionReq.ConflictFacts = facts
	promotionReq.ConflictCandidate = &promotionCandidate
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin losing start: %v", err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, tenantID); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	recording := &recordingExecutor{tx: tx}
	_, startErr := runtime.Start(context.Background(), recording, promotionReq)
	_ = tx.Rollback(context.Background())
	if runtime.CodeOf(startErr) != runtime.CodeHardConflictCompetingTransfer {
		t.Fatalf("code = %q, want %q (%v)", runtime.CodeOf(startErr), runtime.CodeHardConflictCompetingTransfer, startErr)
	}
	if writes := workflowWriteStatements(recording.statements()); len(writes) != 0 {
		t.Fatalf("losing promotion wrote workflow state before refusal: %v", writes)
	}
	if !strings.Contains(startErr.Error(), transfer.Proposal.ProposalRevisionID) {
		t.Fatalf("refusal does not name the winning transfer revision: %v", startErr)
	}

	var instances int
	db.QueryRow(context.Background(), `SELECT count(*) FROM workflow_instance WHERE tenant_id = $1`, tenantID).Scan(&instances)
	if instances != 1 {
		t.Fatalf("workflow instances = %d, want only the committed transfer", instances)
	}
}

// TestPromotionLosesTheConflictRaceToATermination proves the same hard
// conflict boundary for a termination that commits first.
func TestPromotionLosesTheConflictRaceToATermination(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "promotion-conflict-termination")
	tenantSlug := values.TenantId("promotion-conflict-termination")
	termination := newPromotionFixture(t, tenantSlug, "intent:termination-wins")
	promotion := newPromotionFixture(t, tenantSlug, "intent:promotion-loses-termination")
	terminationCandidate := promotionConflictCandidate(t, tenantSlug, termination.Proposal.ProposalRevisionID)
	promotionCandidate := promotionConflictCandidate(t, tenantSlug, promotion.Proposal.ProposalRevisionID)
	facts := hardConflictFacts(terminationCandidate, runtime.ConflictKindTermination)

	terminationReq := termination.baseStartRequest(tenantID, "start:termination-wins")
	terminationReq.BusinessSubjectRefs = []string{promotionConflictSubject}
	terminationReq.ConflictFacts = facts
	terminationReq.ConflictCandidate = &terminationCandidate
	startWithConflict(t, conn, tenantID, terminationReq)

	promotionReq := promotion.baseStartRequest(tenantID, "start:promotion-loses-termination")
	promotionReq.BusinessSubjectRefs = []string{promotionConflictSubject}
	promotionReq.ConflictFacts = facts
	promotionReq.ConflictCandidate = &promotionCandidate
	err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		_, startErr := runtime.Start(context.Background(), tx, promotionReq)
		return startErr
	})
	if runtime.CodeOf(err) != runtime.CodeHardConflictCompetingTermination {
		t.Fatalf("code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeHardConflictCompetingTermination, err)
	}
}

// TestPromotionConflictRaceOnRealConnections proves the durable read/commit
// boundary under concurrent promotion attempts on separate PostgreSQL
// connections after the transfer has committed.
func TestPromotionConflictRaceOnRealConnections(t *testing.T) {
	db := pgtest.New(t)
	setupConn := appConn(t, db)
	tenantID := insertTenant(t, db, "promotion-conflict-race")
	tenantSlug := values.TenantId("promotion-conflict-race")
	transfer := newPromotionFixture(t, tenantSlug, "intent:race-transfer")
	transferCandidate := promotionConflictCandidate(t, tenantSlug, transfer.Proposal.ProposalRevisionID)
	transferReq := transfer.baseStartRequest(tenantID, "start:race-transfer")
	transferReq.BusinessSubjectRefs = []string{promotionConflictSubject}
	transferReq.ConflictFacts = hardConflictFacts(transferCandidate, runtime.ConflictKindTransfer)
	transferReq.ConflictCandidate = &transferCandidate
	startWithConflict(t, setupConn, tenantID, transferReq)

	const contenders = 8
	conns := make([]*pgxadapter.Conn, contenders)
	requests := make([]runtime.StartRequest, contenders)
	for i := range conns {
		conns[i] = appConn(t, db)
		proposal := newPromotionFixture(t, tenantSlug, "intent:race-promotion-"+string(rune('a'+i)))
		candidate := promotionConflictCandidate(t, tenantSlug, proposal.Proposal.ProposalRevisionID)
		req := proposal.baseStartRequest(tenantID, "start:race-promotion-"+string(rune('a'+i)))
		req.BusinessSubjectRefs = []string{promotionConflictSubject}
		req.ConflictFacts = hardConflictFacts(transferCandidate, runtime.ConflictKindTransfer)
		req.ConflictCandidate = &candidate
		requests[i] = req
	}

	var wg sync.WaitGroup
	errs := make([]error, contenders)
	for i := range conns {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = inTenantTxErr(conns[i], tenantID, func(tx dbport.Tx) error {
				_, err := runtime.Start(context.Background(), tx, requests[i])
				return err
			})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if runtime.CodeOf(err) != runtime.CodeHardConflictCompetingTransfer {
			t.Fatalf("contender %d code = %q, want %q (%v)", i, runtime.CodeOf(err), runtime.CodeHardConflictCompetingTransfer, err)
		}
	}
	var instances int
	db.QueryRow(context.Background(), `SELECT count(*) FROM workflow_instance WHERE tenant_id = $1`, tenantID).Scan(&instances)
	if instances != 1 {
		t.Fatalf("workflow instances after race = %d, want the transfer only", instances)
	}
}

// TestApprovedPromotionRebasesAfterAManagerChange proves a relationship
// change invalidates the approval binding, parks the durable instance, and
// refuses the next advancement before its node write or continuation sink.
func TestApprovedPromotionRebasesAfterAManagerChange(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "promotion-manager-rebase")
	pf := newPromotionFixture(t, values.TenantId("promotion-manager-rebase"), "intent:manager-rebase")
	req := pf.baseStartRequest(tenantID, "start:manager-rebase")
	start := startPromotionInstance(t, conn, tenantID, req)
	first, err := advanceOnce(t, conn, tenantID, runtime.AdvanceRequest{
		TenantID: tenantID, InstanceID: start.InstanceID, ExpectedInstanceVersion: start.InstanceVersion,
		Attempt: 1, Plan: pf.Plan,
		Outcome:    frontier.NodeOutcome{NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:snapshot"},
		RecordedAt: fixedInstant, Sink: runtime.NewMemorySink(),
	})
	if err != nil {
		t.Fatalf("snapshot advancement: %v", err)
	}

	revalidation := &runtime.PromotionRevalidation{PinnedManagerRef: "manager:before", CurrentManagerRef: "manager:after"}
	var parked runtime.Instance
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		parked, err = runtime.ParkForReapproval(context.Background(), tx, runtime.ReapprovalParkRequest{
			TenantID: tenantID, InstanceID: start.InstanceID, ExpectedInstanceVersion: first.NewInstanceVersion,
			ReasonRef: "revalidation:manager_relationship",
		})
		return err
	})
	if parked.RuntimeStatus != runtime.InstanceBlocked || parked.LastCheckpointRef != "revalidation:manager_relationship" || len(parked.CurrentNodeIDs) != 1 || parked.CurrentNodeIDs[0] != promotionexec.NodeSimulateCompensation {
		t.Fatalf("parked instance = status %s frontier %v, want BLOCKED at the next node", parked.RuntimeStatus, parked.CurrentNodeIDs)
	}

	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin refused advancement: %v", err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, tenantID); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	recording := &recordingExecutor{tx: tx}
	_, err = runtime.Advance(context.Background(), recording, secondPromotionNodeRequest(runtime.StartReceipt{
		InstanceID: parked.InstanceID, InstanceVersion: parked.InstanceVersion,
	}, pf.Plan, tenantID, revalidation))
	_ = tx.Rollback(context.Background())
	if runtime.CodeOf(err) != runtime.CodeApprovalBindingStale || !strings.Contains(err.Error(), "manager_relationship") {
		t.Fatalf("manager rebase refusal = %q (%v), want typed manager fact", runtime.CodeOf(err), err)
	}
	if writes := workflowWriteStatements(recording.statements()); len(writes) != 0 {
		t.Fatalf("manager rebase refusal wrote workflow state: %v", writes)
	}
}

// TestPolicyVersionChangeBeforeEffectiveDateRequiresRevalidation proves a
// changed rule-pack identity is not CONFIRMED and is routed to reapproval.
func TestPolicyVersionChangeBeforeEffectiveDateRequiresRevalidation(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "promotion-policy-revalidation")
	pf := newPromotionFixture(t, values.TenantId("promotion-policy-revalidation"), "intent:policy-revalidation")
	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start:policy-revalidation"))
	first, err := advanceOnce(t, conn, tenantID, runtime.AdvanceRequest{
		TenantID: tenantID, InstanceID: start.InstanceID, ExpectedInstanceVersion: start.InstanceVersion,
		Attempt: 1, Plan: pf.Plan,
		Outcome:    frontier.NodeOutcome{NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:snapshot"},
		RecordedAt: fixedInstant, Sink: runtime.NewMemorySink(),
	})
	if err != nil {
		t.Fatalf("snapshot advancement: %v", err)
	}

	revalidation := &runtime.PromotionRevalidation{PinnedPolicyVersion: "rules.compensation/v1", CurrentPolicyVersion: "rules.compensation/v2"}
	result, err := runtime.EvaluatePromotionRevalidation(*revalidation)
	if runtime.CodeOf(err) != runtime.CodePolicyVersionChanged {
		t.Fatalf("policy result code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodePolicyVersionChanged, err)
	}
	if result.Confirmed || result.Requirement != runtime.RevalidationReapprovalRequired || result.ChangedFact != "policy_version" || !strings.Contains(result.Explanation, runtime.CodeReapprovalRequired) {
		t.Fatalf("policy revalidation result = %+v, want refused REAPPROVAL_REQUIRED", result)
	}

	var parked runtime.Instance
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		parked, err = runtime.ParkForReapproval(context.Background(), tx, runtime.ReapprovalParkRequest{
			TenantID: tenantID, InstanceID: start.InstanceID, ExpectedInstanceVersion: first.NewInstanceVersion,
			ReasonRef: "revalidation:policy_version",
		})
		return err
	})
	if parked.RuntimeStatus != runtime.InstanceBlocked || parked.LastCheckpointRef != "revalidation:policy_version" {
		t.Fatalf("policy-changed instance status = %s, want BLOCKED", parked.RuntimeStatus)
	}
	refused, err := advanceOnce(t, conn, tenantID, secondPromotionNodeRequest(runtime.StartReceipt{
		InstanceID: parked.InstanceID, InstanceVersion: parked.InstanceVersion,
	}, pf.Plan, tenantID, revalidation))
	if runtime.CodeOf(err) != runtime.CodePolicyVersionChanged {
		t.Fatalf("policy advancement code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodePolicyVersionChanged, err)
	}
	if refused.InstanceID != uuid.Nil || refused.NodeID != "" || refused.NewInstanceVersion != 0 || len(refused.Frontier) != 0 {
		t.Fatalf("refused policy advancement returned receipt %+v", refused)
	}
}
