package observe_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/fakeincumbent"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
)

const testTenant = "5e3f1c2b-0000-4000-8000-000000000001"

var (
	publishedAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	draftedAt   = time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	// freshnessBudget is generous enough that the fixture watermarks - which
	// are historical business dates - would be stale under a realistic budget.
	// Tests that care about staleness set their own.
	freshnessBudget = 100 * 365 * 24 * time.Hour
)

func evidence(reason string, offset time.Duration) connectivity.TransitionEvidence {
	return connectivity.TransitionEvidence{
		Reason:      reason,
		ActorRef:    "user:ops@harborcare",
		EvidenceRef: "evd:" + reason,
		OccurredAt:  draftedAt.Add(offset),
	}
}

// activeConnection publishes the fake incumbent's definition and walks a
// connection to ACTIVE through the legal lifecycle path.
func activeConnection(t *testing.T) *connectivity.ConnectorConnection {
	t.Helper()
	registry := connectivity.NewRegistry()
	pub, err := registry.Publish(fakeincumbent.DefaultDefinition(), connectivity.PublicationMeta{
		PublishedBy: "user:platform@hcmnext", PublishedAt: publishedAt,
	})
	if err != nil {
		t.Fatalf("publish definition: %v", err)
	}
	credential, err := connectivity.ParseCredentialRef("secretref://harborcare/workday/client")
	if err != nil {
		t.Fatalf("credential: %v", err)
	}
	conn, err := connectivity.NewConnection(pub, connectivity.ConnectionSpec{
		ConnectionID:     fakeincumbent.DefaultDescriptor().ConnectionID,
		TenantID:         testTenant,
		OrgID:            "org-harborcare-us",
		SystemID:         "sys-workday-prod",
		Environment:      connectivity.EnvironmentProduction,
		Residency:        "us-east",
		ConnectorID:      pub.Definition.ConnectorID,
		ConnectorVersion: pub.Definition.Version,
		AuthMode:         connectivity.AuthOAuth2ClientCredentials,
		CredentialRef:    credential,
		Scopes:           []string{"worker.read", "position.read", "compensation.read"},
		EndpointPolicy: connectivity.EndpointPolicy{
			AllowedHosts:  []string{"api.workday.example"},
			RequireTLS:    true,
			EgressProfile: "cell-egress/us-east",
		},
		Capabilities: connectivity.ReadCapabilities(connectivity.ObjectKinds()...),
		Bounds:       pub.Definition.Bounds,
		CreatedAt:    draftedAt,
	})
	if err != nil {
		t.Fatalf("new connection: %v", err)
	}
	for i, step := range []connectivity.LifecycleState{
		connectivity.StateValidating, connectivity.StateReady, connectivity.StateActive,
	} {
		if err := conn.Transition(step, evidence("enable", time.Duration(i+1)*time.Minute)); err != nil {
			t.Fatalf("transition to %s: %v", step, err)
		}
	}
	return conn
}

// fixedClock returns a clock advancing one minute per call, so checkpoint
// timestamps are deterministic and strictly increasing.
func fixedClock() func() time.Time {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	var tick int
	return func() time.Time {
		t := base.Add(time.Duration(tick) * time.Minute)
		tick++
		return t
	}
}

type harness struct {
	Incumbent *fakeincumbent.Incumbent
	Store     *observe.MemoryStore
	Runner    *observe.Runner
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	incumbent, err := fakeincumbent.New(fakeincumbent.Options{})
	if err != nil {
		t.Fatalf("new incumbent: %v", err)
	}
	store := observe.NewMemoryStore()
	return &harness{
		Incumbent: incumbent,
		Store:     store,
		Runner: &observe.Runner{
			Connector:       incumbent,
			Connection:      activeConnection(t),
			Observations:    store,
			Checkpoints:     store,
			FreshnessBudget: freshnessBudget,
			Now:             fixedClock(),
		},
	}
}

func request(object connectivity.ObjectKind) observe.RunRequest {
	return observe.RunRequest{
		RunID:    "run-" + string(object),
		TenantID: testTenant,
		Object:   object,
		Mode:     connectivity.ReadFull,
	}
}

// externalIDs replays a traversal and returns every observed record id in page
// order, decoded from the stored evidence rather than from the connector.
func observedPages(t *testing.T, store observe.ObservationStore, object connectivity.ObjectKind) []observe.Observation {
	t.Helper()
	pages, err := store.List(context.Background(), observe.Query{TenantID: testTenant, Object: object})
	if err != nil {
		t.Fatalf("list observations: %v", err)
	}
	return pages
}
