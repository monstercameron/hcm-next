package approval

import (
	"reflect"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Rule identifiers cited in a materiality explanation.
const (
	// RuleMaterialChange fires when the PROPOSAL profile's material encoding
	// differs between two revisions.
	RuleMaterialChange = "intent.materiality.material_field_changed"
	// RuleControlRevalidation fires when only revalidated context changed.
	RuleControlRevalidation = "intent.materiality.control_snapshot_revalidated"
	// RuleMandatoryDeny fires when revalidation surfaced a mandatory deny.
	RuleMandatoryDeny = "intent.materiality.mandatory_deny"
	// RuleApprovalStands fires for each decision that survives revalidation.
	RuleApprovalStands = "intent.materiality.approval_stands"
	// RuleApprovalInvalidated fires for each decision invalidated.
	RuleApprovalInvalidated = "intent.materiality.approval_invalidated"
	// RuleBindingNotCurrent fires for a decision bound to neither revision. It
	// is reported rather than silently dropped: a decision the assessment
	// cannot place is exactly the one an auditor asks about.
	RuleBindingNotCurrent = "intent.materiality.binding_not_current"
	// RuleMaterialFieldUnclassified fires when the material encodings differ
	// but no named material field does. It is a drift alarm on this package's
	// explanation list, never on the verdict: the verdict always comes from the
	// encoding.
	RuleMaterialFieldUnclassified = "intent.materiality.material_field_unclassified"
)

// Verdict is the outcome of a materiality assessment. There are exactly three,
// and every assessment produces exactly one.
type Verdict string

// Verdicts.
const (
	// VerdictApprovalStands means the material result is unchanged: bound
	// approvals survive and the revalidation is recorded.
	VerdictApprovalStands Verdict = "APPROVAL_STANDS"
	// VerdictNewRevision means the material result changed: bound approvals are
	// invalidated and the new revision needs its own.
	VerdictNewRevision Verdict = "NEW_REVISION"
	// VerdictBlocked means revalidation surfaced a mandatory deny. Approvals
	// are invalidated and nothing proceeds.
	VerdictBlocked Verdict = "BLOCKED"
)

// MandatoryDeny is a control-plane denial observed while revalidating a
// proposal under a new control context.
type MandatoryDeny struct {
	RuleID     string
	ControlRef string
	Reason     string
}

// Revalidation is the record written for one decision that survived.
type Revalidation struct {
	DecisionID     string
	FromRevisionID string
	ToRevisionID   string
	FromDigest     string
	ToDigest       string
	RevalidatedAt  values.Instant
	RuleID         string
}

// MaterialityAssessment is the whole answer, including why.
type MaterialityAssessment struct {
	IntentID string

	FromRevisionID string
	ToRevisionID   string
	FromDigest     digest.Reference
	ToDigest       digest.Reference

	Verdict Verdict

	// MaterialChange is true only when the PROPOSAL profile's material encoding
	// differs. Nothing else sets it.
	MaterialChange bool
	// ChangedMaterialFields names the material fields that differ, for the
	// explanation. It never decides the verdict.
	ChangedMaterialFields []string
	// ChangedControlSnapshots names the revalidated-context fields that differ.
	// A change here alone never invalidates anything.
	ChangedControlSnapshots []string

	InvalidatedDecisionIDs []string
	StandingDecisionIDs    []string
	UnboundDecisionIDs     []string
	Revalidations          []Revalidation

	Denies []MandatoryDeny

	// Explanation is deterministic and rule-cited.
	Explanation []string

	AssessedAt values.Instant
	// Receipt proves the assessment caused nothing.
	Receipt evidence.ZeroEffectReceipt
}

// AssessRequest is one materiality question.
type AssessRequest struct {
	DefinitionRef intent.Ref
	// From is the revision the approvals are bound to; To is the candidate
	// successor. They may be the same revision, which is the plain
	// revalidate-in-place case.
	From intent.ProposalRevision
	To   intent.ProposalRevision

	Decisions []ApprovalDecision
	// Denies are the mandatory denies revalidation surfaced under To's control
	// context.
	Denies []MandatoryDeny

	Clock intent.Clock
}

// Assess decides whether approvals bound to From survive To.
//
// The verdict comes from one place: whether the PROPOSAL profile's material
// encoding differs. Control snapshots, creator, creation time and invalidator
// bookkeeping are outside that encoding, so republishing a policy bundle, a
// taxonomy or a reference dataset produces APPROVAL_STANDS and a recorded
// revalidation - which is what keeps a compensation cycle's pending approvals
// from being invalidated by an unrelated control republish.
func Assess(req AssessRequest) (MaterialityAssessment, error) {
	if req.Clock == nil {
		return MaterialityAssessment{}, newError("Assess", "clock",
			CodeInvalidAssessment, ErrInvalidAssessment, "no clock supplied")
	}
	if req.From.MaterialDigest.Digest == "" || req.To.MaterialDigest.Digest == "" {
		return MaterialityAssessment{}, newError("Assess", "material_digest",
			CodeInvalidAssessment, ErrInvalidAssessment,
			"both revisions must carry a minted material digest")
	}
	if req.From.IntentID != req.To.IntentID {
		return MaterialityAssessment{}, newError("Assess", "intent_id",
			CodeInvalidAssessment, ErrInvalidAssessment,
			"revisions belong to different intents (%q and %q)",
			req.From.IntentID, req.To.IntentID)
	}
	if err := req.DefinitionRef.Validate(); err != nil {
		return MaterialityAssessment{}, newError("Assess", "definition_ref",
			CodeInvalidAssessment, ErrInvalidAssessment, "%v", err)
	}
	now := req.Clock()
	if !now.IsSet() {
		return MaterialityAssessment{}, newError("Assess", "assessed_at",
			CodeInvalidAssessment, ErrInvalidAssessment, "clock returned an unset instant")
	}

	a := MaterialityAssessment{
		IntentID:       req.From.IntentID,
		FromRevisionID: req.From.ProposalRevisionID,
		ToRevisionID:   req.To.ProposalRevisionID,
		FromDigest:     req.From.MaterialDigest,
		ToDigest:       req.To.MaterialDigest,
		MaterialChange: !MaterialResultEqual(req.From, req.To),
		Denies:         append([]MandatoryDeny(nil), req.Denies...),
		AssessedAt:     now,
	}
	a.ChangedControlSnapshots = changedControlSnapshots(
		req.From.ControlSnapshots, req.To.ControlSnapshots)
	if a.MaterialChange {
		a.ChangedMaterialFields = changedMaterialFields(req.From, req.To)
	}

	switch {
	case len(a.Denies) > 0:
		a.Verdict = VerdictBlocked
	case a.MaterialChange:
		a.Verdict = VerdictNewRevision
	default:
		a.Verdict = VerdictApprovalStands
	}

	a.classify(req, now)
	a.explain()

	receipt, err := buildReceipt(req, a)
	if err != nil {
		return MaterialityAssessment{}, newError("Assess", "receipt",
			CodeInvalidAssessment, ErrInvalidAssessment, "%v", err)
	}
	a.Receipt = receipt
	return a, nil
}

// classify sorts each supplied decision into standing, invalidated or unbound.
func (a *MaterialityAssessment) classify(req AssessRequest, now values.Instant) {
	for _, d := range req.Decisions {
		switch d.Binding.ProposalDigest.Digest {
		case req.From.MaterialDigest.Digest:
			if a.Verdict == VerdictApprovalStands {
				a.StandingDecisionIDs = append(a.StandingDecisionIDs, d.DecisionID)
				a.Revalidations = append(a.Revalidations, Revalidation{
					DecisionID:     d.DecisionID,
					FromRevisionID: req.From.ProposalRevisionID,
					ToRevisionID:   req.To.ProposalRevisionID,
					FromDigest:     req.From.MaterialDigest.Digest,
					ToDigest:       req.To.MaterialDigest.Digest,
					RevalidatedAt:  now,
					RuleID:         RuleApprovalStands,
				})
				continue
			}
			a.InvalidatedDecisionIDs = append(a.InvalidatedDecisionIDs, d.DecisionID)
		case req.To.MaterialDigest.Digest:
			// Already bound to the successor; nothing to decide.
			a.StandingDecisionIDs = append(a.StandingDecisionIDs, d.DecisionID)
		default:
			a.UnboundDecisionIDs = append(a.UnboundDecisionIDs, d.DecisionID)
		}
	}
	sort.Strings(a.StandingDecisionIDs)
	sort.Strings(a.InvalidatedDecisionIDs)
	sort.Strings(a.UnboundDecisionIDs)
}

func (a *MaterialityAssessment) explain() {
	lines := []string{
		string(a.Verdict) + " " + a.FromRevisionID + " -> " + a.ToRevisionID,
	}
	for _, d := range a.Denies {
		lines = append(lines, "  ["+RuleMandatoryDeny+"] "+d.RuleID+" on "+d.ControlRef+": "+d.Reason)
	}
	if a.MaterialChange {
		if len(a.ChangedMaterialFields) == 0 {
			lines = append(lines, "  ["+RuleMaterialFieldUnclassified+
				"] the material encoding differs but no named material field does")
		}
		for _, f := range a.ChangedMaterialFields {
			lines = append(lines, "  ["+RuleMaterialChange+"] "+f)
		}
	}
	for _, f := range a.ChangedControlSnapshots {
		lines = append(lines, "  ["+RuleControlRevalidation+"] "+f)
	}
	for _, id := range a.StandingDecisionIDs {
		lines = append(lines, "  ["+RuleApprovalStands+"] "+id)
	}
	for _, id := range a.InvalidatedDecisionIDs {
		lines = append(lines, "  ["+RuleApprovalInvalidated+"] "+id)
	}
	for _, id := range a.UnboundDecisionIDs {
		lines = append(lines, "  ["+RuleBindingNotCurrent+"] "+id)
	}
	a.Explanation = lines
}

// MaterialResultEqual reports whether two revisions carry the same material
// result: the PROPOSAL profile's material encoding with revision identity and
// lineage neutralized.
//
// It is deliberately a hair weaker than [intent.MaterialEqual], which clears
// the revision id and number but not SupersedesRevisionID. That difference
// matters here and nowhere else: every successor revision links a different
// predecessor, so comparing lineage would make every revision "material" and
// there would be no immaterial case left for the materiality rule to protect.
// Lineage is identity, not result. Anything intent.MaterialEqual calls equal,
// this calls equal too.
func MaterialResultEqual(a, b intent.ProposalRevision) bool {
	return string(materialResultBytes(a)) == string(materialResultBytes(b))
}

func materialResultBytes(p intent.ProposalRevision) []byte {
	p.ProposalRevisionID = ""
	p.Revision = 0
	p.SupersedesRevisionID = nil
	return p.MaterialPayload().WireBytes
}

// materialFields is the named material field list, used only to explain a
// difference the encoding already found. The encoding is the authority; if
// these disagree, the explanation says so via RuleMaterialFieldUnclassified
// rather than the verdict moving.
var materialFields = []struct {
	name string
	get  func(intent.ProposalRevision) any
}{
	{"intent_id", func(p intent.ProposalRevision) any { return p.IntentID }},
	{"tenant", func(p intent.ProposalRevision) any { return p.Tenant }},
	{"organization_scope_id", func(p intent.ProposalRevision) any { return p.OrganizationScopeID }},
	{"legal_entity_id", func(p intent.ProposalRevision) any { return p.LegalEntityID }},
	{"subjects", func(p intent.ProposalRevision) any { return p.Subjects }},
	{"effective_time", func(p intent.ProposalRevision) any { return p.EffectiveTime }},
	{"current_state", func(p intent.ProposalRevision) any { return p.CurrentState }},
	{"proposed_state", func(p intent.ProposalRevision) any { return p.ProposedState }},
	{"writes", func(p intent.ProposalRevision) any { return p.Writes }},
	{"effects", func(p intent.ProposalRevision) any { return p.Effects }},
	{"children", func(p intent.ProposalRevision) any { return p.Children }},
	{"reservations", func(p intent.ProposalRevision) any { return p.Reservations }},
	{"required_approvals", func(p intent.ProposalRevision) any { return p.RequiredApprovals }},
	{"obligations", func(p intent.ProposalRevision) any { return p.Obligations }},
	{"compensations", func(p intent.ProposalRevision) any { return p.Compensations }},
	{"source_baselines", func(p intent.ProposalRevision) any { return p.SourceBaselines }},
	{"attachments", func(p intent.ProposalRevision) any { return p.Attachments }},
	{"purpose", func(p intent.ProposalRevision) any { return p.Purpose }},
	{"cost", func(p intent.ProposalRevision) any { return p.Cost }},
	{"revalidation_rules", func(p intent.ProposalRevision) any { return p.Revalidation.Rules }},
}

// MaterialFieldNames returns the named material fields, in encoding order.
func MaterialFieldNames() []string {
	out := make([]string, 0, len(materialFields))
	for _, f := range materialFields {
		out = append(out, f.name)
	}
	return out
}

func changedMaterialFields(a, b intent.ProposalRevision) []string {
	var out []string
	for _, f := range materialFields {
		if !reflect.DeepEqual(f.get(a), f.get(b)) {
			out = append(out, f.name)
		}
	}
	return out
}

// controlSnapshotFields is the revalidated-context list, in a fixed order so an
// explanation is deterministic.
var controlSnapshotFields = []struct {
	name string
	get  func(intent.ControlSnapshots) string
}{
	{"capability_registry_digest", func(c intent.ControlSnapshots) string { return c.CapabilityRegistryDigest }},
	{"policy_bundle_digest", func(c intent.ControlSnapshots) string { return c.PolicyBundleDigest }},
	{"legal_context_digest", func(c intent.ControlSnapshots) string { return c.LegalContextDigest }},
	{"entitlement_digest", func(c intent.ControlSnapshots) string { return c.EntitlementDigest }},
	{"reference_data_digest", func(c intent.ControlSnapshots) string { return c.ReferenceDataDigest }},
	{"workflow_definition_digest", func(c intent.ControlSnapshots) string { return c.WorkflowDefinitionDigest }},
	{"connector_configuration_digest", func(c intent.ControlSnapshots) string { return c.ConnectorConfigurationDigest }},
	{"classification_taxonomy_digest", func(c intent.ControlSnapshots) string { return c.ClassificationTaxonomyDigest }},
	{"classification_label_set_digest", func(c intent.ControlSnapshots) string { return c.ClassificationLabelSetDigest }},
	{"classification_propagation_watermark", func(c intent.ControlSnapshots) string { return c.ClassificationPropagationWatermark }},
	{"dlp_decision_digest", func(c intent.ControlSnapshots) string { return c.DLPDecisionDigest }},
	{"destination_trust_digest", func(c intent.ControlSnapshots) string { return c.DestinationTrustDigest }},
	{"purpose_and_residency_digest", func(c intent.ControlSnapshots) string { return c.PurposeAndResidencyDigest }},
}

func changedControlSnapshots(a, b intent.ControlSnapshots) []string {
	var out []string
	for _, f := range controlSnapshotFields {
		if f.get(a) != f.get(b) {
			out = append(out, f.name)
		}
	}
	return out
}

// buildReceipt mints the zero-effect receipt for the assessment. The receipt
// refuses to exist over a non-zero effect count, which is what makes "assessing
// materiality changes nothing" a counted artifact rather than a claim.
func buildReceipt(req AssessRequest, a MaterialityAssessment) (evidence.ZeroEffectReceipt, error) {
	inputs := canonicalbytes.New(assessmentSchema+".inputs", 1).
		String("definition", req.DefinitionRef.String()).
		String("from_revision_id", req.From.ProposalRevisionID).
		String("to_revision_id", req.To.ProposalRevisionID).
		Field("from_material", materialResultBytes(req.From)).
		Field("to_material", materialResultBytes(req.To)).
		Count("decisions", len(req.Decisions))
	ids := make([]string, 0, len(req.Decisions))
	for _, d := range req.Decisions {
		ids = append(ids, d.DecisionID+"@"+d.Binding.ProposalDigest.Digest)
	}
	inputs.SortedStrings("decision_bindings", ids)
	for _, d := range req.Denies {
		inputs.String("deny", d.RuleID+"@"+d.ControlRef)
	}
	inputsRaw, err := inputs.Bytes()
	if err != nil {
		return evidence.ZeroEffectReceipt{}, err
	}

	result := canonicalbytes.New(assessmentSchema+".result", 1).
		String("verdict", string(a.Verdict)).
		Bool("material_change", a.MaterialChange).
		SortedStrings("changed_material_fields", a.ChangedMaterialFields).
		SortedStrings("changed_control_snapshots", a.ChangedControlSnapshots).
		SortedStrings("invalidated", a.InvalidatedDecisionIDs).
		SortedStrings("standing", a.StandingDecisionIDs).
		SortedStrings("unbound", a.UnboundDecisionIDs)
	resultRaw, err := result.Bytes()
	if err != nil {
		return evidence.ZeroEffectReceipt{}, err
	}

	var controls []evidence.ControlVersion
	for _, f := range controlSnapshotFields {
		if v := f.get(req.To.ControlSnapshots); v != "" {
			controls = append(controls, evidence.ControlVersion{Name: f.name, Version: v})
		}
	}
	return evidence.NewZeroEffectReceipt(
		req.DefinitionRef.TypeID,
		req.DefinitionRef.String(),
		evidence.ModePreflight,
		evidence.RequestStatePreflighted,
		controls,
		canonicalbytes.Digest(inputsRaw),
		canonicalbytes.Digest(resultRaw),
		evidence.ZeroEffects(),
	)
}
