package repair

import (
	"context"
	"errors"
	"testing"
)

type scriptedAdmitter struct {
	calls int
	out   Result
	err   error
}

func (a *scriptedAdmitter) RevalidateAndFence(context.Context, RevalidationRequest) (Result, error) {
	a.calls++
	return a.out, a.err
}

func TestAdmitRepair_FencesOnlyReadyPlansAndRefusesUnboundFences(t *testing.T) {
	req := testRequest()
	got, err := AdmitRepair(context.Background(), NewMemoryStore(), req)
	if err != nil || got.Status != StatusReady || got.Fence.FenceID == "" || got.Fence.PlanDigest != req.Plan.Digest {
		t.Fatalf("ready plan must be fenced by the memory store, got %+v %v", got, err)
	}

	stale := req
	stale.Current.PlanDigest = "sha256:" + "0000000000000000000000000000000000000000000000000000000000000000"
	admitter := &scriptedAdmitter{}
	pre, err := AdmitRepair(context.Background(), admitter, stale)
	if err != nil || pre.Status == StatusReady || admitter.calls != 0 {
		t.Fatalf("a plan the evidence rejects must never reach the admitter, got %+v %v calls=%d", pre, err, admitter.calls)
	}

	unbound := &scriptedAdmitter{out: Result{Status: StatusReady, PlanDigest: req.Plan.Digest, Fence: Fence{FenceID: "fence-1", PlanDigest: "sha256:other"}}}
	if _, err := AdmitRepair(context.Background(), unbound, req); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("a fence bound to another plan must be refused, got %v", err)
	}
	boom := errors.New("admitter down")
	if _, err := AdmitRepair(context.Background(), &scriptedAdmitter{err: boom}, req); !errors.Is(err, boom) {
		t.Fatalf("admitter error must propagate, got %v", err)
	}
	if _, err := AdmitRepair(context.Background(), nil, req); err == nil {
		t.Fatal("nil admitter must be refused")
	}
}
