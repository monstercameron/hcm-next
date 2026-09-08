package incidentstate

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func alertFixture() Alert {
	return Alert{TenantID: "tenant-a", RuleID: "queue.lag", RuleVersion: 2, Fingerprint: "fp-1", Severity: "SEV2", Service: "workflow", Capability: "resume", ObservedAt: time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC), EvidenceDigest: "digest-1", Count: 1}
}

func TestTodo_OBS_007(t *testing.T) {
	got, err := Route(alertFixture(), Routes{Primary: "on-call", Secondary: "incident-review"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got.IncidentKey == "" || got.CorrelationKey == "" || !got.AcknowledgementRequired || !strings.Contains(got.CustomerSafeEvidence, "status=OPEN") {
		t.Fatalf("decision=%+v", got)
	}
}

func TestTodo_OBS_007_IdentityTupleCollision(t *testing.T) {
	routes := Routes{Primary: "on-call", Secondary: "backup"}
	a := alertFixture()
	a.RuleID, a.RuleVersion, a.Fingerprint = "a", 1, "2:b"
	b := a
	b.RuleID, b.RuleVersion, b.Fingerprint = "a:1", 2, "b"
	first, err := Route(a, routes, 3)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Route(b, routes, 3)
	if err != nil {
		t.Fatal(err)
	}
	if first.IncidentKey == second.IncidentKey || first.CorrelationKey == second.CorrelationKey {
		t.Fatal("distinct rule/version/fingerprint tuples collided")
	}
	b = a
	b.TenantID = "tenant-b"
	otherTenant, err := Route(b, routes, 3)
	if err != nil {
		t.Fatal(err)
	}
	if first.CorrelationKey == otherTenant.CorrelationKey {
		t.Fatal("correlation crossed tenant boundary")
	}
}

func TestTodo_OBS_007_Golden(t *testing.T) {
	got, err := Route(alertFixture(), Routes{Primary: "on-call", Secondary: "incident-review"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	const wantIncidentKey = "alert:v1:1a3c816ca0ccdcfbbea7b8a732d1d161a4ff7036c3f22fc18038c2d17bf5aaee"
	const wantCorrelationKey = "be3f4f7dbf0ca82087c79bde38392102e026ee7fa385ad394da5cfb1de084c37"
	if got.IncidentKey != wantIncidentKey || got.CorrelationKey != wantCorrelationKey {
		t.Fatalf("identity=(%q, %q), want (%q, %q)", got.IncidentKey, got.CorrelationKey, wantIncidentKey, wantCorrelationKey)
	}
}

func TestTodo_OBS_007_Race(t *testing.T) {
	a := alertFixture()
	routes := Routes{Primary: "on-call", Secondary: "backup"}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := Route(a, routes, 3)
			if err != nil || got.IncidentKey == "" {
				t.Errorf("route=%+v err=%v", got, err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_OBS_007_Security(t *testing.T) {
	a := alertFixture()
	a.Fingerprint = "worker@example.com payload=secret"
	if _, err := Route(a, Routes{Primary: "on-call", Secondary: "backup"}, 3); err == nil {
		t.Fatal("unbounded alert input accepted")
	}
	a = alertFixture()
	a.Severity = "SEV2;tenant=tenant-a"
	if _, err := Route(a, Routes{Primary: "on-call", Secondary: "backup"}, 3); !errors.Is(err, ErrAlertInvalid) {
		t.Fatalf("customer-evidence injection err=%v", err)
	}
	for _, tc := range []struct {
		name   string
		routes Routes
		count  int
		want   error
	}{
		{"owner", Routes{Secondary: "backup"}, 1, ErrNoOwner}, {"route", Routes{Primary: "on-call"}, 1, ErrNoRoute}, {"same route", Routes{Primary: "on-call", Secondary: "on-call"}, 1, ErrNoRoute}, {"storm", Routes{Primary: "on-call", Secondary: "backup"}, 4, ErrAlertStorm},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := alertFixture()
			if tc.name == "storm" {
				a.Count = 4
			}
			_, err := Route(a, tc.routes, 3)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
}
