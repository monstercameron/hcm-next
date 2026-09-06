package subscription

import (
	"errors"
	"sync"
	"testing"
)

func subscriptionRequest(id, requester string) RevisionRequest {
	return RevisionRequest{
		SubscriptionID: id,
		Requester:      requester,
		Subscriber:     Subscriber{PartnerRef: "partner:acme"},
		EventKinds:     []EventKind{EventWorkerChanged},
		DeclaredFields: map[EventKind][]string{
			EventWorkerChanged: {"worker.status", "worker.id"},
		},
		Filter:              Filter{Predicates: []Predicate{{Field: "worker.status", Operator: OperatorEquals, Value: "ACTIVE"}}},
		DeliveryEndpointRef: "endpoint:webhook-1",
		DeliveryGuarantee:   GuaranteeAtLeastOnce,
		TenantScope:         "tenant-a",
	}
}

func TestTodo_SUB_001(t *testing.T) {
	draft, err := NewDraft(subscriptionRequest("sub-1", "requester-1"))
	if err != nil {
		t.Fatal(err)
	}
	active, err := draft.Activate("requester-2", "approver-1")
	if err != nil {
		t.Fatal(err)
	}
	if draft.State != StateDraft || draft.Revision != 1 || active.State != StateActive || active.Revision != 2 {
		t.Fatalf("draft=%+v active=%+v", draft, active)
	}
	if draft.Digest == "" || active.Digest == "" || draft.Digest == active.Digest {
		t.Fatalf("revision digests=%q/%q", draft.Digest, active.Digest)
	}
	if err := draft.Verify(); err != nil {
		t.Fatalf("draft changed during activation: %v", err)
	}
	if _, err := draft.Activate("requester-1", "requester-1"); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("self-approval error=%v", err)
	}
	paused, err := active.Pause("operator-1")
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := paused.Revoke("operator-2")
	if err != nil {
		t.Fatal(err)
	}
	if revoked.State != StateRevoked || revoked.Revision != 4 {
		t.Fatalf("revoked=%+v", revoked)
	}
	if _, err := revoked.Revise(revisionRequestFrom(revoked)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("revise revoked error=%v", err)
	}
}

func TestTodo_SUB_001_Golden(t *testing.T) {
	first, err := NewDraft(subscriptionRequest("sub-golden", "requester-1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewDraft(subscriptionRequest("sub-golden", "requester-1"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("same canonical revision changed digest: %q != %q", first.Digest, second.Digest)
	}
	if explanation := Explain(first); explanation.RevisionDigest != first.Digest || explanation.SubscriberKind != "PARTNER" {
		t.Fatalf("explanation=%+v", explanation)
	}
}

func TestTodo_SUB_001_Race(t *testing.T) {
	registry := NewRegistry()
	draft, err := NewDraft(subscriptionRequest("sub-race", "requester-1"))
	if err != nil {
		t.Fatal(err)
	}
	active, err := draft.Activate("requester-2", "approver-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Append(draft); err != nil {
		t.Fatal(err)
	}
	if err := registry.Append(active); err != nil {
		t.Fatal(err)
	}
	event := EventDigest{TenantScope: "tenant-a", Kind: EventWorkerChanged, Digest: "event-1", FieldDigests: map[string]string{"worker.status": DigestValue("ACTIVE")}}
	var wait sync.WaitGroup
	for i := 0; i < 16; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			matches, matchErr := registry.Match(event)
			if matchErr != nil || len(matches) != 1 || matches[0].Revision != 2 {
				t.Errorf("matches=%+v err=%v", matches, matchErr)
			}
		}()
	}
	wait.Wait()
}

func TestTodo_SUB_001_Integration(t *testing.T) {
	draft, err := NewDraft(subscriptionRequest("sub-match", "requester-1"))
	if err != nil {
		t.Fatal(err)
	}
	active, err := draft.Activate("requester-2", "approver-1")
	if err != nil {
		t.Fatal(err)
	}
	matching := EventDigest{TenantScope: "tenant-a", Kind: EventWorkerChanged, Digest: "sha-event-1", FieldDigests: map[string]string{"worker.status": DigestValue("ACTIVE")}}
	matches, err := MatchActive(matching, []EventSubscription{active})
	if err != nil || len(matches) != 1 || matches[0].SubscriptionID != "sub-match" {
		t.Fatalf("matching=%+v err=%v", matches, err)
	}
	nonmatching := matching
	nonmatching.FieldDigests = map[string]string{"worker.status": DigestValue("INACTIVE")}
	matches, err = MatchActive(nonmatching, []EventSubscription{active})
	if err != nil || len(matches) != 0 {
		t.Fatalf("nonmatching=%+v err=%v", matches, err)
	}
}

func TestTodo_SUB_001_Fault(t *testing.T) {
	badKind := subscriptionRequest("sub-bad-kind", "requester-1")
	badKind.EventKinds = []EventKind{"NOT_DECLARED"}
	if _, err := NewDraft(badKind); !errors.Is(err, ErrInvalidEventKind) {
		t.Fatalf("unknown event kind error=%v", err)
	}
	badField := subscriptionRequest("sub-bad-field", "requester-1")
	badField.Filter.Predicates[0].Field = "worker.secret"
	if _, err := NewDraft(badField); !errors.Is(err, ErrUndeclaredField) {
		t.Fatalf("undeclared field error=%v", err)
	}
	badEvent := EventDigest{TenantScope: "tenant-a", Kind: EventWorkerChanged, Digest: "event-1"}
	if _, err := MatchActive(badEvent, nil); err != nil {
		t.Fatal("payload-free event without filters should remain valid:", err)
	}
}

func revisionRequestFrom(s EventSubscription) RevisionRequest {
	return RevisionRequest{SubscriptionID: s.SubscriptionID, Requester: "requester", Subscriber: s.Subscriber, EventKinds: s.EventKinds, DeclaredFields: s.DeclaredFields, Filter: s.Filter, DeliveryEndpointRef: s.DeliveryEndpointRef, DeliveryGuarantee: s.DeliveryGuarantee, TenantScope: s.TenantScope, OrganizationScopeRef: s.OrganizationScopeRef, PopulationScopeRef: s.PopulationScopeRef}
}
