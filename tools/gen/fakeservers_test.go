package gen

import (
	"context"

	"google.golang.org/protobuf/proto"

	capabilitiesv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/capabilities/v1"
	commonv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/registry/v1"
)

// fakeRegistryServer serves the fixture IntentDefinitions and
// CapabilityDefinitions declared in helpers_test.go, returning an opaque
// cursor whenever a page is requested.
type fakeRegistryServer struct {
	registryv1.UnimplementedRegistryServiceServer

	definitions  []*intentsv1.IntentDefinition
	capabilities []*capabilitiesv1.CapabilityDefinition
}

func newFakeRegistryServer() *fakeRegistryServer {
	return &fakeRegistryServer{
		definitions:  fixtureIntentDefinitions(),
		capabilities: fixtureCapabilities(),
	}
}

func (s *fakeRegistryServer) ListIntentDefinitions(_ context.Context, req *registryv1.ListIntentDefinitionsRequest) (*registryv1.ListIntentDefinitionsResponse, error) {
	return &registryv1.ListIntentDefinitionsResponse{
		IntentDefinitions: s.definitions,
		Page:              &commonv1.PageResponse{NextCursor: opaqueCursor("intent-definitions:" + req.GetScope().GetTenantId())},
	}, nil
}

func (s *fakeRegistryServer) GetIntentDefinition(_ context.Context, req *registryv1.GetIntentDefinitionRequest) (*registryv1.GetIntentDefinitionResponse, error) {
	for _, d := range s.definitions {
		if d.GetReference().GetIntentTypeId() == req.GetDefinition().GetIntentTypeId() {
			return &registryv1.GetIntentDefinitionResponse{IntentDefinition: d}, nil
		}
	}
	return &registryv1.GetIntentDefinitionResponse{}, nil
}

func (s *fakeRegistryServer) ListCapabilities(_ context.Context, req *registryv1.ListCapabilitiesRequest) (*registryv1.ListCapabilitiesResponse, error) {
	return &registryv1.ListCapabilitiesResponse{
		Capabilities: s.capabilities,
		Page:         &commonv1.PageResponse{NextCursor: opaqueCursor("capabilities:" + req.GetScope().GetTenantId())},
	}, nil
}

func (s *fakeRegistryServer) GetCapability(_ context.Context, req *registryv1.GetCapabilityRequest) (*registryv1.GetCapabilityResponse, error) {
	for _, c := range s.capabilities {
		if c.GetCapabilityId() == req.GetCapabilityId() {
			return &registryv1.GetCapabilityResponse{Capability: c}, nil
		}
	}
	return &registryv1.GetCapabilityResponse{}, nil
}

// fakeIntentServer serves the fixture IntentInstances declared in
// helpers_test.go and implements every IntentService method so a client-side
// round trip can exercise the full lifecycle surface.
type fakeIntentServer struct {
	intentsv1.UnimplementedIntentServiceServer

	instances map[string]*intentsv1.IntentInstance
}

func newFakeIntentServer() *fakeIntentServer {
	m := make(map[string]*intentsv1.IntentInstance)
	for _, inst := range fixtureIntentInstances() {
		m[inst.GetIntentId()] = inst
	}
	return &fakeIntentServer{instances: m}
}

func (s *fakeIntentServer) CreateIntent(_ context.Context, req *intentsv1.CreateIntentRequest) (*intentsv1.CreateIntentResponse, error) {
	inst := &intentsv1.IntentInstance{
		IntentId:            "created-" + req.GetDefinition().GetIntentTypeId(),
		Definition:          req.GetDefinition(),
		TenantId:            req.GetScope().GetTenantId(),
		OrganizationScopeId: req.GetScope().GetOrganizationScopeId(),
		Initiator:           req.GetInitiator(),
		Subjects:            req.GetSubjects(),
		Request:             req.GetRequest(),
		IdempotencyKey:      req.GetIdempotencyKey(),
		ExecutionMode:       req.GetExecutionMode(),
		InstanceVersion:     1,
		Lifecycle: &intentsv1.LifecycleDimensions{
			Request: intentsv1.RequestState_REQUEST_STATE_DRAFT,
		},
	}
	s.instances[inst.GetIntentId()] = inst
	return &intentsv1.CreateIntentResponse{Intent: inst}, nil
}

func (s *fakeIntentServer) GetIntent(_ context.Context, req *intentsv1.GetIntentRequest) (*intentsv1.GetIntentResponse, error) {
	return &intentsv1.GetIntentResponse{Intent: s.instances[req.GetIntentId()]}, nil
}

