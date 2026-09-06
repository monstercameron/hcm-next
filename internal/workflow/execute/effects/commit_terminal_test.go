package effects_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	transactioncommit "github.com/monstercameron/hcm-next/internal/transaction/commit"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
	"github.com/monstercameron/hcm-next/internal/transaction/plan"
	"github.com/monstercameron/hcm-next/internal/workflow/execute"
	"github.com/monstercameron/hcm-next/internal/workflow/execute/effects"
)

type fakeCommitter struct {
	calls   int
	receipt transactioncommit.Receipt
}

func (f *fakeCommitter) CommitInTx(context.Context, dbport.Tx, plan.TransactionPlan) (transactioncommit.Receipt, error) {
	f.calls++
	return f.receipt, nil
}

type delegateTerminal struct{ calls int }

func (d *delegateTerminal) Write(context.Context, dbport.Tx, execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
	d.calls++
	return idempotency.ResultIdentity{ResultRef: "delegated"}, nil
}

func TestCommitTerminalWriterImplementsTerminalPortAndCommitsAPlan(t *testing.T) {
	tenant := uuid.New()
	fake := &fakeCommitter{receipt: transactioncommit.Receipt{ReceiptID: uuid.New(), PlanID: "plan-1"}}
	writer := &effects.CommitTerminalWriter{
		Committer: fake,
		Plan: func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (plan.TransactionPlan, error) {
			return plan.TransactionPlan{Tenant: values.TenantId(tenant.String()), PlanID: "plan-1", IdempotencyKey: "key-1"}, nil
		},
	}
	identity, err := writer.Write(context.Background(), nil, execute.TerminalWriteRequest{TenantID: tenant})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if fake.calls != 1 || identity.EvidenceID != fake.receipt.ReceiptID.String() || identity.EffectIdentity != "key-1" {
		t.Fatalf("calls/identity = %d/%+v", fake.calls, identity)
	}
}

func TestCommitTerminalWriterRetainsAdditiveDelegateDuringPlanRollout(t *testing.T) {
	delegate := &delegateTerminal{}
	writer := &effects.CommitTerminalWriter{Next: delegate}
	identity, err := writer.Write(context.Background(), nil, execute.TerminalWriteRequest{})
	if err != nil || identity.ResultRef != "delegated" || delegate.calls != 1 {
		t.Fatalf("delegate Write = %+v, %v; calls=%d", identity, err, delegate.calls)
	}
}
