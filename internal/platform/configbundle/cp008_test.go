package configbundle

import (
	"errors"
	"testing"
	"time"
)

func killSwitchRequest(id string, target KillSwitchTarget, priority uint32) KillSwitchRequest {
	issued := time.Date(2026, 9, 5, 15, 0, 0, 0, time.UTC)
	return KillSwitchRequest{SwitchID: id, Target: target, Priority: priority, Reason: "contain unsafe execution", IncidentRef: "incident:" + id, Operator: "operator-1", Approver: "approver-1", EvidenceRef: "evidence:" + id, IssuedAt: issued, ExpiresAt: issued.Add(time.Hour), PropagationSLO: 10 * time.Minute}
}

// TestTodo_CP_008 proves scoped dual-control switches stop matching work,
// select the highest precedence, and emit signed applied receipts.
func TestTodo_CP_008(t *testing.T) {
	signer, publicKey := cp005Signer(t)
	store := NewKillSwitchStore(signer, publicKey)
	global, err := store.Issue(killSwitchRequest("global", KillSwitchTarget{TenantID: "kill-tenant", Capability: "promotion.execute"}, 1))
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := store.Issue(killSwitchRequest("service", KillSwitchTarget{TenantID: "kill-tenant", CellID: "cell-a", Service: "promotion"}, 5))
	if err != nil {
		t.Fatal(err)
	}
	globalReceipt, err := store.Apply(global, global.Request.IssuedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	scopedReceipt, err := store.Apply(scoped, scoped.Request.IssuedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !globalReceipt.WithinSLO || !scopedReceipt.WithinSLO {
		t.Fatalf("receipts=%+v %+v", globalReceipt, scopedReceipt)
	}
	if err := scoped.Verify(publicKey); err != nil {
		t.Fatalf("switch Verify: %v", err)
	}
	if err := scopedReceipt.Verify(publicKey); err != nil {
		t.Fatalf("receipt Verify: %v", err)
	}
	decision := store.Evaluate(KillSwitchTarget{TenantID: "kill-tenant", CellID: "cell-a", Service: "promotion", Capability: "promotion.execute"}, scoped.Request.IssuedAt.Add(2*time.Minute))
	if !decision.Disabled || decision.SwitchID != "service" || decision.Priority != 5 {
		t.Fatalf("decision=%+v", decision)
	}
}

func TestTodo_CP_008_Golden(t *testing.T) {
	signer, _ := cp005Signer(t)
	request := killSwitchRequest("golden", KillSwitchTarget{TenantID: "kill-tenant", Model: "model-v1"}, 2)
	first := NewKillSwitchStore(signer)
	one, err := first.Issue(request)
	if err != nil {
		t.Fatal(err)
	}
	second := NewKillSwitchStore(signer)
	two, err := second.Issue(request)
	if err != nil || one.Digest != two.Digest || one.Explain() == "" {
		t.Fatalf("first=%+v second=%+v err=%v", one, two, err)
	}
}

func TestTodo_CP_008_Integration(t *testing.T) {
	signer, _ := cp005Signer(t)
	store := NewKillSwitchStore(signer)
	switchValue, err := store.Publish(killSwitchRequest("connector", KillSwitchTarget{TenantID: "kill-tenant", Connector: "workday"}, 9))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplySwitch(switchValue, switchValue.Request.IssuedAt.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	allowed := store.Evaluate(KillSwitchTarget{TenantID: "other-tenant", Connector: "workday"}, switchValue.Request.IssuedAt.Add(3*time.Minute))
	if allowed.Disabled {
		t.Fatalf("cross-tenant switch applied: %+v", allowed)
	}
}

func TestTodo_CP_008_Fault(t *testing.T) {
	signer, _ := cp005Signer(t)
	store := NewKillSwitchStore(signer)
	switchValue, err := store.Issue(killSwitchRequest("late", KillSwitchTarget{TenantID: "kill-tenant", Service: "worker"}, 3))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := store.Apply(switchValue, switchValue.Request.IssuedAt.Add(20*time.Minute))
	if err != nil || receipt.Status != "APPLIED_LATE" || receipt.WithinSLO {
		t.Fatalf("late receipt=%+v err=%v", receipt, err)
	}
	if !store.Evaluate(KillSwitchTarget{TenantID: "kill-tenant", Service: "worker"}, switchValue.Request.IssuedAt.Add(21*time.Minute)).Disabled {
		t.Fatal("late control did not fail closed")
	}
}

func TestTodo_CP_008_Security(t *testing.T) {
	signer, _ := cp005Signer(t)
	store := NewKillSwitchStore(signer)
	request := killSwitchRequest("security", KillSwitchTarget{TenantID: "kill-tenant", Capability: "access.revoke"}, 1)
	request.Operator = request.Approver
	if _, err := store.Issue(request); !errors.Is(err, ErrKillSwitchInvalid) {
		t.Fatalf("self-approval error=%v", err)
	}
	request = killSwitchRequest("security-2", KillSwitchTarget{TenantID: "kill-tenant", Capability: "access.revoke"}, 1)
	request.Target.Capability = "*"
	if _, err := store.Issue(request); !errors.Is(err, ErrKillSwitchInvalid) {
		t.Fatalf("wildcard target error=%v", err)
	}
}
