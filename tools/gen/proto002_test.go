package gen

import (
	"context"
	"strconv"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/registry/v1"
)

// assertOpaqueCursor fails the test if cursor is empty or looks like a raw
// offset/ordinal rather than an opaque token. List endpoints must use an
// opaque signed cursor per
// planning/specs/http-grpc-endpoint-contract.md#common-wire-and-http-behavior.
func assertOpaqueCursor(t *testing.T, label, cursor string) {
	t.Helper()
	if cursor == "" {
		t.Fatalf("%s: expected a non-empty next_cursor", label)
	}
	if _, err := strconv.ParseInt(cursor, 10, 64); err == nil {
		t.Fatalf("%s: next_cursor %q parses as a plain integer offset, which is not opaque", label, cursor)
	}
}

// TestTodo_PROTO_002 constructs an in-memory gRPC server (bufconn) serving
// both RegistryService and IntentService, calls every method on both
// generated clients, and asserts a typed round trip covering all three
// kernel families plus opaque list-pagination cursors.
func TestTodo_PROTO_002(t *testing.T) {
	registrySrv := newFakeRegistryServer()
	intentSrv := newFakeIntentServer()

	conn := dialBufconn(t, func(s *grpc.Server) {
		registryv1.RegisterRegistryServiceServer(s, registrySrv)
		intentsv1.RegisterIntentServiceServer(s, intentSrv)
	})

	registryClient := registryv1.NewRegistryServiceClient(conn)
	intentClient := intentsv1.NewIntentServiceClient(conn)

	ctx := context.Background()
	scope := &commonv1.ScopeContext{TenantId: "tenant-1", OrganizationScopeId: "org-1", Purpose: "fixture"}

	t.Run("RegistryService.ListIntentDefinitions", func(t *testing.T) {
		resp, err := registryClient.ListIntentDefinitions(ctx, &registryv1.ListIntentDefinitionsRequest{
			Scope: scope,
			Page:  &commonv1.PageRequest{PageSize: 10},
		})
		if err != nil {
			t.Fatalf("ListIntentDefinitions: %v", err)
		}
		if len(resp.GetIntentDefinitions()) != len(kernelFamilies) {
			t.Fatalf("expected %d intent definitions, got %d", len(kernelFamilies), len(resp.GetIntentDefinitions()))
		}
		seen := map[intentsv1.KernelFamily]bool{}
		for i, want := range fixtureIntentDefinitions() {
			got := resp.GetIntentDefinitions()[i]
			if !proto.Equal(want, got) {
				t.Fatalf("definition %d round-trip mismatch:\nwant %v\ngot  %v", i, want, got)
			}
			seen[got.GetKernelFamily()] = true
		}
		for _, kf := range kernelFamilies {
			if !seen[kf] {
				t.Fatalf("kernel family %v missing from ListIntentDefinitions round trip", kf)
			}
		}
		assertOpaqueCursor(t, "ListIntentDefinitions", resp.GetPage().GetNextCursor())
	})

	t.Run("RegistryService.GetIntentDefinition", func(t *testing.T) {
		want := fixtureIntentDefinitions()[0]
		resp, err := registryClient.GetIntentDefinition(ctx, &registryv1.GetIntentDefinitionRequest{
			Scope:      scope,
			Definition: want.GetReference(),
		})
		if err != nil {
			t.Fatalf("GetIntentDefinition: %v", err)
		}
		if !proto.Equal(want, resp.GetIntentDefinition()) {
			t.Fatalf("GetIntentDefinition round-trip mismatch:\nwant %v\ngot  %v", want, resp.GetIntentDefinition())
		}
	})

	t.Run("RegistryService.ListCapabilities", func(t *testing.T) {
		resp, err := registryClient.ListCapabilities(ctx, &registryv1.ListCapabilitiesRequest{
			Scope: scope,
			Page:  &commonv1.PageRequest{PageSize: 10},
		})
		if err != nil {
			t.Fatalf("ListCapabilities: %v", err)
		}
		want := fixtureCapabilities()
		if len(resp.GetCapabilities()) != len(want) {
			t.Fatalf("expected %d capabilities, got %d", len(want), len(resp.GetCapabilities()))
		}
		for i := range want {
			if !proto.Equal(want[i], resp.GetCapabilities()[i]) {
				t.Fatalf("capability %d round-trip mismatch:\nwant %v\ngot  %v", i, want[i], resp.GetCapabilities()[i])
			}
		}
		assertOpaqueCursor(t, "ListCapabilities", resp.GetPage().GetNextCursor())
	})

	t.Run("RegistryService.GetCapability", func(t *testing.T) {
		want := fixtureCapabilities()[0]
		resp, err := registryClient.GetCapability(ctx, &registryv1.GetCapabilityRequest{
			Scope:        scope,
			CapabilityId: want.GetCapabilityId(),
			Version:      want.GetVersion(),
		})
		if err != nil {
			t.Fatalf("GetCapability: %v", err)
		}
		if !proto.Equal(want, resp.GetCapability()) {
			t.Fatalf("GetCapability round-trip mismatch:\nwant %v\ngot  %v", want, resp.GetCapability())
		}
	})

	var createdIntentIDs []string

	t.Run("IntentService.CreateIntent", func(t *testing.T) {
		for _, kf := range kernelFamilies {
			name := kernelFamilyName(kf)
			resp, err := intentClient.CreateIntent(ctx, &intentsv1.CreateIntentRequest{
				IdempotencyKey: "idem-create-" + name,
				Scope:          scope,
				Definition:     &intentsv1.DefinitionReference{IntentTypeId: "fixture." + name, Version: 1},
				Initiator:      &intentsv1.PrincipalReference{PrincipalId: "user-1", Kind: intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN},
				ExecutionMode:  intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE,
			})
			if err != nil {
				t.Fatalf("CreateIntent(%s): %v", name, err)
			}
			got := resp.GetIntent()
			if got.GetDefinition().GetIntentTypeId() != "fixture."+name {
				t.Fatalf("CreateIntent(%s): definition round-trip mismatch: got %v", name, got.GetDefinition())
			}
			if got.GetTenantId() != scope.GetTenantId() {
				t.Fatalf("CreateIntent(%s): tenant round-trip mismatch: got %q", name, got.GetTenantId())
			}
			createdIntentIDs = append(createdIntentIDs, got.GetIntentId())
		}
	})

	t.Run("IntentService.GetIntent", func(t *testing.T) {
		for _, kf := range kernelFamilies {
			name := kernelFamilyName(kf)
			want := fixtureIntentInstances()
			var wantInst *intentsv1.IntentInstance
			for _, w := range want {
				if w.GetIntentId() == "intent-"+name {
					wantInst = w
				}
			}
			resp, err := intentClient.GetIntent(ctx, &intentsv1.GetIntentRequest{Scope: scope, IntentId: "intent-" + name})
			if err != nil {
				t.Fatalf("GetIntent(%s): %v", name, err)
			}
			if !proto.Equal(wantInst, resp.GetIntent()) {
				t.Fatalf("GetIntent(%s) round-trip mismatch:\nwant %v\ngot  %v", name, wantInst, resp.GetIntent())
			}
			if resp.GetIntent().GetDefinition().GetIntentTypeId() == "" {
				t.Fatalf("GetIntent(%s): kernel family %v not represented", name, kf)
			}
		}
	})

	t.Run("IntentService.ListIntents", func(t *testing.T) {
		resp, err := intentClient.ListIntents(ctx, &intentsv1.ListIntentsRequest{
			Scope: scope,
			Page:  &commonv1.PageRequest{PageSize: 10},
		})
		if err != nil {
			t.Fatalf("ListIntents: %v", err)
		}
		if len(resp.GetIntents()) != len(kernelFamilies) {
			t.Fatalf("expected %d intents, got %d", len(kernelFamilies), len(resp.GetIntents()))
		}
		want := fixtureIntentInstances()
		for i := range want {
			if !proto.Equal(want[i], resp.GetIntents()[i]) {
				t.Fatalf("ListIntents[%d] round-trip mismatch:\nwant %v\ngot  %v", i, want[i], resp.GetIntents()[i])
			}
		}
		assertOpaqueCursor(t, "ListIntents", resp.GetPage().GetNextCursor())
	})

	t.Run("IntentService.SimulateIntent", func(t *testing.T) {
		resp, err := intentClient.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{
			Scope:    scope,
			IntentId: "intent-change_request",
			Proposal: &intentsv1.TypedPayload{},
		})
		if err != nil {
			t.Fatalf("SimulateIntent: %v", err)
		}
		sim := resp.GetSimulation()
		if sim.GetIntentId() != "intent-change_request" {
			t.Fatalf("SimulateIntent: intent_id round-trip mismatch: got %q", sim.GetIntentId())
		}
		if !sim.GetZeroEffectReceipt().GetZeroEffect() {
			t.Fatalf("SimulateIntent: expected a populated zero-effect receipt")
		}
	})

	t.Run("IntentService.SubmitIntent", func(t *testing.T) {
		resp, err := intentClient.SubmitIntent(ctx, &intentsv1.SubmitIntentRequest{
			IdempotencyKey:          "idem-submit",
			Scope:                   scope,
			IntentId:                "intent-change_request",
			ProposalRevisionId:      "proposal-1",
			ExpectedInstanceVersion: 1,
		})
		if err != nil {
			t.Fatalf("SubmitIntent: %v", err)
		}
		if resp.GetIntent().GetLifecycle().GetRequest() != intentsv1.RequestState_REQUEST_STATE_SUBMITTED {
			t.Fatalf("SubmitIntent: expected REQUEST_STATE_SUBMITTED, got %v", resp.GetIntent().GetLifecycle().GetRequest())
		}
		if resp.GetIntent().GetInstanceVersion() != 2 {
			t.Fatalf("SubmitIntent: expected instance_version 2, got %d", resp.GetIntent().GetInstanceVersion())
		}
	})

	t.Run("IntentService.CancelIntent", func(t *testing.T) {
		resp, err := intentClient.CancelIntent(ctx, &intentsv1.CancelIntentRequest{
			IdempotencyKey:          "idem-cancel",
			Scope:                   scope,
			IntentId:                "intent-calculation_request",
			ExpectedInstanceVersion: 1,
			ReasonRef:               "fixture-reason",
		})
		if err != nil {
			t.Fatalf("CancelIntent: %v", err)
		}
		if resp.GetIntent().GetLifecycle().GetRequest() != intentsv1.RequestState_REQUEST_STATE_CANCELLED {
			t.Fatalf("CancelIntent: expected REQUEST_STATE_CANCELLED, got %v", resp.GetIntent().GetLifecycle().GetRequest())
		}
	})

	t.Run("IntentService.SupersedeIntent", func(t *testing.T) {
		resp, err := intentClient.SupersedeIntent(ctx, &intentsv1.SupersedeIntentRequest{
			IdempotencyKey:          "idem-supersede",
			Scope:                   scope,
			SupersededIntentId:      "intent-analytical_request",
			ExpectedInstanceVersion: 1,
			Definition:              &intentsv1.DefinitionReference{IntentTypeId: "fixture.analytical_request", Version: 1},
			ReasonRef:               "fixture-reason",
		})
		if err != nil {
			t.Fatalf("SupersedeIntent: %v", err)
		}
		if resp.GetSupersedingIntent().GetIntentId() != "superseding-intent-analytical_request" {
			t.Fatalf("SupersedeIntent: unexpected superseding intent id %q", resp.GetSupersedingIntent().GetIntentId())
		}
	})

	t.Run("IntentService.ExplainIntent", func(t *testing.T) {
		resp, err := intentClient.ExplainIntent(ctx, &intentsv1.ExplainIntentRequest{
			Scope:    scope,
			IntentId: "intent-change_request",
			Purpose:  "audit",
		})
		if err != nil {
			t.Fatalf("ExplainIntent: %v", err)
		}
		if len(resp.GetEvidenceRefs()) == 0 {
			t.Fatalf("ExplainIntent: expected at least one evidence ref")
		}
	})

	t.Run("IntentService.ListIntentTimeline", func(t *testing.T) {
		resp, err := intentClient.ListIntentTimeline(ctx, &intentsv1.ListIntentTimelineRequest{
			Scope:    scope,
			IntentId: "intent-change_request",
			Page:     &commonv1.PageRequest{PageSize: 10},
		})
		if err != nil {
			t.Fatalf("ListIntentTimeline: %v", err)
		}
		if len(resp.GetEvents()) == 0 {
			t.Fatalf("ListIntentTimeline: expected at least one event")
		}
		assertOpaqueCursor(t, "ListIntentTimeline", resp.GetPage().GetNextCursor())
	})

	if len(createdIntentIDs) != len(kernelFamilies) {
		t.Fatalf("expected %d created intents, got %d", len(kernelFamilies), len(createdIntentIDs))
	}
}
