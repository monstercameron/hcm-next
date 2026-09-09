package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/opsmeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/incidentstate"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/securityevidence"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errIncidentAlertStub = errors.New("incident alert stub")

type incidentAlertTxStub struct{}

func (incidentAlertTxStub) Exec(context.Context, string, ...any) (int64, error) {
	return 0, errIncidentAlertStub
}
func (incidentAlertTxStub) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errIncidentAlertStub
}
func (incidentAlertTxStub) QueryRow(context.Context, string, ...any) dbport.Row {
	return incidentAlertRowStub{}
}
func (incidentAlertTxStub) Commit(context.Context) error   { return nil }
func (incidentAlertTxStub) Rollback(context.Context) error { return nil }

type incidentAlertRowStub struct{}

func (incidentAlertRowStub) Scan(...any) error { return errIncidentAlertStub }

func testRoutedAlert() securityevidence.RoutedAlert {
	evidence := sha256.Sum256([]byte("alert evidence"))
	a := securityevidence.RoutedAlert{Revision: 1, RuleID: "queue-lag", RuleVersion: 2, Sequence: securityevidence.SequenceRepeatedDLPRefusal, AlertName: "Queue lag", Route: securityevidence.RouteSecurityOnCall, WindowStart: time.Unix(10, 0), WindowEnd: time.Unix(20, 0), ObservedCount: 2, EvidenceDigest: hex.EncodeToString(evidence[:])}
	a.Digest = securityevidence.DigestOfAlert(a)
	return a
}

func testTrustedAlert(tenant uuid.UUID) TrustedRoutedAlert {
	return TrustedRoutedAlert{tenant: tenant, alert: testRoutedAlert()}
}

func testIncidentPrincipal(t testing.TB, subject string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session-incident", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(1000, 0), CredentialDigest: "credential-digest",
	})
	if err != nil {
		t.Fatalf("principal: %v", err)
	}
	return p
}

func incidentTenant(id uuid.UUID) IncidentTenantResolver {
	return IncidentTenantResolverFunc(func(context.Context, dbport.Querier, values.TenantId) (uuid.UUID, error) {
		return id, nil
	})
}

func TestTodo_OBS_007(t *testing.T) {
	tenant := uuid.New()
	policy := IncidentRoutePolicy{PrimaryOwner: "on-call", SecondaryRoute: "incident-review", StormLimit: 3, StormWindow: time.Hour}
	if _, err := RouteOwnedAlert(context.Background(), incidentAlertTxStub{}, testTrustedAlert(tenant), policy); !errors.Is(err, errIncidentAlertStub) {
		t.Fatalf("valid owned alert error = %v, want store sentinel", err)
	}
	if _, err := RouteOwnedAlert(context.Background(), incidentAlertTxStub{}, testTrustedAlert(tenant), IncidentRoutePolicy{SecondaryRoute: "incident-review", StormLimit: 3, StormWindow: time.Hour}); !errors.Is(err, incidentstate.ErrNoOwner) {
		t.Fatalf("missing owner error = %v", err)
	}
	if _, err := RouteOwnedAlert(context.Background(), incidentAlertTxStub{}, testTrustedAlert(tenant), IncidentRoutePolicy{PrimaryOwner: "on-call", StormLimit: 3, StormWindow: time.Hour}); !errors.Is(err, incidentstate.ErrNoRoute) {
		t.Fatalf("missing route error = %v", err)
	}
}

func TestTodo_OBS_007_Security(t *testing.T) {
	policy := IncidentRoutePolicy{PrimaryOwner: "on-call", SecondaryRoute: "fallback", StormLimit: 3, StormWindow: time.Hour}
	if _, err := DetectOwnedAlerts(uuid.New(), nil, securityevidence.Window{}); !errors.Is(err, ErrIncidentAlertInvalid) {
		t.Fatalf("nil detector error=%v", err)
	}
	port, err := securityevidence.NewMemoryPort()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := securityevidence.DefaultAlertRuleRegistry()
	if err != nil {
		t.Fatal(err)
	}
	detector, err := securityevidence.NewSequenceDetector(port, registry)
	if err != nil {
		t.Fatal(err)
	}
	alerts, err := DetectOwnedAlerts(uuid.New(), detector, securityevidence.Window{Start: time.Unix(1, 0), End: time.Unix(2, 0)})
	if err != nil || len(alerts) != 0 {
		t.Fatalf("empty detector alerts=%v error=%v", alerts, err)
	}
	if _, err := RouteOwnedAlert(context.Background(), incidentAlertTxStub{}, TrustedRoutedAlert{}, policy); !errors.Is(err, ErrIncidentAlertInvalid) {
		t.Fatalf("untrusted alert error=%v", err)
	}
	bad := testTrustedAlert(uuid.New())
	bad.alert.Digest = "forged"
	if _, err := RouteOwnedAlert(context.Background(), incidentAlertTxStub{}, bad, policy); !errors.Is(err, ErrIncidentAlertTampered) {
		t.Fatalf("tampered alert error=%v", err)
	}
	ack := IncidentAcknowledgement{TenantID: uuid.New(), IncidentID: uuid.New(), Receipt: "receipt", EvidenceTime: time.Now()}
	if err := AcknowledgeIncident(context.Background(), incidentAlertTxStub{}, incidentTenant(ack.TenantID), ack); !errors.Is(err, ErrIncidentAckInvalid) {
		t.Fatalf("unauthenticated acknowledgement error=%v", err)
	}
	ctx := trust.WithPrincipal(context.Background(), testIncidentPrincipal(t, "on-call"))
	if err := AcknowledgeIncident(ctx, incidentAlertTxStub{}, incidentTenant(uuid.New()), ack); !errors.Is(err, ErrIncidentAckInvalid) {
		t.Fatalf("cross-tenant same-subject acknowledgement error=%v", err)
	}
	if err := AcknowledgeIncident(ctx, incidentAlertTxStub{}, incidentTenant(ack.TenantID), ack); !errors.Is(err, errIncidentAlertStub) {
		t.Fatalf("authenticated acknowledgement error=%v", err)
	}
}

