package edge_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/hcm-next/internal/transport"
	"github.com/monstercameron/hcm-next/internal/transport/envelope"
	"github.com/monstercameron/hcm-next/internal/transport/transporttest"
	"github.com/monstercameron/hcm-next/internal/trust"
)

// TestEndpointTrustedContextRejectsCallerSelectedAuthorityAcrossTransports is
// the ENDPOINT-002 primary test.
//
// RED: a caller must not be able to override PrincipalContext, tenant,
// organization scope, purpose, session assurance, authority or processing
// placement through a body field, a header or gRPC metadata, and two
// equivalent gRPC and HTTP requests must not resolve different trusted
// context.
//
// GREEN: authenticated interceptors construct the trusted context server-side,
// reject conflicting reserved fields, and hand one immutable invocation
// context to the handler; the same vector produces the same trusted
// fingerprint and the same evidence reference on both transports.
func TestEndpointTrustedContextRejectsCallerSelectedAuthorityAcrossTransports(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	t.Run("every reserved metadata name is rejected on both transports", func(t *testing.T) {
		for _, key := range trust.ReservedMetadataKeys() {
			t.Run(key, func(t *testing.T) {
				request := &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID}

				_, grpcErr := h.grpcIntent.GetIntent(h.grpcContext(ctx, key, "attacker-value"), clone(request))
				_, edgeErr := h.edgeIntent.GetIntent(ctx, edgeRequest(h, clone(request), key, "attacker-value"))
				if grpcErr == nil || edgeErr == nil {
					t.Fatalf("%q was accepted: grpc=%v edge=%v", key, grpcErr, edgeErr)
				}

				viaGRPC, viaEdge := ownedFromGRPC(t, grpcErr), ownedFromEdge(t, edgeErr)
				if viaGRPC.Code() != envelope.CodeInvalidArgument {
					t.Errorf("owned code = %v, want INVALID_ARGUMENT", viaGRPC.Code())
				}
				if viaGRPC.ReasonRef() != "" && viaGRPC.ReasonRef() != "trusted_context.caller_selected_authority" {
					t.Errorf("reason ref = %q", viaGRPC.ReasonRef())
				}
				violations := viaGRPC.Violations()
				if len(violations) != 1 || violations[0].FieldPath != "metadata."+key {
					t.Errorf("violations = %+v, want one naming metadata.%s", violations, key)
				}
				assertOwnedParity(t, key, viaGRPC, viaEdge)
			})
		}
	})

	t.Run("a body field cannot select tenant or organization scope", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			scope *commonv1.ScopeContext
			field string
		}{
			{"another tenant", &commonv1.ScopeContext{TenantId: "victim-corp"}, "scope.tenant_id"},
			{"another organization scope", &commonv1.ScopeContext{OrganizationScopeId: "org-emea"}, "scope.organization_scope_id"},
			{"both", &commonv1.ScopeContext{TenantId: "victim-corp", OrganizationScopeId: "org-emea"}, "scope.tenant_id"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				request := &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID, Scope: tc.scope}

				_, grpcErr := h.grpcIntent.GetIntent(h.grpcContext(ctx), clone(request))
				_, edgeErr := h.edgeIntent.GetIntent(ctx, edgeRequest(h, clone(request)))
				if grpcErr == nil || edgeErr == nil {
					t.Fatalf("a caller-selected scope was accepted: grpc=%v edge=%v", grpcErr, edgeErr)
				}

				viaGRPC, viaEdge := ownedFromGRPC(t, grpcErr), ownedFromEdge(t, edgeErr)
				if viaGRPC.Code() != envelope.CodeInvalidArgument {
					t.Errorf("owned code = %v, want INVALID_ARGUMENT", viaGRPC.Code())
				}
				if got := viaGRPC.Violations(); len(got) != 1 || got[0].FieldPath != tc.field {
					t.Errorf("violations = %+v, want one naming %s", got, tc.field)
				}
				assertOwnedParity(t, tc.name, viaGRPC, viaEdge)
			})
		}
	})

	t.Run("a body field cannot select the acting principal", func(t *testing.T) {
		request := transporttest.CanonicalCreateIntentRequest()
		request.Initiator = &intentsv1.PrincipalReference{
			PrincipalId: "user-root",
			Kind:        intentsv1.InitiatorKind_INITIATOR_KIND_SYSTEM_EVENT,
		}

		_, grpcErr := h.grpcIntent.CreateIntent(h.grpcContext(ctx), clone(request))
		_, edgeErr := h.edgeIntent.CreateIntent(ctx, edgeRequest(h, clone(request)))
		if grpcErr == nil || edgeErr == nil {
			t.Fatalf("a caller-selected initiator was accepted: grpc=%v edge=%v", grpcErr, edgeErr)
		}

		viaGRPC, viaEdge := ownedFromGRPC(t, grpcErr), ownedFromEdge(t, edgeErr)
		if got := viaGRPC.Violations(); len(got) != 1 || got[0].FieldPath != "initiator" {
			t.Errorf("violations = %+v, want one naming initiator", got)
		}
		assertOwnedParity(t, "caller-selected initiator", viaGRPC, viaEdge)
	})

	t.Run("a matching scope is accepted and still overwritten server-side", func(t *testing.T) {
		h.intent.Reset()
		request := &intentsv1.GetIntentRequest{
			IntentId: transporttest.KnownIntentID,
			Scope: &commonv1.ScopeContext{
				TenantId:            transporttest.Tenant,
				OrganizationScopeId: transporttest.OrganizationScopeID,
				Purpose:             transporttest.PurposeAnalytics,
			},
		}

		if _, err := h.grpcIntent.GetIntent(h.grpcContext(ctx), clone(request)); err != nil {
			t.Fatalf("gRPC GetIntent: %v", err)
		}
		if _, err := h.edgeIntent.GetIntent(ctx, edgeRequest(h, clone(request))); err != nil {
			t.Fatalf("edge GetIntent: %v", err)
		}

		calls := h.intent.Calls()
		if len(calls) != 2 {
			t.Fatalf("recorded %d calls, want 2", len(calls))
		}
		for _, call := range calls {
			if call.ScopeInRequest.GetTenantId() != transporttest.Tenant {
				t.Errorf("%s: tenant = %q", call.Transport, call.ScopeInRequest.GetTenantId())
			}
			if call.Purpose != transporttest.PurposeAnalytics {
				t.Errorf("%s: purpose = %q, want the authorized purpose the caller requested", call.Transport, call.Purpose)
			}
		}
		if calls[0].TrustedFingerprint != calls[1].TrustedFingerprint {
			t.Error("equivalent gRPC and HTTP requests resolved different trusted context")
		}
	})

	t.Run("equivalent requests resolve identical trusted context", func(t *testing.T) {
		h.intent.Reset()
		vector := transporttest.CanonicalCreateIntentRequest()

		if _, err := h.grpcIntent.CreateIntent(h.grpcContext(ctx), clone(vector)); err != nil {
			t.Fatalf("gRPC CreateIntent: %v", err)
		}
		if _, err := h.edgeIntent.CreateIntent(ctx, edgeRequest(h, clone(vector))); err != nil {
			t.Fatalf("edge CreateIntent: %v", err)
		}
		if _, err := h.edgeJSON.CreateIntent(ctx, edgeRequest(h, clone(vector))); err != nil {
			t.Fatalf("edge CreateIntent (json): %v", err)
		}

		calls := h.intent.Calls()
		if len(calls) != 3 {
			t.Fatalf("recorded %d calls, want 3", len(calls))
		}
		for i := 1; i < len(calls); i++ {
			if calls[i].TrustedFingerprint != calls[0].TrustedFingerprint {
				t.Errorf("call %d (%s) resolved a different trusted context", i, calls[i].Transport)
			}
			if calls[i].EvidenceID != calls[0].EvidenceID {
				t.Errorf("call %d (%s) resolved a different evidence reference", i, calls[i].Transport)
			}
		}
	})
}

