package providerexit

import (
	"strings"
	"testing"
	"time"
)

func validProviderRequest() Request {
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	resources := []Resource{
		{ID: "object-1", Kind: Object, ExternalRef: "obj/1", Enumerated: true, Reachable: true, ExportRequired: true, Retained: true, DeletionDisposition: "RETAIN_HOLD"},
		{ID: "request-1", Kind: RequestResource, ExternalRef: "req/1", Enumerated: true, Reachable: true},
		{ID: "webhook-1", Kind: Webhook, ExternalRef: "hook/1", Enumerated: true, Reachable: true, ActiveAuthority: true, Revoked: true},
		{ID: "credential-1", Kind: Credential, ExternalRef: "cred/1", Enumerated: true, Reachable: true, ActiveAuthority: true, Revoked: true},
		{ID: "endpoint-1", Kind: Endpoint, ExternalRef: "endpoint/1", Enumerated: true, Reachable: true},
		{ID: "grant-1", Kind: Grant, ExternalRef: "grant/1", Enumerated: true, Reachable: true},
	}
	return Request{Tenant: "tenant-1", Provider: "provider-1", RequestedBy: "operator-1", At: at, Reachable: true, Resources: resources, Exports: []ExportReceipt{{ResourceID: "object-1", ExpectedDigest: "sha256:x", ActualDigest: "sha256:x", SchemaVersion: "provider-export-v1", Verified: true}}, Revocations: []RevocationReceipt{{ResourceID: "webhook-1", Kind: Webhook, At: at, Success: true}, {ResourceID: "credential-1", Kind: Credential, At: at, Success: true}}, Operations: []Operation{{ID: "op-1", State: OperationComplete, Disposition: "CLOSED"}}, HoldResourceIDs: []string{"object-1"}}
}

func TestProviderExitEnumeratesExportsRevokesAndReconcilesEveryRemoteResource(t *testing.T) {
	plan, err := Reconcile(validProviderRequest())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != StatusCertifiable || len(plan.Blockers) != 0 {
		t.Fatalf("plan=%+v", plan)
	}
	if !strings.Contains(plan.Explain(), "CERTIFIABLE") {
		t.Fatalf("explanation=%q", plan.Explain())
	}
}

func TestTodo_PROVIDER_003_Property(t *testing.T) {
	plan, err := Reconcile(validProviderRequest())
	if err != nil || plan.Digest == "" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}
func TestTodo_PROVIDER_003_Golden(t *testing.T) {
	first, _ := Reconcile(validProviderRequest())
	second, _ := Reconcile(validProviderRequest())
	if first.Digest != second.Digest {
		t.Fatalf("digest changed: %s != %s", first.Digest, second.Digest)
	}
}
func TestTodo_PROVIDER_003_Integration(t *testing.T) {
	req := validProviderRequest()
	req.Resources[0].Enumerated = false
	plan, _ := Reconcile(req)
	if !hasBlocker(plan.Blockers, "RESOURCE_UNENUMERATED") {
		t.Fatalf("blockers=%+v", plan.Blockers)
	}
}
func TestTodo_PROVIDER_003_Fault(t *testing.T) {
	req := validProviderRequest()
	req.Reachable = false
	plan, _ := Reconcile(req)
	if !hasBlocker(plan.Blockers, "REMOTE_STATE_UNKNOWN") || !hasObligation(plan.Obligations, "MANUAL_PROVIDER_RECONCILIATION") {
		t.Fatalf("plan=%+v", plan)
	}
}
func TestTodo_PROVIDER_003_Security(t *testing.T) {
	req := validProviderRequest()
	req.Resources[3].ActiveAuthority = true
	req.Revocations = nil
	plan, _ := Reconcile(req)
	if !hasBlocker(plan.Blockers, "ACTIVE_AUTHORITY") {
		t.Fatalf("blockers=%+v", plan.Blockers)
	}
}
func TestTodo_PROVIDER_003_Conformance(t *testing.T) {
	plan, _ := Reconcile(validProviderRequest())
	if plan.Status != StatusCertifiable {
		t.Fatalf("valid plan became blocked: %+v", plan)
	}
}
func TestTodo_PROVIDER_003_Recovery(t *testing.T) {
	req := validProviderRequest()
	req.Operations = []Operation{{ID: "op-ambiguous", State: OperationAmbiguous, Disposition: "OBSERVE"}}
	plan, _ := Reconcile(req)
	if !hasBlocker(plan.Blockers, "AMBIGUOUS_OPERATION") {
		t.Fatalf("blockers=%+v", plan.Blockers)
	}
}
func TestTodo_PROVIDER_003_Mutation(t *testing.T) {
	first, _ := Reconcile(validProviderRequest())
	req := validProviderRequest()
	req.Resources[0].ID = "changed"
	second, _ := Reconcile(req)
	if first.Digest == second.Digest {
		t.Fatal("digest did not bind resource identity")
	}
}
func TestVersionAndExplain(t *testing.T) {
	if Version() != 1 || Explain() == "" {
		t.Fatal("contract symbols are not usable")
	}
}

func hasBlocker(blockers []Blocker, code string) bool {
	for _, blocker := range blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}
func hasObligation(obligations []Obligation, code string) bool {
	for _, obligation := range obligations {
		if obligation.Code == code {
			return true
		}
	}
	return false
}
