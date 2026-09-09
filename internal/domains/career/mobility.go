package career

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const internalMobilityRecommendationPurpose = "internal mobility recommendations"

var (
	ErrMobilityVerifierRequired = errors.New("career: mobility verifier is required")
	ErrMobilityNotAuthorized    = errors.New("career: mobility verification rejected")
)

// MobilityVerifier is the trusted port for current consent, preference,
// population and eligibility facts. Entity references and caller booleans are
// identifiers and claims only; implementations must verify them authoritatively.
type MobilityVerifier interface {
	VerifyBasis(context.Context, MobilityBasisRequest) error
	VerifyOpportunity(context.Context, MobilityOpportunityRequest) (MobilityEligibilityEvidence, error)
	VerifyApplication(context.Context, MobilityApplicationRequest) error
}

type MobilityBasisRequest struct {
	Worker             values.EntityRef
	PreferenceID       values.EntityRef
	PreferenceRevision values.RevisionToken
	PreferenceDigest   string
	ConsentRef         values.EntityRef
	Purpose            string
	AsOf               values.LocalDate
}

type MobilityOpportunityRequest struct {
	Basis         MobilityBasisRequest
	OpportunityID values.EntityRef
	Role          values.EntityRef
	RoleRevision  values.RevisionToken
}

type MobilityApplicationRequest struct {
	AssessmentID              values.EntityRef
	AssessmentRevision        values.RevisionToken
	AssessmentDigest          string
	Worker                    values.EntityRef
	PreferenceRevision        values.RevisionToken
	PreferenceDigest          string
	ConsentRef                values.EntityRef
	Purpose                   string
	OpportunityID             values.EntityRef
	Role                      values.EntityRef
	RoleRevision              values.RevisionToken
	PopulationAuthority       values.EntityRef
	EligibilityPolicyRevision values.RevisionToken
	AsOf                      values.LocalDate
}

type MobilityEligibilityEvidence struct {
	PopulationAuthority       values.EntityRef
	EligibilityPolicyRevision values.RevisionToken
	Eligible                  bool
	EligibilityReason         string
	MatchExplanation          string
}

// MobilityConsent is purpose-bound worker permission. It is deliberately not
// a general authorization to disclose or change employment state.
type MobilityConsent struct {
	ConsentRef values.EntityRef
	Worker     values.EntityRef
	Purpose    string
	Effective  values.EffectiveInterval
	Withdrawn  bool
}

func (c MobilityConsent) Validate() error {
	if err := requireRef(c.ConsentRef, "consent", "consent"); err != nil {
		return err
	}
	if err := requireRef(c.Worker, "worker", "worker"); err != nil {
		return err
	}
	if c.ConsentRef.Tenant != c.Worker.Tenant {
		return fmt.Errorf("%w: consent and worker tenants differ", ErrInvalidReference)
	}
	if c.Purpose != internalMobilityRecommendationPurpose {
		return fmt.Errorf("%w: consent is not purpose-bound to internal mobility recommendations", ErrInvalidRevision)
	}
	if err := c.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: consent effective interval: %v", ErrInvalidRevision, err)
	}
	return nil
}

func (c MobilityConsent) ActiveAt(at values.LocalDate) (bool, error) {
	if err := c.Validate(); err != nil {
		return false, err
	}
	if c.Withdrawn {
		return false, nil
	}
	return c.Effective.ContainsDate(at)
}

// Withdraw stops future mobility use while retaining the consent evidence.
func (c MobilityConsent) Withdraw() (MobilityConsent, error) {
	if err := c.Validate(); err != nil {
		return MobilityConsent{}, err
	}
	if c.Withdrawn {
		return MobilityConsent{}, fmt.Errorf("%w: consent is already withdrawn", ErrInvalidRevision)
	}
	c.Withdrawn = true
	return c, nil
}

type MobilityOpportunity struct {
	OpportunityID values.EntityRef
	Role          values.EntityRef
	RoleRevision  values.RevisionToken
}

func (o MobilityOpportunity) Validate(tenant values.TenantId) error {
	if err := requireRef(o.OpportunityID, "opportunity", "mobility_opportunity"); err != nil {
		return err
	}
	if err := requireRef(o.Role, "role", "job_profile"); err != nil {
		return err
	}
	if o.OpportunityID.Tenant != tenant || o.Role.Tenant != tenant {
		return fmt.Errorf("%w: opportunity references have different tenants", ErrInvalidReference)
	}
	if !o.RoleRevision.IsSpecified() {
		return fmt.Errorf("%w: role revision is required", ErrInvalidRevision)
	}
	return nil
}