// TestTodo_ENDPOINT_002_Property asserts the invariant behind the primary
// test, over a generated space rather than a hand-picked list: for any
// combination of caller-supplied scope values, admission either rejects the
// request or resolves exactly the principal's own tenant and organization
// scope. There is no input that produces a third outcome.
func TestTodo_ENDPOINT_002_Property(t *testing.T) {
	cfg, token := admissionConfig(t)
	tenants := []string{"", transporttest.Tenant, "victim-corp", strings.ToUpper(transporttest.Tenant), transporttest.Tenant + " "}
	scopes := []string{"", transporttest.OrganizationScopeID, "org-emea", "ORG-NORTH-AMERICA"}
	purposes := []string{"", transporttest.PurposeOperations, transporttest.PurposeAnalytics, transporttest.PurposeUnauthorized, "hcm_operations "}

	for _, tenant := range tenants {
		for _, scope := range scopes {
			for _, purpose := range purposes {
				name := fmt.Sprintf("tenant=%q scope=%q purpose=%q", tenant, scope, purpose)
				request := &intentsv1.GetIntentRequest{
					IntentId: transporttest.KnownIntentID,
					Scope: &commonv1.ScopeContext{
						TenantId:            tenant,
						OrganizationScopeId: scope,
						Purpose:             purpose,
					},
				}
				_, inv, err := transport.Admit(context.Background(), cfg, transport.AdmissionRequest{
					Metadata: transport.MapMetadata{transport.AuthorizationMetadataKey: {token}},
					Method:   "/hcmnext.intents.v1.IntentService/GetIntent",
					Kind:     transport.KindGRPC,
					Message:  request,
				})
				if err != nil {
					// A rejection is always acceptable. What is never
					// acceptable is a success carrying a caller's value.
					continue
				}
				if inv.TenantID() != transporttest.Tenant {
					t.Errorf("%s: resolved tenant %q", name, inv.TenantID())
				}
				if inv.OrganizationScopeID() != transporttest.OrganizationScopeID {
					t.Errorf("%s: resolved organization scope %q", name, inv.OrganizationScopeID())
				}
				if !inv.Principal().AuthorizesPurpose(inv.Purpose()) {
					t.Errorf("%s: resolved purpose %q is not authorized for the principal", name, inv.Purpose())
				}
				if request.GetScope().GetTenantId() != transporttest.Tenant {
					t.Errorf("%s: request scope was not overwritten server-side (%q)", name, request.GetScope().GetTenantId())
				}
			}
		}
	}
}

