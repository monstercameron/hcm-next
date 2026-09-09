// Package access owns the authoritative workforce-application identity and
// expected-entitlement graph. Platform authentication principals remain in
// internal/trust; a Principal is an actor at a request boundary, while a
// WorkforceIdentity is an effective-dated application-domain record.
//
// External provider state is represented by ExternalAccessObservation. It can
// be validated and reconciled by later work, but Graph.Add always refuses it:
// an observation is evidence about the graph, never authority to mutate it.
package access

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	accessSchemaVersion = 1
	graphSchema         = "hcmnext.domains.access.Graph"
)

// Validation and authorization errors. Every structural error returned by
// this package is a *FieldError, so callers can identify the rejected field
// without parsing error text. The sentinel causes remain matchable with
// errors.Is.
var (
	ErrInvalidGraph         = errors.New("access: invalid graph")
	ErrIncompleteRevision   = errors.New("access: incomplete revision")
	ErrDuplicateRecord      = errors.New("access: duplicate record")
	ErrUnownedReference     = errors.New("access: unowned reference")
	ErrTenantMismatch       = errors.New("access: tenant boundary mismatch")
	ErrAuthorityClass       = errors.New("access: invalid authority class")
	ErrObservationMutation  = errors.New("access: external observation cannot mutate the authoritative graph")
	ErrInvalidAuthorization = errors.New("access: invalid authorization input")
	ErrUnauthorized         = errors.New("access: graph explanation is unauthorized")
)

// RecordKind is the closed vocabulary of ACCESS-001 revision kinds.
type RecordKind string

const (
	RecordGraph                     RecordKind = "GRAPH"
	RecordWorkforceIdentity         RecordKind = "WORKFORCE_IDENTITY"
	RecordAccountLink               RecordKind = "ACCOUNT_LINK"
	RecordEntitlementDefinition     RecordKind = "ENTITLEMENT_DEFINITION"
	RecordExpectedEntitlement       RecordKind = "EXPECTED_ENTITLEMENT"
	RecordExternalAccessObservation RecordKind = "EXTERNAL_ACCESS_OBSERVATION"
)

// FieldError identifies the exact field that made a record or graph unsafe.
type FieldError struct {
	Kind     RecordKind
	RecordID string
	Field    string
	Reason   string
	Cause    error
}

func (e *FieldError) Error() string {
	where := string(e.Kind)
	if e.RecordID != "" {
		where += " " + e.RecordID
	}
	return fmt.Sprintf("access: %s field %s: %s", where, e.Field, e.Reason)
}

func (e *FieldError) Unwrap() error { return e.Cause }

func invalid(kind RecordKind, id, field, reason string, cause error) error {
	return &FieldError{Kind: kind, RecordID: id, Field: field, Reason: reason, Cause: cause}
}

// AuthorityClass states whether a revision is native domain truth or an
// external observation. No other authority spelling is accepted.
type AuthorityClass string

const (
	AuthorityUnspecified         AuthorityClass = ""
	AuthorityNative              AuthorityClass = "NATIVE"
	AuthorityExternalObservation AuthorityClass = "EXTERNAL_OBSERVATION"
)

func (a AuthorityClass) String() string { return string(a) }

// Valid reports whether a is in the closed authority vocabulary.
func (a AuthorityClass) Valid() bool {
	return a == AuthorityNative || a == AuthorityExternalObservation
}

// Lifecycle is the closed lifecycle vocabulary shared by ACCESS-001 records.
type Lifecycle string

const (
	LifecycleUnspecified Lifecycle = ""
	LifecycleDraft       Lifecycle = "DRAFT"
	LifecycleActive      Lifecycle = "ACTIVE"
	LifecycleSuspended   Lifecycle = "SUSPENDED"
	LifecycleDisabled    Lifecycle = "DISABLED"
	LifecycleRevoked     Lifecycle = "REVOKED"
	LifecycleRetired     Lifecycle = "RETIRED"
)

// Compatibility aliases use the concise domain wire vocabulary.
const (
	Draft    = LifecycleDraft
	Active   = LifecycleActive
	Disabled = LifecycleDisabled
	Revoked  = LifecycleRevoked
)

