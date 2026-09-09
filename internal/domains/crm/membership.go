package crm

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type MembershipRole string

const (
	MembershipCandidate MembershipRole = "CANDIDATE"
	MembershipWorker    MembershipRole = "WORKER"
)

// TalentPoolMembershipRevision links a candidate or worker to a pool. Roles
// are intentionally non-exclusive: the same subject may hold either role (or
// both across separate memberships) without an artificial uniqueness rule.
type TalentPoolMembershipRevision struct {
	MembershipID  values.EntityRef
	Revision      values.RevisionToken
	Pool          values.EntityRef
	Subject       values.EntityRef
	Role          MembershipRole
	Purpose       string
	Source        SourceAttribution
	Consent       ConsentAuthority
	Scope         Scope
	Effective     values.EffectiveInterval
	Owner         values.EntityRef
	RemovalPolicy RemovalPolicy
}

type MembershipRevision = TalentPoolMembershipRevision

func (m TalentPoolMembershipRevision) Validate() error {
	if err := requireRef(m.MembershipID, "membership", "talent_pool_membership"); err != nil {
		return err
	}
	if !m.Revision.IsSpecified() {
		return fmt.Errorf("%w: membership revision is required", ErrInvalidMembership)
	}
	if err := requireRef(m.Pool, "pool", "talent_pool"); err != nil {
		return err
	}
	if err := m.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %v", ErrInvalidMembership, err)
	}
	if m.Subject.Kind != "candidate" && m.Subject.Kind != "worker" && m.Subject.Kind != "prospect" {
		return fmt.Errorf("%w: subject must be candidate, worker, or prospect", ErrInvalidMembership)
	}
	if (m.Role != MembershipCandidate && m.Role != MembershipWorker) || (m.Role == MembershipCandidate && m.Subject.Kind == "worker") || (m.Role == MembershipWorker && m.Subject.Kind == "candidate") {
		return fmt.Errorf("%w: role does not match subject", ErrInvalidMembership)
	}
	if strings.TrimSpace(m.Purpose) == "" {
		return fmt.Errorf("%w: purpose is required", ErrInvalidMembership)
	}
	if strings.TrimSpace(m.Source.System) == "" || strings.TrimSpace(m.Source.Reference) == "" {
		return fmt.Errorf("%w: source attribution is required", ErrInvalidMembership)
	}
	if err := requireRef(m.Source.RecordedBy, "source recorder", "principal"); err != nil {
		return err
	}
	if strings.TrimSpace(m.Consent.Basis) == "" {
		return fmt.Errorf("%w: consent/processing basis is required", ErrInvalidMembership)
	}
	if err := requireRef(m.Consent.Authority, "consent authority", "processing_authority"); err != nil {
		return err
	}
	if err := requireRef(m.Consent.Evidence, "consent evidence", "evidence"); err != nil {
		return err
	}
	if err := requireRef(m.Scope.Organization, "scope organization", "organization"); err != nil {
		return err
	}
	if err := requireRef(m.Owner, "owner", "owner"); err != nil {
		return err
	}
	if err := m.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrInvalidMembership, err)
	}
	if m.RemovalPolicy != RemovalManual && m.RemovalPolicy != RemovalAtIntervalEnd && m.RemovalPolicy != RemovalOnConsentEnd && m.RemovalPolicy != RemovalOnProcessingEnd {
		return fmt.Errorf("%w: invalid removal policy %q", ErrInvalidMembership, m.RemovalPolicy)
	}
	if err := sameTenant(m.MembershipID, m.Pool, m.Subject, m.Source.RecordedBy, m.Consent.Authority, m.Consent.Evidence, m.Scope.Organization, m.Owner); err != nil {
		return err
	}
	return nil
}