type MobilityCandidate struct {
	OpportunityID             values.EntityRef
	Role                      values.EntityRef
	RoleRevision              values.RevisionToken
	PopulationAuthority       values.EntityRef
	EligibilityPolicyRevision values.RevisionToken
	Eligible                  bool
	EligibilityReason         string
	MatchExplanation          string
}

type InternalMobilityAssessment struct {
	AssessmentID       values.EntityRef
	Revision           values.RevisionToken
	Supersedes         values.RevisionToken
	Worker             values.EntityRef
	PreferenceRevision values.RevisionToken
	PreferenceDigest   string
	ConsentRef         values.EntityRef
	Candidates         []MobilityCandidate
	Provenance         string
	Visibility         PreferenceVisibility
	CanonicalDigest    string
}

func (a InternalMobilityAssessment) Validate() error {
	if err := requireRef(a.AssessmentID, "assessment", "internal_mobility_assessment"); err != nil {
		return err
	}
	if err := requireRevision(a.Revision, "assessment"); err != nil {
		return err
	}
	if err := requireRef(a.Worker, "worker", "worker"); err != nil {
		return err
	}
	if err := requireRevision(a.PreferenceRevision, "preference"); err != nil {
		return err
	}
	if strings.TrimSpace(a.PreferenceDigest) == "" {
		return fmt.Errorf("%w: preference digest is required", ErrInvalidRevision)
	}
	if err := requireRef(a.ConsentRef, "consent", "consent"); err != nil {
		return err
	}
	if a.AssessmentID.Tenant != a.Worker.Tenant || a.Worker.Tenant != a.ConsentRef.Tenant {
		return fmt.Errorf("%w: assessment references have different tenants", ErrInvalidReference)
	}
	seen := map[string]struct{}{}
	for i, c := range a.Candidates {
		if err := c.OpportunityID.Validate(); err != nil {
			return fmt.Errorf("%w: candidate[%d]: %v", ErrInvalidReference, i, err)
		}
		if c.OpportunityID.Kind != values.Kind("mobility_opportunity") {
			return fmt.Errorf("%w: candidate[%d] opportunity kind is invalid", ErrInvalidReference, i)
		}
		if err := requireRef(c.Role, "candidate role", "job_profile"); err != nil {
			return fmt.Errorf("%w: candidate[%d] role: %v", ErrInvalidReference, i, err)
		}
		if c.OpportunityID.Tenant != a.Worker.Tenant || c.Role.Tenant != a.Worker.Tenant {
			return fmt.Errorf("%w: candidate[%d] tenant differs", ErrInvalidReference, i)
		}
		if !c.RoleRevision.IsSpecified() || strings.TrimSpace(c.EligibilityReason) == "" || strings.TrimSpace(c.MatchExplanation) == "" {
			return fmt.Errorf("%w: candidate[%d] is not explainable", ErrInvalidRevision, i)
		}
		if err := requireRef(c.PopulationAuthority, "candidate population authority", "population_authority"); err != nil {
			return fmt.Errorf("%w: candidate[%d] population authority: %v", ErrInvalidReference, i, err)
		}
		if c.PopulationAuthority.Tenant != a.Worker.Tenant || !c.EligibilityPolicyRevision.IsSpecified() {
			return fmt.Errorf("%w: candidate[%d] lacks tenant-bound eligibility authority", ErrInvalidReference, i)
		}
		if !c.Eligible {
			return fmt.Errorf("%w: candidate[%d] is not eligible and must not be disclosed", ErrInvalidRevision, i)
		}
		if _, ok := seen[c.OpportunityID.String()]; ok {
			return fmt.Errorf("%w: duplicate candidate", ErrInvalidRevision)
		}
		seen[c.OpportunityID.String()] = struct{}{}
	}
	if a.Visibility != VisibilityWorkerOnly && a.Visibility != VisibilityWorkerAndAuthorized {
		return fmt.Errorf("%w: visibility is invalid", ErrInvalidRevision)
	}
	if strings.TrimSpace(a.Provenance) == "" {
		return fmt.Errorf("%w: provenance is required", ErrInvalidRevision)
	}
	if a.CanonicalDigest != "" && a.CanonicalDigest != canonicalbytes.Digest(a.canonical()) {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidRevision)
	}
	return nil
}

