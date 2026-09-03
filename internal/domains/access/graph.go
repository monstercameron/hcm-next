// Package access owns the authoritative workforce identity to expected-access
// vocabulary.  Provider observations are deliberately kept as observations;
// they can never become an authoritative graph edge.
package access

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var (
	ErrInvalidGraph       = errors.New("access: invalid graph")
	ErrUnownedReference   = errors.New("access: reference is not owned by the graph")
	ErrTenantMismatch     = errors.New("access: tenant boundary mismatch")
	ErrObservationOwns    = errors.New("access: external observation cannot be authoritative")
	ErrIncompleteRevision = errors.New("access: revision is incomplete")
)

type Lifecycle string

const (
	LifecycleDraft    Lifecycle = "DRAFT"
	LifecycleActive   Lifecycle = "ACTIVE"
	LifecycleDisabled Lifecycle = "DISABLED"
	LifecycleRevoked  Lifecycle = "REVOKED"
)

// Short aliases are retained for callers that use the domain vocabulary
// without the Lifecycle prefix.
const (
	Draft    = LifecycleDraft
	Active   = LifecycleActive
	Disabled = LifecycleDisabled
	Revoked  = LifecycleRevoked
)

func validLifecycle(v Lifecycle) bool {
	return v == LifecycleDraft || v == LifecycleActive || v == LifecycleDisabled || v == LifecycleRevoked
}

// WorkforceIdentity is the HCM-owned identity joining a worker to application
// subjects. Subject identifiers are links, not credentials or provider state.
type WorkforceIdentity struct {
	ID, Tenant, Subject, System string
	WorkerRef                   values.EntityRef
	Authority                   evidence.SourceAuthority
	Effective                   values.EffectiveInterval
	KnownAt                     values.KnownAt
	Provenance                  evidence.Provenance
	Lifecycle                   Lifecycle
}

// AccountLink binds one workforce identity to one application namespace.
type AccountLink struct {
	ID, Tenant, WorkforceIdentityID, System, Application, AccountID, Subject string
	Authority                                                                evidence.SourceAuthority
	Effective                                                                values.EffectiveInterval
	KnownAt                                                                  values.KnownAt
	Provenance                                                               evidence.Provenance
	Lifecycle                                                                Lifecycle
}

// EntitlementDefinition is the versioned, HCM-owned definition of a grant.
type EntitlementDefinition struct {
	ID, Tenant, System, Application, Code, Owner, Risk, Version string
	Authority                                                   evidence.SourceAuthority
	Effective                                                   values.EffectiveInterval
	KnownAt                                                     values.KnownAt
	Provenance                                                  evidence.Provenance
	Lifecycle                                                   Lifecycle
}

// ExpectedEntitlement is an HCM policy edge. EmploymentRef, PositionRef and
// PolicyRef make its derivation auditable and prevent birthright access from
// being inferred from an observed provider account.
type ExpectedEntitlement struct {
	ID, Tenant, WorkforceIdentityID, AccountLinkID, EntitlementID string
	EmploymentRef, PositionRef, PolicyRef                         string
	Authority                                                     evidence.SourceAuthority
	Effective                                                     values.EffectiveInterval
	KnownAt                                                       values.KnownAt
	Provenance                                                    evidence.Provenance
	Lifecycle                                                     Lifecycle
}

// ExternalAccessObservation reports provider state. Authority must always be
// EXTERNAL_OBSERVATION (or, for a derived comparison, DERIVED), never LOCAL.
type ExternalAccessObservation struct {
	ID, Tenant, System, Application, AccountID, EntitlementID, ProviderVersion string
	Subject                                                                    string
	ObservedState                                                              string
	Authority                                                                  evidence.SourceAuthority
	Effective                                                                  values.EffectiveInterval
	KnownAt                                                                    values.KnownAt
	Provenance                                                                 evidence.Provenance
	Lifecycle                                                                  Lifecycle
}

func requireCommon(kind, id, tenant string, authority evidence.SourceAuthority, effective values.EffectiveInterval, known values.KnownAt, provenance evidence.Provenance, lifecycle Lifecycle) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(tenant) == "" || !validLifecycle(lifecycle) {
		return fmt.Errorf("%w: %s identity/lifecycle", ErrIncompleteRevision, kind)
	}
	if err := authority.Validate(); err != nil {
		return fmt.Errorf("%w: %s authority: %v", ErrIncompleteRevision, kind, err)
	}
	if err := effective.Validate(); err != nil || known.Canonical() == nil {
		return fmt.Errorf("%w: %s effective/known time", ErrIncompleteRevision, kind)
	}
	if err := provenance.Validate(); err != nil {
		return fmt.Errorf("%w: %s provenance: %v", ErrIncompleteRevision, kind, err)
	}
	return values.ValidateKnowledgeOrder(known, provenance.RecordedAt, false)
}

func (w WorkforceIdentity) Validate() error {
	if err := requireCommon("workforce identity", w.ID, w.Tenant, w.Authority, w.Effective, w.KnownAt, w.Provenance, w.Lifecycle); err != nil {
		return err
	}
	if strings.TrimSpace(w.Subject) == "" || strings.TrimSpace(w.System) == "" {
		return fmt.Errorf("%w: workforce identity subject", ErrIncompleteRevision)
	}
	if err := w.WorkerRef.Validate(); err != nil {
		return fmt.Errorf("%w: worker ref: %v", ErrIncompleteRevision, err)
	}
	if w.Authority.Kind != evidence.AuthorityLocal {
		return fmt.Errorf("%w: workforce identity must be locally authoritative", ErrInvalidGraph)
	}
	if string(w.WorkerRef.Tenant) != w.Tenant {
		return ErrTenantMismatch
	}
	return nil
}

