package capability

import (
	"sync"

	capabilitiesv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/capabilities/v1"
	"github.com/monstercameron/hcm-next/internal/kernel/canonical"
	"github.com/monstercameron/hcm-next/internal/kernel/digest"
)

// A suitable canonical profile exists for a Definition's digest:
// hcmnext.capabilities.v1.CapabilityDefinition is exactly the
// capability-registry-and-lifecycle.md "core" manifest shape (capability
// id+version, owner domain, request/response/error schema refs, side-effect
// profile, read/write data domains, risk class, idempotency policy, agent
// eligibility). The additional governance references CAP-001 requires before
// publication (AuthZ/legal/entitlement/SLO/test) are validation-time
// completeness checks, not part of the manifest schema the spec defines, so
// they are not material to this digest - matching the spec's own rule that an
// extension is added to the manifest only when a consumer reads it.
const (
	digestProfileID      = "hcmnext.capability.CapabilityDefinition"
	digestProfileVersion = 1
	digestSchemaID       = "hcmnext.capabilities.v1.CapabilityDefinition"
	digestSchemaVersion  = 1
)

var (
	digestRegistryOnce sync.Once
	digestRegistry     *digest.Registry
	digestRegistryErr  error
)

func sharedDigestRegistry() (*digest.Registry, error) {
	digestRegistryOnce.Do(func() {
		r := digest.NewRegistry()
		err := r.RegisterProfile(canonical.Profile{
			ID:            digestProfileID,
			Version:       digestProfileVersion,
			SchemaID:      digestSchemaID,
			SchemaVersion: digestSchemaVersion,
			MessageName:   (&capabilitiesv1.CapabilityDefinition{}).ProtoReflect().Descriptor().FullName(),
			Material: []string{
				"capability_id",
				"version",
				"owner_domain",
				"request_schema.schema_id",
				"request_schema.version",
				"request_schema.protobuf_full_name",
				"response_schema.schema_id",
				"response_schema.version",
				"response_schema.protobuf_full_name",
				"error_schema.schema_id",
				"error_schema.version",
				"error_schema.protobuf_full_name",
				"side_effect_profile",
				"read_data.data_domains",
				"read_data.field_paths",
				"write_data.data_domains",
				"write_data.field_paths",
				"risk_class",
				"idempotency_policy_ref",
				"agent_eligible",
			},
			RejectUnknownFields: true,
		}, digest.ScopeSpec{})
		digestRegistry, digestRegistryErr = r, err
	})
	return digestRegistry, digestRegistryErr
}

func toEffectProto(e EffectClass) capabilitiesv1.CapabilitySideEffectProfile {
	switch e {
	case EffectPure:
		return capabilitiesv1.CapabilitySideEffectProfile_CAPABILITY_SIDE_EFFECT_PROFILE_PURE
	case EffectReadOnly:
		return capabilitiesv1.CapabilitySideEffectProfile_CAPABILITY_SIDE_EFFECT_PROFILE_READ_ONLY
	case EffectInternalMutation:
		return capabilitiesv1.CapabilitySideEffectProfile_CAPABILITY_SIDE_EFFECT_PROFILE_INTERNAL_MUTATION
	case EffectExternalMutation:
		return capabilitiesv1.CapabilitySideEffectProfile_CAPABILITY_SIDE_EFFECT_PROFILE_EXTERNAL_MUTATION
	case EffectIrreversibleExternalMutation:
		return capabilitiesv1.CapabilitySideEffectProfile_CAPABILITY_SIDE_EFFECT_PROFILE_IRREVERSIBLE_EXTERNAL_MUTATION
	default:
		return capabilitiesv1.CapabilitySideEffectProfile_CAPABILITY_SIDE_EFFECT_PROFILE_UNSPECIFIED
	}
}

func toSchemaRefProto(s SchemaRef) *capabilitiesv1.SchemaRef {
	return &capabilitiesv1.SchemaRef{
		SchemaId:         s.SchemaID,
		Version:          s.Version,
		ProtobufFullName: s.ProtobufFullName,
	}
}

func toDomainProto(d DataDomainFieldSet) *capabilitiesv1.DataDomainFieldSet {
	return &capabilitiesv1.DataDomainFieldSet{
		DataDomains: append([]string(nil), d.DataDomains...),
		FieldPaths:  append([]string(nil), d.FieldPaths...),
	}
}

func toProto(d Definition) *capabilitiesv1.CapabilityDefinition {
	return &capabilitiesv1.CapabilityDefinition{
		CapabilityId:         d.ID,
		Version:              d.Version,
		OwnerDomain:          d.OwnerDomain,
		RequestSchema:        toSchemaRefProto(d.RequestSchema),
		ResponseSchema:       toSchemaRefProto(d.ResponseSchema),
		ErrorSchema:          toSchemaRefProto(d.ErrorSchema),
		SideEffectProfile:    toEffectProto(d.EffectClass),
		ReadData:             toDomainProto(d.ReadData),
		WriteData:            toDomainProto(d.WriteData),
		RiskClass:            d.RiskClass,
		IdempotencyPolicyRef: d.IdempotencyPolicyRef,
		AgentEligible:        d.AgentEligible,
	}
}

// Digest computes the canonical content digest of a definition's core
// manifest fields. It is deterministic - the same definition always yields
// the same digest bytes - and two definitions differing in any core field
// never collide (internal/kernel/digest, internal/kernel/canonical).
func Digest(d Definition) (string, error) {
	reg, err := sharedDigestRegistry()
	if err != nil {
		return "", err
	}
	ref, _, err := reg.Compute(toProto(d), digestProfileID)
	if err != nil {
		return "", err
	}
	return ref.Digest, nil
}
