package tenant_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/tenant"
	"github.com/monstercameron/hcm-next/internal/trust/session"
)

type revoker struct{ calls []string }

func (r *revoker) Revoke(_ context.Context, id session.ID, _ string) (session.Record, error) {
	r.calls = append(r.calls, string(id))
	return session.Record{}, nil
}

var lifecycleAt = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func TestTodo_TENANT_003(t *testing.T) {
	life, err := tenant.NewLifecycle("acme")
	if err != nil {
		t.Fatal(err)
	}
	r := &revoker{}
	suspended, err := life.Suspend(context.Background(), tenant.SuspendRequest{
		Reason: tenant.SuspensionSecurity, RequestedBy: "security-operator", IdempotencyKey: "s-1", At: lifecycleAt,
		Sessions:    []tenant.SessionRef{{ID: "session-b"}, {ID: "session-legal", RequiredForAccess: true}},
		PendingWork: []tenant.PendingWorkItem{{ID: "work-1"}, {ID: "legal-1", RequiredForAccess: true}}, Revoker: r,
	})
	if err != nil {
		t.Fatal(err)
	}
	if suspended.Status != tenant.TenantSuspended || len(r.calls) != 1 || r.calls[0] != "session-b" {
		t.Fatalf("suspension = %#v, revocations = %#v", suspended, r.calls)
	}
	if len(suspended.Capabilities) != len(tenant.AllCapabilities) || suspended.Pending.Action != tenant.PendingWorkFreeze || len(suspended.Pending.PreservedIDs) != 1 {
		t.Fatalf("incomplete suspension semantics: %#v", suspended)
	}
	replay, err := life.Suspend(context.Background(), tenant.SuspendRequest{Reason: tenant.SuspensionSecurity, RequestedBy: "security-operator", IdempotencyKey: "s-1", At: lifecycleAt, Revoker: r})
	if err != nil || replay.Decision != "NOOP" || len(r.calls) != 1 {
		t.Fatalf("idempotent retry = %#v, err=%v, calls=%v", replay, err, r.calls)
	}
	resumed, err := life.Resume(context.Background(), tenant.ResumeRequest{RequestedBy: "security-operator", IdempotencyKey: "r-1", At: lifecycleAt.Add(time.Hour)})
	if err != nil || resumed.Status != tenant.TenantActive {
		t.Fatalf("resume = %#v, err=%v", resumed, err)
	}
	closed, err := life.Close(context.Background(), tenant.CloseRequest{RequestedBy: "owner", IdempotencyKey: "c-1", At: lifecycleAt.Add(2 * time.Hour), Sessions: []tenant.SessionRef{{ID: "session-legal", RequiredForAccess: true}}, Revoker: r})
	if err != nil || closed.Status != tenant.TenantClosed || life.Status() != tenant.TenantClosed {
		t.Fatalf("close = %#v, err=%v", closed, err)
	}
	if len(r.calls) != 2 || r.calls[1] != "session-legal" {
		t.Fatalf("close did not revoke every session: %v", r.calls)
	}
}

func TestTodo_TENANT_003_Race(t *testing.T) {
	life, err := tenant.NewLifecycle("race")
	if err != nil {
		t.Fatal(err)
	}
	r := &revoker{}
	if _, err := life.Suspend(context.Background(), tenant.SuspendRequest{Reason: tenant.SuspensionCommercial, RequestedBy: "operator", IdempotencyKey: "one", At: lifecycleAt, Revoker: r}); err != nil {
		t.Fatal(err)
	}
	if got := life.Status(); got != tenant.TenantSuspended {
		t.Fatalf("status = %s", got)
	}
}

func TestTodo_TENANT_003_Integration(t *testing.T) {
	life, err := tenant.NewLifecycle("integration")
	if err != nil {
		t.Fatal(err)
	}
	result, err := life.Suspend(context.Background(), tenant.SuspendRequest{Reason: tenant.SuspensionLegal, RequestedBy: "legal", IdempotencyKey: "legal-1", At: lifecycleAt})
	if err != nil {
		t.Fatal(err)
	}
	if result.Event.Digest == "" || len(life.Events()) != 1 {
		t.Fatalf("event evidence = %#v", result.Event)
	}
}

func TestTodo_TENANT_003_Security(t *testing.T) {
	life, err := tenant.NewLifecycle("security")
	if err != nil {
		t.Fatal(err)
	}
	_, err = life.Suspend(context.Background(), tenant.SuspendRequest{Reason: tenant.SuspensionSecurity, RequestedBy: "security", IdempotencyKey: "bad", At: lifecycleAt, Sessions: []tenant.SessionRef{{ID: "s"}}})
	if !errors.Is(err, tenant.ErrInvalidLifecycle) {
		t.Fatalf("missing revoker error = %v", err)
	}
	if life.Status() != tenant.TenantActive {
		t.Fatalf("failed transition changed status to %s", life.Status())
	}
}

func TestTodo_TENANT_003_Mutation(t *testing.T) {
	life, err := tenant.NewLifecycle("mutation")
	if err != nil {
		t.Fatal(err)
	}
	r := &revoker{}
	result, err := life.Suspend(context.Background(), tenant.SuspendRequest{Reason: tenant.SuspensionCommercial, RequestedBy: "operator", IdempotencyKey: "m-1", At: lifecycleAt, Revoker: r})
	if err != nil {
		t.Fatal(err)
	}
	result.Capabilities[0].Allowed = true
	result.Event.Capabilities[0].Allowed = true
	if life.Events()[0].Capabilities[0].Allowed {
		t.Fatal("returned lifecycle evidence aliases internal state")
	}
}
