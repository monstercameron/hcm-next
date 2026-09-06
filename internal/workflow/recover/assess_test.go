package recover

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/workflow/lease"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

func TestAssess_DerivedCoordinatesDoNotVaryWithTheAttempt(t *testing.T) {
	instance := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	if got := EffectScopeFor("node-a"); got != "workflow-node:node-a" {
		t.Fatalf("EffectScopeFor = %q", got)
	}
	first, second := AttemptKey(instance, "node-a"), AttemptKey(instance, "node-a")
	if first != second {
		t.Fatalf("AttemptKey is not deterministic: %q vs %q", first, second)
	}
	if !strings.Contains(first, instance.String()) || !strings.Contains(first, "node-a") {
		t.Fatalf("AttemptKey %q names neither the instance nor the node", first)
	}
	if AttemptKey(instance, "node-b") == first {
		t.Fatal("two nodes of one instance share an idempotency key")
	}
	if AttemptKey(uuid.New(), "node-a") == first {
		t.Fatal("two instances share an idempotency key for the same node id")
	}
}

func TestAssess_DefaultsAreAppliedWithoutOverwritingWhatWasStated(t *testing.T) {
	instance := uuid.New()
	req := Request{TenantID: uuid.New(), InstanceID: instance, NodeID: "node-a"}
	scope := req.Scope()
	if scope.EffectScope != EffectScopeFor("node-a") {
		t.Fatalf("effect scope = %q, want the derived one", scope.EffectScope)
	}
	if scope.Key != AttemptKey(instance, "node-a") {
		t.Fatalf("key = %q, want the derived one", scope.Key)
	}

	stated := req
	stated.Capability = "cap-x"
	stated.EffectScope = "scope-x"
	stated.IdempotencyKey = "key-x"
	got := stated.Scope()
	if got.Capability != "cap-x" || got.EffectScope != "scope-x" || got.Key != "key-x" {
		t.Fatalf("stated coordinates were overwritten: %+v", got)
	}
}

func TestAssess_EffectDigestIsStableAndScopeSensitive(t *testing.T) {
	req := Request{TenantID: uuid.New(), InstanceID: uuid.New(), NodeID: "node-a"}
	base := req.EffectDigest()
	if len(base) != 64 {
		t.Fatalf("digest %q is not a 64-character sha256 hex string", base)
	}
	if req.EffectDigest() != base {
		t.Fatal("EffectDigest is not deterministic")
	}

	other := req
	other.NodeID = "node-b"
	if other.EffectDigest() == base {
		t.Fatal("two nodes digest identically")
	}
	keyed := req
	keyed.IdempotencyKey = "key-x"
	if keyed.EffectDigest() == base {
		t.Fatal("the idempotency key is not covered by the digest")
	}
}

func TestAssess_ResourceIsTheNodeExecutionNotTheInstance(t *testing.T) {
	instance := uuid.New()
	req := Request{TenantID: uuid.New(), InstanceID: instance, NodeID: "node-a"}
	res := req.Resource()
	if res.Kind != lease.ResourceNodeExecution {
		t.Fatalf("resource kind = %q, want %q", res.Kind, lease.ResourceNodeExecution)
	}
	if !strings.HasPrefix(res.ID, instance.String()) || !strings.HasSuffix(res.ID, "node-a") {
		t.Fatalf("resource id %q does not name the instance and the node", res.ID)
	}
}

func TestAssess_ValidateRefusesAMalformedRequestBeforeAnyRead(t *testing.T) {
	for name, req := range map[string]Request{
		"no tenant":   {InstanceID: uuid.New(), NodeID: "node-a"},
		"no instance": {TenantID: uuid.New(), NodeID: "node-a"},
		"no node":     {TenantID: uuid.New(), InstanceID: uuid.New()},
		"no plan":     {TenantID: uuid.New(), InstanceID: uuid.New(), NodeID: "node-a"},
	} {
		if err := req.validate(); err == nil {
			t.Fatalf("a request with %s was accepted", name)
		} else if CodeOf(err) != CodeInvalid {
			t.Fatalf("a request with %s: code = %q, want %q", name, CodeOf(err), CodeInvalid)
		}
	}
}

func TestAssess_RetirePathReachesRetryingByLegalHopsOnly(t *testing.T) {
	for _, start := range []runtime.NodeStatus{
		runtime.NodeReady, runtime.NodeRunning, runtime.NodeWaiting, runtime.NodeFailed,
	} {
		current := start
		for _, hop := range retirePath(start) {
			if !runtime.LegalNodeTransition(current, hop) {
				t.Fatalf("retirePath(%s) hops %s -> %s, which the node state machine forbids", start, current, hop)
			}
			current = hop
		}
		if current != runtime.NodeRetrying {
			t.Fatalf("retirePath(%s) ended at %s, want RETRYING", start, current)
		}
		if !runtime.LegalNodeTransition(current, runtime.NodeReady) {
			t.Fatalf("a further attempt cannot follow %s", current)
		}
	}
	if got := retirePath(runtime.NodeRetrying); got != nil {
		t.Fatalf("retirePath(RETRYING) = %v, want nil: an already-retired attempt is retired", got)
	}
	if got := retirePath(runtime.NodeSucceeded); got != nil {
		t.Fatalf("retirePath(SUCCEEDED) = %v, want nil", got)
	}
}

func TestAssess_DispositionRecoverableNamesOnlyTheTwoRecoveryShapes(t *testing.T) {
	if !DispositionReplayResult.Recoverable() || !DispositionExecuteEffect.Recoverable() {
		t.Fatal("a recovery shape reported itself unrecoverable")
	}
	if DispositionNothingToRecover.Recoverable() || DispositionLeaseLive.Recoverable() {
		t.Fatal("a non-recovery disposition reported itself recoverable")
	}
}

func TestAssess_CanonicalDigestIsProfileSeparated(t *testing.T) {
	one := canonicalDigest("profile-a", map[string]string{"k": "v"})
	two := canonicalDigest("profile-b", map[string]string{"k": "v"})
	if one == two {
		t.Fatal("two profiles produced the same digest over the same value")
	}
}
