package career

import (
	"fmt"
	"strings"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TargetRoleRevision records the worker's desired role against an immutable
// JobProfile revision. A role name alone is not an authoritative target.
type TargetRoleRevision struct {
	TargetRoleID       values.EntityRef
	Revision           values.RevisionToken
	Worker             values.EntityRef
	JobProfile         values.EntityRef
	JobProfileRevision values.RevisionToken
	Priority           int
	Rationale          string
	Visibility         PreferenceVisibility
	Effective          values.EffectiveInterval
}

func (t TargetRoleRevision) Validate() error {
	if err := requireRef(t.TargetRoleID, "target role", "career_target_role"); err != nil {
		return err
	}
	if err := requireRevision(t.Revision, "target role"); err != nil {
		return err
	}
	if err := requireRef(t.Worker, "worker", "worker"); err != nil {
		return err
	}
	if err := requireRef(t.JobProfile, "job profile", "job_profile"); err != nil {
		return err
	}
	if t.TargetRoleID.Tenant != t.Worker.Tenant || t.Worker.Tenant != t.JobProfile.Tenant {
		return fmt.Errorf("%w: target references have different tenants", ErrInvalidReference)
	}
	if !t.JobProfileRevision.IsSpecified() {
		return fmt.Errorf("%w: job profile revision is required", ErrInvalidRevision)
	}
	if t.Priority < 0 {
		return fmt.Errorf("%w: priority must not be negative", ErrInvalidRevision)
	}
	if strings.TrimSpace(t.Rationale) == "" {
		return fmt.Errorf("%w: rationale is required", ErrInvalidRevision)
	}
	if t.Visibility != VisibilityWorkerOnly && t.Visibility != VisibilityWorkerAndAuthorized {
		return fmt.Errorf("%w: invalid visibility %q", ErrInvalidRevision, t.Visibility)
	}
	if err := t.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrInvalidRevision, err)
	}
	return nil
}

// EpistemicClass identifies what an assessment means. Predictions and
// recommendations are not workforce facts.
type EpistemicClass string

const (
	EpistemicFact           EpistemicClass = "FACT"
	EpistemicHumanOpinion   EpistemicClass = "HUMAN_OPINION"
	EpistemicModelInference EpistemicClass = "MODEL_INFERENCE"
	EpistemicRecommendation EpistemicClass = "RECOMMENDATION"
)

func (e EpistemicClass) valid() bool {
	return e == EpistemicFact || e == EpistemicHumanOpinion || e == EpistemicModelInference || e == EpistemicRecommendation
}

// CareerAssessmentRevision is a separately sourced opinion or inference about
// a worker and target role. It cannot overwrite the worker preference.
type CareerAssessmentRevision struct {
	AssessmentID values.EntityRef
	Revision     values.RevisionToken
	Worker       values.EntityRef
	TargetRole   values.EntityRef
	Source       values.EntityRef
	Epistemic    EpistemicClass
	Visibility   PreferenceVisibility
	Summary      string
	Effective    values.EffectiveInterval
}

func (a CareerAssessmentRevision) Validate() error {
	if err := requireRef(a.AssessmentID, "assessment", "career_assessment"); err != nil {
		return err
	}
	if err := requireRevision(a.Revision, "assessment"); err != nil {
		return err
	}
	for _, x := range []struct {
		ref        values.EntityRef
		name, kind string
	}{{a.Worker, "worker", "worker"}, {a.TargetRole, "target role", "career_target_role"}, {a.Source, "source", "source"}} {
		if err := requireRef(x.ref, x.name, x.kind); err != nil {
			if x.name == "source" && string(a.Source.Kind) == "career_source" {
				continue
			}
			return err
		}
	}
	if a.AssessmentID.Tenant != a.Worker.Tenant || a.Worker.Tenant != a.TargetRole.Tenant || a.Worker.Tenant != a.Source.Tenant {
		return fmt.Errorf("%w: assessment references have different tenants", ErrInvalidReference)
	}
	if !a.Epistemic.valid() || a.Epistemic == EpistemicFact {
		return fmt.Errorf("%w: assessment must be opinion, inference, or recommendation", ErrInvalidRevision)
	}
	if a.Visibility != VisibilityWorkerOnly && a.Visibility != VisibilityWorkerAndAuthorized {
		return fmt.Errorf("%w: invalid visibility %q", ErrInvalidRevision, a.Visibility)
	}
	if strings.TrimSpace(a.Summary) == "" {
		return fmt.Errorf("%w: summary is required", ErrInvalidRevision)
	}
	if err := a.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrInvalidRevision, err)
	}
	return nil
}

func requireRef(ref values.EntityRef, name, kind string) error {
	if err := ref.Validate(); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInvalidReference, name, err)
	}
	if string(ref.Kind) != kind {
		return fmt.Errorf("%w: %s kind %q, want %q", ErrInvalidReference, name, ref.Kind, kind)
	}
	return nil
}
func requireRevision(rev values.RevisionToken, name string) error {
	if !rev.IsSpecified() {
		return fmt.Errorf("%w: %s revision is required", ErrInvalidRevision, name)
	}
	if err := rev.Validate(); err != nil {
		return fmt.Errorf("%w: %s revision: %v", ErrInvalidRevision, name, err)
	}
	return nil
}
