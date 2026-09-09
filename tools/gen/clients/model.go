package clientsgen

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"

	// Blank imports link every hcmnext Protobuf package's descriptors into
	// protoregistry.GlobalFiles/GlobalTypes, which is how this generator
	// resolves a manifest row's Protobuf names to Go import paths and
	// types without hard-coding either. Only intents/v1 and registry/v1
	// currently publish a service the endpoint manifest names, but the
	// generator does not assume that stays true.
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/capabilities/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	_ "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
)

// DefaultManifestPath is the endpoint manifest [BuildModel] reads by
// default, relative to the repository root.
const DefaultManifestPath = "definitions/api/endpoint-manifest.json"

// goType is one resolved Go package/type reference, read from a Protobuf
// file descriptor's go_package option rather than assumed from its
// Protobuf package name.
type goType struct {
	ImportPath string
	Alias      string
	Name       string
}

// Qualified returns the type as it is written in Go source, e.g.
// "intentsv1.CreateIntentRequest".
func (t goType) Qualified() string { return t.Alias + "." + t.Name }

// methodModel is one generated client method.
type methodModel struct {
	Name        string
	Procedure   string
	Disposition string
	Request     goType
	Response    goType
}

// serviceModel is one generated client interface and its two backends.
type serviceModel struct {
	FullName    string
	ShortName   string // Protobuf service name, e.g. "IntentService"
	ClientName  string // generated interface name, e.g. "IntentClient"
	FieldPrefix string // unexported identifier stem, e.g. "intent"
	GoType      goType // the service's own generated Go package/type
	Methods     []methodModel
}

// Imports returns the deduplicated (alias, import path) pairs this
// service's file needs, sorted by alias. It is computed rather than assumed
// to be just [serviceModel.GoType] because nothing in the descriptor
// requires a method's request/response messages to live in the service's
// own file.
func (s serviceModel) Imports() []goType {
	seen := map[string]goType{}
	order := []string{}
	add := func(t goType) {
		if existing, ok := seen[t.Alias]; ok {
			if existing.ImportPath != t.ImportPath {
				panic(fmt.Sprintf("clientsgen: import alias %q claimed by both %q and %q", t.Alias, existing.ImportPath, t.ImportPath))
			}
			return
		}
		seen[t.Alias] = goType{ImportPath: t.ImportPath, Alias: t.Alias}
		order = append(order, t.Alias)
	}
	add(s.GoType)
	for _, m := range s.Methods {
		add(m.Request)
		add(m.Response)
	}
	sort.Strings(order)
	out := make([]goType, 0, len(order))
	for _, alias := range order {
		out = append(out, seen[alias])
	}
	return out
}

// BuildModel loads manifestPath and resolves it, through the Protobuf
// descriptor registry, into the ordered set of services this generator
// emits a client for. Services are sorted by full name and methods by name,
// so the result — and everything rendered from it — does not depend on the
// manifest file's own row order.
func BuildModel(manifestPath string) ([]serviceModel, error) {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("clientsgen: reading %s: %w", manifestPath, err)
	}
	var doc manifest.EndpointManifest
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("clientsgen: parsing %s: %w", manifestPath, err)
	}
	if len(doc.Endpoints) == 0 {
		return nil, fmt.Errorf("clientsgen: %s names no endpoints", manifestPath)
	}

	byService := map[string][]manifest.EndpointDefinition{}
	for _, e := range doc.Endpoints {
		byService[e.ServiceFullName] = append(byService[e.ServiceFullName], e)
	}

	serviceNames := make([]string, 0, len(byService))
	for name := range byService {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	services := make([]serviceModel, 0, len(serviceNames))
	for _, name := range serviceNames {
		sm, err := buildServiceModel(name, byService[name])
		if err != nil {
			return nil, err
		}
		services = append(services, sm)
	}
	return services, nil
}