func (l Lifecycle) String() string { return string(l) }

// Valid reports whether l is a defined lifecycle state.
func (l Lifecycle) Valid() bool {
	switch l {
	case LifecycleDraft, LifecycleActive, LifecycleSuspended, LifecycleDisabled, LifecycleRevoked, LifecycleRetired:
		return true
	default:
		return false
	}
}

// RiskClass is the closed entitlement-risk vocabulary.
type RiskClass string

const (
	RiskUnspecified RiskClass = ""
	RiskLow         RiskClass = "LOW"
	RiskModerate    RiskClass = "MODERATE"
	RiskHigh        RiskClass = "HIGH"
	RiskPrivileged  RiskClass = "PRIVILEGED"
)

func (r RiskClass) String() string { return string(r) }

// Valid reports whether r is a governed risk class.
func (r RiskClass) Valid() bool {
	switch r {
	case RiskLow, RiskModerate, RiskHigh, RiskPrivileged:
		return true
	default:
		return false
	}
}

// Record is one ACCESS-001 revision accepted by Graph.Add. The unexported
// marker keeps the set closed to the five record kinds defined here.
type Record interface {
	Validate() error
	Canonical() []byte
	recordKind() RecordKind
}

// WorkforceIdentity is the native, tenant-scoped identity joining a governed
// worker record to application subject links. It is not a trust.Principal and
// carries no session, credential, role, or authentication assurance state.
type WorkforceIdentity struct {
	ID        string
	Tenant    values.TenantId
	Subject   string
	System    string
	WorkerRef values.EntityRef

	Revision   values.RevisionToken
	Authority  AuthorityClass
	Effective  values.EffectiveInterval
	KnownAt    values.KnownAt
	Provenance evidence.Provenance
	Lifecycle  Lifecycle
}

func (WorkforceIdentity) recordKind() RecordKind { return RecordWorkforceIdentity }

// AccountLink binds one WorkforceIdentity to one account subject in a named
// system. Source is the governed connection, directory, or mapping reference
// that established the link; it is mandatory independently of provenance.
type AccountLink struct {
	ID                  string
	Tenant              values.TenantId
	Subject             string
	System              string
	Source              string
	WorkforceIdentityID string
	Application         string
	AccountID           string

	Revision   values.RevisionToken
	Authority  AuthorityClass
	Effective  values.EffectiveInterval
	KnownAt    values.KnownAt
	Provenance evidence.Provenance
	Lifecycle  Lifecycle
}

func (AccountLink) recordKind() RecordKind { return RecordAccountLink }

// EntitlementDefinition is a governed, versioned entitlement catalog entry.
type EntitlementDefinition struct {
	ID          string
	Tenant      values.TenantId
	Subject     string
	System      string
	Application string
	Code        string
	Version     string
	RiskClass   RiskClass
	Owner       string

	Revision   values.RevisionToken
	Authority  AuthorityClass
	Effective  values.EffectiveInterval
	KnownAt    values.KnownAt
	Provenance evidence.Provenance
	Lifecycle  Lifecycle
}

func (EntitlementDefinition) recordKind() RecordKind { return RecordEntitlementDefinition }

// ExpectedEntitlement is one native expected-access edge. At least one of
// EmploymentRef, PositionRef, or PolicyRef is required as its governed basis.
// AccountLinkID is optional because expected access may exist before an
// account is provisioned; when present, Graph.Validate checks its ownership.
type ExpectedEntitlement struct {
	ID                  string
	Tenant              values.TenantId
	Subject             string
	System              string
	WorkforceIdentityID string
	AccountLinkID       string
	EntitlementID       string
	EmploymentRef       string
	PositionRef         string
	PolicyRef           string

	Revision   values.RevisionToken
	Authority  AuthorityClass
	Effective  values.EffectiveInterval
	KnownAt    values.KnownAt
	Provenance evidence.Provenance
	Lifecycle  Lifecycle
}

func (ExpectedEntitlement) recordKind() RecordKind { return RecordExpectedEntitlement }