// FuzzTodo_ENDPOINT_002 fuzzes the metadata screen and the message-level
// trusted-field enforcement together. Whatever a caller sends, an admitted
// request carries the credential's own tenant and subject, and a reserved
// metadata name is never admitted at all.
func FuzzTodo_ENDPOINT_002(f *testing.F) {
	cfg, token := admissionConfig(f)

	f.Add("x-request-id", "abc", transporttest.Tenant, transporttest.PurposeOperations)
	f.Add("x-tenant", "victim-corp", "", "")
	f.Add("X-Principal", "root", "victim-corp", "")
	f.Add("content-type", "application/grpc", "", transporttest.PurposeUnauthorized)
	f.Add("", "", strings.Repeat("t", 5000), strings.Repeat("p", 5000))
	f.Add("x-purpose", "anything", transporttest.Tenant, "")

	f.Fuzz(func(t *testing.T, metaName, metaValue, tenant, purpose string) {
		md := transport.MapMetadata{transport.AuthorizationMetadataKey: {token}}
		if metaName != "" {
			md[strings.ToLower(metaName)] = []string{metaValue}
		}
		request := &intentsv1.GetIntentRequest{
			IntentId: transporttest.KnownIntentID,
			Scope:    &commonv1.ScopeContext{TenantId: tenant, Purpose: purpose},
		}

		_, inv, err := transport.Admit(context.Background(), cfg, transport.AdmissionRequest{
			Metadata: md,
			Method:   "/hcmnext.intents.v1.IntentService/GetIntent",
			Kind:     transport.KindGRPC,
			Message:  request,
		})
		if err != nil {
			if inv != nil {
				t.Fatal("Admit returned both an error and an invocation")
			}
			return
		}
		if trust.IsReservedMetadataKey(metaName) {
			t.Fatalf("reserved metadata name %q was admitted", metaName)
		}
		if inv.TenantID() != transporttest.Tenant {
			t.Fatalf("admitted request resolved tenant %q", inv.TenantID())
		}
		if inv.Principal().Subject() != transporttest.Subject {
			t.Fatalf("admitted request resolved subject %q", inv.Principal().Subject())
		}
		if !inv.Principal().AuthorizesPurpose(inv.Purpose()) {
			t.Fatalf("admitted request resolved unauthorized purpose %q", inv.Purpose())
		}
		if request.GetScope().GetTenantId() != transporttest.Tenant {
			t.Fatalf("admitted request kept the caller's tenant %q", request.GetScope().GetTenantId())
		}
	})
}

