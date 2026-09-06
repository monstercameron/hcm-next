package advisory

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/operations/incidentstate"
)

func advisoryIncident(t *testing.T, state incidentstate.State) incidentstate.Incident {
	t.Helper()
	return incidentstate.Incident{ID: "inc-1", TenantID: "tenant-a", State: state, Version: 4, Affected: incidentstate.AffectedSet{Known: true, Verified: true, Facts: []incidentstate.AffectedFact{{TenantID: "tenant-a", Kind: "capability", Value: "promotion"}, {TenantID: "tenant-b", Kind: "capability", Value: "other"}, {TenantID: "tenant-a", Kind: "worker", Value: "worker-secret"}}}}
}

func advisoryRequest(t *testing.T, state incidentstate.State) Request {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	return Request{ID: "adv-1", PolicyVersion: "v1", Incident: advisoryIncident(t, state), Audience: Audience{TenantID: "tenant-a", Role: "customer-admin", Authorized: true}, Delivery: DeliveryEvidence{ReceiptID: "delivery-1", Channel: "secure-inbox", Status: "delivered", At: now}, PublishedAt: now}
}

func TestTodo_OPS_005(t *testing.T) {
	p := NewPublisher()
	a, err := p.Publish(advisoryRequest(t, incidentstate.Resolved))
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != StatusMonitoring {
		t.Fatalf("unreviewed resolution must remain monitoring: %+v", a)
	}
	request := advisoryRequest(t, incidentstate.Reviewed)
	request.ID = "adv-2"
	request.Incident.Version = 5
	a, err = p.Publish(request)
	if err != nil || a.Status != StatusResolved {
		t.Fatalf("reviewed advisory=%+v err=%v", a, err)
	}
	if len(p.History("tenant-a", "inc-1")) != 2 {
		t.Fatal("advisory history must retain versions")
	}
}

func TestTodo_OPS_005_Golden(t *testing.T) {
	a, err := NewPublisher().Publish(advisoryRequest(t, incidentstate.Mitigating))
	if err != nil {
		t.Fatal(err)
	}
	if a.Message == "" || a.Scope != "tenant:tenant-a" || len(a.Facts) != 1 || a.Facts[0].Value != "promotion" || a.Digest == "" {
		t.Fatalf("advisory=%+v", a)
	}
}

func TestTodo_OPS_005_Security(t *testing.T) {
	request := advisoryRequest(t, incidentstate.Reviewed)
	if _, err := NewPublisher().Publish(request); err != nil {
		t.Fatal(err)
	}
	request.Audience.TenantID = "tenant-b"
	if _, err := NewPublisher().Publish(request); err != ErrUnauthorized {
		t.Fatalf("cross-tenant audience error=%v", err)
	}
	request = advisoryRequest(t, incidentstate.Reviewed)
	request.Incident.Affected.Facts = append(request.Incident.Affected.Facts, incidentstate.AffectedFact{TenantID: "tenant-a", Kind: "system", Value: "database.internal"})
	a, err := NewPublisher().Publish(request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(a.Message, "database.internal") || strings.Contains(a.Scope, "service") {
		t.Fatal("internal forensic detail escaped advisory")
	}
}