// ExternalAccessObservation is non-authoritative provider evidence. A valid
// observation must say EXTERNAL_OBSERVATION, and Graph.Add refuses even a
// valid observation so no ingestion path can silently promote it to truth.
type ExternalAccessObservation struct {
	ID              string
	Tenant          values.TenantId
	Subject         string
	System          string
	Application     string
	AccountID       string
	EntitlementID   string
	ProviderVersion string
	ObservedState   string

	Revision   values.RevisionToken
	Authority  AuthorityClass
	Effective  values.EffectiveInterval
	KnownAt    values.KnownAt
	Provenance evidence.Provenance
	Lifecycle  Lifecycle
}

func (ExternalAccessObservation) recordKind() RecordKind {
	return RecordExternalAccessObservation
}

type revisionFields struct {
	kind       RecordKind
	id         string
	tenant     values.TenantId
	subject    string
	system     string
	revision   values.RevisionToken
	authority  AuthorityClass
	effective  values.EffectiveInterval
	knownAt    values.KnownAt
	provenance evidence.Provenance
	lifecycle  Lifecycle
}

func (r revisionFields) validate(wantAuthority AuthorityClass) error {
	if strings.TrimSpace(r.id) == "" {
		return invalid(r.kind, r.id, "id", "is required", ErrIncompleteRevision)
	}
	if err := r.tenant.Validate(); err != nil {
		return invalid(r.kind, r.id, "tenant", err.Error(), ErrIncompleteRevision)
	}
	if strings.TrimSpace(r.subject) == "" {
		return invalid(r.kind, r.id, "subject", "is required", ErrIncompleteRevision)
	}
	if strings.TrimSpace(r.system) == "" {
		return invalid(r.kind, r.id, "system", "is required", ErrIncompleteRevision)
	}
	if !r.revision.IsSpecified() {
		return invalid(r.kind, r.id, "revision", "is required", ErrIncompleteRevision)
	}
	if !r.authority.Valid() || r.authority != wantAuthority {
		return invalid(r.kind, r.id, "authority_class", "must be "+wantAuthority.String(), ErrAuthorityClass)
	}
	if err := r.effective.Validate(); err != nil {
		return invalid(r.kind, r.id, "effective", err.Error(), ErrIncompleteRevision)
	}
	if r.knownAt.Canonical() == nil {
		return invalid(r.kind, r.id, "known_at", "is required", ErrIncompleteRevision)
	}
	if err := r.provenance.Validate(); err != nil {
		return invalid(r.kind, r.id, "provenance", err.Error(), ErrIncompleteRevision)
	}
	if err := values.ValidateKnowledgeOrder(r.knownAt, r.provenance.RecordedAt, false); err != nil {
		return invalid(r.kind, r.id, "known_at", err.Error(), ErrIncompleteRevision)
	}
	if !r.lifecycle.Valid() {
		return invalid(r.kind, r.id, "lifecycle", "is not in the closed vocabulary", ErrIncompleteRevision)
	}
	return nil
}

func (r revisionFields) writer(schema string) *canonicalbytes.Writer {
	return canonicalbytes.New(schema, accessSchemaVersion).
		String("id", r.id).
		String("tenant", r.tenant.String()).
		String("subject", r.subject).
		String("system", r.system).
		Value("revision", r.revision).
		String("authority_class", r.authority.String()).
		Value("effective", r.effective).
		Value("known_at", r.knownAt).
		Value("provenance", r.provenance).
		String("lifecycle", r.lifecycle.String())
}

func (w WorkforceIdentity) fields() revisionFields {
	return revisionFields{RecordWorkforceIdentity, w.ID, w.Tenant, w.Subject, w.System, w.Revision, w.Authority, w.Effective, w.KnownAt, w.Provenance, w.Lifecycle}
}

// Validate reports whether the identity revision is complete and native.
func (w WorkforceIdentity) Validate() error {
	if err := w.fields().validate(AuthorityNative); err != nil {
		return err
	}
	if err := w.WorkerRef.Validate(); err != nil {
		return invalid(w.recordKind(), w.ID, "worker_ref", err.Error(), ErrIncompleteRevision)
	}
	if w.WorkerRef.Tenant != w.Tenant {
		return invalid(w.recordKind(), w.ID, "worker_ref.tenant", "does not match revision tenant", ErrTenantMismatch)
	}
	return nil
}

