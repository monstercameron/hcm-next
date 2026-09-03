package career

import (
	"fmt"
	"strings"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// DevelopmentObjectiveRevision is a worker/manager-agreed objective, kept
// distinct from role-fit assessment and from predicted outcomes.
type DevelopmentObjectiveRevision struct {
	ObjectiveID values.EntityRef
	Revision    values.RevisionToken
	Worker      values.EntityRef
	TargetRole  values.EntityRef
	SkillRefs   []values.EntityRef
	Description string
	Owner       values.EntityRef
	Visibility  PreferenceVisibility
	Effective   values.EffectiveInterval
}

func (o DevelopmentObjectiveRevision) Validate() error {
	if err := requireRef(o.ObjectiveID, "objective", "development_objective"); err != nil {
		return err
	}
	if err := requireRevision(o.Revision, "objective"); err != nil {
		return err
	}
	if err := requireRef(o.Worker, "worker", "worker"); err != nil {
		return err
	}
	if err := requireRef(o.TargetRole, "target role", "career_target_role"); err != nil {
		return err
	}
	if err := requireRef(o.Owner, "owner", "career_owner"); err != nil {
		return err
	}
	if o.ObjectiveID.Tenant != o.Worker.Tenant || o.Worker.Tenant != o.TargetRole.Tenant || o.Worker.Tenant != o.Owner.Tenant {
		return fmt.Errorf("%w: objective references have different tenants", ErrInvalidReference)
	}
	if strings.TrimSpace(o.Description) == "" {
		return fmt.Errorf("%w: objective description is required", ErrInvalidRevision)
	}
	if len(o.SkillRefs) == 0 {
		return fmt.Errorf("%w: at least one skill is required", ErrInvalidRevision)
	}
	for i, skill := range o.SkillRefs {
		if err := requireRef(skill, fmt.Sprintf("skill[%d]", i), "skill"); err != nil {
			return err
		}
		if skill.Tenant != o.Worker.Tenant {
			return fmt.Errorf("%w: skill tenant differs", ErrInvalidReference)
		}
	}
	if o.Visibility != VisibilityWorkerOnly && o.Visibility != VisibilityWorkerAndAuthorized {
		return fmt.Errorf("%w: invalid visibility %q", ErrInvalidRevision, o.Visibility)
	}
	if err := o.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrInvalidRevision, err)
	}
	return nil
}
