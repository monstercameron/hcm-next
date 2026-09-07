package approval

import (
	"context"
	"errors"
	"testing"

	transactionplan "github.com/monstercameron/hcm-next/internal/transaction/plan"
)

type scriptedConsumer struct {
	out ExecutionEligibility
	err error
}

func (c scriptedConsumer) ConsumeAndElect(context.Context, ConsumeRequest) (ExecutionEligibility, error) {
	return c.out, c.err
}

func TestElectExecutablePlan_BindsTheElectionToThePresentedPlan(t *testing.T) {
	req := ConsumeRequest{Plan: transactionplan.TransactionPlan{Digest: "sha256:plan-1"}}
	good := ExecutionEligibility{PlanDigest: "sha256:plan-1", Executable: true, ConsumedDecisionIDs: []string{"decision-1"}}
	if got, err := ElectExecutablePlan(context.Background(), scriptedConsumer{out: good}, req); err != nil || got.PlanDigest != "sha256:plan-1" {
		t.Fatalf("bound election must pass through, got %+v %v", got, err)
	}
	wrong := good
	wrong.PlanDigest = "sha256:plan-2"
	if _, err := ElectExecutablePlan(context.Background(), scriptedConsumer{out: wrong}, req); !errors.Is(err, ErrElectionMismatch) {
		t.Fatalf("foreign plan digest must be refused, got %v", err)
	}
	hollow := good
	hollow.ConsumedDecisionIDs = nil
	if _, err := ElectExecutablePlan(context.Background(), scriptedConsumer{out: hollow}, req); !errors.Is(err, ErrElectionMismatch) {
		t.Fatalf("executable without consumed decisions must be refused, got %v", err)
	}
	boom := errors.New("port down")
	if _, err := ElectExecutablePlan(context.Background(), scriptedConsumer{err: boom}, req); !errors.Is(err, boom) {
		t.Fatalf("port error must propagate, got %v", err)
	}
	if _, err := ElectExecutablePlan(context.Background(), nil, req); err == nil {
		t.Fatal("nil consumer must be refused")
	}
}