// buildServiceModel resolves one service's manifest rows into a
// [serviceModel].
func buildServiceModel(fullName string, endpoints []manifest.EndpointDefinition) (serviceModel, error) {
	svcType, shortName, err := resolveServiceGoType(fullName)
	if err != nil {
		return serviceModel{}, err
	}
	base := strings.TrimSuffix(shortName, "Service")
	if base == "" || base == shortName {
		return serviceModel{}, fmt.Errorf("clientsgen: service %s is named %q, which does not end in \"Service\"", fullName, shortName)
	}

	sort.Slice(endpoints, func(i, j int) bool { return endpoints[i].MethodName < endpoints[j].MethodName })

	sm := serviceModel{
		FullName:    fullName,
		ShortName:   shortName,
		ClientName:  base + "Client",
		FieldPrefix: lowerFirst(base),
		GoType:      svcType,
	}
	for _, e := range endpoints {
		wantProcedure := "/" + fullName + "/" + e.MethodName
		if e.GRPCProcedure != wantProcedure {
			return serviceModel{}, fmt.Errorf("clientsgen: %s declares grpc_procedure %q, want %q", e.EndpointID, e.GRPCProcedure, wantProcedure)
		}
		reqType, err := resolveMessageGoType(e.RequestType)
		if err != nil {
			return serviceModel{}, fmt.Errorf("clientsgen: %s: %w", e.EndpointID, err)
		}
		respType, err := resolveMessageGoType(e.ResponseType)
		if err != nil {
			return serviceModel{}, fmt.Errorf("clientsgen: %s: %w", e.EndpointID, err)
		}
		sm.Methods = append(sm.Methods, methodModel{
			Name:        e.MethodName,
			Procedure:   e.GRPCProcedure,
			Disposition: string(e.Disposition),
			Request:     reqType,
			Response:    respType,
		})
	}
	if len(sm.Methods) == 0 {
		return serviceModel{}, fmt.Errorf("clientsgen: service %s has no methods in the manifest", fullName)
	}
	return sm, nil
}

// resolveServiceGoType finds fullName's Protobuf service descriptor and
// resolves the Go package it compiled to.
func resolveServiceGoType(fullName string) (goType, string, error) {
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(fullName))
	if err != nil {
		return goType{}, "", fmt.Errorf("clientsgen: resolving service %s: %w", fullName, err)
	}
	sd, ok := d.(protoreflect.ServiceDescriptor)
	if !ok {
		return goType{}, "", fmt.Errorf("clientsgen: %s is a %T, not a service", fullName, d)
	}
	t, err := goTypeFromFile(sd.ParentFile(), string(sd.Name()))
	if err != nil {
		return goType{}, "", err
	}
	return t, string(sd.Name()), nil
}

// resolveMessageGoType finds fullName's Protobuf message descriptor and
// resolves the Go package/type it compiled to.
func resolveMessageGoType(fullName string) (goType, error) {
	mt, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(fullName))
	if err != nil {
		return goType{}, fmt.Errorf("clientsgen: resolving message %s: %w", fullName, err)
	}
	desc := mt.Descriptor()
	return goTypeFromFile(desc.ParentFile(), string(desc.Name()))
}

// goTypeFromFile reads fd's go_package file option — the same option
// protoc-gen-go itself reads — and resolves name (a top-level Protobuf
// declaration in that file) to the Go import path, alias and identifier it
// compiled to.
func goTypeFromFile(fd protoreflect.FileDescriptor, name string) (goType, error) {
	opts, ok := fd.Options().(*descriptorpb.FileOptions)
	if !ok || opts.GetGoPackage() == "" {
		return goType{}, fmt.Errorf("clientsgen: %s declares no go_package option", fd.Path())
	}
	raw := opts.GetGoPackage()
	importPath, alias := raw, ""
	if i := strings.LastIndex(raw, ";"); i >= 0 {
		importPath, alias = raw[:i], raw[i+1:]
	} else if i := strings.LastIndex(raw, "/"); i >= 0 {
		alias = raw[i+1:]
	} else {
		alias = raw
	}
	if importPath == "" || alias == "" {
		return goType{}, fmt.Errorf("clientsgen: %s declares an unparsable go_package option %q", fd.Path(), raw)
	}
	return goType{ImportPath: importPath, Alias: alias, Name: name}, nil
}

// lowerFirst lower-cases s's first rune, leaving the rest untouched. It is
// ASCII-only because every input is a Go exported identifier stem.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'A' && s[0] <= 'Z' {
		return string(s[0]+('a'-'A')) + s[1:]
	}
	return s
}
