package crm

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ProspectSourceAttribution is the immutable provenance captured when a
// prospect enters recruiting. Source is the system/event lineage; campaign
// and referral are independent dimensions and must never be inferred later.
type ProspectSourceAttribution struct {
	Source   SourceAttribution
	Campaign string
	Referral string
	Channel  string
}

func (a ProspectSourceAttribution) Validate() error {
	if strings.TrimSpace(a.Source.System) == "" || strings.TrimSpace(a.Source.Reference) == "" {
		return fmt.Errorf("%w: source system and reference are required", ErrInvalidProspect)
	}
	if err := requireRef(a.Source.RecordedBy, "source recorder", "principal"); err != nil {
		return err
	}
	if strings.TrimSpace(a.Campaign) == "" && strings.TrimSpace(a.Referral) == "" {
		return fmt.Errorf("%w: campaign or referral lineage is required", ErrInvalidProspect)
	}
	if strings.TrimSpace(a.Channel) == "" {
		return fmt.Errorf("%w: source channel is required", ErrInvalidProspect)
	}
	return nil
}

// ProspectConsent is the processing authority for prospect outreach. It is
// append-only by convention: Withdraw returns a new value and leaves the
// original grant auditable. Expiry and withdrawal both stop future outreach.
type ProspectConsent struct {
	Authority   values.EntityRef
	Basis       string
	Evidence    values.EntityRef
	GrantedAt   values.Instant
	ExpiresAt   values.Instant
	WithdrawnAt values.Instant
}

const ProspectConsentBasis = "consent"

type ProspectConsentStatus string

const (
	ProspectConsentGranted   ProspectConsentStatus = "GRANTED"
	ProspectConsentExpired   ProspectConsentStatus = "EXPIRED"
	ProspectConsentWithdrawn ProspectConsentStatus = "WITHDRAWN"
	ProspectConsentInvalid   ProspectConsentStatus = "INVALID"
)

func (c ProspectConsent) Validate() error {
	if err := requireRef(c.Authority, "consent authority", "processing_authority"); err != nil {
		return err
	}
	if strings.ToLower(strings.TrimSpace(c.Basis)) != ProspectConsentBasis {
		return fmt.Errorf("%w: basis must be %q", ErrInvalidProspect, ProspectConsentBasis)
	}
	if err := requireRef(c.Evidence, "consent evidence", "evidence"); err != nil {
		return err
	}
	if err := c.GrantedAt.Validate(); err != nil {
		return fmt.Errorf("%w: granted_at: %v", ErrInvalidProspect, err)
	}
	if c.ExpiresAt.IsSet() && !c.GrantedAt.Before(c.ExpiresAt) {
		return fmt.Errorf("%w: expires_at must follow granted_at", ErrInvalidProspect)
	}
	if c.WithdrawnAt.IsSet() && !c.GrantedAt.Before(c.WithdrawnAt) {
		return fmt.Errorf("%w: withdrawn_at must follow granted_at", ErrInvalidProspect)
	}
	if c.WithdrawnAt.IsSet() && c.ExpiresAt.IsSet() && !c.WithdrawnAt.Before(c.ExpiresAt) {
		return fmt.Errorf("%w: withdrawn_at must precede expires_at", ErrInvalidProspect)
	}
	if err := sameTenant(c.Authority, c.Evidence); err != nil {
		return err
	}
	return nil
}

func (c ProspectConsent) StatusAt(at values.Instant) ProspectConsentStatus {
	if c.Validate() != nil || !at.IsSet() || at.Before(c.GrantedAt) {
		return ProspectConsentInvalid
	}
	if c.WithdrawnAt.IsSet() && !at.Before(c.WithdrawnAt) {
		return ProspectConsentWithdrawn
	}
	if c.ExpiresAt.IsSet() && !at.Before(c.ExpiresAt) {
		return ProspectConsentExpired
	}
	return ProspectConsentGranted
}

func (c ProspectConsent) Withdraw(at values.Instant) (ProspectConsent, error) {
	if err := c.Validate(); err != nil {
		return ProspectConsent{}, err
	}
	if !at.IsSet() || !c.GrantedAt.Before(at) {
		return ProspectConsent{}, fmt.Errorf("%w: withdrawal must follow grant", ErrInvalidProspect)
	}
	if c.WithdrawnAt.IsSet() {
		return ProspectConsent{}, fmt.Errorf("%w: consent is already withdrawn", ErrInvalidProspect)
	}
	if c.ExpiresAt.IsSet() && !at.Before(c.ExpiresAt) {
		return ProspectConsent{}, fmt.Errorf("%w: expired consent cannot be withdrawn", ErrInvalidProspect)
	}
	c.WithdrawnAt = at
	return c, nil
}

// OutreachDecision is explicit so callers cannot accidentally treat expired
// or withdrawn consent as a boolean preference.
type OutreachDecision struct {
	Allowed     bool
	Status      ProspectConsentStatus
	Obligations []RetentionObligation
}

type RetentionObligation string

const (
	ObligationStopFutureOutreach        RetentionObligation = "STOP_FUTURE_OUTREACH"
	ObligationRetainConsentEvidence     RetentionObligation = "RETAIN_CONSENT_EVIDENCE"
	ObligationDeleteDerivedProspectData RetentionObligation = "DELETE_DERIVED_PROSPECT_DATA"
)

func (c ProspectConsent) OutreachAt(at values.Instant) OutreachDecision {
	status := c.StatusAt(at)
	if status == ProspectConsentGranted {
		return OutreachDecision{Allowed: true, Status: status}
	}
	if status == ProspectConsentInvalid {
		return OutreachDecision{Status: status, Obligations: []RetentionObligation{ObligationStopFutureOutreach}}
	}
	return OutreachDecision{Status: status, Obligations: []RetentionObligation{
		ObligationStopFutureOutreach, ObligationRetainConsentEvidence, ObligationDeleteDerivedProspectData,
	}}
}

// ProspectRevision binds source lineage and processing authority to one
// immutable, effective-dated prospect record.
type ProspectRevision struct {
	ProspectID  values.EntityRef
	Revision    values.RevisionToken
	Attribution ProspectSourceAttribution
	Consent     ProspectConsent
	Effective   values.EffectiveInterval
	Owner       values.EntityRef
}

func (p ProspectRevision) Validate() error {
	if err := requireRef(p.ProspectID, "prospect", "prospect"); err != nil {
		return err
	}
	if !p.Revision.IsSpecified() {
		return fmt.Errorf("%w: revision is required", ErrInvalidProspect)
	}
	if err := p.Attribution.Validate(); err != nil {
		return err
	}
	if err := p.Consent.Validate(); err != nil {
		return err
	}
	if err := p.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrInvalidProspect, err)
	}
	if err := requireRef(p.Owner, "owner", "owner"); err != nil {
		return err
	}
	return sameTenant(p.ProspectID, p.Attribution.Source.RecordedBy, p.Consent.Authority, p.Consent.Evidence, p.Owner)
}
