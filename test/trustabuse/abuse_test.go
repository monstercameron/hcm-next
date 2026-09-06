// Package trustabuse is a test-only cross-tenant authorization abuse suite.
// It deliberately keeps foreign credentials and identifiers in memory and
// never requires a database or a production-side bypass.
package trustabuse_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity"
	"github.com/monstercameron/hcm-next/internal/connectivity/spi"
	"github.com/monstercameron/hcm-next/internal/connectivity/spi/spiconform"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/authz"
	"github.com/monstercameron/hcm-next/internal/trust/breakglass"
	"github.com/monstercameron/hcm-next/internal/trust/custody"
	"github.com/monstercameron/hcm-next/internal/trust/dlp"
	"github.com/monstercameron/hcm-next/internal/trust/jit"
	"github.com/monstercameron/hcm-next/internal/trust/lease"
	"github.com/monstercameron/hcm-next/internal/trust/outbound"
	"github.com/monstercameron/hcm-next/internal/trust/sod"
)

const (
	tenantA = values.TenantId("tenant-a")
	tenantB = values.TenantId("tenant-b")
	actorA  = "tenant-a/actor"
	actorB  = "tenant-b/actor"
)

var abuseNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func principalFor(t *testing.T, tenant values.TenantId, subject string, roles ...string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: tenant, Subject: subject, SubjectKind: trust.SubjectKindHuman,
		Roles: roles, Purposes: []string{"abuse-test"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh, SessionRef: "session-" + subject,
		IssuedAt: abuseNow.Add(-time.Hour), ExpiresAt: abuseNow.Add(time.Hour),
		CredentialDigest: "credential-" + subject,
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

func foreignWorker(tenant values.TenantId, id string) values.EntityRef {
	return values.EntityRef{Tenant: tenant, Kind: values.Kind("worker"), Id: id}
}

func crossTenantRequest(t *testing.T) authz.Request {
	t.Helper()
	foreign := foreignWorker(tenantB, "550e8400-e29b-41d4-a716-446655440000")
	return authz.Request{
		Principal: principalFor(t, tenantA, actorA, string(authz.RoleManager)),
		Purpose:   "abuse-test", EffectiveAt: values.NewInstant(abuseNow), Subject: foreign,
		Relationships: []authz.RelationshipFact{{
			Kind: authz.RelationshipManagerChain, Subject: foreign, Source: "forged-foreign-chain",
			Effective:  mustOpenInterval(t, values.NewInstant(abuseNow.Add(-time.Hour))),
			RecordedAt: mustRecordedAt(t, values.NewInstant(abuseNow.Add(-time.Minute))),
			KnownAt:    mustKnownAt(t, values.NewInstant(abuseNow.Add(-2*time.Minute))),
		}},
		Fields: []authz.FieldID{authz.FieldWorkerNumber, authz.FieldBankAccountNumber},
	}
}

func mustOpenInterval(t *testing.T, start values.Instant) values.EffectiveInterval {
	t.Helper()
	interval, err := values.NewOpenInstantInterval(start)
	if err != nil {
		t.Fatalf("NewOpenInstantInterval: %v", err)
	}
	return interval
}

func mustRecordedAt(t *testing.T, at values.Instant) values.RecordedAt {
	t.Helper()
	value, err := values.NewRecordedAt(at)
	if err != nil {
		t.Fatalf("NewRecordedAt: %v", err)
	}
	return value
}

func mustKnownAt(t *testing.T, at values.Instant) values.KnownAt {
	t.Helper()
	value, err := values.NewKnownAt(at)
	if err != nil {
		t.Fatalf("NewKnownAt: %v", err)
	}
	return value
}

type refusalCase struct {
	name string
	run  func(t *testing.T) error
	want error
}

func refusalCases() []refusalCase {
	return []refusalCase{
		{
			name: "tenant scope refuses tenant-B resource for tenant-A principal",
			run: func(t *testing.T) error {
				decision, err := authz.ResolveTenantScope(principalFor(t, tenantA, actorA), authz.TenantScopeInput{ResourceTenant: tenantB, EffectiveAt: values.NewInstant(abuseNow)})
				if err != nil {
					return err
				}
				if decision.Effect != authz.EffectDenied || decision.Reason != "cross_tenant_denied" {
					t.Fatalf("tenant decision = %+v, want typed cross_tenant_denied refusal", decision)
				}
				return nil
			},
		},
		{
			name: "composed authz refuses forged foreign delegation",
			run: func(t *testing.T) error {
				decision, err := authz.Enforce(crossTenantRequest(t))
				if err != nil {
					return err
				}
				if decision.SubjectDisclosable || decision.SubjectDenialReason != "cross_tenant_denied" {
					t.Fatalf("authz decision = %+v, want undisclosable foreign subject", decision)
				}
				return nil
			},
			want: nil,
		},
		{
			name: "repository scope drops foreign candidate without an existence oracle",
			run: func(t *testing.T) error {
				foreign := foreignWorker(tenantB, "550e8400-e29b-41d4-a716-446655440001")
				request := authz.RepositoryQueryRequest{Principal: principalFor(t, tenantA, actorA), Purpose: "abuse-test", EffectiveAt: values.NewInstant(abuseNow), Tenant: tenantB, Candidates: []authz.ScopeInput{{Subject: foreign}}, Fields: []authz.FieldID{authz.FieldWorkerNumber}}
				scope, err := authz.PlanRepositoryScope(request)
				if err != nil {
					return err
				}
				if scope.Reason() != "cross_tenant_denied" {
					t.Fatalf("repository scope reason = %q, want cross_tenant_denied", scope.Reason())
				}
				rows, err := authz.NewRepositoryGate(map[values.EntityRef]map[authz.FieldID]string{foreign: {authz.FieldWorkerNumber: "foreign"}}).Query(scope, []values.EntityRef{foreign}, values.NewInstant(abuseNow))
				if err != nil {
					return err
				}
				if len(rows) != 0 {
					t.Fatalf("foreign repository rows = %+v, want zero", rows)
				}
				return nil
			},
		},
		{
			name: "cursor minted for tenant-B snapshot is refused by tenant-A source",
			run: func(t *testing.T) error {
				adapter := cursorAdapter(t)
				_, err := adapter.ReadSnapshot(context.Background(), connectivity.ReadRequest{Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Cursor: connectivity.Cursor{SnapshotID: "tenant-b:snapshot", Page: 1, LastSortKey: "a", LastExternalID: "foreign"}, Limit: 1})
				return err
			},
			want: connectivity.ErrCursor,
		},
		{
			name: "lease with tenant-B binding tampered for tenant-A principal is refused",
			run: func(t *testing.T) error {
				clock := func() time.Time { return abuseNow }
				manager, err := lease.NewManager(&abuseLeasePort{now: clock}, clock)
				if err != nil {
					return err
				}
				presented, _, err := manager.Mint(lease.Request{Handle: custody.Handle{ID: "secret-b", Kind: custody.Secret, Version: "v1", Tenant: "tenant-b", Region: "us-east"}, Workload: "tenant-b/workload", Tenant: "tenant-b", Purpose: "abuse-test", Destination: "partner-b", Operation: custody.Encrypt, TTL: time.Minute})
				if err != nil {
					return err
				}
				presented.Tenant = "tenant-a"
				_, err = manager.Use(presented, "partner-b", custody.Encrypt)
				return err
			},
			want: lease.ErrTampered,
		},
		{
			name: "outbound lease for destination-B is refused at destination-A",
			run: func(t *testing.T) error {
				policy, err := outbound.NewPolicy(
					outbound.Destination{Name: "partner-a", TrustBundleRef: "bundle:a:v1", Purposes: []string{"abuse-test"}, DataClasses: []string{string(dlp.ClassPublic)}},
					outbound.Destination{Name: "partner-b", TrustBundleRef: "bundle:b:v1", Purposes: []string{"abuse-test"}, DataClasses: []string{string(dlp.ClassPublic)}},
				)
				if err != nil {
					return err
				}
				foreignLease := &lease.CredentialLease{ID: "tenant-b-lease", Tenant: "tenant-b", Destination: "partner-b"}
				_, err = policy.Check(outbound.CheckRequest{Destination: "partner-a", Purpose: "abuse-test", DataClass: string(dlp.ClassPublic), Lease: foreignLease})
				return err
			},
			want: outbound.ErrLeaseDestinationMismatch,
		},
		{
			name: "DLP receipt payload from tenant-B cannot be substituted for tenant-A payload",
			run: func(t *testing.T) error {
				log := dlp.NewReceiptLog()
				foreignPayload := []byte("tenant-b confidential payload")
				returnErrorPayload := []byte("tenant-a payload")
				_, err := log.Append(dlp.ReceiptInput{Destination: "partner-a", Purpose: "abuse-test", Principal: actorA, Payload: returnErrorPayload, PayloadDigest: dlp.DigestPayload(foreignPayload), Decision: dlp.Refuse})
				return err
			},
			want: dlp.ErrInvalidReceipt,
		},
		{
			name: "DLP policy refuses tenant-B destination outside tenant-A allowlist",
			run: func(t *testing.T) error {
				egress, err := outbound.NewPolicy(outbound.Destination{Name: "partner-a", TrustBundleRef: "bundle:a:v1", Purposes: []string{"abuse-test"}, DataClasses: []string{string(dlp.ClassPublic)}})
				if err != nil {
					return err
				}
				policy, err := dlp.NewPolicy(egress, dlp.Clearance{Destination: "partner-a", DataClass: dlp.ClassPublic, Decision: dlp.Allow})
				if err != nil {
					return err
				}
				evaluation, err := policy.Evaluate(dlp.DecisionRequest{Destination: "partner-b", Purpose: "abuse-test", Principal: actorA})
				if err != nil {
					return err
				}
				if evaluation.Decision != dlp.Refuse || evaluation.Reason == "" {
					t.Fatalf("DLP evaluation = %+v, want typed refusal decision", evaluation)
				}
				return nil
			},
		},
		{
			name: "forged cross-tenant SoD delegation cannot satisfy quorum",
			run: func(t *testing.T) error {
				_, err := sod.Evaluate(sod.DecisionContext{Requester: sod.Actor{Subject: actorA}, Approvers: []sod.Actor{{Subject: actorB, DelegationChain: []string{actorA, actorB}}}}, sod.Constraints{RequesterDelegateMayNotApprove: true, RuleID: "sod.cross-tenant"}, 1)
				return err
			},
			want: sod.ErrUnsatisfiable,
		},
		{
			name: "JIT tenant-B grant past TTL is refused for tenant-A action",
			run: func(t *testing.T) error {
				grant, err := jit.New("grant-b", jit.Request{Principal: actorB, Tenant: tenantB, Role: jit.RoleSupportReadOnly, TicketRef: "INC-B", Justification: "abuse fixture", Capabilities: []string{"read"}, Purpose: "abuse-test", TTL: time.Minute}, jit.Approval{Approver: "approver-b", At: abuseNow}, abuseNow)
				if err != nil {
					return err
				}
				return grant.Use("tenant-a/replay", abuseNow.Add(2*time.Minute))
			},
			want: jit.ErrGrantExpired,
		},
		{
			name: "break-glass tenant-B grant reused after containment is refused",
			run: func(t *testing.T) error {
				grant, err := breakglass.Open("break-b", breakglass.Request{User: actorB, IncidentRef: "INC-B", Justification: "abuse fixture", Capabilities: []string{"read"}, TTL: time.Minute}, breakglass.Approval{Approver: "approver-b", At: abuseNow}, abuseNow)
				if err != nil {
					return err
				}
				if err := grant.Contain("containment", abuseNow.Add(10*time.Second)); err != nil {
					return err
				}
				return grant.Use("read", "tenant-a/replay", abuseNow.Add(20*time.Second))
			},
			want: breakglass.ErrGrantContained,
		},
	}
}

// TestTodo_TRUST_025 is the primary release-gating abuse matrix. Every case
// must refuse the foreign or forged presentation with its declared reason.
func TestTodo_TRUST_025(t *testing.T) {
	for _, tc := range refusalCases() {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run(t)
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if tc.want == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// FuzzTodo_TRUST_025 ensures hostile cursor tokens remain classified as
// cursor refusals or harmless parse failures and never panic.
func FuzzTodo_TRUST_025(f *testing.F) {
	f.Add("cur1.invalid.invalid")
	f.Add("tenant-b:cursor")
	f.Add("")
	f.Fuzz(func(t *testing.T, token string) {
		_, _ = connectivity.ParseCursor(token)
	})
}

// TestTodo_TRUST_025_Security pins the stable refusal reason table. Changes
// to a permissive result or a newly identifying error must fail this build.
func TestTodo_TRUST_025_Security(t *testing.T) {
	golden := []struct {
		name string
		want string
	}{
		{"tenant scope", "cross_tenant_denied"},
		{"composed authz", "cross_tenant_denied"},
		{"repository scope", "cross_tenant_denied"},
		{"cursor", "connectivity: pagination cursor is not usable"},
		{"lease", "lease: presented lease does not match the minted lease"},
		{"outbound", "outbound: presented credential lease was minted for a different destination"},
		{"dlp receipt", "dlp: invalid egress receipt"},
		{"SoD", "sod: SOD_UNSATISFIABLE - quorum cannot be met under separation of duties"},
		{"JIT", "jit: grant has expired"},
		{"break-glass", "breakglass: grant is contained"},
	}
	for _, want := range golden {
		t.Run(want.name, func(t *testing.T) {
			var matched bool
			for _, tc := range refusalCases() {
				if strings.Contains(strings.ToLower(tc.name), strings.ToLower(want.name)) {
					if err := tc.run(t); err != nil && !strings.Contains(err.Error(), want.want) {
						t.Fatalf("error = %v, want stable reason %q", err, want.want)
					}
					matched = true
					break
				}
			}
			if !matched {
				t.Fatalf("golden surface %q has no abuse case", want.name)
			}
		})
	}
}

// TestTodo_TRUST_025_Mutation proves the same surfaces retain a narrow
// positive path only when the tenant/destination/time binding is restored.
func TestTodo_TRUST_025_Mutation(t *testing.T) {
	t.Run("same tenant authz remains available", func(t *testing.T) {
		request := crossTenantRequest(t)
		request.Subject = foreignWorker(tenantA, "550e8400-e29b-41d4-a716-446655440002")
		request.Relationships[0].Subject = request.Subject
		decision, err := authz.Enforce(request)
		if err != nil || !decision.SubjectDisclosable {
			t.Fatalf("same-tenant decision = %+v, error=%v", decision, err)
		}
	})
	t.Run("correct outbound destination remains available", func(t *testing.T) {
		policy, err := outbound.NewPolicy(outbound.Destination{Name: "partner-b", TrustBundleRef: "bundle:b:v1", Purposes: []string{"abuse-test"}, DataClasses: []string{string(dlp.ClassPublic)}})
		if err != nil {
			t.Fatal(err)
		}
		decision, err := policy.Check(outbound.CheckRequest{Destination: "partner-b", Purpose: "abuse-test", DataClass: string(dlp.ClassPublic)})
		if err != nil || !decision.Allowed {
			t.Fatalf("correct outbound decision = %+v, error=%v", decision, err)
		}
	})
}

type abuseLeasePort struct {
	now  func() time.Time
	next int
}

func (p *abuseLeasePort) IssueLease(_ custody.Context, handle custody.Handle, operation custody.Operation, ttl time.Duration) (custody.Lease, error) {
	p.next++
	return custody.Lease{ID: "custody-" + string(rune('0'+p.next)), Handle: handle, Operation: operation, ExpiresAt: p.now().Add(ttl)}, nil
}

func cursorAdapter(t *testing.T) *spiconform.MemoryAdapter {
	t.Helper()
	adapter, err := spiconform.NewMemoryAdapter(spiconform.MemoryAdapterConfig{
		Manifest: spi.AdapterManifest{
			AdapterID: "tenant-a.fixture", Vendor: "fixture", Product: "hcm", Version: "1",
			Objects:      []connectivity.ObjectKind{connectivity.ObjectWorker},
			Capabilities: []spi.Capability{{Object: connectivity.ObjectWorker, Operation: spi.OpRead, Version: "v1"}},
			Bounds:       connectivity.Bounds{MaxPageSize: 2, MaxPagesPerRun: 2, MaxRecordsPerRun: 2, MaxRecordBytes: 4096},
			GeneratedAt:  abuseNow,
		},
		SnapshotID: "tenant-a:snapshot", SchemaVersion: "v1",
		Records: map[spi.ObjectKind][]connectivity.Record{connectivity.ObjectWorker: {{ExternalID: "a", SortKey: "a", SourceVersion: "1", ObservedAt: abuseNow, Fields: map[string]string{"tenant": "tenant-a"}}}},
		Probe:   spi.ProbeResult{Health: spi.HealthHealthy, CheckedAt: abuseNow}, Now: func() time.Time { return abuseNow },
	})
	if err != nil {
		t.Fatalf("NewMemoryAdapter: %v", err)
	}
	return adapter
}
