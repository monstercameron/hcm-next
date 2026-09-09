package intentcontrol

// This file is the durable, explicit JSON envelope for a complete proposal.
// It intentionally does not marshal intent.ProposalRevision: that value has
// private kernel fields and its wire representation is owned by protomap.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const FullProposalSchemaVersion uint32 = 1

const (
	maxFullProposalBytes = 4 << 20
	maxJSONDepth         = 64
)

type FullProposalVerifier interface {
	VerifyProposalDigest(intent.ProposalRevision) error
}

type fullInterval struct{ Kind, Start, End, CalendarRef, CalendarVersion, ZoneID, TZDBVersion, Disambiguation string }
type fullSubject struct{ Kind, SubjectID, AuthorityDomain string }
type fullAssertion struct {
	Subject                fullSubject
	ResourceKey, FieldPath string
	CanonicalText          *string
}
type fullWrite struct {
	Subject                                                                                                                   fullSubject
	ResourceKey, FieldPath, CurrentCanonicalText, ProposedCanonicalText, SourceAuthorityDecision, ExpectedRevision, Operation string
	EffectiveInterval                                                                                                         *fullInterval
}
type fullEffect struct{ EffectID, Kind, DestinationRef, Reversibility, CompensationRef, ObservationRef string }
type fullChild struct {
	Definition, ChildIntentID string
	Ordinal                   uint32
	MaterialInputDigest       string
}
type fullReservation struct{ ReservationID, Kind, Expiry string }
type fullApproval struct{ RequirementID, SeparationConstraint string }
type fullObligation struct{ ObligationID, Kind string }
type fullCompensation struct{ EffectID, Strategy, RepairPlanID string }
type fullBaseline struct{ StreamID, ExpectedRevision string }
type fullAttachment struct{ ArtifactID, AlgorithmID, Digest string }
type fullMoney struct{ Amount, Currency string }
type fullPrincipal struct {
	PrincipalID          string
	Kind                 intent.Initiator
	IdentityAssuranceRef string
}
type fullDigest struct {
	ProfileID                                                                   string
	ProfileVersion                                                              uint32
	SchemaID                                                                    string
	SchemaVersion                                                               uint32
	AlgorithmID                                                                 string
	CanonicalLength                                                             uint64
	Digest, ScopeBindingDigest                                                  string
	CanonicalBytesArtifactRef, IntentID, ProposalRevisionID, MaterialProfileRef *string
}
type fullControl struct{ CapabilityRegistryDigest, PolicyBundleDigest, LegalContextDigest, EntitlementDigest, ReferenceDataDigest, WorkflowDefinitionDigest, ConnectorConfigurationDigest, ClassificationTaxonomyDigest, ClassificationLabelSetDigest, ClassificationPropagationWatermark, DLPDecisionDigest, DestinationTrustDigest, PurposeAndResidencyDigest string }

type fullProposalDTO struct {
	SchemaVersion                              uint32
	ProposalRevisionID, IntentID               string
	Revision                                   uint64
	Tenant, OrganizationScopeID, LegalEntityID string
	Subjects                                   []fullSubject
	EffectiveTime                              fullInterval
	CurrentState, ProposedState                []fullAssertion
	Writes                                     []fullWrite
	Effects                                    []fullEffect
	Children                                   []fullChild
	Reservations                               []fullReservation
	RequiredApprovals                          []fullApproval
	Obligations                                []fullObligation
	Compensations                              []fullCompensation
	SourceBaselines                            []fullBaseline
	Attachments                                []fullAttachment
	Purpose                                    intent.PurposeDecision
	Cost                                       *fullMoney
	Revalidation                               intent.RevalidationPlan
	SupersedesRevisionID                       *string
	ControlSnapshots                           fullControl
	CreatedBy                                  fullPrincipal
	CreatedAt                                  string
	InvalidatorRefs                            []string
	MaterialDigest                             fullDigest
}

func EncodeFullProposal(p intent.ProposalRevision) ([]byte, error) {
	d, err := proposalDTO(p)
	if err != nil {
		return nil, err
	}
	return json.Marshal(d)
}

