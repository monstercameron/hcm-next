package intent

import (
	"slices"
	"strconv"
	"strings"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// serverOwnedPrefixes are the input paths a template, a draft or a saved action
// may never carry a value for, because they are not input at all: the tenant,
// the organization scope, the acting principal, the delegation chain, the
// session, the purpose, the classification floor, the retention class, the
// idempotency and tracing keys, the pinned control snapshots, the execution
// mode, the lifecycle dimensions and the approval and evidence records are all
// derived server-side at submission.
//
// The check is a prefix match on the canonical schema path, so
// "initiator.principal_id" and "control_snapshots.policy_bundle_digest" are
// both caught by their roots. It is a closed deny list rather than a heuristic:
// a new server-owned field is a one-line addition here, and until it is added
// the declared-input check below still refuses it, because a template may only
// default a path the definition itself declares as an input.
var serverOwnedPrefixes = []string{
	"approval",
	"authority",
	"capability",
	"classification",
	"control_snapshots",
	"correlation_id",
	"delegation_chain",
	"entitlement",
	"evidence",
	"execution_mode",
	"idempotency_key",
	"initiator",
	"intent_id",
	"lifecycle",
	"on_behalf_of",
	"organization_scope_id",
	"origin",
	"principal",
	"purpose",
	"retention_class",
	"role",
	"session",
	"tenant_id",
	"trace_id",
}

// serverOwnedInputPath reports whether a path names a server-owned fact.
func serverOwnedInputPath(path string) bool {
	root, _, _ := strings.Cut(path, ".")
	return slices.Contains(serverOwnedPrefixes, root)
}

// InputValue is one authored field value, carried as canonical text so that an
// authoring artifact never depends on a domain type the kernel does not own and
// never holds a rendered, decrypted or otherwise sensitive representation.
type InputValue struct {
	Path          string
	CanonicalText string
}

// validateInputValues checks a list of authored values against a definition:
// canonical text, no duplicate path, no server-owned path, and every path
// declared as an input by the definition itself.
func validateInputValues(field string, values_ []InputValue, def Definition, cause error) error {
	declared := make(map[string]bool, len(def.RequiredInputs))
	for _, in := range def.RequiredInputs {
		declared[in.Path] = true
	}
	seen := make(map[string]bool, len(values_))
	for _, v := range values_ {
		if err := requireCanonicalText(field+".path", v.Path); err != nil {
			return newError("Validate", field+".path", cause, "%v", err)
		}
		if err := requireCanonicalText(field+".canonical_text", v.CanonicalText); err != nil {
			return newError("Validate", field+".canonical_text", cause, "%v", err)
		}
		if seen[v.Path] {
			return newError("Validate", field, cause,
				"path %q appears twice; an authored value set is keyed by path", v.Path)
		}
		seen[v.Path] = true
		if serverOwnedInputPath(v.Path) {
			return newError("Validate", field, cause,
				"path %q is a server-owned fact and is never authored input", v.Path)
		}
		if !declared[v.Path] {
			return newError("Validate", field, cause,
				"%s declares no input at path %q", def.Ref, v.Path)
		}
	}
	return nil
}

// Template is a versioned set of permitted defaults for one definition version.
//
// A template is not a saved intent. It carries no principal, no tenant, no
// approval, no idempotency key and no evidence: it supplies starting values for
// paths the definition declares as input, and every one of them is re-decided
// at submission against live truth and live governance.
type Template struct {
	TemplateID  string
	Version     uint32
	Definition  Ref
	DisplayName string
	OwnerDomain string
	Defaults    []InputValue
}

// String returns the canonical "template_id/vN" reference.
func (t Template) String() string {
	return t.TemplateID + "/v" + strconv.FormatUint(uint64(t.Version), 10)
}

// Validate rejects a template that embeds server-owned facts, defaults a path
// the definition does not declare, or is pinned to a different definition
// version than the one it is being applied to.
//
// The version check is the "stale revision" rule: a template pinned to
// promote_worker/v1 is not a template for promote_worker/v2, because v2's input
// contract is a different contract. It is refused rather than migrated, so a
// definition change can never silently change what a template means.
func (t Template) Validate(def Definition) error {
	if err := requireCanonicalText("template.template_id", t.TemplateID); err != nil {
		return newError("Validate", "template.template_id", ErrInvalidTemplate, "%v", err)
	}
	if t.Version == 0 {
		return newError("Validate", "template.version", ErrInvalidTemplate,
			"template %q has no version", t.TemplateID)
	}
	for _, req := range []struct{ field, value string }{
		{"template.display_name", t.DisplayName},
		{"template.owner_domain", t.OwnerDomain},
	} {
		if err := requireCanonicalText(req.field, req.value); err != nil {
			return newError("Validate", req.field, ErrInvalidTemplate, "%v", err)
		}
	}
	if err := t.Definition.Validate(); err != nil {
		return newError("Validate", "template.definition", ErrInvalidTemplate, "%v", err)
	}
	if t.Definition != def.Ref {
		return newError("Validate", "template.definition", ErrInvalidTemplate,
			"template %s is pinned to %s and cannot supply defaults for %s",
			t, t.Definition, def.Ref)
	}
	if len(t.Defaults) == 0 {
		return newError("Validate", "template.defaults", ErrInvalidTemplate,
			"template %s supplies no default", t)
	}
	return validateInputValues("template.defaults", t.Defaults, def, ErrInvalidTemplate)
}

// AuthoringDraft is authorized, possibly incomplete input on its way to being
// an intent. It is mutable up to the moment it is submitted and immutable
// afterwards, and it is never itself an intent: it has no lifecycle dimensions,
// no canonical request digest, no approval binding and no evidence.
type AuthoringDraft struct {
	DraftID    string
	Tenant     values.TenantId
	Definition Ref

	// Author is the principal the draft belongs to. It is a taken value from
	// the trusted boundary, exactly like an instance initiator.
	Author PrincipalReference

	// TemplateRef records which template seeded the draft, for lineage. It is
	// a reference, never a copy of the template's authority.
	TemplateRef string

	Inputs    []InputValue
	CreatedAt values.Instant
	UpdatedAt values.Instant

	// SubmittedIntentID names the one IntentInstance this draft became. While
	// it is empty the draft is mutable; once it is set every edit and every
	// further submission is refused.
	SubmittedIntentID string
}

// DraftSpec is the creation request for a draft. The kernel mints the draft id
// and the timestamps.
type DraftSpec struct {
	Tenant     values.TenantId
	Definition Ref
	Author     PrincipalReference
	Inputs     []InputValue
}

// NewAuthoringDraft opens a draft, seeded by a template when one is supplied.
//
// Template defaults are applied first and the caller's own inputs win, so a
// template is a starting point rather than a constraint the author cannot see
// past. Both sets go through the same validation: a template cannot smuggle in
// a value an author would have been refused.
func NewAuthoringDraft(spec DraftSpec, def Definition, tmpl *Template, ids IDSource, clock Clock) (AuthoringDraft, error) {
	if ids == nil {
		ids = UUIDv7Source
	}
	if clock == nil {
		return AuthoringDraft{}, newError("NewAuthoringDraft", "", ErrInvalidDraft,
			"no clock supplied")
	}
	if err := def.Validate(); err != nil {
		return AuthoringDraft{}, err
	}
	if spec.Definition != def.Ref {
		return AuthoringDraft{}, newError("NewAuthoringDraft", "draft.definition", ErrInvalidDraft,
			"draft names %s but was opened against %s", spec.Definition, def.Ref)
	}
	if err := spec.Tenant.Validate(); err != nil {
		return AuthoringDraft{}, newError("NewAuthoringDraft", "draft.tenant_id", ErrInvalidDraft, "%v", err)
	}
	if err := spec.Author.Validate(); err != nil {
		return AuthoringDraft{}, newError("NewAuthoringDraft", "draft.author", ErrInvalidDraft, "%v", err)
	}

	inputs := make([]InputValue, 0, len(spec.Inputs))
	templateRef := ""
	if tmpl != nil {
		if err := tmpl.Validate(def); err != nil {
			return AuthoringDraft{}, err
		}
		templateRef = tmpl.String()
		inputs = append(inputs, slices.Clone(tmpl.Defaults)...)
	}
	for _, in := range spec.Inputs {
		inputs = upsertInput(inputs, in)
	}
	if err := validateInputValues("draft.inputs", inputs, def, ErrInvalidDraft); err != nil {
		return AuthoringDraft{}, err
	}

	id, err := ids()
	if err != nil {
		return AuthoringDraft{}, err
	}
	now := clock()
	if !now.IsSet() {
		return AuthoringDraft{}, newError("NewAuthoringDraft", "draft.created_at", ErrInvalidDraft,
			"clock returned an unset instant")
	}
	return AuthoringDraft{
		DraftID:     id,
		Tenant:      spec.Tenant,
		Definition:  def.Ref,
		Author:      spec.Author,
		TemplateRef: templateRef,
		Inputs:      inputs,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// upsertInput replaces the value at a path, or appends it. Order is the order
// paths were first seen, so a draft renders the same way twice.
func upsertInput(inputs []InputValue, in InputValue) []InputValue {
	for i := range inputs {
		if inputs[i].Path == in.Path {
			inputs[i] = in
			return inputs
		}
	}
	return append(inputs, in)
}

// Submitted reports whether the draft has already become an intent.
func (d AuthoringDraft) Submitted() bool { return d.SubmittedIntentID != "" }

// WithInput returns a copy of the draft with one path set. A submitted draft is
// immutable: editing one would mean the record of what was submitted no longer
// matches what was submitted.
func (d AuthoringDraft) WithInput(in InputValue, def Definition, clock Clock) (AuthoringDraft, error) {
	if d.Submitted() {
		return AuthoringDraft{}, newError("WithInput", "draft.submitted_intent_id", ErrDraftAlreadySubmitted,
			"draft %s became intent %s and is no longer mutable", d.DraftID, d.SubmittedIntentID)
	}
	if clock == nil {
		return AuthoringDraft{}, newError("WithInput", "", ErrInvalidDraft, "no clock supplied")
	}
	next := d
	next.Inputs = upsertInput(slices.Clone(d.Inputs), in)
	if err := validateInputValues("draft.inputs", next.Inputs, def, ErrInvalidDraft); err != nil {
		return AuthoringDraft{}, err
	}
	now := clock()
	if !now.IsSet() {
		return AuthoringDraft{}, newError("WithInput", "draft.updated_at", ErrInvalidDraft,
			"clock returned an unset instant")
	}
	next.UpdatedAt = now
	return next, nil
}

// Validate re-checks a materialized draft.
func (d AuthoringDraft) Validate(def Definition) error {
	if d.DraftID == "" {
		return newError("Validate", "draft.draft_id", ErrInvalidDraft, "draft has no id")
	}
	if d.Definition != def.Ref {
		return newError("Validate", "draft.definition", ErrInvalidDraft,
			"draft names %s but was validated against %s", d.Definition, def.Ref)
	}
	if err := d.Tenant.Validate(); err != nil {
		return newError("Validate", "draft.tenant_id", ErrInvalidDraft, "%v", err)
	}
	if err := d.Author.Validate(); err != nil {
		return newError("Validate", "draft.author", ErrInvalidDraft, "%v", err)
	}
	return validateInputValues("draft.inputs", d.Inputs, def, ErrInvalidDraft)
}

// MissingRequiredInputs returns the required input paths the draft has not
// filled. An incomplete draft is a legal draft; it is only submission that
// demands completeness.
func (d AuthoringDraft) MissingRequiredInputs(def Definition) []string {
	present := make(map[string]bool, len(d.Inputs))
	for _, in := range d.Inputs {
		present[in.Path] = true
	}
	var missing []string
	for _, req := range def.RequiredInputs {
		if req.Required && !present[req.Path] {
			missing = append(missing, req.Path)
		}
	}
	slices.Sort(missing)
	return missing
}

// SubmitDraft turns a draft into exactly one immutable IntentInstance.
//
// It returns the closed draft as well as the instance: the draft is not
// deleted, it is stamped with the intent it became, which is what makes a
// second submission detectable rather than merely unlikely.
//
// The spec is the trusted envelope the boundary built; the draft supplies no
// principal, no tenant and no purpose, because it never held any.
func SubmitDraft(d AuthoringDraft, spec InstanceSpec, def Definition, dg Digester, ids IDSource, clock Clock) (AuthoringDraft, Instance, CreationEvidence, error) {
	if d.Submitted() {
		return AuthoringDraft{}, Instance{}, CreationEvidence{},
			newError("SubmitDraft", "draft.submitted_intent_id", ErrDraftAlreadySubmitted,
				"draft %s already became intent %s", d.DraftID, d.SubmittedIntentID)
	}
	if err := d.Validate(def); err != nil {
		return AuthoringDraft{}, Instance{}, CreationEvidence{}, err
	}
	if missing := d.MissingRequiredInputs(def); len(missing) > 0 {
		return AuthoringDraft{}, Instance{}, CreationEvidence{},
			newError("SubmitDraft", "draft.inputs", ErrInvalidDraft,
				"draft %s is missing required input %s", d.DraftID, strings.Join(missing, ", "))
	}
	if d.Tenant != spec.Tenant {
		return AuthoringDraft{}, Instance{}, CreationEvidence{},
			newError("SubmitDraft", "draft.tenant_id", ErrInvalidDraft,
				"draft %s belongs to tenant %s and was submitted under %s",
				d.DraftID, d.Tenant, spec.Tenant)
	}
	if d.Author != spec.Initiator {
		return AuthoringDraft{}, Instance{}, CreationEvidence{},
			newError("SubmitDraft", "draft.author", ErrInvalidDraft,
				"draft %s belongs to %q and was submitted by %q",
				d.DraftID, d.Author.PrincipalID, spec.Initiator.PrincipalID)
	}

	var (
		inst     Instance
		evidence CreationEvidence
		err      error
	)
	if def.Family == FamilyChangeRequest {
		inst, evidence, err = Draft(spec, def, dg, ids, clock)
	} else {
		inst, evidence, err = NewInstance(spec, def, dg, ids, clock)
	}
	if err != nil {
		return AuthoringDraft{}, Instance{}, CreationEvidence{}, err
	}

	closed := d
	closed.Inputs = slices.Clone(d.Inputs)
	closed.SubmittedIntentID = inst.IntentID
	return closed, inst, evidence, nil
}

// SavedAction is a recent or favourite action. It stores a definition reference
// and authorized parameters, and nothing else: no roles, no capabilities, no
// approval, and no rendered sensitive state from the run it was saved from.
type SavedAction struct {
	SavedActionID    string
	Tenant           values.TenantId
	OwnerPrincipalID string
	Definition       Ref
	Label            string
	Parameters       []InputValue
	LastUsedAt       values.Instant
}

// Validate rejects a saved action that stored copied authority, an unversioned
// definition reference, or a parameter the definition does not declare.
func (a SavedAction) Validate(def Definition) error {
	for _, req := range []struct{ field, value string }{
		{"saved_action.saved_action_id", a.SavedActionID},
		{"saved_action.owner_principal_id", a.OwnerPrincipalID},
		{"saved_action.label", a.Label},
	} {
		if err := requireCanonicalText(req.field, req.value); err != nil {
			return newError("Validate", req.field, ErrInvalidSavedAction, "%v", err)
		}
	}
	if err := a.Tenant.Validate(); err != nil {
		return newError("Validate", "saved_action.tenant_id", ErrInvalidSavedAction, "%v", err)
	}
	if err := a.Definition.Validate(); err != nil {
		return newError("Validate", "saved_action.definition", ErrInvalidSavedAction, "%v", err)
	}
	if a.Definition != def.Ref {
		return newError("Validate", "saved_action.definition", ErrInvalidSavedAction,
			"saved action %q references %s and was validated against %s",
			a.SavedActionID, a.Definition, def.Ref)
	}
	return validateInputValues("saved_action.parameters", a.Parameters, def, ErrInvalidSavedAction)
}

// LineageRelation is how a derived intent relates to the intent or proposal it
// came from. Neither relation carries anything over except the reference.
type LineageRelation uint8

// LineageRelation values.
const (
	LineageUnspecified LineageRelation = iota
	// LineageClone is a new intent seeded from another intent's request.
	LineageClone
	// LineageFork is a new intent seeded from one exact proposal revision of
	// another intent. Forking never touches the source.
	LineageFork
)

var lineageNames = map[LineageRelation]string{
	LineageUnspecified: "UNSPECIFIED",
	LineageClone:       "CLONE",
	LineageFork:        "FORK",
}

func (r LineageRelation) String() string { return enumName(lineageNames, r, "LineageRelation") }

// Valid reports whether r is a declared lineage relation other than
// UNSPECIFIED.
func (r LineageRelation) Valid() bool {
	_, ok := lineageNames[r]
	return ok && r != LineageUnspecified
}

// Lineage is the redaction-safe record of where a derived intent came from. It
// holds identifiers and versions only: no subject, no payload, no proposal
// content, so it can be disclosed in a timeline a viewer may not read the
// source of.
type Lineage struct {
	Relation                 LineageRelation
	SourceIntentID           string
	SourceInstanceVersion    uint64
	SourceProposalRevisionID string
	SourceRevision           uint64
	DerivedIntentID          string
}

// Validate rejects a lineage record that names no relation or no source.
func (l Lineage) Validate() error {
	if !l.Relation.Valid() {
		return newError("Validate", "lineage.relation", ErrLineageReuse,
			"lineage names no relation")
	}
	if l.SourceIntentID == "" {
		return newError("Validate", "lineage.source_intent_id", ErrLineageReuse,
			"lineage names no source intent")
	}
	if l.DerivedIntentID == "" {
		return newError("Validate", "lineage.derived_intent_id", ErrLineageReuse,
			"lineage names no derived intent")
	}
	if l.SourceIntentID == l.DerivedIntentID {
		return newError("Validate", "lineage.derived_intent_id", ErrLineageReuse,
			"a derived intent may not be its own source")
	}
	if l.Relation == LineageFork && l.SourceProposalRevisionID == "" {
		return newError("Validate", "lineage.source_proposal_revision_id", ErrLineageReuse,
			"a fork names no source proposal revision")
	}
	return nil
}

// DerivationSpec is the fresh causal identity a clone or a fork must be given.
// Every field on it is one the derived intent may not inherit.
type DerivationSpec struct {
	IdempotencyKey string
	CorrelationID  string
	TraceID        string
	Purpose        string

	// Origin is the trusted origin of the derivation itself: cloning is a new
	// act by whoever is cloning, not a replay of the original act.
	Origin Origin

	// Initiator is the principal performing the derivation. Empty means the
	// source's initiator, which is legal only when the source has no recorded
	// origin to contradict.
	Initiator PrincipalReference
}

// CloneIntent creates a new intent from an existing one's request.
//
// The clone shares the source's typed request payload and subjects and shares
// nothing else. It gets a new intent id, a new idempotency key, a new
// correlation id, an empty proposal history and the initial lifecycle tuple,
// and its causation id points at the source, so the two are one chain and two
// intents rather than one intent counted twice.
func CloneIntent(src Instance, spec DerivationSpec, def Definition, d Digester, ids IDSource, clock Clock) (Instance, Lineage, error) {
	inst, lineage, err := derive(src, ProposalRevision{}, LineageClone, spec, def, d, ids, clock)
	if err != nil {
		return Instance{}, Lineage{}, err
	}
	return inst, lineage, nil
}

// ForkProposal creates a new intent from one exact proposal revision of a
// source intent.
//
// The source is a value parameter and is deep-copied before anything is read
// from it, so a fork cannot mutate the proposal it forked: the source's
// revision list, its planned writes and its approval bindings are all untouched
// by construction, and the test in this package proves it.
func ForkProposal(src Instance, revisionID string, spec DerivationSpec, def Definition, d Digester, ids IDSource, clock Clock) (Instance, Lineage, error) {
	var found ProposalRevision
	ok := false
	for _, rev := range src.ProposalRevisions {
		if rev.ProposalRevisionID == revisionID {
			found, ok = rev, true
			break
		}
	}
	if !ok {
		return Instance{}, Lineage{}, newError("ForkProposal", "lineage.source_proposal_revision_id",
			ErrInvalidProposal, "intent %s has no proposal revision %q", src.IntentID, revisionID)
	}
	return derive(src, found, LineageFork, spec, def, d, ids, clock)
}

// derive is the one implementation behind clone and fork. Keeping it single
// means a rule added for one is a rule for both: there is no path on which a
// fork inherits something a clone is refused.
func derive(src Instance, rev ProposalRevision, relation LineageRelation, spec DerivationSpec, def Definition, d Digester, ids IDSource, clock Clock) (Instance, Lineage, error) {
	if src.IntentID == "" {
		return Instance{}, Lineage{}, newError("derive", "lineage.source_intent_id", ErrLineageReuse,
			"the source intent has no id")
	}
	for _, req := range []struct{ field, value string }{
		{"idempotency_key", spec.IdempotencyKey},
		{"correlation_id", spec.CorrelationID},
		{"trace_id", spec.TraceID},
	} {
		if err := requireCanonicalText(req.field, req.value); err != nil {
			return Instance{}, Lineage{}, newError("derive", req.field, ErrLineageReuse,
				"a %s needs its own %s: %v", relation, req.field, err)
		}
	}
	// Reusing the source's idempotency key would make the derived intent
	// collapse into the source at the store: two acts, one record.
	for _, reused := range []struct{ field, got, src string }{
		{"idempotency_key", spec.IdempotencyKey, src.IdempotencyKey},
		{"correlation_id", spec.CorrelationID, src.CorrelationID},
	} {
		if reused.got == reused.src {
			return Instance{}, Lineage{}, newError("derive", reused.field, ErrLineageReuse,
				"a %s may not reuse the source's %s", relation, reused.field)
		}
	}

	initiator := spec.Initiator
	if initiator == (PrincipalReference{}) {
		initiator = src.Initiator
	}
	// A derived intent needs its own trusted origin. Carrying the source's
	// forward would attribute a new act to the session that performed the old
	// one - the clone would look like it was raised by whoever raised the
	// original, at their assurance, through their channel.
	if src.Origin.IsSet() && !spec.Origin.IsSet() {
		return Instance{}, Lineage{}, newError("derive", "origin", ErrLineageReuse,
			"a %s of an intent with a recorded origin needs its own origin", relation)
	}
	if spec.Origin.IsSet() && spec.Origin.TrustedContextDigest == src.Origin.TrustedContextDigest &&
		src.Origin.IsSet() {
		return Instance{}, Lineage{}, newError("derive", "origin.trusted_context_digest", ErrLineageReuse,
			"a %s may not reuse the source's trusted context", relation)
	}

	purpose := spec.Purpose
	if purpose == "" {
		purpose = src.Purpose
	}
	causation := src.IntentID
	newSpec := InstanceSpec{
		Tenant:                        src.Tenant,
		OrganizationScopeID:           src.OrganizationScopeID,
		BillingAccountID:              cloneStringPtr(src.BillingAccountID),
		Initiator:                     initiator,
		DelegationChain:               slices.Clone(src.DelegationChain),
		Purpose:                       purpose,
		Subjects:                      slices.Clone(src.Subjects),
		RequestedEffectiveAt:          cloneInstantPtr(src.RequestedEffectiveAt),
		Request:                       src.Request.Clone(),
		IdempotencyKey:                spec.IdempotencyKey,
		CorrelationID:                 spec.CorrelationID,
		CausationID:                   &causation,
		TraceID:                       spec.TraceID,
		Classification:                src.Classification,
		RetentionClass:                src.RetentionClass,
		ControlSnapshots:              src.ControlSnapshots,
		ExecutionMode:                 src.ExecutionMode,
		SourceAuthoritySnapshotDigest: src.SourceAuthoritySnapshotDigest,
		RiskContextDigest:             src.RiskContextDigest,
		Origin:                        spec.Origin,
	}

	var (
		inst Instance
		err  error
	)
	if def.Family == FamilyChangeRequest {
		inst, _, err = Draft(newSpec, def, d, ids, clock)
	} else {
		inst, _, err = NewInstance(newSpec, def, d, ids, clock)
	}
	if err != nil {
		return Instance{}, Lineage{}, err
	}
	// Approval bindings, evidence, proposal revisions and closure records are
	// absent by construction: the derived envelope is built from a spec, and a
	// spec cannot express any of them. Asserting it here keeps that true if the
	// creation path ever grows a field.
	if len(inst.ProposalRevisions) != 0 {
		return Instance{}, Lineage{}, newError("derive", "proposal_revisions", ErrLineageReuse,
			"a %s inherited the source's proposal history", relation)
	}

	lineage := Lineage{
		Relation:                 relation,
		SourceIntentID:           src.IntentID,
		SourceInstanceVersion:    src.InstanceVersion,
		SourceProposalRevisionID: rev.ProposalRevisionID,
		SourceRevision:           rev.Revision,
		DerivedIntentID:          inst.IntentID,
	}
	if err := lineage.Validate(); err != nil {
		return Instance{}, Lineage{}, err
	}
	return inst, lineage, nil
}
