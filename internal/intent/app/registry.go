package app

import (
	"context"
	"sort"

	capabilitiesv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/capabilities/v1"
	commonv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/intent/protomap"
	"github.com/monstercameron/hcm-next/internal/transport/envelope"
)

// ListIntentDefinitions publishes the compiled-in definition catalog.
//
// The registry profile is BOOTSTRAP: the build is the publication, so the list
// is the catalog this binary compiled, in catalog order, with no filtering
// step that could quietly serve a different set than the one the kernel
// resolves against.
func (s *IntentService) ListIntentDefinitions(ctx context.Context, req *registryv1.ListIntentDefinitionsRequest) (*registryv1.ListIntentDefinitionsResponse, error) {
	if _, _, ownedErr := caller(ctx); ownedErr != nil {
		return nil, ownedErr
	}
	defs := s.defs.Definitions()
	out := &registryv1.ListIntentDefinitionsResponse{
		IntentDefinitions: make([]*intentsv1.IntentDefinition, 0, len(defs)),
		Page:              &commonv1.PageResponse{},
	}
	for _, def := range defs {
		msg, err := protomap.DefinitionToProto(def)
		if err != nil {
			return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
				"the operation could not be completed").WithDiagnostic(err)
		}
		out.IntentDefinitions = append(out.IntentDefinitions, msg)
	}
	return out, nil
}

// GetIntentDefinition resolves one definition by (intent_type_id, version).
func (s *IntentService) GetIntentDefinition(ctx context.Context, req *registryv1.GetIntentDefinitionRequest) (*registryv1.GetIntentDefinitionResponse, error) {
	if _, _, ownedErr := caller(ctx); ownedErr != nil {
		return nil, ownedErr
	}
	def, ownedErr := s.resolveDefinition(req.GetDefinition(), false)
	if ownedErr != nil {
		return nil, ownedErr
	}
	msg, err := protomap.DefinitionToProto(def)
	if err != nil {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(err)
	}
	return &registryv1.GetIntentDefinitionResponse{IntentDefinition: msg}, nil
}

// ListCapabilities publishes the BOOTSTRAP capability table this cell serves.
func (s *IntentService) ListCapabilities(ctx context.Context, req *registryv1.ListCapabilitiesRequest) (*registryv1.ListCapabilitiesResponse, error) {
	if _, _, ownedErr := caller(ctx); ownedErr != nil {
		return nil, ownedErr
	}
	records := s.caps.List()
	sort.Slice(records, func(i, j int) bool {
		if records[i].Definition.ID != records[j].Definition.ID {
			return records[i].Definition.ID < records[j].Definition.ID
		}
		return records[i].Definition.Version < records[j].Definition.Version
	})
	out := &registryv1.ListCapabilitiesResponse{
		Capabilities: make([]*capabilitiesv1.CapabilityDefinition, 0, len(records)),
		Page:         &commonv1.PageResponse{},
	}
	for _, rec := range records {
		out.Capabilities = append(out.Capabilities, capabilityToProto(rec.Definition))
	}
	return out, nil
}

// GetCapability resolves one exact capability version.
func (s *IntentService) GetCapability(ctx context.Context, req *registryv1.GetCapabilityRequest) (*registryv1.GetCapabilityResponse, error) {
	if _, _, ownedErr := caller(ctx); ownedErr != nil {
		return nil, ownedErr
	}
	version := req.GetVersion()
	if version == 0 {
		version = bootstrapCapabilityVersion
	}
	rec, found := s.caps.Lookup(capability.Key{ID: req.GetCapabilityId(), Version: version})
	if !found {
		return nil, envelope.New(envelope.CodeNotFound, reasonCapabilityUnknown,
			"the resource does not exist or is not visible").
			WithViolation("capability_id", "no capability is visible at this identifier", "registry.visibility")
	}
	return &registryv1.GetCapabilityResponse{Capability: capabilityToProto(rec.Definition)}, nil
}

// capabilityToProto projects one published definition onto the wire contract.
func capabilityToProto(def capability.Definition) *capabilitiesv1.CapabilityDefinition {
	return &capabilitiesv1.CapabilityDefinition{
		CapabilityId:         def.ID,
		Version:              def.Version,
		OwnerDomain:          def.OwnerDomain,
		RequestSchema:        capabilitySchemaToProto(def.RequestSchema),
		ResponseSchema:       capabilitySchemaToProto(def.ResponseSchema),
		ErrorSchema:          capabilitySchemaToProto(def.ErrorSchema),
		SideEffectProfile:    capabilityEffectToProto(def.EffectClass),
		ReadData:             capabilityDataToProto(def.ReadData),
		WriteData:            capabilityDataToProto(def.WriteData),
		RiskClass:            def.RiskClass,
		IdempotencyPolicyRef: def.IdempotencyPolicyRef,
		AgentEligible:        def.AgentEligible,
	}
}

func capabilitySchemaToProto(s capability.SchemaRef) *capabilitiesv1.SchemaRef {
	return &capabilitiesv1.SchemaRef{
		SchemaId:         s.SchemaID,
		Version:          s.Version,
		ProtobufFullName: s.ProtobufFullName,
	}
}

func capabilityDataToProto(d capability.DataDomainFieldSet) *capabilitiesv1.DataDomainFieldSet {
	return &capabilitiesv1.DataDomainFieldSet{
		DataDomains: d.DataDomains,
		FieldPaths:  d.FieldPaths,
	}
}

// capabilityEffectToProto maps the capability effect class onto the wire
// profile. There is no default arm: a class the wire does not know is
// UNSPECIFIED, which a caller must treat as "do not invoke", rather than
// silently rendered as READ_ONLY.
func capabilityEffectToProto(e capability.EffectClass) capabilitiesv1.CapabilitySideEffectProfile {
	switch e {
	case capability.EffectPure:
		return capabilitiesv1.CapabilitySideEffectProfile_CAPABILITY_SIDE_EFFECT_PROFILE_PURE
	case capability.EffectReadOnly:
		return capabilitiesv1.CapabilitySideEffectProfile_CAPABILITY_SIDE_EFFECT_PROFILE_READ_ONLY
	case capability.EffectInternalMutation:
		return capabilitiesv1.CapabilitySideEffectProfile_CAPABILITY_SIDE_EFFECT_PROFILE_INTERNAL_MUTATION
	case capability.EffectExternalMutation:
		return capabilitiesv1.CapabilitySideEffectProfile_CAPABILITY_SIDE_EFFECT_PROFILE_EXTERNAL_MUTATION
	case capability.EffectIrreversibleExternalMutation:
		return capabilitiesv1.CapabilitySideEffectProfile_CAPABILITY_SIDE_EFFECT_PROFILE_IRREVERSIBLE_EXTERNAL_MUTATION
	default:
		return capabilitiesv1.CapabilitySideEffectProfile_CAPABILITY_SIDE_EFFECT_PROFILE_UNSPECIFIED
	}
}