func (a AccountLink) Validate() error {
	if err := requireCommon("account link", a.ID, a.Tenant, a.Authority, a.Effective, a.KnownAt, a.Provenance, a.Lifecycle); err != nil {
		return err
	}
	if a.WorkforceIdentityID == "" || a.System == "" || a.Application == "" || a.AccountID == "" || a.Subject == "" {
		return fmt.Errorf("%w: account link owner/system/subject", ErrIncompleteRevision)
	}
	if a.Authority.Kind != evidence.AuthorityLocal {
		return fmt.Errorf("%w: account link must be locally authoritative", ErrInvalidGraph)
	}
	return nil
}

func (e EntitlementDefinition) Validate() error {
	if err := requireCommon("entitlement", e.ID, e.Tenant, e.Authority, e.Effective, e.KnownAt, e.Provenance, e.Lifecycle); err != nil {
		return err
	}
	if e.System == "" || e.Application == "" || e.Code == "" || e.Owner == "" || e.Risk == "" || e.Version == "" {
		return fmt.Errorf("%w: entitlement system/version/risk/owner", ErrIncompleteRevision)
	}
	if e.Authority.Kind != evidence.AuthorityLocal {
		return fmt.Errorf("%w: entitlement definition must be locally authoritative", ErrInvalidGraph)
	}
	return nil
}

func (e ExpectedEntitlement) Validate() error {
	if err := requireCommon("expected entitlement", e.ID, e.Tenant, e.Authority, e.Effective, e.KnownAt, e.Provenance, e.Lifecycle); err != nil {
		return err
	}
	if e.WorkforceIdentityID == "" || e.EntitlementID == "" || e.EmploymentRef == "" || e.PositionRef == "" || e.PolicyRef == "" {
		return fmt.Errorf("%w: expected edge requires identity/entitlement/employment/position/policy", ErrIncompleteRevision)
	}
	if e.Authority.Kind != evidence.AuthorityLocal {
		return fmt.Errorf("%w: expected entitlement must be locally authoritative", ErrInvalidGraph)
	}
	return nil
}

func (o ExternalAccessObservation) Validate() error {
	if err := requireCommon("external observation", o.ID, o.Tenant, o.Authority, o.Effective, o.KnownAt, o.Provenance, o.Lifecycle); err != nil {
		return err
	}
	if o.System == "" || o.Application == "" || o.AccountID == "" || o.ProviderVersion == "" || o.ObservedState == "" {
		return fmt.Errorf("%w: observation source/version/state", ErrIncompleteRevision)
	}
	if o.Authority.Kind != evidence.AuthorityExternalObservation && o.Authority.Kind != evidence.AuthorityDerived {
		return ErrObservationOwns
	}
	return nil
}

// Graph is a tenant-scoped, internally linked snapshot of the authoritative
// identity and expected-access graph plus non-authoritative observations.
type Graph struct {
	Tenant       string
	Identities   []WorkforceIdentity
	Accounts     []AccountLink
	Entitlements []EntitlementDefinition
	Expected     []ExpectedEntitlement
	Observations []ExternalAccessObservation
}

func (g Graph) Validate() error {
	if strings.TrimSpace(g.Tenant) == "" {
		return fmt.Errorf("%w: tenant", ErrInvalidGraph)
	}
	ids, accounts, ents := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, v := range g.Identities {
		if err := v.Validate(); err != nil {
			return err
		}
		if v.Tenant != g.Tenant {
			return ErrTenantMismatch
		}
		if ids[v.ID] {
			return fmt.Errorf("%w: duplicate identity %s", ErrInvalidGraph, v.ID)
		}
		ids[v.ID] = true
	}
	for _, v := range g.Accounts {
		if err := v.Validate(); err != nil {
			return err
		}
		if v.Tenant != g.Tenant {
			return ErrTenantMismatch
		}
		if !ids[v.WorkforceIdentityID] {
			return fmt.Errorf("%w: account %s identity", ErrUnownedReference, v.ID)
		}
		accounts[v.ID] = true
	}
	for _, v := range g.Entitlements {
		if err := v.Validate(); err != nil {
			return err
		}
		if v.Tenant != g.Tenant {
			return ErrTenantMismatch
		}
		ents[v.ID] = true
	}
	for _, v := range g.Expected {
		if err := v.Validate(); err != nil {
			return err
		}
		if v.Tenant != g.Tenant {
			return ErrTenantMismatch
		}
		if !ids[v.WorkforceIdentityID] || !ents[v.EntitlementID] {
			return fmt.Errorf("%w: expected %s", ErrUnownedReference, v.ID)
		}
		if v.AccountLinkID != "" && !accounts[v.AccountLinkID] {
			return fmt.Errorf("%w: expected %s account", ErrUnownedReference, v.ID)
		}
	}
	for _, v := range g.Observations {
		if err := v.Validate(); err != nil {
			return err
		}
		if v.Tenant != g.Tenant {
			return ErrTenantMismatch
		}
	}
	return nil
}

func (g Graph) Canonical() []byte {
	if g.Validate() != nil {
		return nil
	}
	b, _ := json.Marshal(g)
	return b
}
func (g Graph) Digest() string {
	b := g.Canonical()
	if b == nil {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Revision aliases make the immutable revision vocabulary explicit to callers
// that persist each graph member as its own stream.
type WorkforceIdentityRevision = WorkforceIdentity
type AccountLinkRevision = AccountLink
type EntitlementDefinitionRevision = EntitlementDefinition
type ExpectedEntitlementRevision = ExpectedEntitlement
type ExternalAccessObservationRevision = ExternalAccessObservation
