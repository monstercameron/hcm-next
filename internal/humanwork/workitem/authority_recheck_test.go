package workitem_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
)

type authorityRecheckDouble struct {
	mu      sync.Mutex
	allowed bool
	calls   int
}

func (d *authorityRecheckDouble) Recheck(_ context.Context, _ workitem.Executor, _ workitem.AuthorityRecheckRequest) (workitem.AuthorityRecheckDecision, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	return workitem.AuthorityRecheckDecision{Allowed: d.allowed, DecisionRef: "authz:test/v1", Reason: "current authority changed"}, nil
}

func (d *authorityRecheckDouble) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

type sessionRecheckDouble struct {
	mu    sync.Mutex
	err   error
	calls int
}

func (d *sessionRecheckDouble) CheckRevocation(context.Context, string, time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	return d.err
}

func (d *sessionRecheckDouble) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

func authorityRecheckItem(t *testing.T) (workitem.WorkItem, *pgxFixture) {
	t.Helper()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "work-006-authority-recheck")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	conn := appConn(t, db)
	item, err := workitem.NewWorkItem(newTaskInput(tenant, instance))
	if err != nil {
		t.Fatalf("NewWorkItem: %v", err)
	}
	workitemStore := workitem.Store{}
	resolution := singleCandidateResolution("principal:completer")
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = workitemStore.Create(context.Background(), tx, item, meta("created"))
		if err != nil {
			return err
		}
		item, err = workitemStore.Route(context.Background(), tx, tenant, item.WorkItemID, item.ItemVersion,
			workitem.Assignment{Resolution: resolution, Trigger: workitem.TriggerInitialRouting}, meta("routed"))
		if err != nil {
			return err
		}
		item, err = workitemStore.Claim(context.Background(), tx, workitem.ClaimInput{
			TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
			ClaimantPrincipalID: "principal:completer", ClaimExpiresAt: fixedInstant.Add(time.Hour), Now: fixedInstant,
			Meta: meta("claimed"),
		})
		if err != nil {
			return err
		}
		item, err = workitemStore.Start(context.Background(), tx, tenant, item.WorkItemID, item.ItemVersion,
			fixedInstant, meta("started"))
		return err
	})
	return item, &pgxFixture{db: db, conn: conn, tenant: tenant}
}

type pgxFixture struct {
	db     *pgtest.DB
	conn   *pgxadapter.Conn
	tenant uuid.UUID
}

func TestTodo_WORK_006(t *testing.T) {
	item, f := authorityRecheckItem(t)
	authority := &authorityRecheckDouble{allowed: true}
	sessions := &sessionRecheckDouble{}
	completed, err := completeWithRecheck(t, f, item, authority, sessions)
	if err != nil {
		t.Fatalf("CompleteWithAuthorityRecheck: %v", err)
	}
	if completed.Status != workitem.StatusCompleted || completed.CompletedBy != "principal:completer" {
		t.Fatalf("completed item = %+v", completed)
	}
	if authority.callCount() != 1 || sessions.callCount() != 1 {
		t.Fatalf("authority calls=%d session calls=%d", authority.callCount(), sessions.callCount())
	}
}