func DecodeFullProposal(data []byte, verifier FullProposalVerifier) (intent.ProposalRevision, error) {
	if len(data) == 0 || len(data) > maxFullProposalBytes {
		return intent.ProposalRevision{}, fmt.Errorf("full proposal: envelope size %d is outside 1..%d bytes", len(data), maxFullProposalBytes)
	}
	if err := rejectDuplicateJSON(data); err != nil {
		return intent.ProposalRevision{}, err
	}
	var raw map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&raw); err != nil {
		return intent.ProposalRevision{}, fmt.Errorf("full proposal: malformed JSON: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return intent.ProposalRevision{}, fmt.Errorf("full proposal: trailing JSON")
	}
	allowed := map[string]bool{}
	typ := reflect.TypeOf(fullProposalDTO{})
	for i := 0; i < typ.NumField(); i++ {
		allowed[typ.Field(i).Name] = true
	}
	for k := range raw {
		if !allowed[k] {
			return intent.ProposalRevision{}, fmt.Errorf("full proposal: unknown field %q", k)
		}
	}
	b, _ := json.Marshal(raw)
	var d fullProposalDTO
	dd := json.NewDecoder(bytes.NewReader(b))
	dd.DisallowUnknownFields()
	if err := dd.Decode(&d); err != nil {
		return intent.ProposalRevision{}, fmt.Errorf("full proposal: %w", err)
	}
	if d.SchemaVersion != FullProposalSchemaVersion {
		return intent.ProposalRevision{}, fmt.Errorf("full proposal: unsupported schema version %d", d.SchemaVersion)
	}
	p, err := proposalFromDTO(d)
	if err != nil {
		return intent.ProposalRevision{}, err
	}
	if nilInterface(verifier) {
		return intent.ProposalRevision{}, fmt.Errorf("full proposal: verifier required")
	}
	if err := verifier.VerifyProposalDigest(p); err != nil {
		return intent.ProposalRevision{}, fmt.Errorf("full proposal: digest verification: %w", err)
	}
	return p, nil
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// rejectDuplicateJSON is deliberately small and recursive. encoding/json
// accepts duplicate object names (last value wins), which is unsafe for a
// durable envelope because it makes the authenticated meaning ambiguous.
func rejectDuplicateJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	if err := scanJSONValue(d, "$", 0); err != nil {
		return fmt.Errorf("full proposal: malformed JSON: %w", err)
	}
	var x any
	if err := d.Decode(&x); err != io.EOF {
		return fmt.Errorf("full proposal: trailing JSON")
	}
	return nil
}
func scanJSONValue(d *json.Decoder, path string, depth int) error {
	if depth > maxJSONDepth {
		return fmt.Errorf("%s: nesting exceeds %d", path, maxJSONDepth)
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	if delim, ok := t.(json.Delim); ok {
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := k.(string)
				if !ok {
					return fmt.Errorf("%s: non-string object key", path)
				}
				if seen[key] {
					return fmt.Errorf("%s: duplicate key %q", path, key)
				}
				seen[key] = true
				if err := scanJSONValue(d, path+"."+key, depth+1); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		case '[':
			i := 0
			for d.More() {
				if err := scanJSONValue(d, fmt.Sprintf("%s[%d]", path, i), depth+1); err != nil {
					return err
				}
				i++
			}
			_, err = d.Token()
			return err
		}
	}
	return nil
}