// Canonical returns the canonical encoding, or nil for an invalid revision.
func (w WorkforceIdentity) Canonical() []byte {
	if w.Validate() != nil {
		return nil
	}
	raw, err := w.fields().writer("hcmnext.domains.access.WorkforceIdentity").
		Value("worker_ref", w.WorkerRef).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (a AccountLink) fields() revisionFields {
	return revisionFields{RecordAccountLink, a.ID, a.Tenant, a.Subject, a.System, a.Revision, a.Authority, a.Effective, a.KnownAt, a.Provenance, a.Lifecycle}
}

// Validate reports whether the account revision names its identity, system,
// source, and account coordinates.
func (a AccountLink) Validate() error {
	if err := a.fields().validate(AuthorityNative); err != nil {
		return err
	}
	for _, required := range []struct{ field, value string }{
		{"source", a.Source},
		{"workforce_identity_id", a.WorkforceIdentityID},
		{"application", a.Application},
		{"account_id", a.AccountID},
	} {
		if strings.TrimSpace(required.value) == "" {
			return invalid(a.recordKind(), a.ID, required.field, "is required", ErrIncompleteRevision)
		}
	}
	return nil
}

// Canonical returns the canonical encoding, or nil for an invalid revision.
func (a AccountLink) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	raw, err := a.fields().writer("hcmnext.domains.access.AccountLink").
		String("source", a.Source).
		String("workforce_identity_id", a.WorkforceIdentityID).
		String("application", a.Application).
		String("account_id", a.AccountID).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (e EntitlementDefinition) fields() revisionFields {
	return revisionFields{RecordEntitlementDefinition, e.ID, e.Tenant, e.Subject, e.System, e.Revision, e.Authority, e.Effective, e.KnownAt, e.Provenance, e.Lifecycle}
}

// Validate reports whether the entitlement has a catalog version, governed
// risk class, owner, application, and code.
func (e EntitlementDefinition) Validate() error {
	if err := e.fields().validate(AuthorityNative); err != nil {
		return err
	}
	for _, required := range []struct{ field, value string }{
		{"application", e.Application}, {"code", e.Code}, {"version", e.Version}, {"owner", e.Owner},
	} {
		if strings.TrimSpace(required.value) == "" {
			return invalid(e.recordKind(), e.ID, required.field, "is required", ErrIncompleteRevision)
		}
	}
	if !e.RiskClass.Valid() {
		return invalid(e.recordKind(), e.ID, "risk_class", "is not in the closed vocabulary", ErrIncompleteRevision)
	}
	return nil
}

// Canonical returns the canonical encoding, or nil for an invalid revision.
func (e EntitlementDefinition) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	raw, err := e.fields().writer("hcmnext.domains.access.EntitlementDefinition").
		String("application", e.Application).
		String("code", e.Code).
		String("version", e.Version).
		String("risk_class", e.RiskClass.String()).
		String("owner", e.Owner).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (e ExpectedEntitlement) fields() revisionFields {
	return revisionFields{RecordExpectedEntitlement, e.ID, e.Tenant, e.Subject, e.System, e.Revision, e.Authority, e.Effective, e.KnownAt, e.Provenance, e.Lifecycle}
}

// Validate reports whether the expected edge names its endpoints and at least
// one governed workforce or policy basis.
func (e ExpectedEntitlement) Validate() error {
	if err := e.fields().validate(AuthorityNative); err != nil {
		return err
	}
	if strings.TrimSpace(e.WorkforceIdentityID) == "" {
		return invalid(e.recordKind(), e.ID, "workforce_identity_id", "is required", ErrIncompleteRevision)
	}
	if strings.TrimSpace(e.EntitlementID) == "" {
		return invalid(e.recordKind(), e.ID, "entitlement_id", "is required", ErrIncompleteRevision)
	}
	if strings.TrimSpace(e.EmploymentRef) == "" && strings.TrimSpace(e.PositionRef) == "" && strings.TrimSpace(e.PolicyRef) == "" {
		return invalid(e.recordKind(), e.ID, "basis", "requires employment_ref, position_ref, or policy_ref", ErrIncompleteRevision)
	}
	return nil
}

// Canonical returns the canonical encoding, or nil for an invalid revision.
func (e ExpectedEntitlement) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	raw, err := e.fields().writer("hcmnext.domains.access.ExpectedEntitlement").
		String("workforce_identity_id", e.WorkforceIdentityID).
		String("account_link_id", e.AccountLinkID).
		String("entitlement_id", e.EntitlementID).
		String("employment_ref", e.EmploymentRef).
		String("position_ref", e.PositionRef).
		String("policy_ref", e.PolicyRef).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (o ExternalAccessObservation) fields() revisionFields {
	return revisionFields{RecordExternalAccessObservation, o.ID, o.Tenant, o.Subject, o.System, o.Revision, o.Authority, o.Effective, o.KnownAt, o.Provenance, o.Lifecycle}
}

// Validate reports whether the observation is complete and explicitly
// non-authoritative.
func (o ExternalAccessObservation) Validate() error {
	if err := o.fields().validate(AuthorityExternalObservation); err != nil {
		return err
	}
	for _, required := range []struct{ field, value string }{
		{"application", o.Application}, {"account_id", o.AccountID},
		{"provider_version", o.ProviderVersion}, {"observed_state", o.ObservedState},
	} {
		if strings.TrimSpace(required.value) == "" {
			return invalid(o.recordKind(), o.ID, required.field, "is required", ErrIncompleteRevision)
		}
	}
	return nil
}

// Canonical returns the canonical encoding, or nil for an invalid observation.
func (o ExternalAccessObservation) Canonical() []byte {
	if o.Validate() != nil {
		return nil
	}
	raw, err := o.fields().writer("hcmnext.domains.access.ExternalAccessObservation").
		String("application", o.Application).
		String("account_id", o.AccountID).
		String("entitlement_id", o.EntitlementID).
		String("provider_version", o.ProviderVersion).
		String("observed_state", o.ObservedState).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Explicit revision aliases make the append-only record vocabulary visible to
// persistence adapters without defining a second representation.
type WorkforceIdentityRevision = WorkforceIdentity
type AccountLinkRevision = AccountLink
type EntitlementDefinitionRevision = EntitlementDefinition
type ExpectedEntitlementRevision = ExpectedEntitlement
type ExternalAccessObservationRevision = ExternalAccessObservation

// Graph is a tenant-scoped snapshot of native identity and expected-access
// revisions. Observations is exported solely so decoding adapters can be
// validated; any non-empty value makes the graph invalid.
type Graph struct {
	Tenant       values.TenantId
	Identities   []WorkforceIdentity
	Accounts     []AccountLink
	Entitlements []EntitlementDefinition
	Expected     []ExpectedEntitlement
	Observations []ExternalAccessObservation
}

// NewGraph returns an empty valid tenant graph.
func NewGraph(tenant values.TenantId) (Graph, error) {
	g := Graph{Tenant: tenant}
	if err := g.Validate(); err != nil {
		return Graph{}, err
	}
	return g, nil
}

// Add atomically adds one native revision. The receiver is unchanged on every
// failure, including an unowned reference or external observation.
func (g *Graph) Add(record Record) error {
	if g == nil {
		return invalid(RecordGraph, "", "graph", "receiver is nil", ErrInvalidGraph)
	}
	if record == nil {
		return invalid(RecordGraph, "", "record", "is nil", ErrInvalidGraph)
	}

	candidate := g.clone()
	switch r := record.(type) {
	case WorkforceIdentity:
		candidate.Identities = append(candidate.Identities, r)
	case *WorkforceIdentity:
		if r == nil {
			return invalid(RecordWorkforceIdentity, "", "record", "is nil", ErrInvalidGraph)
		}
		candidate.Identities = append(candidate.Identities, *r)
	case AccountLink:
		candidate.Accounts = append(candidate.Accounts, r)
	case *AccountLink:
		if r == nil {
			return invalid(RecordAccountLink, "", "record", "is nil", ErrInvalidGraph)
		}
		candidate.Accounts = append(candidate.Accounts, *r)
	case EntitlementDefinition:
		candidate.Entitlements = append(candidate.Entitlements, r)
	case *EntitlementDefinition:
		if r == nil {
			return invalid(RecordEntitlementDefinition, "", "record", "is nil", ErrInvalidGraph)
		}
		candidate.Entitlements = append(candidate.Entitlements, *r)
	case ExpectedEntitlement:
		candidate.Expected = append(candidate.Expected, r)
	case *ExpectedEntitlement:
		if r == nil {
			return invalid(RecordExpectedEntitlement, "", "record", "is nil", ErrInvalidGraph)
		}
		candidate.Expected = append(candidate.Expected, *r)
	case ExternalAccessObservation:
		return invalid(r.recordKind(), r.ID, "authority_class", "external observations cannot be added to an authoritative graph", ErrObservationMutation)
	case *ExternalAccessObservation:
		id := ""
		if r != nil {
			id = r.ID
		}
		return invalid(RecordExternalAccessObservation, id, "authority_class", "external observations cannot be added to an authoritative graph", ErrObservationMutation)
	default:
		return invalid(RecordGraph, "", "record", "has an unsupported record kind", ErrInvalidGraph)
	}

	if err := candidate.Validate(); err != nil {
		return err
	}
	*g = candidate
	return nil
}

func (g Graph) clone() Graph {
	g.Identities = append([]WorkforceIdentity(nil), g.Identities...)
	g.Accounts = append([]AccountLink(nil), g.Accounts...)
	g.Entitlements = append([]EntitlementDefinition(nil), g.Entitlements...)
	g.Expected = append([]ExpectedEntitlement(nil), g.Expected...)
	g.Observations = append([]ExternalAccessObservation(nil), g.Observations...)
	return g
}

// Validate checks record completeness, tenant isolation, uniqueness, endpoint
// ownership, system agreement, and subject agreement. It never repairs or
// infers a missing link.
func (g Graph) Validate() error {
	if err := g.Tenant.Validate(); err != nil {
		return invalid(RecordGraph, "", "tenant", err.Error(), ErrInvalidGraph)
	}
	if len(g.Observations) != 0 {
		return invalid(RecordGraph, "", "observations", "external observations cannot be authoritative graph members", ErrObservationMutation)
	}

	identities := make(map[string]WorkforceIdentity, len(g.Identities))
	accounts := make(map[string]AccountLink, len(g.Accounts))
	entitlements := make(map[string]EntitlementDefinition, len(g.Entitlements))
	expected := make(map[string]struct{}, len(g.Expected))

	for i, identity := range g.Identities {
		if err := identity.Validate(); err != nil {
			return err
		}
		if identity.Tenant != g.Tenant {
			return invalid(identity.recordKind(), identity.ID, "tenant", "does not match graph tenant", ErrTenantMismatch)
		}
		if _, duplicate := identities[identity.ID]; duplicate {
			return invalid(identity.recordKind(), identity.ID, "id", fmt.Sprintf("duplicates identities[%d]", i), ErrDuplicateRecord)
		}
		identities[identity.ID] = identity
	}

	for i, account := range g.Accounts {
		if err := account.Validate(); err != nil {
			return err
		}
		if account.Tenant != g.Tenant {
			return invalid(account.recordKind(), account.ID, "tenant", "does not match graph tenant", ErrTenantMismatch)
		}
		identity, owned := identities[account.WorkforceIdentityID]
		if !owned {
			return invalid(account.recordKind(), account.ID, "workforce_identity_id", "does not name an identity in this graph", ErrUnownedReference)
		}
		if identity.Tenant != account.Tenant {
			return invalid(account.recordKind(), account.ID, "workforce_identity_id", "names an identity outside the account tenant", ErrTenantMismatch)
		}
		if _, duplicate := accounts[account.ID]; duplicate {
			return invalid(account.recordKind(), account.ID, "id", fmt.Sprintf("duplicates accounts[%d]", i), ErrDuplicateRecord)
		}
		accounts[account.ID] = account
	}

	for i, entitlement := range g.Entitlements {
		if err := entitlement.Validate(); err != nil {
			return err
		}
		if entitlement.Tenant != g.Tenant {
			return invalid(entitlement.recordKind(), entitlement.ID, "tenant", "does not match graph tenant", ErrTenantMismatch)
		}
		if _, duplicate := entitlements[entitlement.ID]; duplicate {
			return invalid(entitlement.recordKind(), entitlement.ID, "id", fmt.Sprintf("duplicates entitlements[%d]", i), ErrDuplicateRecord)
		}
		entitlements[entitlement.ID] = entitlement
	}

	for i, edge := range g.Expected {
		if err := edge.Validate(); err != nil {
			return err
		}
		if edge.Tenant != g.Tenant {
			return invalid(edge.recordKind(), edge.ID, "tenant", "does not match graph tenant", ErrTenantMismatch)
		}
		identity, owned := identities[edge.WorkforceIdentityID]
		if !owned {
			return invalid(edge.recordKind(), edge.ID, "workforce_identity_id", "does not name an identity in this graph", ErrUnownedReference)
		}
		entitlement, owned := entitlements[edge.EntitlementID]
		if !owned {
			return invalid(edge.recordKind(), edge.ID, "entitlement_id", "does not name an entitlement in this graph", ErrUnownedReference)
		}
		if edge.Subject != identity.Subject {
			return invalid(edge.recordKind(), edge.ID, "subject", "does not match the workforce identity subject", ErrUnownedReference)
		}
		if edge.System != entitlement.System {
			return invalid(edge.recordKind(), edge.ID, "system", "does not match the entitlement system", ErrUnownedReference)
		}
		if edge.AccountLinkID != "" {
			account, linked := accounts[edge.AccountLinkID]
			if !linked {
				return invalid(edge.recordKind(), edge.ID, "account_link_id", "does not name an account in this graph", ErrUnownedReference)
			}
			if account.WorkforceIdentityID != edge.WorkforceIdentityID {
				return invalid(edge.recordKind(), edge.ID, "account_link_id", "belongs to a different workforce identity", ErrUnownedReference)
			}
			if account.System != edge.System {
				return invalid(edge.recordKind(), edge.ID, "system", "does not match the linked account system", ErrUnownedReference)
			}
		}
		if _, duplicate := expected[edge.ID]; duplicate {
			return invalid(edge.recordKind(), edge.ID, "id", fmt.Sprintf("duplicates expected[%d]", i), ErrDuplicateRecord)
		}
		expected[edge.ID] = struct{}{}
	}
	return nil
}

// Canonical returns an insertion-order-independent canonical graph encoding,
// or nil when any graph invariant fails.
func (g Graph) Canonical() []byte {
	if g.Validate() != nil {
		return nil
	}

	identities := append([]WorkforceIdentity(nil), g.Identities...)
	accounts := append([]AccountLink(nil), g.Accounts...)
	entitlements := append([]EntitlementDefinition(nil), g.Entitlements...)
	expected := append([]ExpectedEntitlement(nil), g.Expected...)
	sort.Slice(identities, func(i, j int) bool { return identities[i].ID < identities[j].ID })
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
	sort.Slice(entitlements, func(i, j int) bool { return entitlements[i].ID < entitlements[j].ID })
	sort.Slice(expected, func(i, j int) bool { return expected[i].ID < expected[j].ID })

	w := canonicalbytes.New(graphSchema, accessSchemaVersion).
		String("tenant", g.Tenant.String()).
		Count("identities", len(identities))
	for _, record := range identities {
		w.Field("identity", record.Canonical())
	}
	w.Count("accounts", len(accounts))
	for _, record := range accounts {
		w.Field("account", record.Canonical())
	}
	w.Count("entitlements", len(entitlements))
	for _, record := range entitlements {
		w.Field("entitlement", record.Canonical())
	}
	w.Count("expected", len(expected))
	for _, record := range expected {
		w.Field("expected", record.Canonical())
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the algorithm-tagged canonical graph digest, or an empty
// string for an invalid graph.
func (g Graph) Digest() string {
	raw := g.Canonical()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// CanonicalDigest is the explicit name used by evidence callers.
func (g Graph) CanonicalDigest() string { return g.Digest() }

// Authorizer is a server-resolved scope check used by Graph.Explain. The
// package deliberately does not accept or translate a platform trust.Principal.
type Authorizer func(tenant values.TenantId, purpose string) bool

// ExplainRequest binds an explanation to a tenant, purpose, and evaluated
// policy version. A nil Authorize hook is denied, never treated as allow.
type ExplainRequest struct {
	Tenant        values.TenantId
	Purpose       string
	PolicyVersion string
	Authorize     Authorizer
}

// Authorize applies the fail-closed scope gate for an explanation. Panics in
// an injected authorizer are converted to denial.
func (g Graph) Authorize(req ExplainRequest) error {
	if err := req.Tenant.Validate(); err != nil {
		return invalid(RecordGraph, "", "authorize.tenant", err.Error(), ErrInvalidAuthorization)
	}
	if req.Tenant != g.Tenant {
		return invalid(RecordGraph, "", "authorize.tenant", "does not match graph tenant", ErrUnauthorized)
	}
	if strings.TrimSpace(req.Purpose) == "" {
		return invalid(RecordGraph, "", "authorize.purpose", "is required", ErrInvalidAuthorization)
	}
	if strings.TrimSpace(req.PolicyVersion) == "" {
		return invalid(RecordGraph, "", "authorize.policy_version", "is required", ErrInvalidAuthorization)
	}
	if req.Authorize == nil || !invokeAuthorizer(req.Authorize, req.Tenant, req.Purpose) {
		return invalid(RecordGraph, "", "authorize", "scope was not explicitly allowed", ErrUnauthorized)
	}
	return nil
}

func invokeAuthorizer(authorize Authorizer, tenant values.TenantId, purpose string) (allowed bool) {
	defer func() {
		if recover() != nil {
			allowed = false
		}
	}()
	return authorize(tenant, purpose)
}

// Explanation is a bounded, authorized description of a graph snapshot. It
// carries stable record references and relationships, not credentials or
// platform-principal state.
type Explanation struct {
	Tenant           values.TenantId
	Purpose          string
	PolicyVersion    string
	CanonicalDigest  string
	IdentityCount    int
	AccountCount     int
	EntitlementCount int
	ExpectedCount    int
	Lines            []string
}

// Explain returns an authorized deterministic explanation. Authorization is
// checked before graph validation so a denied caller receives no graph detail.
func (g Graph) Explain(req ExplainRequest) (Explanation, error) {
	if err := g.Authorize(req); err != nil {
		return Explanation{}, err
	}
	if err := g.Validate(); err != nil {
		return Explanation{}, err
	}

	lines := make([]string, 0, len(g.Identities)+len(g.Accounts)+len(g.Entitlements)+len(g.Expected))
	for _, identity := range g.Identities {
		lines = append(lines, fmt.Sprintf("identity %s subject %s lifecycle %s", identity.ID, identity.Subject, identity.Lifecycle))
	}
	for _, account := range g.Accounts {
		lines = append(lines, fmt.Sprintf("account %s links identity %s in system %s", account.ID, account.WorkforceIdentityID, account.System))
	}
	for _, entitlement := range g.Entitlements {
		lines = append(lines, fmt.Sprintf("entitlement %s version %s risk %s owner %s", entitlement.ID, entitlement.Version, entitlement.RiskClass, entitlement.Owner))
	}
	for _, edge := range g.Expected {
		basis := make([]string, 0, 3)
		if edge.EmploymentRef != "" {
			basis = append(basis, "employment="+edge.EmploymentRef)
		}
		if edge.PositionRef != "" {
			basis = append(basis, "position="+edge.PositionRef)
		}
		if edge.PolicyRef != "" {
			basis = append(basis, "policy="+edge.PolicyRef)
		}
		lines = append(lines, fmt.Sprintf("expected %s links identity %s to entitlement %s by %s", edge.ID, edge.WorkforceIdentityID, edge.EntitlementID, strings.Join(basis, ",")))
	}
	sort.Strings(lines)

	return Explanation{
		Tenant:           g.Tenant,
		Purpose:          req.Purpose,
		PolicyVersion:    req.PolicyVersion,
		CanonicalDigest:  g.Digest(),
		IdentityCount:    len(g.Identities),
		AccountCount:     len(g.Accounts),
		EntitlementCount: len(g.Entitlements),
		ExpectedCount:    len(g.Expected),
		Lines:            lines,
	}, nil
}
