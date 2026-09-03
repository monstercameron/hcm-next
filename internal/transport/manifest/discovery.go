package manifest

import (
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/intent/definitions"
)

// EndpointDescriptor is one method's served-shape discovery row: exactly
// what a caller needs to find and call a method (or learn why it cannot
// yet), never the internal review metadata (rate budget refs, evidence
// policy refs, presence rules) that only the manifest itself and its own
// policy checker need.
type EndpointDescriptor struct {
	EndpointID       string         `json:"endpoint_id"`
	ServiceFullName  string         `json:"service_full_name"`
	MethodName       string         `json:"method_name"`
	OwnerDomain      string         `json:"owner_domain"`
	CapabilityRefs   []string       `json:"capability_definition_refs"`
	IntentBehavior   IntentBehavior `json:"intent_behavior"`
	RequestType      string         `json:"request_type"`
	ResponseType     string         `json:"response_type"`
	GRPCProcedure    string         `json:"grpc_procedure"`
	HTTPMethod       string         `json:"http_method"`
	HTTPPathTemplate string         `json:"http_path_template"`
	Disposition      Disposition    `json:"disposition"`
	// DispositionReason is included deliberately: a caller who cannot yet
	// invoke SubmitIntent/CancelIntent/SupersedeIntent is told why
	// (REFUSED_P1A, and the released reason), rather than being left to
	// guess between "not authorized", "not implemented" and "unstable".
	DispositionReason string `json:"disposition_reason"`
}

// CapabilityDescriptor is one BOOTSTRAP capability's served-shape discovery
// row.
type CapabilityDescriptor struct {
	CapabilityID  string `json:"capability_id"`
	Version       uint32 `json:"version"`
	OwnerDomain   string `json:"owner_domain"`
	EffectClass   string `json:"effect_class"`
	RiskClass     string `json:"risk_class"`
	AgentEligible bool   `json:"agent_eligible"`
}

// IntentDefinitionDescriptor is one catalog definition's served-shape
// discovery row.
type IntentDefinitionDescriptor struct {
	DefinitionRef string `json:"definition_ref"`
	DisplayName   string `json:"display_name"`
	OwnerDomain   string `json:"owner_domain"`
	Family        string `json:"family"`
	Release       string `json:"release"`
	RiskClass     string `json:"risk_class"`
}

// DiscoveryDocument is the API-001 served shape: a self-describing
// discovery document a gRPC reflection or HTTP discovery surface can
// publish verbatim, derived purely from an [EndpointManifest] plus the
// capability and intent-definition inputs it was built from. Rendering it
// has no side effect and binds to no transport; wiring a live server to
// serve this shape is a different lane's work
// (planning/specs/http-grpc-endpoint-contract.md "Non-goals": "gRPC
// reflection and HTTP discovery never reveal tenant-specific unavailable
// capabilities to an unauthorized caller" is an authorization-time
// filtering concern for that wiring, not a property of this pure render).
type DiscoveryDocument struct {
	// ManifestDigest is the [EndpointManifest.Digest] this document was
	// rendered from, so a consumer can detect a stale cached copy.
	ManifestDigest    string                       `json:"manifest_digest"`
	Endpoints         []EndpointDescriptor         `json:"endpoints"`
	Capabilities      []CapabilityDescriptor       `json:"capabilities"`
	IntentDefinitions []IntentDefinitionDescriptor `json:"intent_definitions"`
}

// RenderDiscoveryDocument is the API-001 pure function: given an already
// built manifest and the capability/intent-definition inputs it names, it
// projects the served shape with no I/O, no clock read and no global state.
// Calling it twice on the same inputs (even concurrently:
// [TestTodo_API_001_Race]) returns equal documents.
func RenderDiscoveryDocument(m *EndpointManifest, defs []intent.Definition, records []capability.Record) (*DiscoveryDocument, error) {
	if m == nil {
		return nil, fmt.Errorf("manifest: RenderDiscoveryDocument: nil manifest")
	}
	digest, err := m.Digest()
	if err != nil {
		return nil, err
	}

	doc := &DiscoveryDocument{ManifestDigest: digest}

	for _, e := range m.Endpoints {
		doc.Endpoints = append(doc.Endpoints, EndpointDescriptor{
			EndpointID:        e.EndpointID,
			ServiceFullName:   e.ServiceFullName,
			MethodName:        e.MethodName,
			OwnerDomain:       e.OwnerDomain,
			CapabilityRefs:    append([]string(nil), e.CapabilityRefs...),
			IntentBehavior:    e.IntentBehavior,
			RequestType:       e.RequestType,
			ResponseType:      e.ResponseType,
			GRPCProcedure:     e.GRPCProcedure,
			HTTPMethod:        e.HTTPMethod,
			HTTPPathTemplate:  e.HTTPPathTemplate,
			Disposition:       e.Disposition,
			DispositionReason: e.DispositionReason,
		})
	}
	sort.Slice(doc.Endpoints, func(i, j int) bool { return doc.Endpoints[i].EndpointID < doc.Endpoints[j].EndpointID })

	for _, rec := range records {
		doc.Capabilities = append(doc.Capabilities, CapabilityDescriptor{
			CapabilityID:  rec.Definition.ID,
			Version:       rec.Definition.Version,
			OwnerDomain:   rec.Definition.OwnerDomain,
			EffectClass:   string(rec.Definition.EffectClass),
			RiskClass:     rec.Definition.RiskClass,
			AgentEligible: rec.Definition.AgentEligible,
		})
	}
	sort.Slice(doc.Capabilities, func(i, j int) bool {
		if doc.Capabilities[i].CapabilityID != doc.Capabilities[j].CapabilityID {
			return doc.Capabilities[i].CapabilityID < doc.Capabilities[j].CapabilityID
		}
		return doc.Capabilities[i].Version < doc.Capabilities[j].Version
	})

	for _, d := range defs {
		doc.IntentDefinitions = append(doc.IntentDefinitions, IntentDefinitionDescriptor{
			DefinitionRef: d.Ref.String(),
			DisplayName:   d.DisplayName,
			OwnerDomain:   d.OwnerDomain,
			Family:        d.Family.String(),
			Release:       d.Release.String(),
			RiskClass:     d.RiskClass,
		})
	}
	sort.Slice(doc.IntentDefinitions, func(i, j int) bool {
		return doc.IntentDefinitions[i].DefinitionRef < doc.IntentDefinitions[j].DefinitionRef
	})

	return doc, nil
}

// RenderDefaultDiscoveryDocument is [RenderDiscoveryDocument] wired to the
// production manifest, capability and intent-definition sources.
func RenderDefaultDiscoveryDocument() (*DiscoveryDocument, error) {
	m, err := Build()
	if err != nil {
		return nil, err
	}
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		return nil, err
	}
	return RenderDiscoveryDocument(m, definitions.All(), registry.List())
}