func (a InternalMobilityAssessment) canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.career.InternalMobilityAssessment", careerSchemaVersion).Value("assessment_id", a.AssessmentID).Value("revision", a.Revision).Value("supersedes", a.Supersedes).Value("worker", a.Worker).Value("preference_revision", a.PreferenceRevision).String("preference_digest", a.PreferenceDigest).Value("consent_ref", a.ConsentRef).String("provenance", a.Provenance).String("visibility", string(a.Visibility)).Count("candidates", len(a.Candidates))
	for _, c := range a.Candidates {
		w.Value("opportunity_id", c.OpportunityID).Value("role", c.Role).Value("role_revision", c.RoleRevision).Value("population_authority", c.PopulationAuthority).Value("eligibility_policy_revision", c.EligibilityPolicyRevision).Bool("eligible", c.Eligible).String("eligibility_reason", c.EligibilityReason).String("match_explanation", c.MatchExplanation)
	}
	b, _ := w.Bytes()
	return b
}

func NewInternalMobilityAssessment(a InternalMobilityAssessment) (InternalMobilityAssessment, error) {
	a.CanonicalDigest = ""
	if err := a.Validate(); err != nil {
		return InternalMobilityAssessment{}, err
	}
	a.CanonicalDigest = canonicalbytes.Digest(a.canonical())
	return a, nil
}

// EvaluateInternalMobility fails closed: no consent or a withdrawn consent
// yields no candidate set and cannot reveal denied opportunities.
func EvaluateInternalMobility(ctx context.Context, verifier MobilityVerifier, worker values.EntityRef, preference CareerPreferenceProfileRevision, consent MobilityConsent, asOf values.LocalDate, opportunities []MobilityOpportunity, assessmentID values.EntityRef, revision values.RevisionToken) (InternalMobilityAssessment, error) {
	if verifier == nil {
		return InternalMobilityAssessment{}, ErrMobilityVerifierRequired
	}
	if err := requireRef(worker, "worker", "worker"); err != nil {
		return InternalMobilityAssessment{}, err
	}
	if err := preference.Validate(); err != nil {
		return InternalMobilityAssessment{}, err
	}
	if preference.Worker != worker {
		return InternalMobilityAssessment{}, fmt.Errorf("%w: worker differs from preference", ErrInvalidReference)
	}
	if consent.Worker != worker {
		return InternalMobilityAssessment{}, fmt.Errorf("%w: worker differs from consent", ErrInvalidReference)
	}
	active, err := consent.ActiveAt(asOf)
	if err != nil {
		return InternalMobilityAssessment{}, err
	}
	if !active {
		return InternalMobilityAssessment{}, fmt.Errorf("%w: mobility consent is not active", ErrInvalidRevision)
	}
	basis := MobilityBasisRequest{Worker: worker, PreferenceID: preference.PreferenceID, PreferenceRevision: preference.Revision, PreferenceDigest: preference.CanonicalDigest, ConsentRef: consent.ConsentRef, Purpose: consent.Purpose, AsOf: asOf}
	if err := verifier.VerifyBasis(ctx, basis); err != nil {
		return InternalMobilityAssessment{}, fmt.Errorf("%w: basis: %v", ErrMobilityNotAuthorized, err)
	}
	candidates := make([]MobilityCandidate, 0, len(opportunities))
	for _, o := range opportunities {
		if err := o.Validate(worker.Tenant); err != nil {
			return InternalMobilityAssessment{}, err
		}
		evidence, err := verifier.VerifyOpportunity(ctx, MobilityOpportunityRequest{Basis: basis, OpportunityID: o.OpportunityID, Role: o.Role, RoleRevision: o.RoleRevision})
		if err != nil {
			return InternalMobilityAssessment{}, fmt.Errorf("%w: opportunity: %v", ErrMobilityNotAuthorized, err)
		}
		candidate := MobilityCandidate{OpportunityID: o.OpportunityID, Role: o.Role, RoleRevision: o.RoleRevision, PopulationAuthority: evidence.PopulationAuthority, EligibilityPolicyRevision: evidence.EligibilityPolicyRevision, Eligible: evidence.Eligible, EligibilityReason: evidence.EligibilityReason, MatchExplanation: evidence.MatchExplanation}
		if evidence.Eligible {
			candidates = append(candidates, candidate)
		}
	}
	return NewInternalMobilityAssessment(InternalMobilityAssessment{AssessmentID: assessmentID, Revision: revision, Worker: worker, PreferenceRevision: preference.Revision, PreferenceDigest: preference.CanonicalDigest, ConsentRef: consent.ConsentRef, Candidates: candidates, Provenance: preference.CanonicalDigest, Visibility: VisibilityWorkerAndAuthorized})
}

