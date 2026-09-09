package session_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type revokedWorkflowFixture struct {
	db        *pgtest.DB
	conn      *pgxadapter.Conn
	tenant    uuid.UUID
	item      workitem.WorkItem
	manager   *session.PersistentManager
	session   session.Record
	checkedAt time.Time
}

type allowedAuthority struct{}

func (allowedAuthority) Recheck(context.Context, workitem.Executor, workitem.AuthorityRecheckRequest) (workitem.AuthorityRecheckDecision, error) {
	return workitem.AuthorityRecheckDecision{Allowed: true, DecisionRef: "authz:trust-005/v1"}, nil
}

func newRevokedWorkflowFixture(t *testing.T) revokedWorkflowFixture {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()
	when := time.Date(2026, 9, 5, 14, 0, 0, 0, time.UTC)
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'TRUST-005 tenant', 'ACTIVE', $3)`,
		tenant, "trust-005-"+tenant.String(), when.Add(-time.Hour))

	sessionConn := db.NewConn(t)
	if _, err := sessionConn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("SET ROLE session connection: %v", err)
	}
	manager, err := session.NewPersistentManager(session.PersistentManagerConfig{
		Now: func() time.Time { return when }, Store: pgstore.New(sessionConn),
	})
	if err != nil {
		t.Fatalf("NewPersistentManager: %v", err)
	}
	rec, _, err := manager.Create(context.Background(), session.CreateSpec{
		Tenant: values.TenantId(tenant.String()), Subject: "user:approver",
		PrincipalFingerprint: "fp:trust-005", Assurance: trust.AssuranceHigh,
		IdleTimeout: time.Hour, AbsoluteTimeout: 4 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("SET ROLE work connection: %v", err)
	}
	instance := uuid.New()
	db.Exec(t, `
		INSERT INTO workflow_instance (
			tenant_id, instance_id, cell_id, workflow_id, workflow_version,
			compiled_plan_hash, execution_mode, runtime_status, input_ref,
			current_node_ids, correlation_id, created_at)
		VALUES ($1, $2, 'cell-local', 'wf.promotion', 1,
			'`+strings.Repeat("0", 64)+`', 'EXECUTE', 'WAITING', 'sha256:input',
			ARRAY['approval_node'], 'corr:trust-005', $3)`, tenant, instance, when)

	item, err := workitem.NewWorkItem(workitem.NewWorkItemInput{
		TenantID: tenant, Kind: workitem.KindTask, WorkType: "promotion.approval",
		CorrelationID: "corr:trust-005", WorkflowInstanceID: instance, NodeID: "approval_node",
		SubjectRefs: []string{"worker:jane"}, PolicyRouteRef: "route:manager/v1",
		Visibility: workitem.VisibilityAssigneeOnly, OrganizationScopeID: "org:acme/people",
		DeadlineAt: when.Add(time.Hour), CreatedAt: when,
	})
	if err != nil {
		t.Fatalf("NewWorkItem: %v", err)
	}
	store := workitem.Store{}
	if err := sessionTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		item, err = store.Create(context.Background(), tx, item, sessionMeta("created", when))
		if err != nil {
			return err
		}
		item, err = store.Route(context.Background(), tx, tenant, item.WorkItemID, item.ItemVersion,
			workitem.Assignment{Resolution: sessionResolution(), Trigger: workitem.TriggerInitialRouting}, sessionMeta("routed", when))
		if err != nil {
			return err
		}
		item, err = store.Claim(context.Background(), tx, workitem.ClaimInput{
			TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
			ClaimantPrincipalID: "user:approver", ClaimExpiresAt: when.Add(time.Hour), Now: when,
			Meta: sessionMeta("claimed", when),
		})
		if err != nil {
			return err
		}
		item, err = store.Start(context.Background(), tx, tenant, item.WorkItemID, item.ItemVersion, when, sessionMeta("started", when))
		return err
	}); err != nil {
		t.Fatalf("seed claimed work item: %v", err)
	}
	return revokedWorkflowFixture{db: db, conn: conn, tenant: tenant, item: item, manager: manager, session: rec, checkedAt: when}
}

func TestTodo_TRUST_005(t *testing.T) {
	f := newRevokedWorkflowFixture(t)
	if _, err := f.manager.Revoke(context.Background(), session.ID(f.session.ID()), "operator_revoked"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	err := sessionTenantTx(t, f.conn, f.tenant, func(tx dbport.Tx) error {
		_, err := (workitem.Store{}).CompleteWithAuthorityRecheck(context.Background(), tx, workitem.CompleteWithAuthorityRecheckInput{
			CompleteInput: workitem.CompleteInput{
				TenantID: f.tenant, WorkItemID: f.item.WorkItemID, ExpectedVersion: f.item.ItemVersion,
				CompletedBy: "user:approver", CompletedOutputDigest: "sha256:" + strings.Repeat("a", 64),
				Now: f.checkedAt, Meta: sessionMeta("completed", f.checkedAt),
			},
			SessionRef: string(f.session.ID()), Session: f.manager, Authority: allowedAuthority{},
		})
		return err
	})
	if workitem.CodeOf(err) != workitem.CodeSessionRevoked {
		t.Fatalf("completion error = %v, code=%q", err, workitem.CodeOf(err))
	}
	assertParkedAfterRevocation(t, f)
}

func TestTodo_TRUST_005_Security(t *testing.T) {
	m := newManager(t, &clock{at: baseTime}, time.Hour, 2*time.Hour)
	rec, _ := mustCreate(t, m, validSpec())
	if _, err := m.Revoke(context.Background(), rec.ID(), "security_test"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	err := m.CheckRevocation(context.Background(), string(rec.ID()), baseTime)
	var typed *session.RevocationError
	if !errors.As(err, &typed) || typed.Code() != session.CodeSessionRevoked {
		t.Fatalf("CheckRevocation error = %v, want typed %s", err, session.CodeSessionRevoked)
	}
}

func TestTodo_TRUST_005_Mutation(t *testing.T) {
	m := newManager(t, &clock{at: baseTime}, time.Hour, 2*time.Hour)
	if err := m.CheckRevocation(context.Background(), "sess:unknown", baseTime); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("unknown session error = %v", err)
	}
	rec, _ := mustCreate(t, m, validSpec())
	if err := m.CheckRevocation(context.Background(), string(rec.ID()), baseTime.Add(2*time.Hour)); !errors.Is(err, session.ErrSessionNotActive) {
		t.Fatalf("expired session error = %v", err)
	}
	if _, err := m.Revoke(context.Background(), rec.ID(), "again"); !errors.Is(err, session.ErrSessionNotActive) {
		t.Fatalf("revoke after expiry error = %v", err)
	}
}

func TestRevocationError_CodeAndUnwrap(t *testing.T) {
	err := &session.RevocationError{SessionRef: "sess-1", Status: session.StatusRevoked}
	if err.Error() != "session: SESSION_REVOKED for sess-1" || err.Code() != session.CodeSessionRevoked {
		t.Fatalf("revocation error = %q code=%q", err.Error(), err.Code())
	}
	if !errors.Is(err, session.ErrSessionRevoked) || !errors.Is(err, session.ErrSessionNotActive) {
		t.Fatalf("RevocationError does not unwrap both sentinels: %v", err)
	}
	if session.CodeOf(err) != session.CodeSessionRevoked || session.CodeOf(errors.New("other")) != "" || session.CodeOf(nil) != "" {
		t.Fatalf("CodeOf results: typed=%q plain=%q nil=%q", session.CodeOf(err), session.CodeOf(errors.New("other")), session.CodeOf(nil))
	}
}

func TestPersistentManager_CheckRevocationUsesCurrentStoreState(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	mgr, err := session.NewPersistentManager(session.PersistentManagerConfig{Now: clockAt(baseTime), Store: store})
	if err != nil {
		t.Fatalf("NewPersistentManager: %v", err)
	}
	rec, _ := mustCreatePersistent(t, mgr)
	if err := mgr.CheckRevocation(ctx, string(rec.ID()), baseTime); err != nil {
		t.Fatalf("active persistent check = %v", err)
	}
	if _, err := mgr.Revoke(ctx, rec.ID(), "disabled"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	err = mgr.CheckRevocation(ctx, string(rec.ID()), baseTime)
	if !errors.Is(err, session.ErrSessionRevoked) || session.CodeOf(err) != session.CodeSessionRevoked {
		t.Fatalf("revoked persistent check = %v code=%q", err, session.CodeOf(err))
	}
	if err := mgr.CheckRevocation(ctx, "missing", baseTime); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("missing persistent check = %v, want ErrSessionNotFound", err)
	}

	expiredStore := newFakeStore()
	expiredMgr, err := session.NewPersistentManager(session.PersistentManagerConfig{Now: clockAt(baseTime), Store: expiredStore})
	if err != nil {
		t.Fatalf("NewPersistentManager expired fixture: %v", err)
	}
	expired, _ := mustCreatePersistentWithSpec(t, expiredMgr, func(s *session.CreateSpec) { s.IdleTimeout = time.Minute; s.AbsoluteTimeout = time.Hour })
	if err := expiredMgr.CheckRevocation(ctx, string(expired.ID()), baseTime.Add(2*time.Minute)); !errors.Is(err, session.ErrSessionNotActive) {
		t.Fatalf("expired persistent check = %v, want ErrSessionNotActive", err)
	}
}

func mustCreatePersistent(t *testing.T, mgr *session.PersistentManager) (session.Record, session.RefreshToken) {
	return mustCreatePersistentWithSpec(t, mgr, func(*session.CreateSpec) {})
}

func mustCreatePersistentWithSpec(t *testing.T, mgr *session.PersistentManager, mutate func(*session.CreateSpec)) (session.Record, session.RefreshToken) {
	t.Helper()
	spec := validSpec()
	mutate(&spec)
	rec, token, err := mgr.Create(context.Background(), spec)
	if err != nil {
		t.Fatalf("persistent Create: %v", err)
	}
	return rec, token
}

func FuzzTodo_TRUST_005(f *testing.F) {
	f.Add("session:fuzz:active")
	f.Add("session:fuzz:revoked")
	f.Fuzz(func(t *testing.T, ref string) {
		if ref == "" || strings.ContainsAny(ref, " \t") {
			t.Skip()
		}
		m := newManager(t, &clock{at: baseTime}, time.Hour, 2*time.Hour)
		rec, _ := mustCreate(t, m, validSpec())
		if err := m.CheckRevocation(context.Background(), string(rec.ID()), baseTime); err != nil {
			t.Fatalf("active check: %v", err)
		}
	})
}

func assertParkedAfterRevocation(t *testing.T, f revokedWorkflowFixture) {
	t.Helper()
	var status string
	var completedBy *string
	var completedTransitions int
	var runtimeStatus string
	var ledgerRows int
	if err := sessionTenantTx(t, f.conn, f.tenant, func(tx dbport.Tx) error {
		if err := tx.QueryRow(context.Background(), `SELECT status, completed_by FROM work_item WHERE tenant_id=$1 AND work_item_id=$2`, f.tenant, f.item.WorkItemID).Scan(&status, &completedBy); err != nil {
			return err
		}
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM work_item_transition WHERE tenant_id=$1 AND work_item_id=$2 AND to_status='COMPLETED'`, f.tenant, f.item.WorkItemID).Scan(&completedTransitions); err != nil {
			return err
		}
		if err := tx.QueryRow(context.Background(), `SELECT runtime_status FROM workflow_instance WHERE tenant_id=$1 AND instance_id=$2`, f.tenant, f.item.WorkflowInstanceID).Scan(&runtimeStatus); err != nil {
			return err
		}
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id=$1 AND correlation_id=$2`, f.tenant, effects.CorrelationUUID("corr:trust-005")).Scan(&ledgerRows)
	}); err != nil {
		t.Fatalf("assert durable parked state: %v", err)
	}
	if status != string(workitem.StatusInProgress) || completedBy != nil || completedTransitions != 0 || runtimeStatus != "WAITING" || ledgerRows != 0 {
		t.Fatalf("status=%s completed_by=%v completed_transitions=%d runtime=%s ledger_rows=%d", status, completedBy, completedTransitions, runtimeStatus, ledgerRows)
	}
}

func sessionTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	t.Helper()
	tx, err := conn.Begin(context.Background())
	if err != nil {
		return err
	}
	if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
		_ = tx.Rollback(context.Background())
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(context.Background())
		return err
	}
	return tx.Commit(context.Background())
}

func sessionMeta(reason string, at time.Time) workitem.TransitionMeta {
	return workitem.TransitionMeta{ActorPrincipalID: "system:trust-005", Reason: reason, At: at}
}

func sessionResolution() humanwork.Resolution {
	return humanwork.Resolution{
		RequirementID: "req:trust-005", Outcome: humanwork.OutcomeResolved,
		Candidates: []humanwork.Candidate{{PrincipalID: "user:approver", Via: humanwork.SourceDirect, TermRef: "term:approver"}},
		ResolvedAt: values.NewInstant(baseTime), EffectiveAt: values.NewInstant(baseTime), DirectoryVersion: "directory:trust-005",
		ExpressionDigest: "sha256:" + strings.Repeat("b", 64), RequirementDigest: "sha256:" + strings.Repeat("c", 64), QuorumRequired: 1,
	}
}