func (s *fakeIntentServer) ListIntents(_ context.Context, req *intentsv1.ListIntentsRequest) (*intentsv1.ListIntentsResponse, error) {
	out := make([]*intentsv1.IntentInstance, 0, len(s.instances))
	for _, kf := range kernelFamilies {
		out = append(out, s.instances["intent-"+kernelFamilyName(kf)])
	}
	return &intentsv1.ListIntentsResponse{
		Intents: out,
		Page:    &commonv1.PageResponse{NextCursor: opaqueCursor("intents:" + req.GetScope().GetTenantId())},
	}, nil
}

func (s *fakeIntentServer) SimulateIntent(_ context.Context, req *intentsv1.SimulateIntentRequest) (*intentsv1.SimulateIntentResponse, error) {
	return &intentsv1.SimulateIntentResponse{
		Simulation: &intentsv1.SimulationArtifact{
			IntentId:           req.GetIntentId(),
			ProposalRevisionId: "proposal-1",
			MaterialProposalDigest: &intentsv1.CanonicalDigestReference{
				ProfileId: "PROPOSAL",
				Digest:    "deadbeef",
			},
			ZeroEffectReceipt: &intentsv1.ZeroEffectReceipt{ZeroEffect: true, ReasonRef: "no-material-write"},
		},
	}, nil
}

func (s *fakeIntentServer) SubmitIntent(_ context.Context, req *intentsv1.SubmitIntentRequest) (*intentsv1.SubmitIntentResponse, error) {
	inst := s.instances[req.GetIntentId()]
	if inst != nil {
		updated := proto.Clone(inst).(*intentsv1.IntentInstance)
		updated.InstanceVersion = req.GetExpectedInstanceVersion() + 1
		updated.Lifecycle = &intentsv1.LifecycleDimensions{Request: intentsv1.RequestState_REQUEST_STATE_SUBMITTED}
		s.instances[req.GetIntentId()] = updated
		inst = updated
	}
	return &intentsv1.SubmitIntentResponse{Intent: inst}, nil
}

func (s *fakeIntentServer) CancelIntent(_ context.Context, req *intentsv1.CancelIntentRequest) (*intentsv1.CancelIntentResponse, error) {
	inst := s.instances[req.GetIntentId()]
	if inst != nil {
		updated := proto.Clone(inst).(*intentsv1.IntentInstance)
		updated.Lifecycle = &intentsv1.LifecycleDimensions{Request: intentsv1.RequestState_REQUEST_STATE_CANCELLED}
		s.instances[req.GetIntentId()] = updated
		inst = updated
	}
	return &intentsv1.CancelIntentResponse{Intent: inst}, nil
}

func (s *fakeIntentServer) SupersedeIntent(_ context.Context, req *intentsv1.SupersedeIntentRequest) (*intentsv1.SupersedeIntentResponse, error) {
	inst := &intentsv1.IntentInstance{
		IntentId:            "superseding-" + req.GetSupersededIntentId(),
		Definition:          req.GetDefinition(),
		TenantId:            req.GetScope().GetTenantId(),
		OrganizationScopeId: req.GetScope().GetOrganizationScopeId(),
		Request:             req.GetRequest(),
		InstanceVersion:     1,
		Lifecycle:           &intentsv1.LifecycleDimensions{Request: intentsv1.RequestState_REQUEST_STATE_DRAFT},
	}
	s.instances[inst.GetIntentId()] = inst
	return &intentsv1.SupersedeIntentResponse{SupersedingIntent: inst}, nil
}

func (s *fakeIntentServer) ExplainIntent(_ context.Context, req *intentsv1.ExplainIntentRequest) (*intentsv1.ExplainIntentResponse, error) {
	return &intentsv1.ExplainIntentResponse{
		IntentId:        req.GetIntentId(),
		EvidenceRefs:    []*commonv1.EvidenceRef{{EvidenceId: "evidence-1", EvidenceKind: "simulation", Digest: "deadbeef"}},
		ExplanationText: "fixture explanation",
	}, nil
}

func (s *fakeIntentServer) ListIntentTimeline(_ context.Context, req *intentsv1.ListIntentTimelineRequest) (*intentsv1.ListIntentTimelineResponse, error) {
	return &intentsv1.ListIntentTimelineResponse{
		Events: []*intentsv1.TimelineEvent{
			{EventId: "event-1", Kind: "CREATED", Description: "fixture timeline event for " + req.GetIntentId()},
		},
		Page: &commonv1.PageResponse{NextCursor: opaqueCursor("timeline:" + req.GetIntentId())},
	}, nil
}