type MobilityApplicationIntent struct {
	IntentID, Worker, OpportunityID, AssessmentID values.EntityRef
	AssessmentRevision                            values.RevisionToken
}

func CreateMobilityApplicationIntent(ctx context.Context, verifier MobilityVerifier, asOf values.LocalDate, id values.EntityRef, a InternalMobilityAssessment, opportunity values.EntityRef) (MobilityApplicationIntent, error) {
	if verifier == nil {
		return MobilityApplicationIntent{}, ErrMobilityVerifierRequired
	}
	if err := a.Validate(); err != nil {
		return MobilityApplicationIntent{}, err
	}
	if err := requireRef(id, "intent", "application_intent"); err != nil {
		return MobilityApplicationIntent{}, err
	}
	if err := requireRef(opportunity, "opportunity", "mobility_opportunity"); err != nil {
		return MobilityApplicationIntent{}, err
	}
	if id.Tenant != a.Worker.Tenant || opportunity.Tenant != a.Worker.Tenant {
		return MobilityApplicationIntent{}, fmt.Errorf("%w: application intent references have different tenants", ErrInvalidReference)
	}
	if err := asOf.Validate(); err != nil {
		return MobilityApplicationIntent{}, fmt.Errorf("%w: application as-of date: %v", ErrInvalidRevision, err)
	}
	for _, c := range a.Candidates {
		if c.OpportunityID == opportunity && c.Eligible {
			request := MobilityApplicationRequest{AssessmentID: a.AssessmentID, AssessmentRevision: a.Revision, AssessmentDigest: a.CanonicalDigest, Worker: a.Worker, PreferenceRevision: a.PreferenceRevision, PreferenceDigest: a.PreferenceDigest, ConsentRef: a.ConsentRef, Purpose: internalMobilityRecommendationPurpose, OpportunityID: c.OpportunityID, Role: c.Role, RoleRevision: c.RoleRevision, PopulationAuthority: c.PopulationAuthority, EligibilityPolicyRevision: c.EligibilityPolicyRevision, AsOf: asOf}
			if err := verifier.VerifyApplication(ctx, request); err != nil {
				return MobilityApplicationIntent{}, fmt.Errorf("%w: application basis: %v", ErrMobilityNotAuthorized, err)
			}
			return MobilityApplicationIntent{id, a.Worker, opportunity, a.AssessmentID, a.Revision}, nil
		}
	}
	return MobilityApplicationIntent{}, fmt.Errorf("%w: opportunity is not an eligible candidate", ErrInvalidRevision)
}

// Correct appends a new recommendation basis and preserves the prior digest.
func CorrectInternalMobilityAssessment(prior, corrected InternalMobilityAssessment) (InternalMobilityAssessment, error) {
	if err := prior.Validate(); err != nil {
		return InternalMobilityAssessment{}, err
	}
	if corrected.AssessmentID != prior.AssessmentID || corrected.Worker != prior.Worker || corrected.ConsentRef != prior.ConsentRef {
		return InternalMobilityAssessment{}, fmt.Errorf("%w: correction identity, worker, and consent must match prior revision", ErrInvalidReference)
	}
	if comparison, err := corrected.Revision.CompareInStream(prior.Revision); err != nil || comparison <= 0 {
		return InternalMobilityAssessment{}, fmt.Errorf("%w: correction revision must advance prior revision", ErrInvalidRevision)
	}
	if corrected.Supersedes.IsSpecified() && !corrected.Supersedes.Equal(prior.Revision) {
		return InternalMobilityAssessment{}, fmt.Errorf("%w: correction must supersede prior revision", ErrInvalidRevision)
	}
	corrected.Supersedes = prior.Revision
	corrected.Provenance = prior.CanonicalDigest + "\n" + corrected.Provenance
	corrected.Candidates = append([]MobilityCandidate(nil), corrected.Candidates...)
	return NewInternalMobilityAssessment(corrected)
}
