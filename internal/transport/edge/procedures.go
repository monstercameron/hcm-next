package edge

import (
	"google.golang.org/protobuf/proto"

	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/registry/v1"
)

// Procedure paths. They are the gRPC method names verbatim, so one method
// vocabulary covers both transports: the same string appears in
// grpc.UnaryServerInfo.FullMethod, in connect.Spec.Procedure, in
// transport.Invocation.Method and in every log record and endpoint rule.
const (
	ProcedureCreateIntent       = "/hcmnext.intents.v1.IntentService/CreateIntent"
	ProcedureGetIntent          = "/hcmnext.intents.v1.IntentService/GetIntent"
	ProcedureListIntents        = "/hcmnext.intents.v1.IntentService/ListIntents"
	ProcedureSimulateIntent     = "/hcmnext.intents.v1.IntentService/SimulateIntent"
	ProcedureSubmitIntent       = "/hcmnext.intents.v1.IntentService/SubmitIntent"
	ProcedureCancelIntent       = "/hcmnext.intents.v1.IntentService/CancelIntent"
	ProcedureSupersedeIntent    = "/hcmnext.intents.v1.IntentService/SupersedeIntent"
	ProcedureExplainIntent      = "/hcmnext.intents.v1.IntentService/ExplainIntent"
	ProcedureListIntentTimeline = "/hcmnext.intents.v1.IntentService/ListIntentTimeline"

	ProcedureListIntentDefinitions = "/hcmnext.registry.v1.RegistryService/ListIntentDefinitions"
	ProcedureGetIntentDefinition   = "/hcmnext.registry.v1.RegistryService/GetIntentDefinition"
	ProcedureListCapabilities      = "/hcmnext.registry.v1.RegistryService/ListCapabilities"
	ProcedureGetCapability         = "/hcmnext.registry.v1.RegistryService/GetCapability"
)

// requestFactories maps a procedure to a constructor for its request message.
// The strict JSON screen needs the target type before connect decodes the
// body, because "unknown field" is a statement about a specific descriptor.
var requestFactories = map[string]func() proto.Message{
	ProcedureCreateIntent:       func() proto.Message { return &intentsv1.CreateIntentRequest{} },
	ProcedureGetIntent:          func() proto.Message { return &intentsv1.GetIntentRequest{} },
	ProcedureListIntents:        func() proto.Message { return &intentsv1.ListIntentsRequest{} },
	ProcedureSimulateIntent:     func() proto.Message { return &intentsv1.SimulateIntentRequest{} },
	ProcedureSubmitIntent:       func() proto.Message { return &intentsv1.SubmitIntentRequest{} },
	ProcedureCancelIntent:       func() proto.Message { return &intentsv1.CancelIntentRequest{} },
	ProcedureSupersedeIntent:    func() proto.Message { return &intentsv1.SupersedeIntentRequest{} },
	ProcedureExplainIntent:      func() proto.Message { return &intentsv1.ExplainIntentRequest{} },
	ProcedureListIntentTimeline: func() proto.Message { return &intentsv1.ListIntentTimelineRequest{} },

	ProcedureListIntentDefinitions: func() proto.Message { return &registryv1.ListIntentDefinitionsRequest{} },
	ProcedureGetIntentDefinition:   func() proto.Message { return &registryv1.GetIntentDefinitionRequest{} },
	ProcedureListCapabilities:      func() proto.Message { return &registryv1.ListCapabilitiesRequest{} },
	ProcedureGetCapability:         func() proto.Message { return &registryv1.GetCapabilityRequest{} },
}

// Procedures returns every procedure path this edge publishes.
func Procedures() []string {
	out := make([]string, 0, len(requestFactories))
	for p := range requestFactories {
		out = append(out, p)
	}
	return out
}
