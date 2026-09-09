package binding

import (
	"sort"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
)

// This file is the whole of this package's coupling to generated code, and
// it is read-only, exactly as tools/uxqual/pagedef/rpcregistry.go is. It
// never imports google.golang.org/grpc: LIB-003 (definitions/architecture/
// library-firewall.yaml) confines that module to gen/, internal/transport,
// internal/intent/protomap, internal/engines/wire and the composition
// roots, and internal/capability/binding is none of those. Every generated
// *_ServiceDesc package variable is already typed grpc.ServiceDesc by the
// codegen that owns that import; ranging over its exported Methods/Streams
// fields with ":=" reads them without this file ever spelling
// grpc.ServiceDesc or grpc.MethodDesc, because Go requires the import only
// to name a package's type, never to read the exported fields of a value
// you already hold.
//
// The four services are the four "registered services" BIND-001 names:
// intents, registry, admin and journey. A capability that binds to no
// method of any of them binds to nothing a caller can reach.

// IntentServiceName is the intents service's fully qualified name, read
// from the generated descriptor rather than retyped, so it can never drift
// from the .proto it was generated from.
var IntentServiceName = intentsv1.IntentService_ServiceDesc.ServiceName

// RegistryServiceName is the registry service's fully qualified name.
var RegistryServiceName = registryv1.RegistryService_ServiceDesc.ServiceName

// AdminServiceName is the admin service's fully qualified name.
var AdminServiceName = adminv1.AdminService_ServiceDesc.ServiceName

// JourneyServiceName is the journey service's fully qualified name.
var JourneyServiceName = journeyv1.JourneyService_ServiceDesc.ServiceName

// RegisteredServiceNames returns the four service names this package binds
// against, sorted.
func RegisteredServiceNames() []string {
	out := []string{AdminServiceName, IntentServiceName, JourneyServiceName, RegistryServiceName}
	sort.Strings(out)
	return out
}

// WireMethods returns every method — unary and server-streaming — of the
// four registered generated services, sorted by [WireMethod.Ref]. This is
// the closed set of wire descriptors a capability may bind to; a claim
// naming anything outside it is a [GapUnknownWireMethod].
func WireMethods() []WireMethod {
	out := make([]WireMethod, 0, 32)
	out = append(out, intentServiceMethods()...)
	out = append(out, registryServiceMethods()...)
	out = append(out, adminServiceMethods()...)
	out = append(out, journeyServiceMethods()...)
	sortWireMethods(out)
	return out
}

// WireMethodSet returns [WireMethods] keyed by [WireMethod.Ref].
func WireMethodSet() map[string]WireMethod {
	methods := WireMethods()
	out := make(map[string]WireMethod, len(methods))
	for _, m := range methods {
		out[m.Ref()] = m
	}
	return out
}

func sortWireMethods(methods []WireMethod) {
	sort.Slice(methods, func(i, j int) bool { return methods[i].Ref() < methods[j].Ref() })
}

func intentServiceMethods() []WireMethod {
	desc := intentsv1.IntentService_ServiceDesc
	out := make([]WireMethod, 0, len(desc.Methods)+len(desc.Streams))
	for _, m := range desc.Methods {
		out = append(out, WireMethod{ServiceFullName: desc.ServiceName, MethodName: m.MethodName})
	}
	for _, s := range desc.Streams {
		out = append(out, WireMethod{ServiceFullName: desc.ServiceName, MethodName: s.StreamName, Streaming: true})
	}
	return out
}

func registryServiceMethods() []WireMethod {
	desc := registryv1.RegistryService_ServiceDesc
	out := make([]WireMethod, 0, len(desc.Methods)+len(desc.Streams))
	for _, m := range desc.Methods {
		out = append(out, WireMethod{ServiceFullName: desc.ServiceName, MethodName: m.MethodName})
	}
	for _, s := range desc.Streams {
		out = append(out, WireMethod{ServiceFullName: desc.ServiceName, MethodName: s.StreamName, Streaming: true})
	}
	return out
}

func adminServiceMethods() []WireMethod {
	desc := adminv1.AdminService_ServiceDesc
	out := make([]WireMethod, 0, len(desc.Methods)+len(desc.Streams))
	for _, m := range desc.Methods {
		out = append(out, WireMethod{ServiceFullName: desc.ServiceName, MethodName: m.MethodName})
	}
	for _, s := range desc.Streams {
		out = append(out, WireMethod{ServiceFullName: desc.ServiceName, MethodName: s.StreamName, Streaming: true})
	}
	return out
}

func journeyServiceMethods() []WireMethod {
	desc := journeyv1.JourneyService_ServiceDesc
	out := make([]WireMethod, 0, len(desc.Methods)+len(desc.Streams))
	for _, m := range desc.Methods {
		out = append(out, WireMethod{ServiceFullName: desc.ServiceName, MethodName: m.MethodName})
	}
	for _, s := range desc.Streams {
		out = append(out, WireMethod{ServiceFullName: desc.ServiceName, MethodName: s.StreamName, Streaming: true})
	}
	return out
}
