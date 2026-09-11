package onboarding_test

import (
	"crypto/ed25519"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/fakeincumbent"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/onboarding"
)

const testTenant = "5e3f1c2b-0000-4000-8000-000000000001"

var (
	publishedAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	draftedAt   = time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	manifestAt  = time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC)
	// freshnessBudget is generous enough that the fixture watermarks, which
	// are historical business dates, would be stale under a realistic
	// budget. Tests that care about staleness set their own.
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

// fixedClock returns a clock advancing one minute per call, so checkpoint and
// budget timestamps are deterministic and strictly increasing.
//
// The tick counter is atomic because TestTodo_ONBOARD_001_Race drives one
// Preflight from 32 goroutines at once, and Preflight.now() calls this clock
// on every one of them. The previous plain `int` was a genuine data race that
// `go test -race` on Linux CI reported (a read-modify-write from concurrent
// goroutines), failing every test in this package, not just the racing one.
// Add returns each caller a distinct tick, which is all the callers need;
// which goroutine gets which minute was never deterministic anyway.
func fixedClock() func() time.Time {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	var tick atomic.Int64
	return func() time.Time {
		return base.Add(time.Duration(tick.Add(1)-1) * time.Minute)
	}
}

// testKeyPair is a fixed ed25519 key pair, deterministic across runs so a
// golden test's signature bytes never change unless the signed content does.
func testKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	return priv.Public().(ed25519.PublicKey), priv
}

// harness wires an incumbent, its published/active connection, and an
// in-memory observation and checkpoint store, all sharing one deterministic
// clock.
type harness struct {
	Incumbent  *fakeincumbent.Incumbent
	Connection *connectivity.ConnectorConnection
	Store      *observe.MemoryStore
	Clock      func() time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	incumbent, err := fakeincumbent.New(fakeincumbent.Options{})
	if err != nil {
		t.Fatalf("new incumbent: %v", err)
	}
	return &harness{
		Incumbent:  incumbent,
		Connection: activeConnection(t),
		Store:      observe.NewMemoryStore(),
		Clock:      fixedClock(),
	}
}

func (h *harness) Extractor() onboarding.Extractor {
	return onboarding.Extractor{
		Connector:       h.Incumbent,
		Connection:      h.Connection,
		Observations:    h.Store,
		Checkpoints:     h.Store,
		FreshnessBudget: freshnessBudget,
		Now:             h.Clock,
	}
}

// validManifest returns a manifest that Preflight accepts against a freshly
// built harness: it names the harness's connector, connection and tenant
// exactly, pins every object to the incumbent's un-drifted schema versions,
// and carries a generous budget.
func validManifest(h *harness) onboarding.OnboardingManifest {
	return onboarding.OnboardingManifest{
		ManifestID:         "manifest-harborcare-2026-09",
		TenantID:           testTenant,
		OwnerRef:           "user:ops@harborcare",
		SourceAuthorityRef: h.Incumbent.Descriptor().AuthorityRef,
		Classification:     "PII,COMPENSATION",
		Residency:          "us-east",
		RetentionPolicy:    "retention.onboarding.default",
		RollbackPolicy:     "rollback.onboarding.default",
		IdempotencyKey:     "idem-harborcare-2026-09",
		ConnectorID:        h.Incumbent.Descriptor().ConnectorID,
		ConnectorVersion:   h.Incumbent.Descriptor().Version,
		ConnectionID:       h.Connection.ID(),
		Objects:            connectivity.ObjectKinds(),
		SchemaVersionPins: map[connectivity.ObjectKind]string{
			connectivity.ObjectWorker:       fakeincumbent.SchemaWorkerV1,
			connectivity.ObjectPosition:     fakeincumbent.SchemaPositionV1,
			connectivity.ObjectCompensation: fakeincumbent.SchemaCompensationV1,
		},
		Budget: onboarding.Budget{
			MaxPages: 1024, MaxRecords: 65536, MaxBytes: 16 << 20, MaxWallTime: time.Hour,
		},
		CreatedAt: manifestAt,
		CreatedBy: "user:ops@harborcare",
	}
}

// narrowToObjects restricts m to exactly the given objects, keeping every
// per-object map (schema pins, field allow list, expected population)
// consistent with the narrowed object list so the result still validates.
func narrowToObjects(m *onboarding.OnboardingManifest, objects ...connectivity.ObjectKind) {
	want := make(map[connectivity.ObjectKind]bool, len(objects))
	for _, o := range objects {
		want[o] = true
	}
	m.Objects = objects
	pins := map[connectivity.ObjectKind]string{}
	for o, v := range m.SchemaVersionPins {
		if want[o] {
			pins[o] = v
		}
	}
	m.SchemaVersionPins = pins
	if len(m.FieldAllowList) > 0 {
		allow := map[connectivity.ObjectKind][]string{}
		for o, v := range m.FieldAllowList {
			if want[o] {
				allow[o] = v
			}
		}
		m.FieldAllowList = allow
	}
	if len(m.ExpectedPopulation) > 0 {
		pop := map[connectivity.ObjectKind]int64{}
		for o, v := range m.ExpectedPopulation {
			if want[o] {
				pop[o] = v
			}
		}
		m.ExpectedPopulation = pop
	}
}

func mustSign(t *testing.T, m onboarding.OnboardingManifest, priv ed25519.PrivateKey) onboarding.SignedManifest {
	t.Helper()
	sm, err := onboarding.SignManifest(m, "key-2026-09", priv)
	if err != nil {
		t.Fatalf("sign manifest: %v", err)
	}
	return sm
}
