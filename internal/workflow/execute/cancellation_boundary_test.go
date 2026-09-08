package execute

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	transactioncancel "github.com/monstercameron/hcm-next/internal/transaction/cancel"
	transactioncommit "github.com/monstercameron/hcm-next/internal/transaction/commit"
	"github.com/monstercameron/hcm-next/internal/transaction/conflict"
	"github.com/monstercameron/hcm-next/internal/transaction/plan"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

func TestGovernedCancellationRequiresDatabase(t *testing.T) {
	var d *Driver
	if _, err := d.CancelGoverned(context.Background(), transactioncancel.Request{}, nil); err == nil {
		t.Fatal("nil driver accepted governed cancellation")
	}
}

func TestGovernedCommitRequiresDatabase(t *testing.T) {
	var d *Driver
	if _, err := d.CommitGoverned(context.Background(), plan.TransactionPlan{}, transactioncancel.Request{}); err == nil {
		t.Fatal("nil driver accepted governed commit")
	}
}

// TestTodo_CONFLICT_003_DriverPort proves the execute boundary forwards a
// plan-bound conflict reference to the caller-composed durable fence. It also
// proves that a governed plan cannot silently fall back to an unfenced commit
// when that adapter is absent.
func TestTodo_CONFLICT_003_DriverPort(t *testing.T) {
	db := pgtest.New(t)
	prepared, tenant, at := governedBoundaryFixture(t, db)
	refused := errors.New("fence refused")
	fence := &recordingBoundaryFence{err: refused}
	driver, err := New(Options{DB: db.Conn, Steps: boundarySteps{}, ConflictFence: fence, Clock: func() time.Time { return at }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := driver.CommitGoverned(context.Background(), prepared, transactioncancel.NewRequest(prepared, "principal:reviewer", "commit", at)); !errors.Is(err, refused) {
		t.Fatalf("CommitGoverned with refusing fence = %v, want %v", err, refused)
	}
	if fence.req.TenantID != tenant.String() || fence.req.IntentID != prepared.ConflictIntentID || fence.req.SnapshotDigest != prepared.ConflictSnapshotDigest {
		t.Fatalf("forwarded fence request = %+v", fence.req)
	}

	withoutFence, err := New(Options{DB: db.Conn, Steps: boundarySteps{}, Clock: func() time.Time { return at }})
	if err != nil {
		t.Fatalf("New without fence: %v", err)
	}
	if _, err := withoutFence.CommitGoverned(context.Background(), prepared, transactioncancel.NewRequest(prepared, "principal:reviewer", "commit", at)); !errors.Is(err, transactioncommit.ErrInvalidPlan) {
		t.Fatalf("CommitGoverned without required fence = %v, want ErrInvalidPlan", err)
	}
}

type recordingBoundaryFence struct {
	req conflict.CommitRequest
	err error
}

func (f *recordingBoundaryFence) ValidateAtCommit(_ context.Context, _ dbport.Tx, req conflict.CommitRequest) (conflict.CommitResult, error) {
	f.req = req
	return conflict.CommitResult{}, f.err
}

type boundarySteps struct{}

func (boundarySteps) Run(context.Context, StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, nil
}

func governedBoundaryFixture(t *testing.T, db *pgtest.DB) (plan.TransactionPlan, uuid.UUID, time.Time) {
	t.Helper()
	tenant := uuid.New()
	at := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'Execute conflict boundary', 'ACTIVE', $3)`, tenant, "execute-conflict-"+tenant.String(), at)
	prepared := plan.TransactionPlan{
		PlanID: "execute-conflict-" + tenant.String(), Tenant: values.TenantId(tenant.String()),
		ProposalRevisionID: "proposal:" + tenant.String(), ProposalDigest: "sha256:" + strings.Repeat("a", 64),
		IdempotencyKey: "execute-conflict:" + tenant.String(), ExpiresAt: values.NewInstant(at.Add(time.Hour)),
		ConflictIntentID: "intent:" + tenant.String(), ConflictSnapshotDigest: "sha256:" + strings.Repeat("b", 64),
		ConflictFootprintDigests: []string{"sha256:" + strings.Repeat("c", 64)},
	}
	digest := sha256.Sum256(prepared.CanonicalBytes())
	prepared.Digest = "sha256:" + hex.EncodeToString(digest[:])
	return prepared, tenant, at
}
