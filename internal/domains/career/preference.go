package career

import (
	"fmt"
	"strings"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// PreferenceVisibility controls who may read a preference. Sensitive
// mobility details must never be exposed by a broader visibility setting.
type PreferenceVisibility string

const (
	VisibilityWorkerOnly          PreferenceVisibility = "WORKER_ONLY"
	VisibilityWorkerAndAuthorized PreferenceVisibility = "WORKER_AND_AUTHORIZED"
)

// CareerPreferenceRevision is an immutable, worker-authored career preference.
// It is intentionally not an assessment or a prediction.
type CareerPreferenceRevision struct {
	PreferenceID       values.EntityRef
	Revision           values.RevisionToken
	Worker             values.EntityRef
	Roles              []string
	Locations          []string
	WorkArrangements   []string
	MobilityPreference string
	TravelPreference   string
	TimingConstraints  string
	Visibility         PreferenceVisibility
	ConsentRef         string
	Effective          values.EffectiveInterval
}

func (p CareerPreferenceRevision) Validate() error {
	if err := requireRef(p.PreferenceID, "preference", "career_preference"); err != nil {
		return err
	}
	if err := requireRevision(p.Revision, "preference"); err != nil {
		return err
	}
	if err := requireRef(p.Worker, "worker", "worker"); err != nil {
		return err
	}
	if p.PreferenceID.Tenant != p.Worker.Tenant {
		return fmt.Errorf("%w: preference and worker tenants differ", ErrInvalidReference)
	}
	if len(p.Roles) == 0 && len(p.Locations) == 0 && len(p.WorkArrangements) == 0 && strings.TrimSpace(p.MobilityPreference) == "" && strings.TrimSpace(p.TravelPreference) == "" && strings.TrimSpace(p.TimingConstraints) == "" {
		return fmt.Errorf("%w: preference must contain at least one choice", ErrInvalidRevision)
	}
	if p.Visibility != VisibilityWorkerOnly && p.Visibility != VisibilityWorkerAndAuthorized {
		return fmt.Errorf("%w: invalid visibility %q", ErrInvalidRevision, p.Visibility)
	}
	if strings.TrimSpace(p.MobilityPreference) != "" && strings.TrimSpace(p.ConsentRef) == "" {
		return fmt.Errorf("%w: mobility preference requires consent", ErrInvalidRevision)
	}
	if err := p.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrInvalidRevision, err)
	}
	return nil
}