func subject(s intent.SubjectReference) fullSubject {
	return fullSubject{s.Kind, s.SubjectID, s.AuthorityDomain}
}
func fromSubject(s fullSubject) intent.SubjectReference {
	return intent.SubjectReference{Kind: s.Kind, SubjectID: s.SubjectID, AuthorityDomain: s.AuthorityDomain}
}
func intervalDTO(iv values.EffectiveInterval) (fullInterval, error) {
	c, e := encodeInterval(iv)
	if e != nil {
		return fullInterval{}, e
	}
	return fullInterval{Kind: stringValue(c.kind), Start: stringValue(c.start), End: stringValue(c.end), CalendarRef: stringValue(c.calendarRef), CalendarVersion: stringValue(c.calendarVersion), ZoneID: stringValue(c.zoneID), TZDBVersion: stringValue(c.tzdbVersion), Disambiguation: stringValue(c.disambiguation)}, nil
}
func intervalFromDTO(x fullInterval) (values.EffectiveInterval, error) {
	var end, cr, cv, z, t, d *string
	if x.End != "" {
		end = &x.End
	}
	if x.CalendarRef != "" {
		cr = &x.CalendarRef
	}
	if x.CalendarVersion != "" {
		cv = &x.CalendarVersion
	}
	if x.ZoneID != "" {
		z = &x.ZoneID
	}
	if x.TZDBVersion != "" {
		t = &x.TZDBVersion
	}
	if x.Disambiguation != "" {
		d = &x.Disambiguation
	}
	k := x.Kind
	if k == "" {
		return values.EffectiveInterval{}, fmt.Errorf("full proposal: required interval kind")
	}
	return decodeInterval(&k, x.Start, end, cr, cv, z, t, d)
}
func digestDTO(r digest.Reference) fullDigest {
	return fullDigest{r.ProfileID, r.ProfileVersion, r.SchemaID, r.SchemaVersion, r.AlgorithmID, r.CanonicalLength, r.Digest, r.ScopeBindingDigest, r.CanonicalBytesArtifactRef, r.IntentID, r.ProposalRevisionID, r.MaterialProfileRef}
}
func digestFromDTO(r fullDigest) digest.Reference {
	return digest.Reference{ProfileID: r.ProfileID, ProfileVersion: r.ProfileVersion, SchemaID: r.SchemaID, SchemaVersion: r.SchemaVersion, AlgorithmID: r.AlgorithmID, CanonicalLength: r.CanonicalLength, Digest: r.Digest, ScopeBindingDigest: r.ScopeBindingDigest, CanonicalBytesArtifactRef: r.CanonicalBytesArtifactRef, IntentID: r.IntentID, ProposalRevisionID: r.ProposalRevisionID, MaterialProfileRef: r.MaterialProfileRef}
}

