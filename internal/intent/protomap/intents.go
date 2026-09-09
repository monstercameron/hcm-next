package protomap

import (
	"fmt"
	"slices"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// FamilyToProto encodes a kernel family.
func FamilyToProto(f intent.Family) (intentsv1.KernelFamily, error) {
	if !f.Valid() {
		return intentsv1.KernelFamily_KERNEL_FAMILY_UNSPECIFIED,
			fmt.Errorf("%w: %s is not one of the three kernel families", ErrUnknownEnum, f)
	}
	return intentsv1.KernelFamily(f), nil
}

// FamilyFromProto decodes a kernel family.
//
// The four reserved numbers are rejected by name. A record carrying 2 was
// written when PROCESS_REQUEST existed; saying so is more useful than saying
// "unknown", and silently mapping it onto CHANGE_REQUEST would rewrite history.
func FamilyFromProto(p intentsv1.KernelFamily) (intent.Family, error) {
	if name, reserved := intent.ReservedFamilyName(uint8(p)); reserved {
		return intent.FamilyUnspecified,
			fmt.Errorf("%w: %d is the reserved retired family %s", ErrUnknownEnum, p, name)
	}
	f := intent.Family(p)
	if !f.Valid() {
		return intent.FamilyUnspecified, fmt.Errorf("%w: KernelFamily(%d)", ErrUnknownEnum, p)
	}
	return f, nil
}

// MaturityToProto encodes a definition maturity.
func MaturityToProto(m intent.Maturity) (intentsv1.DefinitionMaturity, error) {
	if !m.Valid() {
		return intentsv1.DefinitionMaturity_DEFINITION_MATURITY_UNSPECIFIED,
			fmt.Errorf("%w: DefinitionMaturity(%d)", ErrUnknownEnum, m)
	}
	return intentsv1.DefinitionMaturity(m), nil
}

// MaturityFromProto decodes a definition maturity. Number 1 is reserved:
// CATALOGUED is retired, and a name below DRAFT_CONTRACT is not in the catalog.
func MaturityFromProto(p intentsv1.DefinitionMaturity) (intent.Maturity, error) {
	if p == 1 {
		return intent.MaturityUnspecified,
			fmt.Errorf("%w: 1 is the reserved retired maturity CATALOGUED", ErrUnknownEnum)
	}
	m := intent.Maturity(p)
	if !m.Valid() {
		return intent.MaturityUnspecified,
			fmt.Errorf("%w: DefinitionMaturity(%d)", ErrUnknownEnum, p)
	}
	return m, nil
}

// InitiatorToProto encodes an initiator kind.
func InitiatorToProto(i intent.Initiator) (intentsv1.InitiatorKind, error) {
	if !i.Valid() {
		return intentsv1.InitiatorKind_INITIATOR_KIND_UNSPECIFIED,
			fmt.Errorf("%w: InitiatorKind(%d)", ErrUnknownEnum, i)
	}
	return intentsv1.InitiatorKind(i), nil
}

// InitiatorFromProto decodes an initiator kind.
func InitiatorFromProto(p intentsv1.InitiatorKind) (intent.Initiator, error) {
	i := intent.Initiator(p)
	if !i.Valid() {
		return intent.InitiatorUnspecified, fmt.Errorf("%w: InitiatorKind(%d)", ErrUnknownEnum, p)
	}
	return i, nil
}

// ModeToProto encodes an execution mode.
func ModeToProto(m intent.Mode) (intentsv1.ExecutionMode, error) {
	if !m.Valid() {
		return intentsv1.ExecutionMode_EXECUTION_MODE_UNSPECIFIED,
			fmt.Errorf("%w: ExecutionMode(%d)", ErrUnknownEnum, m)
	}
	return intentsv1.ExecutionMode(m), nil
}

// ModeFromProto decodes an execution mode.
func ModeFromProto(p intentsv1.ExecutionMode) (intent.Mode, error) {
	m := intent.Mode(p)
	if !m.Valid() {
		return intent.ModeUnspecified, fmt.Errorf("%w: ExecutionMode(%d)", ErrUnknownEnum, p)
	}
	return m, nil
}

// SideEffectToProto encodes a side-effect profile.
func SideEffectToProto(s intent.SideEffect) (intentsv1.SideEffectProfile, error) {
	if !s.Valid() {
		return intentsv1.SideEffectProfile_SIDE_EFFECT_PROFILE_UNSPECIFIED,
			fmt.Errorf("%w: SideEffectProfile(%d)", ErrUnknownEnum, s)
	}
	return intentsv1.SideEffectProfile(s), nil
}

// SideEffectFromProto decodes a side-effect profile.
func SideEffectFromProto(p intentsv1.SideEffectProfile) (intent.SideEffect, error) {
	s := intent.SideEffect(p)
	if !s.Valid() {
		return intent.SideEffectUnspecified,
			fmt.Errorf("%w: SideEffectProfile(%d)", ErrUnknownEnum, p)
	}
	return s, nil
}

// DimensionsToProto encodes the five lifecycle dimensions.
func DimensionsToProto(d lifecycle.Dimensions) (*intentsv1.LifecycleDimensions, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return &intentsv1.LifecycleDimensions{
		Request:     intentsv1.RequestState(d.Request),
		Execution:   intentsv1.ExecutionState(d.Execution),
		Business:    intentsv1.BusinessState(d.Business),
		Consistency: intentsv1.ConsistencyState(d.Consistency),
		Obligation:  intentsv1.ObligationState(d.Obligation),
	}, nil
}

// DimensionsFromProto decodes the five lifecycle dimensions. Every value is
// checked against its declared set, so the reserved ObligationState number 6
// where DISPUTED used to sit fails rather than decoding as WAIVED-adjacent
// nonsense.
func DimensionsFromProto(p *intentsv1.LifecycleDimensions) (lifecycle.Dimensions, error) {
	if p == nil {
		return lifecycle.Dimensions{}, fmt.Errorf("%w: LifecycleDimensions", ErrNilMessage)
	}
	d := lifecycle.Dimensions{
		Request:     lifecycle.RequestState(p.GetRequest()),
		Execution:   lifecycle.ExecutionState(p.GetExecution()),
		Business:    lifecycle.BusinessState(p.GetBusiness()),
		Consistency: lifecycle.ConsistencyState(p.GetConsistency()),
		Obligation:  lifecycle.ObligationState(p.GetObligation()),
	}
	if err := d.Validate(); err != nil {
		return lifecycle.Dimensions{}, err
	}
	return d, nil
}

// SchemaRefToProto encodes a schema reference.
func SchemaRefToProto(s intent.SchemaRef) *intentsv1.SchemaReference {
	return &intentsv1.SchemaReference{
		SchemaId:         s.SchemaID,
		Version:          s.Version,
		ProtobufFullName: s.ProtobufFullName,
		DescriptorDigest: s.DescriptorDigest,
	}
}

// SchemaRefFromProto decodes a schema reference.
func SchemaRefFromProto(p *intentsv1.SchemaReference) (intent.SchemaRef, error) {
	if p == nil {
		return intent.SchemaRef{}, fmt.Errorf("%w: SchemaReference", ErrNilMessage)
	}
	s := intent.SchemaRef{
		SchemaID:         p.GetSchemaId(),
		Version:          p.GetVersion(),
		ProtobufFullName: p.GetProtobufFullName(),
		DescriptorDigest: p.GetDescriptorDigest(),
	}
	if err := s.Validate(); err != nil {
		return intent.SchemaRef{}, err
	}
	return s, nil
}

// DefinitionRefToProto encodes a definition reference.
func DefinitionRefToProto(r intent.Ref) (*intentsv1.DefinitionReference, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &intentsv1.DefinitionReference{IntentTypeId: r.TypeID, Version: r.Version}, nil
}

// DefinitionRefFromProto decodes a definition reference.
func DefinitionRefFromProto(p *intentsv1.DefinitionReference) (intent.Ref, error) {
	if p == nil {
		return intent.Ref{}, fmt.Errorf("%w: DefinitionReference", ErrNilMessage)
	}
	r := intent.Ref{TypeID: p.GetIntentTypeId(), Version: p.GetVersion()}
	if err := r.Validate(); err != nil {
		return intent.Ref{}, err
	}
	return r, nil
}

// PrincipalToProto encodes a principal reference.
func PrincipalToProto(p intent.PrincipalReference) (*intentsv1.PrincipalReference, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	kind, err := InitiatorToProto(p.Kind)
	if err != nil {
		return nil, err
	}
	return &intentsv1.PrincipalReference{
		PrincipalId:          p.PrincipalID,
		Kind:                 kind,
		IdentityAssuranceRef: p.IdentityAssuranceRef,
	}, nil
}

// PrincipalFromProto decodes a principal reference.
func PrincipalFromProto(p *intentsv1.PrincipalReference) (intent.PrincipalReference, error) {
	if p == nil {
		return intent.PrincipalReference{}, fmt.Errorf("%w: PrincipalReference", ErrNilMessage)
	}
	kind, err := InitiatorFromProto(p.GetKind())
	if err != nil {
		return intent.PrincipalReference{}, err
	}
	out := intent.PrincipalReference{
		PrincipalID:          p.GetPrincipalId(),
		Kind:                 kind,
		IdentityAssuranceRef: p.GetIdentityAssuranceRef(),
	}
	if err := out.Validate(); err != nil {
		return intent.PrincipalReference{}, err
	}
	return out, nil
}

func delegationToProto(d intent.DelegationReference) *intentsv1.DelegationReference {
	return &intentsv1.DelegationReference{
		DelegationId:          d.DelegationID,
		DelegatingPrincipalId: d.DelegatingPrincipalID,
		DelegatedPrincipalId:  d.DelegatedPrincipalID,
		AuthorityDigest:       d.AuthorityDigest,
	}
}

func delegationFromProto(p *intentsv1.DelegationReference) (intent.DelegationReference, error) {
	if p == nil {
		return intent.DelegationReference{}, fmt.Errorf("%w: DelegationReference", ErrNilMessage)
	}
	d := intent.DelegationReference{
		DelegationID:          p.GetDelegationId(),
		DelegatingPrincipalID: p.GetDelegatingPrincipalId(),
		DelegatedPrincipalID:  p.GetDelegatedPrincipalId(),
		AuthorityDigest:       p.GetAuthorityDigest(),
	}
	if err := d.Validate(); err != nil {
		return intent.DelegationReference{}, err
	}
	return d, nil
}

func subjectToProto(s intent.SubjectReference) *intentsv1.SubjectReference {
	return &intentsv1.SubjectReference{
		SubjectKind:     s.Kind,
		SubjectId:       s.SubjectID,
		AuthorityDomain: s.AuthorityDomain,
	}
}

func subjectFromProto(p *intentsv1.SubjectReference) (intent.SubjectReference, error) {
	if p == nil {
		return intent.SubjectReference{}, fmt.Errorf("%w: SubjectReference", ErrNilMessage)
	}
	s := intent.SubjectReference{
		Kind:            p.GetSubjectKind(),
		SubjectID:       p.GetSubjectId(),
		AuthorityDomain: p.GetAuthorityDomain(),
	}
	if err := s.Validate(); err != nil {
		return intent.SubjectReference{}, err
	}
	return s, nil
}

// PayloadToProto encodes a typed payload together with the digest reference it
// carries. A payload whose digest has not been minted yet passes a zero
// reference, which encodes as an empty CanonicalDigestReference rather than as
// a fabricated one.
func PayloadToProto(p intent.TypedPayload, ref digest.Reference) *intentsv1.TypedPayload {
	return &intentsv1.TypedPayload{
		Schema:            SchemaRefToProto(p.Schema),
		ProtobufWireBytes: slices.Clone(p.WireBytes),
		CanonicalDigest:   ref.ToProto(),
	}
}

// PayloadFromProto decodes a typed payload.
func PayloadFromProto(p *intentsv1.TypedPayload) (intent.TypedPayload, error) {
	if p == nil {
		return intent.TypedPayload{}, fmt.Errorf("%w: TypedPayload", ErrNilMessage)
	}
	schema, err := SchemaRefFromProto(p.GetSchema())
	if err != nil {
		return intent.TypedPayload{}, err
	}
	return intent.TypedPayload{
		Schema:    schema,
		WireBytes: slices.Clone(p.GetProtobufWireBytes()),
	}, nil
}

// ControlSnapshotsToProto encodes the control context.
func ControlSnapshotsToProto(c intent.ControlSnapshots) *intentsv1.ControlSnapshotReferences {
	return &intentsv1.ControlSnapshotReferences{
		CapabilityRegistryDigest:           c.CapabilityRegistryDigest,
		PolicyBundleDigest:                 c.PolicyBundleDigest,
		LegalContextDigest:                 c.LegalContextDigest,
		EntitlementDigest:                  c.EntitlementDigest,
		ReferenceDataDigest:                c.ReferenceDataDigest,
		WorkflowDefinitionDigest:           c.WorkflowDefinitionDigest,
		ConnectorConfigurationDigest:       c.ConnectorConfigurationDigest,
		ClassificationTaxonomyDigest:       c.ClassificationTaxonomyDigest,
		ClassificationLabelSetDigest:       c.ClassificationLabelSetDigest,
		ClassificationPropagationWatermark: c.ClassificationPropagationWatermark,
		DlpDecisionDigest:                  c.DLPDecisionDigest,
		DestinationTrustDigest:             c.DestinationTrustDigest,
		PurposeAndResidencyDigest:          c.PurposeAndResidencyDigest,
	}
}

// ControlSnapshotsFromProto decodes the control context.
func ControlSnapshotsFromProto(p *intentsv1.ControlSnapshotReferences) (intent.ControlSnapshots, error) {
	if p == nil {
		return intent.ControlSnapshots{}, fmt.Errorf("%w: ControlSnapshotReferences", ErrNilMessage)
	}
	return intent.ControlSnapshots{
		CapabilityRegistryDigest:           p.GetCapabilityRegistryDigest(),
		PolicyBundleDigest:                 p.GetPolicyBundleDigest(),
		LegalContextDigest:                 p.GetLegalContextDigest(),
		EntitlementDigest:                  p.GetEntitlementDigest(),
		ReferenceDataDigest:                p.GetReferenceDataDigest(),
		WorkflowDefinitionDigest:           p.GetWorkflowDefinitionDigest(),
		ConnectorConfigurationDigest:       p.GetConnectorConfigurationDigest(),
		ClassificationTaxonomyDigest:       p.GetClassificationTaxonomyDigest(),
		ClassificationLabelSetDigest:       p.GetClassificationLabelSetDigest(),
		ClassificationPropagationWatermark: p.GetClassificationPropagationWatermark(),
		DLPDecisionDigest:                  p.GetDlpDecisionDigest(),
		DestinationTrustDigest:             p.GetDestinationTrustDigest(),
		PurposeAndResidencyDigest:          p.GetPurposeAndResidencyDigest(),
	}, nil
}

// DefinitionToProto encodes an intent definition.
//
// Release and effect class have no Protobuf counterpart: they are delivery
// facts the registry enforces, not wire state. A caller that needs them across
// a wire boundary must resolve the definition from the registry rather than
// trust a transmitted copy, which is why the round-trip below is exact for
// every field the contract does carry.
func DefinitionToProto(d intent.Definition) (*intentsv1.IntentDefinition, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	ref, err := DefinitionRefToProto(d.Ref)
	if err != nil {
		return nil, err
	}
	family, err := FamilyToProto(d.Family)
	if err != nil {
		return nil, err
	}
	maturity, err := MaturityToProto(d.Maturity)
	if err != nil {
		return nil, err
	}
	sideEffect, err := SideEffectToProto(d.SideEffect)
	if err != nil {
		return nil, err
	}
	initiators := make([]intentsv1.InitiatorKind, 0, len(d.AllowedInitiators))
	for _, i := range d.AllowedInitiators {
		v, err := InitiatorToProto(i)
		if err != nil {
			return nil, err
		}
		initiators = append(initiators, v)
	}
	modes := make([]intentsv1.ExecutionMode, 0, len(d.AllowedModes))
	for _, m := range d.AllowedModes {
		v, err := ModeToProto(m)
		if err != nil {
			return nil, err
		}
		modes = append(modes, v)
	}
	out := &intentsv1.IntentDefinition{
		Reference:                 ref,
		DisplayName:               d.DisplayName,
		Description:               d.Description,
		OwnerPlane:                d.OwnerPlane,
		OwnerDomain:               d.OwnerDomain,
		KernelFamily:              family,
		Maturity:                  maturity,
		InputSchema:               SchemaRefToProto(d.InputSchema),
		ResultSchema:              SchemaRefToProto(d.ResultSchema),
		AllowedInitiators:         initiators,
		AllowedExecutionModes:     modes,
		SideEffectProfile:         sideEffect,
		RiskClass:                 d.RiskClass,
		DataClassificationFloor:   d.DataClassificationFloor,
		SubjectKinds:              slices.Clone(d.SubjectKinds),
		RequiredCapabilityRefs:    slices.Clone(d.RequiredCapabilities),
		GovernanceRequirementRefs: slices.Clone(d.GovernanceRequirements),
		PreconditionRefs:          slices.Clone(d.Preconditions),
		InvariantRefs:             slices.Clone(d.Invariants),
		IdempotencyPolicyRef:      d.IdempotencyScope,
		ConflictFootprintRuleRef:  d.ConflictFootprintRule,
		ProposalBindingRuleRef:    d.ProposalBindingRule,
		RevalidationRuleRef:       d.RevalidationRule,
		CancellationRuleRef:       d.CancellationRule,
		CompensationRuleRef:       d.CompensationRule,
		EvidenceRuleRef:           d.EvidenceRule,
		RetentionClass:            d.RetentionClass,
		OutcomeContractRef:        d.OutcomeContract,
		SloClass:                  d.SLOClass,
		AvailabilityPolicyRef:     d.AvailabilityPolicy,
		PhaseDepth:                d.PhaseDepth,
	}
	if d.PopulationScope != nil {
		scope := d.PopulationScope.ScopeRef
		out.PopulationScopeRef = &scope
	}
	return out, nil
}

// DefinitionFromProto decodes an intent definition. Fields the contract does
// not carry — release, effect class, required inputs, allowed transitions,
// applicable negative states, approval and closure policy — come back zeroed;
// resolve the definition from the registry when those matter.
func DefinitionFromProto(p *intentsv1.IntentDefinition) (intent.Definition, error) {
	if p == nil {
		return intent.Definition{}, fmt.Errorf("%w: IntentDefinition", ErrNilMessage)
	}
	ref, err := DefinitionRefFromProto(p.GetReference())
	if err != nil {
		return intent.Definition{}, err
	}
	family, err := FamilyFromProto(p.GetKernelFamily())
	if err != nil {
		return intent.Definition{}, err
	}
	maturity, err := MaturityFromProto(p.GetMaturity())
	if err != nil {
		return intent.Definition{}, err
	}
	sideEffect, err := SideEffectFromProto(p.GetSideEffectProfile())
	if err != nil {
		return intent.Definition{}, err
	}
	inputSchema, err := SchemaRefFromProto(p.GetInputSchema())
	if err != nil {
		return intent.Definition{}, err
	}
	resultSchema, err := SchemaRefFromProto(p.GetResultSchema())
	if err != nil {
		return intent.Definition{}, err
	}
	initiators := make([]intent.Initiator, 0, len(p.GetAllowedInitiators()))
	for _, i := range p.GetAllowedInitiators() {
		v, err := InitiatorFromProto(i)
		if err != nil {
			return intent.Definition{}, err
		}
		initiators = append(initiators, v)
	}
	modes := make([]intent.Mode, 0, len(p.GetAllowedExecutionModes()))
	for _, m := range p.GetAllowedExecutionModes() {
		v, err := ModeFromProto(m)
		if err != nil {
			return intent.Definition{}, err
		}
		modes = append(modes, v)
	}
	return intent.Definition{
		Ref:                     ref,
		DisplayName:             p.GetDisplayName(),
		Description:             p.GetDescription(),
		OwnerPlane:              p.GetOwnerPlane(),
		OwnerDomain:             p.GetOwnerDomain(),
		Family:                  family,
		Maturity:                maturity,
		SideEffect:              sideEffect,
		InputSchema:             inputSchema,
		ResultSchema:            resultSchema,
		AllowedInitiators:       initiators,
		AllowedModes:            modes,
		RiskClass:               p.GetRiskClass(),
		DataClassificationFloor: p.GetDataClassificationFloor(),
		SubjectKinds:            slices.Clone(p.GetSubjectKinds()),
		RequiredCapabilities:    slices.Clone(p.GetRequiredCapabilityRefs()),
		GovernanceRequirements:  slices.Clone(p.GetGovernanceRequirementRefs()),
		Preconditions:           slices.Clone(p.GetPreconditionRefs()),
		Invariants:              slices.Clone(p.GetInvariantRefs()),
		IdempotencyScope:        p.GetIdempotencyPolicyRef(),
		ConflictFootprintRule:   p.GetConflictFootprintRuleRef(),
		ProposalBindingRule:     p.GetProposalBindingRuleRef(),
		RevalidationRule:        p.GetRevalidationRuleRef(),
		CancellationRule:        p.GetCancellationRuleRef(),
		CompensationRule:        p.GetCompensationRuleRef(),
		EvidenceRule:            p.GetEvidenceRuleRef(),
		RetentionClass:          p.GetRetentionClass(),
		OutcomeContract:         p.GetOutcomeContractRef(),
		SLOClass:                p.GetSloClass(),
		AvailabilityPolicy:      p.GetAvailabilityPolicyRef(),
		PhaseDepth:              p.GetPhaseDepth(),
	}, nil
}

// InstanceToProto encodes an intent instance envelope.
func InstanceToProto(i intent.Instance) (*intentsv1.IntentInstance, error) {
	defRef, err := DefinitionRefToProto(i.Definition)
	if err != nil {
		return nil, err
	}
	initiator, err := PrincipalToProto(i.Initiator)
	if err != nil {
		return nil, err
	}
	mode, err := ModeToProto(i.ExecutionMode)
	if err != nil {
		return nil, err
	}
	dims, err := DimensionsToProto(i.Lifecycle)
	if err != nil {
		return nil, err
	}
	chain := make([]*intentsv1.DelegationReference, 0, len(i.DelegationChain))
	for _, d := range i.DelegationChain {
		chain = append(chain, delegationToProto(d))
	}
	subjects := make([]*intentsv1.SubjectReference, 0, len(i.Subjects))
	for _, s := range i.Subjects {
		subjects = append(subjects, subjectToProto(s))
	}
	revisions := make([]*intentsv1.ProposalRevision, 0, len(i.ProposalRevisions))
	for _, rev := range i.ProposalRevisions {
		p, err := ProposalToProto(rev)
		if err != nil {
			return nil, err
		}
		revisions = append(revisions, p)
	}
	out := &intentsv1.IntentInstance{
		IntentId:                      i.IntentID,
		Definition:                    defRef,
		TenantId:                      string(i.Tenant),
		OrganizationScopeId:           i.OrganizationScopeID,
		BillingAccountId:              clonePtr(i.BillingAccountID),
		Initiator:                     initiator,
		DelegationChain:               chain,
		Purpose:                       i.Purpose,
		Subjects:                      subjects,
		Request:                       PayloadToProto(i.Request, digest.Reference{}),
		IdempotencyKey:                i.IdempotencyKey,
		CorrelationId:                 i.CorrelationID,
		CausationId:                   clonePtr(i.CausationID),
		TraceId:                       i.TraceID,
		Classification:                i.Classification,
		RetentionClass:                i.RetentionClass,
		ControlSnapshots:              ControlSnapshotsToProto(i.ControlSnapshots),
		CanonicalRequestDigest:        i.CanonicalRequestDigest.ToProto(),
		Lifecycle:                     dims,
		CreatedAt:                     InstantToProto(i.CreatedAt),
		ExecutionMode:                 mode,
		InstanceVersion:               i.InstanceVersion,
		RecordedAt:                    InstantToProto(i.RecordedAt),
		LastTransitionAt:              InstantToProto(i.LastTransitionAt),
		OriginEventRef:                clonePtr(i.OriginEventRef),
		SourceAuthoritySnapshotDigest: i.SourceAuthoritySnapshotDigest,
		RiskContextDigest:             i.RiskContextDigest,
		ProposalRevisions:             revisions,
	}
	if i.RequestedEffectiveAt != nil {
		out.RequestedEffectiveAt = InstantToProto(*i.RequestedEffectiveAt)
	}
	return out, nil
}

// InstanceFromProto decodes an intent instance envelope.
func InstanceFromProto(p *intentsv1.IntentInstance) (intent.Instance, error) {
	if p == nil {
		return intent.Instance{}, fmt.Errorf("%w: IntentInstance", ErrNilMessage)
	}
	defRef, err := DefinitionRefFromProto(p.GetDefinition())
	if err != nil {
		return intent.Instance{}, err
	}
	initiator, err := PrincipalFromProto(p.GetInitiator())
	if err != nil {
		return intent.Instance{}, err
	}
	mode, err := ModeFromProto(p.GetExecutionMode())
	if err != nil {
		return intent.Instance{}, err
	}
	dims, err := DimensionsFromProto(p.GetLifecycle())
	if err != nil {
		return intent.Instance{}, err
	}
	request, err := PayloadFromProto(p.GetRequest())
	if err != nil {
		return intent.Instance{}, err
	}
	snapshots, err := ControlSnapshotsFromProto(p.GetControlSnapshots())
	if err != nil {
		return intent.Instance{}, err
	}
	requestDigest, err := digest.FromProto(p.GetCanonicalRequestDigest())
	if err != nil {
		return intent.Instance{}, err
	}
	var chain []intent.DelegationReference
	for _, d := range p.GetDelegationChain() {
		hop, err := delegationFromProto(d)
		if err != nil {
			return intent.Instance{}, err
		}
		chain = append(chain, hop)
	}
	var subjects []intent.SubjectReference
	for _, s := range p.GetSubjects() {
		sub, err := subjectFromProto(s)
		if err != nil {
			return intent.Instance{}, err
		}
		subjects = append(subjects, sub)
	}
	var revisions []intent.ProposalRevision
	for _, rev := range p.GetProposalRevisions() {
		r, err := ProposalFromProto(rev)
		if err != nil {
			return intent.Instance{}, err
		}
		revisions = append(revisions, r)
	}
	createdAt, err := InstantFromProto(p.GetCreatedAt())
	if err != nil {
		return intent.Instance{}, err
	}
	recordedAt, err := InstantFromProto(p.GetRecordedAt())
	if err != nil {
		return intent.Instance{}, err
	}
	lastTransitionAt, err := InstantFromProto(p.GetLastTransitionAt())
	if err != nil {
		return intent.Instance{}, err
	}
	out := intent.Instance{
		IntentID:                      p.GetIntentId(),
		Definition:                    defRef,
		Tenant:                        values.TenantId(p.GetTenantId()),
		OrganizationScopeID:           p.GetOrganizationScopeId(),
		BillingAccountID:              clonePtr(p.BillingAccountId),
		Initiator:                     initiator,
		DelegationChain:               chain,
		Purpose:                       p.GetPurpose(),
		Subjects:                      subjects,
		Request:                       request,
		IdempotencyKey:                p.GetIdempotencyKey(),
		CorrelationID:                 p.GetCorrelationId(),
		CausationID:                   clonePtr(p.CausationId),
		TraceID:                       p.GetTraceId(),
		Classification:                p.GetClassification(),
		RetentionClass:                p.GetRetentionClass(),
		ControlSnapshots:              snapshots,
		CanonicalRequestDigest:        requestDigest,
		Lifecycle:                     dims,
		CreatedAt:                     createdAt,
		RecordedAt:                    recordedAt,
		LastTransitionAt:              lastTransitionAt,
		ExecutionMode:                 mode,
		InstanceVersion:               p.GetInstanceVersion(),
		OriginEventRef:                clonePtr(p.OriginEventRef),
		SourceAuthoritySnapshotDigest: p.GetSourceAuthoritySnapshotDigest(),
		RiskContextDigest:             p.GetRiskContextDigest(),
		ProposalRevisions:             revisions,
	}
	if p.RequestedEffectiveAt != nil {
		at, err := InstantFromProto(p.GetRequestedEffectiveAt())
		if err != nil {
			return intent.Instance{}, err
		}
		out.RequestedEffectiveAt = &at
	}
	return out, nil
}

// ProposalToProto encodes a proposal revision.
//
// The structured material content travels as the payload's byte field, using
// the kernel's deterministic material encoding: schema/proto has no message for
// planned writes, effects, child bindings and reservations yet. The PROPOSAL
// canonicalization profile digests exactly those bytes plus the revision
// identity, which is why control snapshots — encoded here as evidence — stay
// outside the digest an approval binds.
func ProposalToProto(p intent.ProposalRevision) (*intentsv1.ProposalRevision, error) {
	createdBy, err := PrincipalToProto(p.CreatedBy)
	if err != nil {
		return nil, err
	}
	out := &intentsv1.ProposalRevision{
		ProposalRevisionId:     p.ProposalRevisionID,
		IntentId:               p.IntentID,
		Revision:               p.Revision,
		MaterialProposalDigest: p.MaterialDigest.ToProto(),
		Proposal:               PayloadToProto(p.MaterialPayload(), digest.Reference{}),
		ControlSnapshots:       ControlSnapshotsToProto(p.ControlSnapshots),
		CreatedBy:              createdBy,
		CreatedAt:              InstantToProto(p.CreatedAt),
		InvalidatorRefs:        slices.Clone(p.InvalidatorRefs),
	}
	out.SupersedesProposalRevisionId = clonePtr(p.SupersedesRevisionID)
	return out, nil
}

// ProposalFromProto decodes the proto-representable subset of a proposal
// revision: identity, digest, control snapshots and provenance. The structured
// material content is not decoded back out of its byte encoding — the kernel
// keeps the structured value, and the bytes exist so that the digest binds it.
func ProposalFromProto(p *intentsv1.ProposalRevision) (intent.ProposalRevision, error) {
	if p == nil {
		return intent.ProposalRevision{}, fmt.Errorf("%w: ProposalRevision", ErrNilMessage)
	}
	createdBy, err := PrincipalFromProto(p.GetCreatedBy())
	if err != nil {
		return intent.ProposalRevision{}, err
	}
	snapshots, err := ControlSnapshotsFromProto(p.GetControlSnapshots())
	if err != nil {
		return intent.ProposalRevision{}, err
	}
	materialDigest, err := digest.FromProto(p.GetMaterialProposalDigest())
	if err != nil {
		return intent.ProposalRevision{}, err
	}
	createdAt, err := InstantFromProto(p.GetCreatedAt())
	if err != nil {
		return intent.ProposalRevision{}, err
	}
	return intent.ProposalRevision{
		ProposalRevisionID:   p.GetProposalRevisionId(),
		IntentID:             p.GetIntentId(),
		Revision:             p.GetRevision(),
		ControlSnapshots:     snapshots,
		CreatedBy:            createdBy,
		CreatedAt:            createdAt,
		SupersedesRevisionID: clonePtr(p.SupersedesProposalRevisionId),
		InvalidatorRefs:      slices.Clone(p.GetInvalidatorRefs()),
		MaterialDigest:       materialDigest,
	}, nil
}

func clonePtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}