// TestTodo_ENDPOINT_002_Integration walks a matrix of methods and asserts that
// each one resolves the same trusted context on both transports.
func TestTodo_ENDPOINT_002_Integration(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	methods := []struct {
		name string
		grpc func() error
		edge func() error
	}{
		{
			name: "GetIntent",
			grpc: func() error {
				_, err := h.grpcIntent.GetIntent(h.grpcContext(ctx), &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID})
				return err
			},
			edge: func() error {
				_, err := h.edgeIntent.GetIntent(ctx, edgeRequest(h, &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID}))
				return err
			},
		},
		{
			name: "ListIntents",
			grpc: func() error {
				_, err := h.grpcIntent.ListIntents(h.grpcContext(ctx), &intentsv1.ListIntentsRequest{})
				return err
			},
			edge: func() error {
				_, err := h.edgeIntent.ListIntents(ctx, edgeRequest(h, &intentsv1.ListIntentsRequest{}))
				return err
			},
		},
		{
			name: "CreateIntent",
			grpc: func() error {
				_, err := h.grpcIntent.CreateIntent(h.grpcContext(ctx), transporttest.CanonicalCreateIntentRequest())
				return err
			},
			edge: func() error {
				_, err := h.edgeIntent.CreateIntent(ctx, edgeRequest(h, transporttest.CanonicalCreateIntentRequest()))
				return err
			},
		},
	}

	for _, method := range methods {
		t.Run(method.name, func(t *testing.T) {
			h.intent.Reset()
			if err := method.grpc(); err != nil {
				t.Fatalf("gRPC: %v", err)
			}
			if err := method.edge(); err != nil {
				t.Fatalf("edge: %v", err)
			}
			calls := h.intent.Calls()
			if len(calls) != 2 {
				t.Fatalf("recorded %d calls, want 2", len(calls))
			}
			if calls[0].TrustedFingerprint != calls[1].TrustedFingerprint {
				t.Errorf("trusted context differs between transports")
			}
			if !calls[0].HadDeadline || !calls[1].HadDeadline {
				t.Error("a handler ran without a server-capped deadline")
			}
		})
	}
}