func proposalDTO(p intent.ProposalRevision) (fullProposalDTO, error) {
	iv, e := intervalDTO(p.EffectiveTime)
	if e != nil {
		return fullProposalDTO{}, e
	}
	d := fullProposalDTO{SchemaVersion: FullProposalSchemaVersion, ProposalRevisionID: p.ProposalRevisionID, IntentID: p.IntentID, Revision: p.Revision, Tenant: string(p.Tenant), OrganizationScopeID: p.OrganizationScopeID, LegalEntityID: p.LegalEntityID, EffectiveTime: iv, Purpose: p.Purpose, Revalidation: p.Revalidation, SupersedesRevisionID: p.SupersedesRevisionID, ControlSnapshots: fullControl{p.ControlSnapshots.CapabilityRegistryDigest, p.ControlSnapshots.PolicyBundleDigest, p.ControlSnapshots.LegalContextDigest, p.ControlSnapshots.EntitlementDigest, p.ControlSnapshots.ReferenceDataDigest, p.ControlSnapshots.WorkflowDefinitionDigest, p.ControlSnapshots.ConnectorConfigurationDigest, p.ControlSnapshots.ClassificationTaxonomyDigest, p.ControlSnapshots.ClassificationLabelSetDigest, p.ControlSnapshots.ClassificationPropagationWatermark, p.ControlSnapshots.DLPDecisionDigest, p.ControlSnapshots.DestinationTrustDigest, p.ControlSnapshots.PurposeAndResidencyDigest}, CreatedBy: fullPrincipal{p.CreatedBy.PrincipalID, p.CreatedBy.Kind, p.CreatedBy.IdentityAssuranceRef}, CreatedAt: p.CreatedAt.String(), InvalidatorRefs: p.InvalidatorRefs, MaterialDigest: digestDTO(p.MaterialDigest)}
	for _, s := range p.Subjects {
		d.Subjects = append(d.Subjects, subject(s))
	}
	for _, a := range p.CurrentState {
		k, e := a.ResourceKey.MarshalText()
		if e != nil {
			return d, e
		}
		text := a.CanonicalText
		d.CurrentState = append(d.CurrentState, fullAssertion{subject(a.Subject), string(k), a.FieldPath, &text})
	}
	for _, a := range p.ProposedState {
		k, e := a.ResourceKey.MarshalText()
		if e != nil {
			return d, e
		}
		text := a.CanonicalText
		d.ProposedState = append(d.ProposedState, fullAssertion{subject(a.Subject), string(k), a.FieldPath, &text})
	}
	for _, w := range p.Writes {
		k, e := w.ResourceKey.MarshalText()
		if e != nil {
			return d, e
		}
		x := fullWrite{subject(w.Subject), string(k), w.FieldPath, w.CurrentCanonicalText, w.ProposedCanonicalText, w.SourceAuthorityDecision, w.ExpectedRevision.String(), string(w.Operation), nil}
		if w.EffectiveInterval != (values.EffectiveInterval{}) {
			q, e := intervalDTO(w.EffectiveInterval)
			if e != nil {
				return d, e
			}
			x.EffectiveInterval = &q
		}
		d.Writes = append(d.Writes, x)
	}
	for _, x := range p.Effects {
		d.Effects = append(d.Effects, fullEffect{x.EffectID, x.Kind, x.DestinationRef, x.Reversibility, x.CompensationRef, x.ObservationRef})
	}
	for _, x := range p.Children {
		d.Children = append(d.Children, fullChild{x.Definition.String(), x.ChildIntentID, x.Ordinal, x.MaterialInputDigest})
	}
	for _, x := range p.Reservations {
		d.Reservations = append(d.Reservations, fullReservation{x.ReservationID, x.Kind, x.Expiry.String()})
	}
	for _, x := range p.RequiredApprovals {
		d.RequiredApprovals = append(d.RequiredApprovals, fullApproval{x.RequirementID, x.SeparationConstraint})
	}
	for _, x := range p.Obligations {
		d.Obligations = append(d.Obligations, fullObligation{x.ObligationID, x.Kind})
	}
	for _, x := range p.Compensations {
		d.Compensations = append(d.Compensations, fullCompensation{x.EffectID, x.Strategy, x.RepairPlanID})
	}
	for _, x := range p.SourceBaselines {
		k, e := x.ExpectedRevision.MarshalText()
		if e != nil {
			return d, e
		}
		d.SourceBaselines = append(d.SourceBaselines, fullBaseline{x.StreamID, string(k)})
	}
	for _, x := range p.Attachments {
		d.Attachments = append(d.Attachments, fullAttachment{x.ArtifactID, x.AlgorithmID, x.Digest})
	}
	if p.Cost != nil {
		a, e := p.Cost.Amount().MarshalText()
		if e != nil {
			return d, e
		}
		d.Cost = &fullMoney{string(a), p.Cost.Currency()}
	}
	return d, nil
}