func TestTodo_WORK_006_Race(t *testing.T) {
	item, f := authorityRecheckItem(t)
	second := f.db.NewConn(t)
	if _, err := second.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("SET ROLE second connection: %v", err)
	}
	authority := &authorityRecheckDouble{allowed: true}
	sessions := &sessionRecheckDouble{}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, conn := range []*pgxadapter.Conn{f.conn, second} {
		go func(conn *pgxadapter.Conn) {
			<-start
			_, err := completeWithRecheckOn(conn, f.tenant, item, authority, sessions)
			results <- err
		}(conn)
	}
	close(start)
	first, secondErr := <-results, <-results
	if first != nil && secondErr != nil {
		t.Fatalf("both racing completions failed: %v; %v", first, secondErr)
	}
	var completedTransitions int
	inTenantTx(t, f.conn, f.tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM work_item_transition WHERE tenant_id=$1 AND work_item_id=$2 AND to_status='COMPLETED'`,
			f.tenant, item.WorkItemID).Scan(&completedTransitions)
	})
	if completedTransitions != 1 {
		t.Fatalf("completed transitions = %d, want 1", completedTransitions)
	}
}

func TestTodo_WORK_006_Security(t *testing.T) {
	item, f := authorityRecheckItem(t)
	authority := &authorityRecheckDouble{allowed: false}
	sessions := &sessionRecheckDouble{}
	_, err := completeWithRecheck(t, f, item, authority, sessions)
	if workitem.CodeOf(err) != workitem.CodeAuthorityChanged {
		t.Fatalf("error = %v, code=%q", err, workitem.CodeOf(err))
	}
	if authority.callCount() != 1 || sessions.callCount() != 1 {
		t.Fatalf("authority calls=%d session calls=%d", authority.callCount(), sessions.callCount())
	}
	assertWorkItemUnchanged(t, f, item)
}

func TestTodo_WORK_006_Mutation(t *testing.T) {
	t.Run("revoked session is checked before authority", func(t *testing.T) {
		item, f := authorityRecheckItem(t)
		authority := &authorityRecheckDouble{allowed: true}
		sessions := &sessionRecheckDouble{err: sessionRevokedError{}}
		_, err := completeWithRecheck(t, f, item, authority, sessions)
		if workitem.CodeOf(err) != workitem.CodeSessionRevoked {
			t.Fatalf("error = %v, code=%q", err, workitem.CodeOf(err))
		}
		if authority.callCount() != 0 {
			t.Fatalf("authority calls = %d, want 0", authority.callCount())
		}
		assertWorkItemUnchanged(t, f, item)
	})
	t.Run("legacy complete remains available", func(t *testing.T) {
		item, f := authorityRecheckItem(t)
		var completed workitem.WorkItem
		inTenantTx(t, f.conn, f.tenant, func(tx dbport.Tx) error {
			var err error
			completed, err = (workitem.Store{}).Complete(context.Background(), tx, workitem.CompleteInput{
				TenantID: f.tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				CompletedBy: "principal:completer", CompletedOutputDigest: "sha256:" + repeatHex("a"),
				Now: fixedInstant, Meta: meta("completed"),
			})
			return err
		})
		if completed.Status != workitem.StatusCompleted {
			t.Fatalf("legacy complete = %+v", completed)
		}
	})
}

type sessionRevokedError struct{}

func (sessionRevokedError) Error() string { return "SESSION_REVOKED" }
func (sessionRevokedError) Code() string  { return workitem.CodeSessionRevoked }

func completeWithRecheck(t *testing.T, f *pgxFixture, item workitem.WorkItem, authority workitem.AuthorityRecheckPort, sessions workitem.SessionRevocationPort) (workitem.WorkItem, error) {
	t.Helper()
	return completeWithRecheckOn(f.conn, f.tenant, item, authority, sessions)
}

func completeWithRecheckOn(conn *pgxadapter.Conn, tenant uuid.UUID, item workitem.WorkItem, authority workitem.AuthorityRecheckPort, sessions workitem.SessionRevocationPort) (workitem.WorkItem, error) {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return workitem.WorkItem{}, err
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return workitem.WorkItem{}, err
	}
	completed, err := (workitem.Store{}).CompleteWithAuthorityRecheck(ctx, tx, workitem.CompleteWithAuthorityRecheckInput{
		CompleteInput: workitem.CompleteInput{
			TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
			CompletedBy: "principal:completer", CompletedOutputDigest: "sha256:" + repeatHex("a"),
			Now: fixedInstant, Meta: meta("completed"),
		},
		SessionRef: "session:test/v1", Session: sessions, Authority: authority,
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		return workitem.WorkItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return workitem.WorkItem{}, err
	}
	return completed, nil
}

func assertWorkItemUnchanged(t *testing.T, f *pgxFixture, item workitem.WorkItem) {
	t.Helper()
	var got workitem.WorkItem
	inTenantTx(t, appConn(t, f.db), f.tenant, func(tx dbport.Tx) error {
		var err error
		got, err = (workitem.Store{}).Load(context.Background(), tx, f.tenant, item.WorkItemID)
		return err
	})
	if got.Status != item.Status || got.ItemVersion != item.ItemVersion || got.CompletedBy != "" {
		t.Fatalf("item changed from %+v to %+v", item, got)
	}
}