// TestTodo_ENDPOINT_002_Security proves the interesting case: a caller who
// holds a perfectly valid credential still cannot reach another tenant, and a
// credential for another tenant resolves that tenant rather than the one the
// caller names.
func TestTodo_ENDPOINT_002_Security(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	t.Run("a valid credential does not enable cross-tenant access", func(t *testing.T) {
		request := &intentsv1.GetIntentRequest{
			IntentId: transporttest.KnownIntentID,
			Scope:    &commonv1.ScopeContext{TenantId: "victim-corp"},
		}
		if _, err := h.grpcIntent.GetIntent(h.grpcContext(ctx), clone(request)); err == nil {
			t.Fatal("gRPC accepted a cross-tenant scope from an authenticated caller")
		}
		if _, err := h.edgeIntent.GetIntent(ctx, edgeRequest(h, clone(request))); err == nil {
			t.Fatal("the edge accepted a cross-tenant scope from an authenticated caller")
		}
	})

	t.Run("the credential decides the tenant, not the request", func(t *testing.T) {
		h.intent.Reset()
		claims := transporttest.DefaultClaims(baseTime)
		claims.Tenant = "beta-corp"
		claims.Subject = "user-beta"
		otherToken, err := transporttest.BearerToken(h.verifier, claims)
		if err != nil {
			t.Fatalf("BearerToken: %v", err)
		}

		request := &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID}
		if _, err := h.grpcIntent.GetIntent(
			h.grpcContextWithoutCredential(ctx, transport.AuthorizationMetadataKey, otherToken), clone(request)); err != nil {
			t.Fatalf("gRPC GetIntent: %v", err)
		}
		if _, err := h.edgeIntent.GetIntent(ctx,
			edgeRequestWithoutCredential(clone(request), transport.AuthorizationMetadataKey, otherToken)); err != nil {
			t.Fatalf("edge GetIntent: %v", err)
		}

		calls := h.intent.Calls()
		if len(calls) != 2 {
			t.Fatalf("recorded %d calls, want 2", len(calls))
		}
		for _, call := range calls {
			if call.TenantID != "beta-corp" {
				t.Errorf("%s: tenant = %q, want the credential's tenant", call.Transport, call.TenantID)
			}
		}
		if calls[0].TrustedFingerprint != calls[1].TrustedFingerprint {
			t.Error("the two transports derived different trusted context for the same credential")
		}
	})

	t.Run("a different credential derives different trusted context", func(t *testing.T) {
		h.intent.Reset()
		claims := transporttest.DefaultClaims(baseTime)
		claims.Subject = "user-other"
		otherToken, err := transporttest.BearerToken(h.verifier, claims)
		if err != nil {
			t.Fatalf("BearerToken: %v", err)
		}
		request := &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID}

		if _, err := h.grpcIntent.GetIntent(h.grpcContext(ctx), clone(request)); err != nil {
			t.Fatalf("gRPC GetIntent: %v", err)
		}
		if _, err := h.grpcIntent.GetIntent(
			h.grpcContextWithoutCredential(ctx, transport.AuthorizationMetadataKey, otherToken), clone(request)); err != nil {
			t.Fatalf("gRPC GetIntent (other): %v", err)
		}
		calls := h.intent.Calls()
		if len(calls) != 2 {
			t.Fatalf("recorded %d calls, want 2", len(calls))
		}
		if calls[0].TrustedFingerprint == calls[1].TrustedFingerprint {
			t.Error("two different credentials derived the same trusted context")
		}
	})
}

// TestTodo_ENDPOINT_002_Conformance checks that the reserved-name list is the
// trusted-boundary table, and that both transports screen the whole list
// rather than a subset.
func TestTodo_ENDPOINT_002_Conformance(t *testing.T) {
	reserved := trust.ReservedMetadataKeys()
	index := make(map[string]bool, len(reserved))
	for _, key := range reserved {
		index[key] = true
		if key != strings.ToLower(key) {
			t.Errorf("reserved name %q is not lowercase", key)
		}
	}

	// One name per row of the trusted-boundary table in
	// planning/specs/http-grpc-endpoint-contract.md#trusted-request-boundary.
	for concept, key := range map[string]string{
		"PrincipalContext":  "x-principal",
		"TenantContext":     "x-tenant",
		"OrganizationScope": "x-organization-scope",
		"Purpose":           "x-purpose",
		"SessionAssurance":  "x-assurance",
		"LegalPolicy":       "x-legal-context",
		"SourceAuthority":   "x-source-authority",
		"Placement":         "x-processing-placement",
	} {
		if !index[key] {
			t.Errorf("the trusted-boundary concept %s has no reserved name (%q missing)", concept, key)
		}
	}

	// A client request identifier is explicitly allowed by the same table.
	if index[transport.RequestIDMetadataKey] {
		t.Errorf("%q must not be reserved: the contract permits a client request identifier", transport.RequestIDMetadataKey)
	}
	if index[transport.AuthorizationMetadataKey] {
		t.Errorf("%q must not be reserved: it carries the credential", transport.AuthorizationMetadataKey)
	}
}

