package intent

import (
	"slices"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"

	"github.com/monstercameron/hcm-next/internal/engines/wire/digest"
	"github.com/monstercameron/hcm-next/internal/intent/lifecycle"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// PrincipalReference identifies the acting principal. The kernel takes this
// value already derived from a verified server-side identity: it performs no
// authentication, and it never populates a principal from a caller-supplied
// field of the same name.
type PrincipalReference struct {
	PrincipalID          string
	Kind                 Initiator
	IdentityAssuranceRef string
}

// Validate rejects an unidentified or unassured principal.
func (p PrincipalReference) Validate() error {
	if p.PrincipalID == "" {
		return newError("Validate", "initiator.principal_id", ErrInvalidInstance,
			"principal reference has no id")
	}
	if !p.Kind.Valid() {
		return newError("Validate", "initiator.kind", ErrInvalidInstance,
			"principal %q has no initiator kind", p.PrincipalID)
	}
	if p.IdentityAssuranceRef == "" {
		return newError("Validate", "initiator.identity_assurance_ref", ErrInvalidInstance,
			"principal %q carries no identity assurance reference", p.PrincipalID)
	}
	return nil
}

// DelegationReference is one hop of a delegation chain.
type DelegationReference struct {
	DelegationID          string
	DelegatingPrincipalID string
	DelegatedPrincipalID  string
	AuthorityDigest       string
}

// Validate rejects a delegation hop that does not name both principals and the
// authority it was granted under.
func (d DelegationReference) Validate() error {
	switch {
	case d.DelegationID == "":
		return newError("Validate", "delegation_chain.delegation_id", ErrInvalidInstance,
			"delegation has no id")
	case d.DelegatingPrincipalID == "":
		return newError("Validate", "delegation_chain.delegating_principal_id", ErrInvalidInstance,
			"delegation %q names no delegating principal", d.DelegationID)
	case d.DelegatedPrincipalID == "":
		return newError("Validate", "delegation_chain.delegated_principal_id", ErrInvalidInstance,
			"delegation %q names no delegated principal", d.DelegationID)
	case d.AuthorityDigest == "":
		return newError("Validate", "delegation_chain.authority_digest", ErrInvalidInstance,
			"delegation %q records no authority digest", d.DelegationID)
	}
	return nil
}

// SubjectReference names one subject the intent is about.
type SubjectReference struct {
	Kind            string
	SubjectID       string
	AuthorityDomain string
}

// Validate rejects a subject with no kind, id or authority domain. Subjects are
// material — they are hashed into the canonical request digest — so they are
// held to the same text rules as every other material string.
func (s SubjectReference) Validate() error {
	for _, req := range []struct{ field, value string }{
		{"subjects.subject_kind", s.Kind},
		{"subjects.subject_id", s.SubjectID},
		{"subjects.authority_domain", s.AuthorityDomain},
	} {
		if err := requireCanonicalText(req.field, req.value); err != nil {
			return err
		}
	}
	return nil
}

// TypedPayload is a Protobuf payload plus the schema reference its descriptor
// must match. Arbitrary JSON, a map[string]any and a model prompt are never
// authoritative request payloads, so this type carries bytes and a schema and
// nothing else.
type TypedPayload struct {
	Schema    SchemaRef
	WireBytes []byte
}

// Validate rejects an untyped or empty payload.
func (p TypedPayload) Validate(field string) error {
	if err := p.Schema.Validate(); err != nil {
		return newError("Validate", field+".schema", ErrUntypedPayload, "%v", err)
	}
	if p.Schema.ProtobufFullName == "" {
		return newError("Validate", field+".schema.protobuf_full_name", ErrUntypedPayload,
			"schema %s names no Protobuf message", p.Schema)
	}
	if len(p.WireBytes) == 0 {
		return newError("Validate", field+".protobuf_wire_bytes", ErrUntypedPayload,
			"payload for schema %s carries no bytes", p.Schema)
	}
	return nil
}

// Clone returns a deep copy, so an envelope never shares a mutable byte slice
// with its caller.
func (p TypedPayload) Clone() TypedPayload {
	p.WireBytes = slices.Clone(p.WireBytes)
	return p
}

// ControlSnapshots record the control context an intent was created and
// simulated in. They are revalidated context, never material: a change here
// triggers revalidation and invalidates an approval only when the material
// result changes or a mandatory deny appears.
type ControlSnapshots struct {
	CapabilityRegistryDigest           string
	PolicyBundleDigest                 string
	LegalContextDigest                 string
	EntitlementDigest                  string
	ReferenceDataDigest                string
	WorkflowDefinitionDigest           string
	ConnectorConfigurationDigest       string
	ClassificationTaxonomyDigest       string
	ClassificationLabelSetDigest       string
	ClassificationPropagationWatermark string
	DLPDecisionDigest                  string
	DestinationTrustDigest             string
	PurposeAndResidencyDigest          string
}

// requiredControlSnapshots are the snapshots every instance must pin. Workflow,
// connector, destination-trust and residency digests are only required once a
// definition actually reaches those planes, so they are not in this list.
func (c ControlSnapshots) requiredControlSnapshots() []struct {
	field, value string
} {
	return []struct{ field, value string }{
		{"control_snapshots.capability_registry_digest", c.CapabilityRegistryDigest},
		{"control_snapshots.policy_bundle_digest", c.PolicyBundleDigest},
		{"control_snapshots.legal_context_digest", c.LegalContextDigest},
		{"control_snapshots.entitlement_digest", c.EntitlementDigest},
		{"control_snapshots.reference_data_digest", c.ReferenceDataDigest},
		{"control_snapshots.classification_taxonomy_digest", c.ClassificationTaxonomyDigest},
		{"control_snapshots.dlp_decision_digest", c.DLPDecisionDigest},
	}
}

// Validate rejects an unpinned control context.
func (c ControlSnapshots) Validate() error {
	for _, req := range c.requiredControlSnapshots() {
		if req.value == "" {
			return newError("Validate", req.field, ErrInvalidInstance,
				"control snapshot is not pinned")
		}
	}
	return nil
}

// Instance is the typed intent envelope: what is wanted, by whom, for which
// subjects, under which definition and control context.
//
// It is a value type. Linked records (proposal revisions, approval bindings,
// execution bindings, cancellation, supersession, correction and closure) hang
// off it as slices; none of them is a sixth lifecycle dimension.
type Instance struct {
	IntentID   string
	Definition Ref

	Tenant              values.TenantId
	OrganizationScopeID string
	BillingAccountID    *string

	Initiator       PrincipalReference
	DelegationChain []DelegationReference
	Purpose         string
	Subjects        []SubjectReference

	RequestedEffectiveAt *values.Instant
	Request              TypedPayload

	IdempotencyKey string
	CorrelationID  string
	CausationID    *string
	TraceID        string

	Classification string
	RetentionClass string

	ControlSnapshots       ControlSnapshots
	CanonicalRequestDigest digest.Reference

	Lifecycle lifecycle.Dimensions

	CreatedAt        values.Instant
	RecordedAt       values.Instant
	LastTransitionAt values.Instant

	ExecutionMode   Mode
	InstanceVersion uint64

	OriginEventRef                *string
	SourceAuthoritySnapshotDigest string
	RiskContextDigest             string

	ProposalRevisions []ProposalRevision
}

// InstanceSpec is the creation request. The kernel derives intent id, lifecycle
// dimensions, timestamps and the canonical request digest; a caller may not
// supply any of them.
type InstanceSpec struct {
	Tenant              values.TenantId
	OrganizationScopeID string
	BillingAccountID    *string

	Initiator       PrincipalReference
	DelegationChain []DelegationReference
	Purpose         string
	Subjects        []SubjectReference

	RequestedEffectiveAt *values.Instant
	Request              TypedPayload

	IdempotencyKey string
	CorrelationID  string
	CausationID    *string
	TraceID        string

	Classification string
	RetentionClass string

	ControlSnapshots ControlSnapshots
	ExecutionMode    Mode

	OriginEventRef                *string
	SourceAuthoritySnapshotDigest string
	RiskContextDigest             string
}

// Digester mints canonical digests for kernel objects. It is a port: the
// canonical models live in the generated Protobuf contracts, so the mapping
// package supplies the implementation and the kernel stays free of wire types.
type Digester interface {
	// RequestDigest computes the IDEMPOTENT_REQUEST digest of an instance.
	RequestDigest(Instance) (digest.Reference, error)

	// ProposalDigest computes the PROPOSAL material digest of a revision.
	ProposalDigest(ProposalRevision) (digest.Reference, error)
}

// IDSource mints identifiers. Production uses [UUIDv7Source]; tests supply a
// deterministic source so that goldens do not drift.
type IDSource func() (string, error)

// UUIDv7Source mints time-ordered UUIDv7 identifiers.
func UUIDv7Source() (string, error) {
	u, err := uuid.NewV7()
	if err != nil {
		return "", newError("UUIDv7Source", "intent_id", ErrInvalidInstance, "%v", err)
	}
	return u.String(), nil
}

// CreationEvidence is the immutable record that an instance was created. It is
// returned rather than stored: retention is the caller's classification
// decision, and the kernel never owns an evidence store.
type CreationEvidence struct {
	IntentID       string
	Definition     Ref
	Tenant         values.TenantId
	Initiator      PrincipalReference
	RequestDigest  digest.Reference
	ExecutionMode  Mode
	IdempotencyKey string
	CreatedAt      values.Instant
	Lifecycle      lifecycle.Dimensions
}

// Clock supplies the recording time. It is a parameter so that replay and tests
// produce identical envelopes.
type Clock func() values.Instant

// NewInstance validates a creation request against its definition and returns
// the typed envelope plus its creation evidence.
//
// The lifecycle starts at DRAFT / NOT_PLANNED, with business, consistency and
// obligation dimensions at their applicable initial values. P1A never leaves
// ExecutionState NOT_PLANNED, which is why a P1A definition may not allow
// EXECUTE at all.
func NewInstance(spec InstanceSpec, def Definition, d Digester, ids IDSource, clock Clock) (Instance, CreationEvidence, error) {
	if d == nil {
		return Instance{}, CreationEvidence{}, newError("NewInstance", "", ErrInvalidInstance,
			"no digester supplied")
	}
	if ids == nil {
		ids = UUIDv7Source
	}
	if clock == nil {
		return Instance{}, CreationEvidence{}, newError("NewInstance", "", ErrInvalidInstance,
			"no clock supplied")
	}
	if err := validateSpec(spec, def); err != nil {
		return Instance{}, CreationEvidence{}, err
	}
	id, err := ids()
	if err != nil {
		return Instance{}, CreationEvidence{}, err
	}
	now := clock()
	if !now.IsSet() {
		return Instance{}, CreationEvidence{}, newError("NewInstance", "created_at", ErrInvalidInstance,
			"clock returned an unset instant")
	}

	inst := Instance{
		IntentID:                      id,
		Definition:                    def.Ref,
		Tenant:                        spec.Tenant,
		OrganizationScopeID:           spec.OrganizationScopeID,
		BillingAccountID:              cloneStringPtr(spec.BillingAccountID),
		Initiator:                     spec.Initiator,
		DelegationChain:               slices.Clone(spec.DelegationChain),
		Purpose:                       spec.Purpose,
		Subjects:                      slices.Clone(spec.Subjects),
		RequestedEffectiveAt:          cloneInstantPtr(spec.RequestedEffectiveAt),
		Request:                       spec.Request.Clone(),
		IdempotencyKey:                spec.IdempotencyKey,
		CorrelationID:                 spec.CorrelationID,
		CausationID:                   cloneStringPtr(spec.CausationID),
		TraceID:                       spec.TraceID,
		Classification:                spec.Classification,
		RetentionClass:                spec.RetentionClass,
		ControlSnapshots:              spec.ControlSnapshots,
		Lifecycle:                     InitialDimensions(def),
		CreatedAt:                     now,
		RecordedAt:                    now,
		LastTransitionAt:              now,
		ExecutionMode:                 spec.ExecutionMode,
		InstanceVersion:               1,
		OriginEventRef:                cloneStringPtr(spec.OriginEventRef),
		SourceAuthoritySnapshotDigest: spec.SourceAuthoritySnapshotDigest,
		RiskContextDigest:             spec.RiskContextDigest,
	}

	ref, err := d.RequestDigest(inst)
	if err != nil {
		return Instance{}, CreationEvidence{}, err
	}
	inst.CanonicalRequestDigest = ref

	// The starting tuple must itself be legal; an envelope is never created in
	// a state a transition would refuse to reach.
	if err := lifecycle.Check(inst.Lifecycle, inst.LifecycleContext(def)); err != nil {
		return Instance{}, CreationEvidence{}, err
	}

	evidence := CreationEvidence{
		IntentID:       inst.IntentID,
		Definition:     inst.Definition,
		Tenant:         inst.Tenant,
		Initiator:      inst.Initiator,
		RequestDigest:  ref,
		ExecutionMode:  inst.ExecutionMode,
		IdempotencyKey: inst.IdempotencyKey,
		CreatedAt:      now,
		Lifecycle:      inst.Lifecycle,
	}
	return inst, evidence, nil
}

// InitialDimensions returns the lifecycle tuple a new instance starts in.
// A family that can never produce an external effect starts with consistency
// NOT_APPLICABLE rather than pretending an observation is pending.
func InitialDimensions(def Definition) lifecycle.Dimensions {
	consistency := lifecycle.ConsistencyNotApplicable
	if def.Family == FamilyChangeRequest && def.SideEffect != SideEffectInternalMutation {
		consistency = lifecycle.ConsistencyPendingObservation
	}
	return lifecycle.Dimensions{
		Request:     lifecycle.RequestDraft,
		Execution:   lifecycle.ExecutionNotPlanned,
		Business:    lifecycle.BusinessNotStarted,
		Consistency: consistency,
		Obligation:  lifecycle.ObligationNotApplicable,
	}
}

// LifecycleContext projects the instance's linked records into the context the
// fixed legality rules read.
func (i Instance) LifecycleContext(def Definition) lifecycle.Context {
	ctx := lifecycle.Context{
		ApprovalRequired:               def.ApprovalRequired,
		ClosurePolicyPermitsOpenRepair: def.ClosurePolicyPermitsOpenRepair,
		Persisted:                      true,
	}
	if rev, ok := i.CurrentRevision(); ok {
		ctx.CurrentMaterialDigest = rev.MaterialDigest.Digest
	}
	return ctx
}

// CurrentRevision returns the highest-numbered proposal revision.
func (i Instance) CurrentRevision() (ProposalRevision, bool) {
	var best ProposalRevision
	found := false
	for _, rev := range i.ProposalRevisions {
		if !found || rev.Revision > best.Revision {
			best, found = rev, true
		}
	}
	return best, found
}

// Validate re-checks a materialized envelope. Storage layers call it on read so
// that a record written by an older binary cannot silently reintroduce a field
// the kernel now requires.
func (i Instance) Validate(def Definition) error {
	if i.IntentID == "" {
		return newError("Validate", "intent_id", ErrInvalidInstance, "instance has no id")
	}
	if i.Definition != def.Ref {
		return newError("Validate", "definition", ErrInvalidInstance,
			"instance names %s but was validated against %s", i.Definition, def.Ref)
	}
	spec := InstanceSpec{
		Tenant:                        i.Tenant,
		OrganizationScopeID:           i.OrganizationScopeID,
		Initiator:                     i.Initiator,
		DelegationChain:               i.DelegationChain,
		Purpose:                       i.Purpose,
		Subjects:                      i.Subjects,
		RequestedEffectiveAt:          i.RequestedEffectiveAt,
		Request:                       i.Request,
		IdempotencyKey:                i.IdempotencyKey,
		CorrelationID:                 i.CorrelationID,
		TraceID:                       i.TraceID,
		Classification:                i.Classification,
		RetentionClass:                i.RetentionClass,
		ControlSnapshots:              i.ControlSnapshots,
		ExecutionMode:                 i.ExecutionMode,
		SourceAuthoritySnapshotDigest: i.SourceAuthoritySnapshotDigest,
		RiskContextDigest:             i.RiskContextDigest,
	}
	return validateSpec(spec, def)
}

func validateSpec(spec InstanceSpec, def Definition) error {
	if err := def.Validate(); err != nil {
		return err
	}
	if err := spec.Tenant.Validate(); err != nil {
		return newError("Validate", "tenant_id", ErrInvalidInstance, "%v", err)
	}
	for _, req := range []struct{ field, value string }{
		{"organization_scope_id", spec.OrganizationScopeID},
		{"purpose", spec.Purpose},
		{"idempotency_key", spec.IdempotencyKey},
		{"correlation_id", spec.CorrelationID},
		{"trace_id", spec.TraceID},
		{"classification", spec.Classification},
		{"retention_class", spec.RetentionClass},
		{"source_authority_snapshot_digest", spec.SourceAuthoritySnapshotDigest},
		{"risk_context_digest", spec.RiskContextDigest},
	} {
		if err := requireCanonicalText(req.field, req.value); err != nil {
			return err
		}
	}
	if err := spec.Initiator.Validate(); err != nil {
		return err
	}
	if !def.AllowsInitiator(spec.Initiator.Kind) {
		return newError("Validate", "initiator.kind", ErrInitiatorNotAllowed,
			"%s does not allow initiator %s", def.Ref, spec.Initiator.Kind)
	}
	for _, hop := range spec.DelegationChain {
		if err := hop.Validate(); err != nil {
			return err
		}
	}
	if len(spec.Subjects) == 0 {
		return newError("Validate", "subjects", ErrInvalidInstance, "instance names no subject")
	}
	seen := map[SubjectReference]bool{}
	for _, s := range spec.Subjects {
		if err := s.Validate(); err != nil {
			return err
		}
		if !def.AllowsSubjectKind(s.Kind) {
			return newError("Validate", "subjects.subject_kind", ErrInvalidInstance,
				"%s does not accept subject kind %q", def.Ref, s.Kind)
		}
		if seen[s] {
			return newError("Validate", "subjects", ErrInvalidInstance,
				"subject %s/%s appears twice; subjects are a set", s.Kind, s.SubjectID)
		}
		seen[s] = true
	}
	if err := spec.Request.Validate("request"); err != nil {
		return err
	}
	if spec.Request.Schema != def.InputSchema {
		return newError("Validate", "request.schema", ErrUntypedPayload,
			"%s expects input schema %s, payload declares %s",
			def.Ref, def.InputSchema, spec.Request.Schema)
	}
	if err := spec.ControlSnapshots.Validate(); err != nil {
		return err
	}
	if !spec.ExecutionMode.Valid() {
		return newError("Validate", "execution_mode", ErrModeNotAllowed,
			"instance declares no execution mode")
	}
	if !def.AllowsMode(spec.ExecutionMode) {
		return newError("Validate", "execution_mode", ErrModeNotAllowed,
			"%s does not allow %s", def.Ref, spec.ExecutionMode)
	}
	if spec.RequestedEffectiveAt != nil {
		if err := spec.RequestedEffectiveAt.Validate(); err != nil {
			return newError("Validate", "requested_effective_at", ErrInvalidInstance, "%v", err)
		}
	}
	return nil
}

// requireCanonicalText rejects an empty, non-UTF-8, non-NFC or control-bearing
// value for a field the canonical encoder will later hash.
//
// The check belongs here rather than in the encoder: an envelope that only
// fails at digest time has already been accepted by validation, and the caller
// gets an encoding error about a field path instead of a typed rejection about
// its own request.
func requireCanonicalText(field, value string) error {
	if value == "" {
		return newError("Validate", field, ErrInvalidInstance, "required field is empty")
	}
	if !utf8.ValidString(value) {
		return newError("Validate", field, ErrInvalidInstance, "value is not valid UTF-8")
	}
	if !norm.NFC.IsNormalString(value) {
		return newError("Validate", field, ErrInvalidInstance, "value is not Unicode NFC normalized")
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return newError("Validate", field, ErrInvalidInstance,
				"value carries the control character %U", r)
		}
	}
	return nil
}

func cloneStringPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

func cloneInstantPtr(i *values.Instant) *values.Instant {
	if i == nil {
		return nil
	}
	v := *i
	return &v
}
