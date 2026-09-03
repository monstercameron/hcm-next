package approval

import (
	"fmt"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/engines/rules"
	"github.com/monstercameron/hcm-next/internal/humanwork"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/intent/definitions"
	"github.com/monstercameron/hcm-next/internal/intent/protomap"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// The promotion fixture's identifiers.
const (
	// FixtureDefinitionText is the intent definition the fixture proposal is
	// built against.
	FixtureDefinitionText = "hcmnext.people.promote_worker/v1"
	// FixtureIntentID is the intent the proposal ledger belongs to.
	FixtureIntentID = "intent:promote:9001"
	// FixtureTenant is the fixture tenant.
	FixtureTenant = "acme-eu"
	// FixtureEmploymentStream is the stream the planned write pins a baseline on.
	FixtureEmploymentStream = "people.employment.9001"
	// FixtureAuthorityDecision is the per-field source-authority decision.
	FixtureAuthorityDecision = "authority.local_master/v1"
	// FixtureSessionRef is the approver session every fixture vote cites.
	FixtureSessionRef = "session:fixture"
	// FixtureAuthorizationRef is the authorization decision every fixture vote
	// cites.
	FixtureAuthorizationRef = "authz:decision:fixture"
)

// FixtureClock returns the clock the fixture mints revisions and decisions
// with. It is pinned so that goldens do not drift.
func FixtureClock() intent.Clock {
	at := humanwork.ScenarioAt()
	return func() values.Instant { return at }
}

// countingIDs mints deterministic identifiers under a prefix. Production uses
// UUIDv7; the fixture needs reproducible ids, which is exactly why the id
// source is a parameter everywhere rather than a package-level call.
func countingIDs(prefix string) intent.IDSource {
	n := 0
	return func() (string, error) {
		n++
		return fmt.Sprintf("%s-%08d-0000-7000-8000-000000000000", prefix, n), nil
	}
}

// Fixture is the assembled promotion scenario: the definition, an immutable
// proposal ledger, the requirement set and resolutions from the human-work
// package, the server-held rendered projections, and a binder over the current
// revision.
type Fixture struct {
	Definition  intent.Definition
	Digester    *protomap.Digester
	Clock       intent.Clock
	Scenario    humanwork.PromotionScenario
	Resolutions []humanwork.Resolution
	Projections []Projection
	Ledger      *intent.ProposalLedger
	Binder      *Binder

	baseSpec      intent.ProposalSpec
	revisionIDs   intent.IDSource
	decisionIDs   intent.IDSource
	currentRevBox intent.ProposalRevision
}

// NewPromotionFixture assembles the promotion scenario at the FINANCE_REQUIRED
// tier: four requirements, all resolvable, one minted proposal revision and a
// binder over it.
func NewPromotionFixture() (*Fixture, error) {
	return newPromotionFixture(humanwork.PromotionInputFinance())
}

// NewPromotionFixtureAt assembles the fixture at whatever tier the supplied
// rules input produces.
func NewPromotionFixtureAt(in rules.PromotionApprovalInput) (*Fixture, error) {
	return newPromotionFixture(in)
}

func newPromotionFixture(in rules.PromotionApprovalInput) (*Fixture, error) {
	reg, err := definitions.NewRegistry()
	if err != nil {
		return nil, err
	}
	def, err := reg.ResolveText(FixtureDefinitionText)
	if err != nil {
		return nil, err
	}
	d, err := protomap.NewDefaultDigester()
	if err != nil {
		return nil, err
	}
	sc, err := humanwork.NewPromotionScenario(in)
	if err != nil {
		return nil, err
	}
	resolutions, err := sc.ResolveAll()
	if err != nil {
		return nil, err
	}

	f := &Fixture{
		Definition:  def,
		Digester:    d,
		Clock:       FixtureClock(),
		Scenario:    sc,
		Resolutions: resolutions,
		Ledger:      intent.NewProposalLedger(FixtureIntentID),
		revisionIDs: countingIDs("revision"),
		decisionIDs: countingIDs("decision"),
	}
	for _, req := range sc.Requirements.Requirements {
		f.Projections = append(f.Projections, Projection{
			RequirementID: req.RequirementID,
			TaskVersion:   1,
			Digest:        RenderedProjectionDigest(req.RequirementID, 1),
		})
	}
	spec, err := promotionSpec(sc)
	if err != nil {
		return nil, err
	}
	f.baseSpec = spec

	rev, err := intent.NewProposalRevision(spec, def, d, f.revisionIDs, f.Clock)
	if err != nil {
		return nil, err
	}
	if err := f.Ledger.Append(rev); err != nil {
		return nil, err
	}
	f.currentRevBox = rev
	if err := f.rebind(); err != nil {
		return nil, err
	}
	return f, nil
}

// RenderedProjectionDigest is the fixture's stand-in for the server-side
// rendering APPROVAL-006 will digest. It is deterministic and server-side, and
// there is deliberately no way for a caller to supply one.
func RenderedProjectionDigest(requirementID string, taskVersion uint64) string {
	raw, err := canonicalbytes.New("hcmnext.intent.approval.RenderedProjection", 1).
		String("requirement_id", requirementID).
		Int("task_version", int64(taskVersion)).
		Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Current returns the latest recorded revision.
func (f *Fixture) Current() intent.ProposalRevision { return f.currentRevBox }

// BaseSpec returns a copy of the proposal specification the fixture builds
// revisions from, for a caller that wants to mutate one field and mint a
// successor.
func (f *Fixture) BaseSpec() intent.ProposalSpec { return f.baseSpec }

// NextRevision mints and appends the next revision, applying mutate to a copy
// of the base specification first. A nil mutate produces a materially identical
// successor, which is the immaterial case the materiality rule protects.
func (f *Fixture) NextRevision(mutate func(*intent.ProposalSpec)) (intent.ProposalRevision, error) {
	spec := f.baseSpec
	spec.Revision = uint64(f.Ledger.Len()) + 1
	prev := f.currentRevBox.ProposalRevisionID
	spec.SupersedesRevisionID = &prev
	if mutate != nil {
		mutate(&spec)
	}
	rev, err := intent.NewProposalRevision(spec, f.Definition, f.Digester, f.revisionIDs, f.Clock)
	if err != nil {
		return intent.ProposalRevision{}, err
	}
	if err := f.Ledger.Append(rev); err != nil {
		return intent.ProposalRevision{}, err
	}
	f.currentRevBox = rev
	if err := f.rebind(); err != nil {
		return intent.ProposalRevision{}, err
	}
	return rev, nil
}

// rebind replaces the binder with one over the current revision. A binder is
// bound to exactly one revision by construction, so a new revision is a new
// binder rather than a mutated one.
func (f *Fixture) rebind() error {
	b, err := NewBinder(f.currentRevBox, f.Scenario.Requirements, f.Resolutions,
		f.Projections, f.Clock, f.decisionIDs)
	if err != nil {
		return err
	}
	f.Binder = b
	return nil
}

// Vote builds a well-formed vote for one requirement from the fixture's
// server-held state. It asserts exactly what a legitimate client would assert:
// the revision, its digest, the control context and the task version it was
// shown.
func (f *Fixture) Vote(requirementID, principalID string, outcome Outcome) Vote {
	v := Vote{
		RequirementID:      requirementID,
		ProposalRevisionID: f.currentRevBox.ProposalRevisionID,
		ProposalDigest:     f.currentRevBox.MaterialDigest,
		ControlSnapshots:   f.currentRevBox.ControlSnapshots,
		TaskVersion:        1,
		Approver: ApproverReference{
			PrincipalID:          principalID,
			IdentityAssuranceRef: "assurance.mfa_session/v1",
			SessionRef:           FixtureSessionRef,
			Via:                  humanwork.SourceDirect,
		},
		Outcome:              outcome,
		Reason:               "reason.promotion_supported/v1",
		AuthorityDecisionRef: FixtureAuthorizationRef,
	}
	for _, res := range f.Resolutions {
		if res.RequirementID != requirementID {
			continue
		}
		if c, ok := res.Authorizes(principalID); ok {
			v.Approver.Via = c.Via
			v.Approver.DelegationID = c.DelegationID
			if c.IdentityAssuranceRef != "" {
				v.Approver.IdentityAssuranceRef = c.IdentityAssuranceRef
			}
		}
	}
	for _, p := range f.Projections {
		if p.RequirementID == requirementID {
			v.TaskVersion = p.TaskVersion
		}
	}
	return v
}

// promotionSpec builds the fixture proposal: the promotion's current and
// proposed state, one planned write with its authority decision and pinned
// baseline, a child compensation intent, a budget reservation, the approval
// requirements the scenario derived, the revalidation plan and the pinned
// control context.
func promotionSpec(sc humanwork.PromotionScenario) (intent.ProposalSpec, error) {
	key, err := values.NewResourceKey(values.TenantId(FixtureTenant),
		values.Kind("assignment"), "employment", "9001", "primary")
	if err != nil {
		return intent.ProposalSpec{}, err
	}
	baseline, err := values.NewSequenceRevision(FixtureEmploymentStream, 42)
	if err != nil {
		return intent.ProposalSpec{}, err
	}
	effective, err := values.NewOpenInstantInterval(
		values.NewInstant(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		return intent.ProposalSpec{}, err
	}
	cost, err := values.NewMoney("18000.00", "EUR", 2, values.RoundingHalfEven)
	if err != nil {
		return intent.ProposalSpec{}, err
	}
	subject := intent.SubjectReference{
		Kind: "EMPLOYMENT", SubjectID: "employment:9001", AuthorityDomain: "PEOPLE",
	}
	var required []intent.RequiredApproval
	for _, req := range sc.Requirements.Requirements {
		required = append(required, intent.RequiredApproval{
			RequirementID:        req.RequirementID,
			SeparationConstraint: req.Separation.RuleID,
		})
	}
	return intent.ProposalSpec{
		IntentID:            FixtureIntentID,
		Revision:            1,
		Tenant:              values.TenantId(FixtureTenant),
		OrganizationScopeID: humanwork.ScenarioOrganizationScopeID,
		LegalEntityID:       humanwork.ScenarioLegalEntityID,
		Subjects:            []intent.SubjectReference{subject},
		EffectiveTime:       effective,
		CurrentState: []intent.StateAssertion{{
			Subject: subject, ResourceKey: key, FieldPath: "assignment.position_ref",
			CanonicalText: "position:senior-engineer",
		}},
		ProposedState: []intent.StateAssertion{{
			Subject: subject, ResourceKey: key, FieldPath: "assignment.position_ref",
			CanonicalText: "position:engineering-manager",
		}},
		Writes: []intent.PlannedWrite{{
			Subject: subject, ResourceKey: key, FieldPath: "assignment.position_ref",
			CurrentCanonicalText:    "position:senior-engineer",
			ProposedCanonicalText:   "position:engineering-manager",
			SourceAuthorityDecision: FixtureAuthorityDecision,
			ExpectedRevision:        baseline,
		}},
		Children: []intent.ChildIntentBinding{{
			Definition:          intent.Ref{TypeID: "hcmnext.rewards.change_base_pay", Version: 1},
			ChildIntentID:       "child:base-pay",
			Ordinal:             1,
			MaterialInputDigest: "child-material-base-pay",
		}},
		Reservations: []intent.Reservation{{
			ReservationID: "reservation:budget-1", Kind: "BUDGET",
			Expiry: values.NewInstant(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)),
		}},
		RequiredApprovals: required,
		SourceBaselines: []intent.SourceBaseline{{
			StreamID: FixtureEmploymentStream, ExpectedRevision: baseline,
		}},
		Attachments: []intent.AttachmentRef{{
			ArtifactID: "artifact:justification", AlgorithmID: "sha256", Digest: "abcd",
		}},
		Purpose: intent.PurposeDecision{
			Purpose:        "promotion.annual_cycle",
			RecipientRef:   "recipient:hr-ops",
			DestinationRef: "destination:internal",
			ResidencyRef:   "residency:eu",
		},
		Cost:         &cost,
		Revalidation: intent.RevalidationPlan{Rules: []string{"promotion_execution_revalidation/v1"}},
		ControlSnapshots: intent.ControlSnapshots{
			CapabilityRegistryDigest:     "cap-registry-1",
			PolicyBundleDigest:           "policy-bundle-1",
			LegalContextDigest:           "legal-1",
			EntitlementDigest:            "entitlement-1",
			ReferenceDataDigest:          "reference-1",
			ClassificationTaxonomyDigest: "taxonomy-1",
			ClassificationLabelSetDigest: "labels-1",
			DLPDecisionDigest:            "dlp-1",
		},
		CreatedBy: intent.PrincipalReference{
			PrincipalID:          humanwork.PrincipalRequester,
			Kind:                 intent.InitiatorHuman,
			IdentityAssuranceRef: "assurance.mfa_session/v1",
		},
		DetectedChildRefs: []intent.Ref{
			{TypeID: "hcmnext.rewards.change_base_pay", Version: 1},
		},
	}, nil
}
