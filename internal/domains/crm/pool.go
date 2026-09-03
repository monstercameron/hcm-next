package crm

import (
	"fmt"
	"strings"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// RemovalPolicy describes how a subject leaves a pool. It is deliberately
// data, rather than an implicit consequence of candidate or worker status.
type RemovalPolicy string

const (
	RemovalManual          RemovalPolicy = "MANUAL"
	RemovalAtIntervalEnd   RemovalPolicy = "AT_INTERVAL_END"
	RemovalOnConsentEnd    RemovalPolicy = "ON_CONSENT_END"
	RemovalOnProcessingEnd RemovalPolicy = "ON_PROCESSING_END"
)

// SourceAttribution records where the pool decision came from.
type SourceAttribution struct {
	System     string
	Reference  string
	RecordedBy values.EntityRef
}

// ConsentAuthority binds processing to an explicit purpose and authority.
type ConsentAuthority struct {
	Authority values.EntityRef
	Basis     string
	Evidence  values.EntityRef
}

// Scope limits the pool to an explicitly named organization or population.
type Scope struct {
	Organization values.EntityRef
	Population   values.EntityRef
}

// TalentPoolRevision is an immutable, effective-dated governed pool.
type TalentPoolRevision struct {
	PoolID        values.EntityRef
	Revision      values.RevisionToken
	Purpose       string
	Criteria      string
	Source        SourceAttribution
	Consent       ConsentAuthority
	Scope         Scope
	Effective     values.EffectiveInterval
	Owner         values.EntityRef
	RemovalPolicy RemovalPolicy
}

// PoolRevision is retained as a concise semantic alias.
type PoolRevision = TalentPoolRevision

func (p TalentPoolRevision) Validate() error {
	if err := requireRef(p.PoolID, "pool", "talent_pool"); err != nil {
		return err
	}
	if !p.Revision.IsSpecified() {
		return fmt.Errorf("%w: pool revision is required", ErrInvalidPool)
	}
	if strings.TrimSpace(p.Purpose) == "" {
		return fmt.Errorf("%w: purpose is required", ErrInvalidPool)
	}
	if strings.TrimSpace(p.Criteria) == "" {
		return fmt.Errorf("%w: criteria is required", ErrInvalidPool)
	}
	if strings.TrimSpace(p.Source.System) == "" || strings.TrimSpace(p.Source.Reference) == "" {
		return fmt.Errorf("%w: source attribution is required", ErrInvalidPool)
	}
	if err := requireRef(p.Source.RecordedBy, "source recorder", "principal"); err != nil {
		return err
	}
	if strings.TrimSpace(p.Consent.Basis) == "" {
		return fmt.Errorf("%w: consent/processing basis is required", ErrInvalidPool)
	}
	if err := requireRef(p.Consent.Authority, "consent authority", "processing_authority"); err != nil {
		return err
	}
	if err := requireRef(p.Consent.Evidence, "consent evidence", "evidence"); err != nil {
		return err
	}
	if err := requireRef(p.Scope.Organization, "scope organization", "organization"); err != nil {
		return err
	}
	if err := requireRef(p.Owner, "owner", "owner"); err != nil {
		return err
	}
	if err := p.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrInvalidPool, err)
	}
	if p.RemovalPolicy != RemovalManual && p.RemovalPolicy != RemovalAtIntervalEnd && p.RemovalPolicy != RemovalOnConsentEnd && p.RemovalPolicy != RemovalOnProcessingEnd {
		return fmt.Errorf("%w: invalid removal policy %q", ErrInvalidPool, p.RemovalPolicy)
	}
	if err := sameTenant(p.PoolID, p.Owner, p.Source.RecordedBy, p.Consent.Authority, p.Consent.Evidence, p.Scope.Organization); err != nil {
		return err
	}
	return nil
}

func requireRef(ref values.EntityRef, name, kind string) error {
	if err := ref.Validate(); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInvalidReference, name, err)
	}
	if string(ref.Kind) != kind {
		return fmt.Errorf("%w: %s must be %s", ErrInvalidReference, name, kind)
	}
	return nil
}
func sameTenant(root values.EntityRef, refs ...values.EntityRef) error {
	for _, ref := range refs {
		if ref.Tenant != root.Tenant {
			return fmt.Errorf("%w: references have different tenants", ErrInvalidReference)
		}
	}
	return nil
}