func TestTodo_OBS_007_Race(t *testing.T) {
	alert := testTrustedAlert(uuid.New())
	policy := IncidentRoutePolicy{PrimaryOwner: "on-call", SecondaryRoute: "fallback", StormLimit: 3, StormWindow: time.Hour}
	const workers = 24
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := RouteOwnedAlert(context.Background(), incidentAlertTxStub{}, alert, policy)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if !errors.Is(err, errIncidentAlertStub) {
			t.Errorf("concurrent route error=%v", err)
		}
	}
}

func TestTodo_OBS_007_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	pool, err := pgxadapter.NewPool(ctx, db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1, 'tenant-a', 'cell-local', 'Incident alert test', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenant)
	policy := IncidentRoutePolicy{PrimaryOwner: "on-call", SecondaryRoute: "fallback", StormLimit: 3, StormWindow: time.Hour}

	routeTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin route: %v", err)
	}
	if err := tenancy.WithTenant(ctx, routeTx, tenant); err != nil {
		t.Fatalf("scope route: %v", err)
	}
	routed, err := RouteOwnedAlert(ctx, routeTx, testTrustedAlert(tenant), policy)
	if err != nil {
		_ = routeTx.Rollback(ctx)
		t.Fatalf("route: %v", err)
	}
	if !routed.Created || routed.Incident.TenantID != tenant || routed.Incident.Status != "OPEN" {
		t.Fatalf("routed incident = %+v", routed)
	}
	if err := routeTx.Commit(ctx); err != nil {
		t.Fatalf("commit route: %v", err)
	}

	ackTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin acknowledgement: %v", err)
	}
	if err := tenancy.WithTenant(ctx, ackTx, tenant); err != nil {
		t.Fatalf("scope acknowledgement: %v", err)
	}
	ackCtx := trust.WithPrincipal(ctx, testIncidentPrincipal(t, "on-call"))
	resolver := IncidentTenantResolverFunc(func(ctx context.Context, q dbport.Querier, key values.TenantId) (uuid.UUID, error) {
		var resolved uuid.UUID
		err := q.QueryRow(ctx, `SELECT tenant_id FROM tenant WHERE tenant_key=$1`, key.String()).Scan(&resolved)
		return resolved, err
	})
	if err := AcknowledgeIncident(ackCtx, ackTx, resolver, IncidentAcknowledgement{TenantID: tenant, IncidentID: routed.Incident.IncidentID, Receipt: "signed-receipt-secret", EvidenceTime: time.Unix(30, 0)}); err != nil {
		_ = ackTx.Rollback(ctx)
		t.Fatalf("acknowledge: %v", err)
	}
	if err := ackTx.Commit(ctx); err != nil {
		t.Fatalf("commit acknowledgement: %v", err)
	}

	readTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin read: %v", err)
	}
	defer readTx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, readTx, tenant); err != nil {
		t.Fatalf("scope read: %v", err)
	}
	var scope []byte
	if err := readTx.QueryRow(ctx, `SELECT scope FROM operational_incident WHERE tenant_id=$1 AND incident_id=$2`, tenant, routed.Incident.IncidentID).Scan(&scope); err != nil {
		t.Fatalf("read incident: %v", err)
	}
	wantDigest := sha256.Sum256([]byte("signed-receipt-secret"))
	var metadata map[string]any
	if err := json.Unmarshal(scope, &metadata); err != nil {
		t.Fatalf("decode acknowledged scope: %v", err)
	}
	if metadata["acknowledged"] != true || metadata["acknowledged_by"] != "on-call" || metadata["acknowledgement_evidence"] != hex.EncodeToString(wantDigest[:]) {
		t.Fatalf("acknowledged scope is not bounded: %s", scope)
	}
	if err := readTx.Rollback(ctx); err != nil {
		t.Fatalf("close read: %v", err)
	}

	replayTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin replay: %v", err)
	}
	if err := tenancy.WithTenant(ctx, replayTx, tenant); err != nil {
		t.Fatalf("scope replay: %v", err)
	}
	replayed, err := RouteOwnedAlert(ctx, replayTx, testTrustedAlert(tenant), policy)
	if err != nil {
		_ = replayTx.Rollback(ctx)
		t.Fatalf("replay: %v", err)
	}
	if replayed.Created || replayed.Incident.IncidentID != routed.Incident.IncidentID {
		t.Fatalf("replay = %+v, want existing incident", replayed)
	}
	if err := replayTx.Commit(ctx); err != nil {
		t.Fatalf("commit replay: %v", err)
	}

	stormTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin storm: %v", err)
	}
	if err := tenancy.WithTenant(ctx, stormTx, tenant); err != nil {
		t.Fatalf("scope storm: %v", err)
	}
	storm := testTrustedAlert(tenant)
	storm.alert.RuleID = "different-rule"
	storm.alert.ObservedCount = 1
	storm.alert.Digest = securityevidence.DigestOfAlert(storm.alert)
	stormPolicy := policy
	stormPolicy.StormLimit = 1
	if _, err := RouteOwnedAlert(ctx, stormTx, storm, stormPolicy); !errors.Is(err, opsmeta.ErrAlertStorm) {
		_ = stormTx.Rollback(ctx)
		t.Fatalf("storm error=%v", err)
	}
	if err := stormTx.Rollback(ctx); err != nil {
		t.Fatalf("rollback storm: %v", err)
	}
}