func proposalFromDTO(d fullProposalDTO) (intent.ProposalRevision, error) {
	if d.ProposalRevisionID == "" || d.IntentID == "" || d.Revision == 0 || d.Tenant == "" || d.OrganizationScopeID == "" || len(d.Subjects) == 0 || d.CreatedAt == "" || d.MaterialDigest.Digest == "" {
		return intent.ProposalRevision{}, fmt.Errorf("full proposal: missing required material or provenance field")
	}
	iv, e := intervalFromDTO(d.EffectiveTime)
	if e != nil {
		return intent.ProposalRevision{}, e
	}
	p := intent.ProposalRevision{ProposalRevisionID: d.ProposalRevisionID, IntentID: d.IntentID, Revision: d.Revision, Tenant: values.TenantId(d.Tenant), OrganizationScopeID: d.OrganizationScopeID, LegalEntityID: d.LegalEntityID, EffectiveTime: iv, Purpose: d.Purpose, Revalidation: d.Revalidation, SupersedesRevisionID: d.SupersedesRevisionID, InvalidatorRefs: d.InvalidatorRefs, MaterialDigest: digestFromDTO(d.MaterialDigest), CreatedBy: intent.PrincipalReference{PrincipalID: d.CreatedBy.PrincipalID, Kind: d.CreatedBy.Kind, IdentityAssuranceRef: d.CreatedBy.IdentityAssuranceRef}, ControlSnapshots: intent.ControlSnapshots{CapabilityRegistryDigest: d.ControlSnapshots.CapabilityRegistryDigest, PolicyBundleDigest: d.ControlSnapshots.PolicyBundleDigest, LegalContextDigest: d.ControlSnapshots.LegalContextDigest, EntitlementDigest: d.ControlSnapshots.EntitlementDigest, ReferenceDataDigest: d.ControlSnapshots.ReferenceDataDigest, WorkflowDefinitionDigest: d.ControlSnapshots.WorkflowDefinitionDigest, ConnectorConfigurationDigest: d.ControlSnapshots.ConnectorConfigurationDigest, ClassificationTaxonomyDigest: d.ControlSnapshots.ClassificationTaxonomyDigest, ClassificationLabelSetDigest: d.ControlSnapshots.ClassificationLabelSetDigest, ClassificationPropagationWatermark: d.ControlSnapshots.ClassificationPropagationWatermark, DLPDecisionDigest: d.ControlSnapshots.DLPDecisionDigest, DestinationTrustDigest: d.ControlSnapshots.DestinationTrustDigest, PurposeAndResidencyDigest: d.ControlSnapshots.PurposeAndResidencyDigest}}
	if e := p.CreatedAt.UnmarshalText([]byte(d.CreatedAt)); e != nil {
		return intent.ProposalRevision{}, fmt.Errorf("full proposal: created_at: %w", e)
	}
	if e := p.Tenant.Validate(); e != nil {
		return p, fmt.Errorf("full proposal: tenant: %w", e)
	}
	if e := p.CreatedBy.Validate(); e != nil {
		return p, fmt.Errorf("full proposal: created_by: %w", e)
	}
	if e := p.ControlSnapshots.Validate(); e != nil {
		return p, fmt.Errorf("full proposal: control snapshots: %w", e)
	}
	for _, x := range d.Subjects {
		if e := fromSubject(x).Validate(); e != nil {
			return p, fmt.Errorf("full proposal: subject: %w", e)
		}
		p.Subjects = append(p.Subjects, fromSubject(x))
	}
	for _, x := range d.CurrentState {
		if x.FieldPath == "" || x.ResourceKey == "" || x.CanonicalText == nil {
			return p, fmt.Errorf("full proposal: incomplete current state assertion")
		}
		if e := fromSubject(x.Subject).Validate(); e != nil {
			return p, fmt.Errorf("full proposal: current state subject: %w", e)
		}
		k := values.ResourceKey{}
		if e := k.UnmarshalText([]byte(x.ResourceKey)); e != nil {
			return p, e
		}
		p.CurrentState = append(p.CurrentState, intent.StateAssertion{Subject: fromSubject(x.Subject), ResourceKey: k, FieldPath: x.FieldPath, CanonicalText: *x.CanonicalText})
	}
	for _, x := range d.ProposedState {
		if x.FieldPath == "" || x.ResourceKey == "" || x.CanonicalText == nil {
			return p, fmt.Errorf("full proposal: incomplete proposed state assertion")
		}
		if e := fromSubject(x.Subject).Validate(); e != nil {
			return p, fmt.Errorf("full proposal: proposed state subject: %w", e)
		}
		k := values.ResourceKey{}
		if e := k.UnmarshalText([]byte(x.ResourceKey)); e != nil {
			return p, e
		}
		p.ProposedState = append(p.ProposedState, intent.StateAssertion{Subject: fromSubject(x.Subject), ResourceKey: k, FieldPath: x.FieldPath, CanonicalText: *x.CanonicalText})
	}
	for _, x := range d.Writes {
		if x.FieldPath == "" || x.CurrentCanonicalText == x.ProposedCanonicalText || x.SourceAuthorityDecision == "" || x.ExpectedRevision == "" || x.Subject.SubjectID == "" || x.ResourceKey == "" {
			return p, fmt.Errorf("full proposal: incomplete planned write")
		}
		k := values.ResourceKey{}
		if e := k.UnmarshalText([]byte(x.ResourceKey)); e != nil {
			return p, e
		}
		var rt values.RevisionToken
		if e := rt.UnmarshalText([]byte(x.ExpectedRevision)); e != nil {
			return p, e
		}
		w := intent.PlannedWrite{Subject: fromSubject(x.Subject), ResourceKey: k, FieldPath: x.FieldPath, CurrentCanonicalText: x.CurrentCanonicalText, ProposedCanonicalText: x.ProposedCanonicalText, SourceAuthorityDecision: x.SourceAuthorityDecision, ExpectedRevision: rt, Operation: intent.WriteOperation(x.Operation)}
		if x.EffectiveInterval != nil {
			w.EffectiveInterval, e = intervalFromDTO(*x.EffectiveInterval)
			if e != nil {
				return p, e
			}
		}
		p.Writes = append(p.Writes, w)
	}
	for _, x := range d.Effects {
		p.Effects = append(p.Effects, intent.PlannedEffect{EffectID: x.EffectID, Kind: x.Kind, DestinationRef: x.DestinationRef, Reversibility: x.Reversibility, CompensationRef: x.CompensationRef, ObservationRef: x.ObservationRef})
	}
	for _, x := range d.Children {
		r, e := intent.ParseRef(x.Definition)
		if e != nil {
			return p, e
		}
		p.Children = append(p.Children, intent.ChildIntentBinding{Definition: r, ChildIntentID: x.ChildIntentID, Ordinal: x.Ordinal, MaterialInputDigest: x.MaterialInputDigest})
	}
	for _, x := range d.Reservations {
		var i values.Instant
		if e := i.UnmarshalText([]byte(x.Expiry)); e != nil {
			return p, e
		}
		p.Reservations = append(p.Reservations, intent.Reservation{ReservationID: x.ReservationID, Kind: x.Kind, Expiry: i})
	}
	for _, x := range d.RequiredApprovals {
		p.RequiredApprovals = append(p.RequiredApprovals, intent.RequiredApproval{RequirementID: x.RequirementID, SeparationConstraint: x.SeparationConstraint})
	}
	for _, x := range d.Obligations {
		p.Obligations = append(p.Obligations, intent.Obligation{ObligationID: x.ObligationID, Kind: x.Kind})
	}
	for _, x := range d.Compensations {
		p.Compensations = append(p.Compensations, intent.CompensationDeclaration{EffectID: x.EffectID, Strategy: x.Strategy, RepairPlanID: x.RepairPlanID})
	}
	for _, x := range d.SourceBaselines {
		var r values.RevisionToken
		if e := r.UnmarshalText([]byte(x.ExpectedRevision)); e != nil {
			return p, e
		}
		p.SourceBaselines = append(p.SourceBaselines, intent.SourceBaseline{StreamID: x.StreamID, ExpectedRevision: r})
	}
	for _, x := range d.Attachments {
		p.Attachments = append(p.Attachments, intent.AttachmentRef{ArtifactID: x.ArtifactID, AlgorithmID: x.AlgorithmID, Digest: x.Digest})
	}
	if d.Cost != nil {
		var a values.Decimal
		if e := a.UnmarshalText([]byte(d.Cost.Amount)); e != nil {
			return p, e
		}
		m, e := values.NewMoneyFromDecimal(a, d.Cost.Currency)
		if e != nil {
			return p, e
		}
		p.Cost = &m
	}
	return p, nil
}
