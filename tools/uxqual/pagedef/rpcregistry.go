package pagedef

import (
	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
)

// This file is the whole of pagedef's coupling to generated code, and it is
// deliberately read-only. It never imports google.golang.org/grpc or
// google.golang.org/protobuf itself -- LIB-003 (definitions/architecture/
// library-firewall.yaml) confines those two modules to gen/, internal/
// transport, internal/intent/protomap, internal/engines/wire, and the
// composition roots, and tools/uxqual/pagedef is none of those. Instead it
// takes each generated *_ServiceDesc package variable (already typed
// grpc.ServiceDesc by the codegen that owns that import) and ranges over its
// exported Methods/Streams fields with ":=" everywhere: Go never requires
// importing a package merely to read the exported fields of a value you
// already hold, only to spell that package's type name, and this file never
// spells grpc.ServiceDesc or grpc.MethodDesc.
//
// journeyServiceRPCs, intentServiceRPCs, registryServiceRPCs, and
// adminServiceRPCs are the four "registered services" the WEB-002 todo
// names (intents/registry/admin/journey). Any RPC a DataBinding or
// ActionRef names must appear in one of them.

// JourneyServiceName is the journey service's fully qualified name, read
// from the generated ServiceDesc rather than retyped, so it can never drift
// from the .proto it was generated from.
var JourneyServiceName = journeyv1.JourneyService_ServiceDesc.ServiceName

// IntentServiceName is the intents service's fully qualified name.
var IntentServiceName = intentsv1.IntentService_ServiceDesc.ServiceName

// RegistryServiceName is the registry service's fully qualified name.
var RegistryServiceName = registryv1.RegistryService_ServiceDesc.ServiceName

// AdminServiceName is the admin service's fully qualified name.
var AdminServiceName = adminv1.AdminService_ServiceDesc.ServiceName

// RPCRef builds the "<ServiceName>/<MethodName>" form every DataBinding.RPC
// and ActionRef.RPC is written in, from the generated service name rather
// than a hand-typed string literal.
func RPCRef(serviceName, method string) string {
	return serviceName + "/" + method
}

// serviceRPCs lists "<ServiceName>/<MethodName>" for every unary method and
// every streaming method a generated grpc.ServiceDesc registers.
func serviceMethods(serviceName string, methods []string, streams []string) []string {
	out := make([]string, 0, len(methods)+len(streams))
	for _, m := range methods {
		out = append(out, RPCRef(serviceName, m))
	}
	for _, s := range streams {
		out = append(out, RPCRef(serviceName, s))
	}
	return out
}

func journeyServiceRPCs() []string {
	desc := journeyv1.JourneyService_ServiceDesc
	methods := make([]string, 0, len(desc.Methods))
	for _, m := range desc.Methods {
		methods = append(methods, m.MethodName)
	}
	streams := make([]string, 0, len(desc.Streams))
	for _, s := range desc.Streams {
		streams = append(streams, s.StreamName)
	}
	return serviceMethods(desc.ServiceName, methods, streams)
}

func intentServiceRPCs() []string {
	desc := intentsv1.IntentService_ServiceDesc
	methods := make([]string, 0, len(desc.Methods))
	for _, m := range desc.Methods {
		methods = append(methods, m.MethodName)
	}
	streams := make([]string, 0, len(desc.Streams))
	for _, s := range desc.Streams {
		streams = append(streams, s.StreamName)
	}
	return serviceMethods(desc.ServiceName, methods, streams)
}

func registryServiceRPCs() []string {
	desc := registryv1.RegistryService_ServiceDesc
	methods := make([]string, 0, len(desc.Methods))
	for _, m := range desc.Methods {
		methods = append(methods, m.MethodName)
	}
	streams := make([]string, 0, len(desc.Streams))
	for _, s := range desc.Streams {
		streams = append(streams, s.StreamName)
	}
	return serviceMethods(desc.ServiceName, methods, streams)
}

func adminServiceRPCs() []string {
	desc := adminv1.AdminService_ServiceDesc
	methods := make([]string, 0, len(desc.Methods))
	for _, m := range desc.Methods {
		methods = append(methods, m.MethodName)
	}
	streams := make([]string, 0, len(desc.Streams))
	for _, s := range desc.Streams {
		streams = append(streams, s.StreamName)
	}
	return serviceMethods(desc.ServiceName, methods, streams)
}

// KnownRPCs returns the closed set of every RPC a PageDefinition may bind
// or act on: every method (unary or streaming) the generated ServiceDesc of
// the journey, intents, registry, and admin services registers, keyed by
// "<ServiceName>/<MethodName>".
func KnownRPCs() map[string]bool {
	out := map[string]bool{}
	for _, list := range [][]string{
		journeyServiceRPCs(),
		intentServiceRPCs(),
		registryServiceRPCs(),
		adminServiceRPCs(),
	} {
		for _, rpc := range list {
			out[rpc] = true
		}
	}
	return out
}