// TestTodo_ENDPOINT_002_Mutation attacks the immutability claim. A handler
// that mutates the request message it received must not thereby change the
// invocation, and admission must fill the server-derived fields even when the
// caller left them empty.
func TestTodo_ENDPOINT_002_Mutation(t *testing.T) {
	cfg, token := admissionConfig(t)
	request := transporttest.CanonicalCreateIntentRequest()

	_, inv, err := transport.Admit(context.Background(), cfg, transport.AdmissionRequest{
		Metadata: transport.MapMetadata{transport.AuthorizationMetadataKey: {token}},
		Method:   "/hcmnext.intents.v1.IntentService/CreateIntent",
		Kind:     transport.KindGRPC,
		Message:  request,
	})
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}

	t.Run("admission fills the server-derived fields", func(t *testing.T) {
		if got := request.GetScope().GetTenantId(); got != transporttest.Tenant {
			t.Errorf("scope tenant = %q, want the derived tenant", got)
		}
		if got := request.GetInitiator().GetPrincipalId(); got != transporttest.Subject {
			t.Errorf("initiator = %q, want the authenticated subject", got)
		}
		if got := request.GetInitiator().GetKind(); got != intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN {
			t.Errorf("initiator kind = %v, want HUMAN", got)
		}
	})

	t.Run("mutating the request does not change the invocation", func(t *testing.T) {
		before := inv.TrustedFingerprint()
		request.Scope = &commonv1.ScopeContext{TenantId: "victim-corp", Purpose: "anything"}
		request.Initiator = &intentsv1.PrincipalReference{PrincipalId: "user-root"}
		if inv.TrustedFingerprint() != before {
			t.Error("mutating the request message changed the invocation")
		}
		if inv.TenantID() != transporttest.Tenant {
			t.Errorf("invocation tenant = %q after mutating the request", inv.TenantID())
		}
		if inv.Principal().Subject() != transporttest.Subject {
			t.Errorf("invocation subject = %q after mutating the request", inv.Principal().Subject())
		}
	})

	t.Run("mutating an accessor result does not change the principal", func(t *testing.T) {
		roles := inv.Principal().Roles()
		if len(roles) > 0 {
			roles[0] = "tenant_admin"
		}
		if inv.Principal().HasRole("tenant_admin") {
			t.Error("mutating the returned role slice escalated the principal")
		}
	})

	t.Run("dropping a trusted field from the fingerprint is detectable", func(t *testing.T) {
		// Two invocations that differ only in the resolved purpose must have
		// different fingerprints. A fingerprint that ignores purpose would
		// pass every other assertion in this package and fail here.
		other := clone(transporttest.CanonicalCreateIntentRequest())
		other.Scope = &commonv1.ScopeContext{Purpose: transporttest.PurposeAnalytics}
		_, otherInv, admitErr := transport.Admit(context.Background(), cfg, transport.AdmissionRequest{
			Metadata: transport.MapMetadata{transport.AuthorizationMetadataKey: {token}},
			Method:   "/hcmnext.intents.v1.IntentService/CreateIntent",
			Kind:     transport.KindHTTPEdge,
			Message:  other,
		})
		if admitErr != nil {
			t.Fatalf("Admit: %v", admitErr)
		}
		if otherInv.TrustedFingerprint() == inv.TrustedFingerprint() {
			t.Error("a different resolved purpose produced the same trusted fingerprint")
		}
		if otherInv.Kind() == inv.Kind() {
			t.Fatal("the two invocations should differ in transport kind for the next assertion to mean anything")
		}
	})
}

// admissionConfig builds a transport.Config plus a valid credential for tests
// that exercise admission directly, without a server.
func admissionConfig(tb testing.TB) (transport.Config, string) {
	tb.Helper()
	verifier, err := transporttest.NewVerifier(func() time.Time { return baseTime })
	if err != nil {
		tb.Fatalf("NewVerifier: %v", err)
	}
	token, err := verifier.Issue(transporttest.DefaultClaims(baseTime))
	if err != nil {
		tb.Fatalf("Issue: %v", err)
	}
	return transporttest.Config(verifier, func() time.Time { return baseTime }, fixedRequestID, nil), "Bearer " + token
}
