package governance

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

type Principal struct {
	TenantID         uuid.UUID       `json:"tenant_id"`
	PrincipalID      uuid.UUID       `json:"principal_id"`
	Kind             string          `json:"kind"`
	Subject          string          `json:"subject"`
	OrgScopeID       *uuid.UUID      `json:"org_scope_id"`
	Assurance        string          `json:"assurance"`
	AuthnMethod      string          `json:"authn_method"`
	CredentialDigest *string         `json:"credential_digest"`
	RevocationEpoch  int64           `json:"revocation_epoch"`
	Lifecycle        string          `json:"lifecycle"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	ExpiresAt        *time.Time      `json:"expires_at"`
	Metadata         json.RawMessage `json:"metadata"`
}

func (p Principal) Validate() error {
	if p.TenantID == uuid.Nil || p.PrincipalID == uuid.Nil {
		return ErrNilTenant
	}
	if p.RevocationEpoch < 1 {
		return ErrInvalidIntervalDetail{Reason: "revocation_epoch must be >=1"}
	}
	if p.ExpiresAt != nil && !p.ExpiresAt.After(p.CreatedAt) {
		return ErrInvalidIntervalDetail{Reason: "expires_at must be after created_at"}
	}
	if p.UpdatedAt.Before(p.CreatedAt) {
		return ErrInvalidIntervalDetail{Reason: "updated_at before created_at"}
	}
	switch p.Kind {
	case "USER", "SERVICE", "SYSTEM", "DELEGATE":
	default:
		return fmt.Errorf("governance: principal kind %q invalid", p.Kind)
	}
	switch p.Assurance {
	case "AAL1", "AAL2", "AAL3":
	default:
		return fmt.Errorf("governance: assurance %q invalid", p.Assurance)
	}
	return nil
}

type AuthoritySource struct {
	TenantID          uuid.UUID  `json:"tenant_id"`
	AuthoritySourceID uuid.UUID  `json:"authority_source_id"`
	Kind              string     `json:"kind"`
	DisplayName       string     `json:"display_name"`
	URI               string     `json:"uri"`
	Version           int64      `json:"version"`
	ValidFrom         time.Time  `json:"valid_from"`
	ValidTo           *time.Time `json:"valid_to"`
	ContentDigest     string     `json:"content_digest"`
	CreatedAt         time.Time  `json:"created_at"`
}

func (a AuthoritySource) Validate() error {
	if a.TenantID == uuid.Nil || a.AuthoritySourceID == uuid.Nil {
		return ErrNilTenant
	}
	if a.ContentDigest == "" {
		return ErrMissingDigest
	}
	if a.Version < 1 {
		return ErrUnversionedDetail{Field: "authority_source.version"}
	}
	if a.ValidTo != nil && !a.ValidTo.After(a.ValidFrom) {
		return ErrInvalidIntervalDetail{Reason: "valid_to must be after valid_from"}
	}
	if a.ValidFrom.IsZero() {
		return ErrInvalidIntervalDetail{Reason: "valid_from required"}
	}
	switch a.Kind {
	case "HRIS", "IDP", "ATTESTATION", "POLICY_BUNDLE":
	default:
		return fmt.Errorf("governance: authority_source kind %q invalid", a.Kind)
	}
	return nil
}

type AuthorityBinding struct {
	TenantID          uuid.UUID       `json:"tenant_id"`
	BindingID         uuid.UUID       `json:"binding_id"`
	PrincipalID       uuid.UUID       `json:"principal_id"`
	AuthoritySourceID uuid.UUID       `json:"authority_source_id"`
	Scope             json.RawMessage `json:"scope"`
	ValidFrom         time.Time       `json:"valid_from"`
	ValidTo           *time.Time      `json:"valid_to"`
	CreatedAt         time.Time       `json:"created_at"`
}

func (a AuthorityBinding) Validate() error {
	if a.TenantID == uuid.Nil || a.BindingID == uuid.Nil || a.PrincipalID == uuid.Nil || a.AuthoritySourceID == uuid.Nil {
		return ErrNilTenant
	}
	if a.ValidTo != nil && !a.ValidTo.After(a.ValidFrom) {
		return ErrInvalidIntervalDetail{Reason: "valid_to must be after valid_from"}
	}
	if a.ValidFrom.IsZero() {
		return ErrInvalidIntervalDetail{Reason: "valid_from required"}
	}
	return nil
}

type AuthenticationSession struct {
	TenantID      uuid.UUID       `json:"tenant_id"`
	SessionID     uuid.UUID       `json:"session_id"`
	PrincipalID   uuid.UUID       `json:"principal_id"`
	IssuedAt      time.Time       `json:"issued_at"`
	ExpiresAt     time.Time       `json:"expires_at"`
	AuthnMethod   string          `json:"authn_method"`
	Assurance     string          `json:"assurance"`
	SessionDigest string          `json:"session_digest"`
	IPHash        *string         `json:"ip_hash"`
	Metadata      json.RawMessage `json:"metadata"`
	RevokedAt     *time.Time      `json:"revoked_at"`
}

func (a AuthenticationSession) Validate() error {
	if a.TenantID == uuid.Nil || a.SessionID == uuid.Nil || a.PrincipalID == uuid.Nil {
		return ErrNilTenant
	}
	if a.SessionDigest == "" {
		return ErrMissingDigest
	}
	if !a.ExpiresAt.After(a.IssuedAt) {
		return ErrInvalidIntervalDetail{Reason: "expires_at must be after issued_at"}
	}
	switch a.Assurance {
	case "AAL1", "AAL2", "AAL3":
	default:
		return fmt.Errorf("governance: assurance %q invalid", a.Assurance)
	}
	return nil
}

func (a AuthenticationSession) IsExpired(at time.Time) bool {
	if a.RevokedAt != nil && !at.Before(*a.RevokedAt) {
		return true
	}
	return !at.Before(a.ExpiresAt)
}

type DelegationGrant struct {
	TenantID             uuid.UUID       `json:"tenant_id"`
	GrantID              uuid.UUID       `json:"grant_id"`
	DelegatorPrincipalID uuid.UUID       `json:"delegator_principal_id"`
	DelegatePrincipalID  uuid.UUID       `json:"delegate_principal_id"`
	DelegatorTenant      uuid.UUID       `json:"delegator_tenant"`
	DelegateTenant       uuid.UUID       `json:"delegate_tenant"`
	Scope                json.RawMessage `json:"scope"`
	ValidFrom            time.Time       `json:"valid_from"`
	ValidTo              *time.Time      `json:"valid_to"`
	CreatedAt            time.Time       `json:"created_at"`
	RevokedAt            *time.Time      `json:"revoked_at"`
	RevokedBy            *uuid.UUID      `json:"revoked_by"`
	Reason               *string         `json:"reason"`
}

func (d DelegationGrant) Validate() error {
	if d.TenantID == uuid.Nil || d.GrantID == uuid.Nil || d.DelegatorPrincipalID == uuid.Nil || d.DelegatePrincipalID == uuid.Nil {
		return ErrNilTenant
	}
	if d.DelegatorTenant == uuid.Nil || d.DelegateTenant == uuid.Nil {
		return ErrNilTenant
	}
	if d.DelegatorTenant != d.TenantID || d.DelegateTenant != d.TenantID || d.DelegatorTenant != d.DelegateTenant {
		return ErrCrossTenant{TenantID: d.TenantID.String(), DelegatorTenant: d.DelegatorTenant.String(), DelegateTenant: d.DelegateTenant.String()}
	}
	if d.DelegatorPrincipalID == d.DelegatePrincipalID {
		return ErrInvalidIntervalDetail{Reason: "delegator and delegate must differ"}
	}
	if d.ValidTo != nil && !d.ValidTo.After(d.ValidFrom) {
		return ErrInvalidIntervalDetail{Reason: "valid_to must be after valid_from"}
	}
	if d.RevokedAt != nil && d.RevokedAt.Before(d.CreatedAt) {
		return ErrInvalidIntervalDetail{Reason: "revoked_at before created_at"}
	}
	return nil
}

type AuthorizationPolicySnapshot struct {
	TenantID      uuid.UUID       `json:"tenant_id"`
	SnapshotID    uuid.UUID       `json:"snapshot_id"`
	PolicyKey     string          `json:"policy_key"`
	Version       int64           `json:"version"`
	ContentDigest string          `json:"content_digest"`
	Body          json.RawMessage `json:"body"`
	ValidFrom     time.Time       `json:"valid_from"`
	ValidTo       *time.Time      `json:"valid_to"`
	CreatedAt     time.Time       `json:"created_at"`
}

func (a AuthorizationPolicySnapshot) Validate() error {
	if a.TenantID == uuid.Nil || a.SnapshotID == uuid.Nil {
		return ErrNilTenant
	}
	if a.Version < 1 {
		return ErrUnversionedDetail{Field: "policy_snapshot.version"}
	}
	if a.ContentDigest == "" {
		return ErrMissingDigest
	}
	if a.PolicyKey == "" {
		return fmt.Errorf("%w: policy_key required", ErrUnversionedAuthority)
	}
	if a.ValidTo != nil && !a.ValidTo.After(a.ValidFrom) {
		return ErrInvalidIntervalDetail{Reason: "valid_to must be after valid_from"}
	}
	return nil
}

type AuthorizationDecision struct {
	TenantID          uuid.UUID       `json:"tenant_id"`
	DecisionID        uuid.UUID       `json:"decision_id"`
	PrincipalID       uuid.UUID       `json:"principal_id"`
	PolicySnapshotID  uuid.UUID       `json:"policy_snapshot_id"`
	InputDigest       string          `json:"input_digest"`
	ResultDigest      string          `json:"result_digest"`
	EvidenceID        uuid.UUID       `json:"evidence_id"`
	Result            string          `json:"result"`
	ValidUntil        time.Time       `json:"valid_until"`
	EvaluatedAt       time.Time       `json:"evaluated_at"`
	PolicySnapshotIDs []uuid.UUID     `json:"policy_snapshot_ids"`
	SessionID         *uuid.UUID      `json:"session_id"`
	Metadata          json.RawMessage `json:"metadata"`
}

func (a AuthorizationDecision) Validate() error {
	if a.TenantID == uuid.Nil || a.DecisionID == uuid.Nil || a.PrincipalID == uuid.Nil || a.PolicySnapshotID == uuid.Nil {
		return ErrNilTenant
	}
	if a.EvidenceID == uuid.Nil {
		return fmt.Errorf("%w: evidence_id required", ErrMissingDigest)
	}
	if a.InputDigest == "" || a.ResultDigest == "" {
		return ErrMissingDigest
	}
	if len(a.PolicySnapshotIDs) == 0 {
		return ErrEmptyPolicySnapshot
	}
	for _, id := range a.PolicySnapshotIDs {
		if id == uuid.Nil {
			return ErrUnversionedDetail{Field: "policy_snapshot_ids contains nil"}
		}
	}
	if !a.ValidUntil.After(a.EvaluatedAt) {
		return ErrInvalidIntervalDetail{Reason: "valid_until must be after evaluated_at"}
	}
	switch a.Result {
	case "ALLOW", "DENY", "ABSTAIN":
	default:
		return fmt.Errorf("governance: decision result %q invalid", a.Result)
	}
	return nil
}

func (a AuthorizationDecision) Verify(at time.Time) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if !at.Before(a.ValidUntil) {
		return ErrExpiredEvidence
	}
	return nil
}

type DataAccessManifest struct {
	TenantID    uuid.UUID       `json:"tenant_id"`
	ManifestID  uuid.UUID       `json:"manifest_id"`
	DecisionID  uuid.UUID       `json:"decision_id"`
	PrincipalID uuid.UUID       `json:"principal_id"`
	ResourceRef string          `json:"resource_ref"`
	AccessKind  string          `json:"access_kind"`
	Fields      json.RawMessage `json:"fields"`
	ValidFrom   time.Time       `json:"valid_from"`
	ValidTo     *time.Time      `json:"valid_to"`
	CreatedAt   time.Time       `json:"created_at"`
}

func (d DataAccessManifest) Validate() error {
	if d.TenantID == uuid.Nil || d.ManifestID == uuid.Nil || d.DecisionID == uuid.Nil || d.PrincipalID == uuid.Nil {
		return ErrNilTenant
	}
	if d.ResourceRef == "" {
		return fmt.Errorf("governance: resource_ref required")
	}
	switch d.AccessKind {
	case "READ", "WRITE", "ADMIN", "EXECUTE":
	default:
		return fmt.Errorf("governance: access_kind %q invalid", d.AccessKind)
	}
	if d.ValidTo != nil && !d.ValidTo.After(d.ValidFrom) {
		return ErrInvalidIntervalDetail{Reason: "valid_to must be after valid_from"}
	}
	return nil
}

type Jurisdiction struct {
	JurisdictionID uuid.UUID  `json:"jurisdiction_id"`
	Country        string     `json:"country"`
	State          *string    `json:"state"`
	Region         *string    `json:"region"`
	Version        int64      `json:"version"`
	ValidFrom      time.Time  `json:"valid_from"`
	ValidTo        *time.Time `json:"valid_to"`
	CreatedAt      time.Time  `json:"created_at"`
}

func (j Jurisdiction) Validate() error {
	if j.JurisdictionID == uuid.Nil {
		return ErrNilTenant
	}
	if j.Country == "" {
		return fmt.Errorf("governance: country required")
	}
	if j.Version < 1 {
		return ErrUnversionedDetail{Field: "jurisdiction.version"}
	}
	if j.ValidTo != nil && !j.ValidTo.After(j.ValidFrom) {
		return ErrInvalidIntervalDetail{Reason: "valid_to must be after valid_from"}
	}
	return nil
}

type LegalRulePack struct {
	TenantID       uuid.UUID       `json:"tenant_id"`
	PackID         uuid.UUID       `json:"pack_id"`
	JurisdictionID uuid.UUID       `json:"jurisdiction_id"`
	PackKey        string          `json:"pack_key"`
	Version        int64           `json:"version"`
	ValidFrom      time.Time       `json:"valid_from"`
	ValidTo        *time.Time      `json:"valid_to"`
	ContentDigest  string          `json:"content_digest"`
	Body           json.RawMessage `json:"body"`
	CreatedAt      time.Time       `json:"created_at"`
}

func (l LegalRulePack) Validate() error {
	if l.TenantID == uuid.Nil || l.PackID == uuid.Nil || l.JurisdictionID == uuid.Nil {
		return ErrNilTenant
	}
	if l.Version < 1 {
		return ErrUnversionedDetail{Field: "legal_rule_pack.version"}
	}
	if l.ContentDigest == "" {
		return ErrMissingDigest
	}
	if l.PackKey == "" {
		return fmt.Errorf("%w: pack_key required", ErrUnversionedAuthority)
	}
	if l.ValidTo != nil && !l.ValidTo.After(l.ValidFrom) {
		return ErrInvalidIntervalDetail{Reason: "valid_to must be after valid_from"}
	}
	return nil
}

type RuleEvaluation struct {
	TenantID      uuid.UUID       `json:"tenant_id"`
	EvaluationID  uuid.UUID       `json:"evaluation_id"`
	PackID        uuid.UUID       `json:"pack_id"`
	InputDigest   string          `json:"input_digest"`
	ResultDigest  string          `json:"result_digest"`
	EvidenceID    uuid.UUID       `json:"evidence_id"`
	Result        string          `json:"result"`
	ValidUntil    time.Time       `json:"valid_until"`
	EvaluatedAt   time.Time       `json:"evaluated_at"`
	InputSnapshot json.RawMessage `json:"input_snapshot"`
}

func (r RuleEvaluation) Validate() error {
	if r.TenantID == uuid.Nil || r.EvaluationID == uuid.Nil || r.PackID == uuid.Nil {
		return ErrNilTenant
	}
	if r.EvidenceID == uuid.Nil {
		return fmt.Errorf("%w: evidence_id required", ErrMissingDigest)
	}
	if r.InputDigest == "" || r.ResultDigest == "" {
		return ErrMissingDigest
	}
	if !r.ValidUntil.After(r.EvaluatedAt) {
		return ErrInvalidIntervalDetail{Reason: "valid_until must be after evaluated_at"}
	}
	switch r.Result {
	case "COMPLIANT", "NON_COMPLIANT", "NEEDS_REVIEW", "NOT_APPLICABLE":
	default:
		return fmt.Errorf("governance: rule_evaluation result %q invalid", r.Result)
	}
	return nil
}

func (r RuleEvaluation) Verify(at time.Time) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if !at.Before(r.ValidUntil) {
		return ErrExpiredEvidence
	}
	return nil
}

type Obligation struct {
	TenantID      uuid.UUID       `json:"tenant_id"`
	ObligationID  uuid.UUID       `json:"obligation_id"`
	EvaluationID  uuid.UUID       `json:"evaluation_id"`
	Kind          string          `json:"kind"`
	Status        string          `json:"status"`
	DueAt         *time.Time      `json:"due_at"`
	Payload       json.RawMessage `json:"payload"`
	ContentDigest string          `json:"content_digest"`
	CreatedAt     time.Time       `json:"created_at"`
}

func (o Obligation) Validate() error {
	if o.TenantID == uuid.Nil || o.ObligationID == uuid.Nil || o.EvaluationID == uuid.Nil {
		return ErrNilTenant
	}
	if o.Kind == "" {
		return fmt.Errorf("governance: obligation kind required")
	}
	if o.ContentDigest == "" {
		return ErrMissingDigest
	}
	switch o.Status {
	case "OPEN", "FULFILLED", "WAIVED", "OVERDUE", "CANCELLED":
	default:
		return fmt.Errorf("governance: obligation status %q invalid", o.Status)
	}
	return nil
}

type ObligationBinding struct {
	TenantID     uuid.UUID       `json:"tenant_id"`
	BindingID    uuid.UUID       `json:"binding_id"`
	ObligationID uuid.UUID       `json:"obligation_id"`
	PrincipalID  uuid.UUID       `json:"principal_id"`
	BoundAt      time.Time       `json:"bound_at"`
	Status       string          `json:"status"`
	Metadata     json.RawMessage `json:"metadata"`
}

func (o ObligationBinding) Validate() error {
	if o.TenantID == uuid.Nil || o.BindingID == uuid.Nil || o.ObligationID == uuid.Nil || o.PrincipalID == uuid.Nil {
		return ErrNilTenant
	}
	switch o.Status {
	case "BOUND", "RELEASED", "FULFILLED":
	default:
		return fmt.Errorf("governance: obligation_binding status %q invalid", o.Status)
	}
	return nil
}

type EvidenceArtifact struct {
	TenantID       uuid.UUID       `json:"tenant_id"`
	ArtifactID     uuid.UUID       `json:"artifact_id"`
	Kind           string          `json:"kind"`
	ContentDigest  string          `json:"content_digest"`
	ObjectStoreRef string          `json:"object_store_ref"`
	SizeBytes      int64           `json:"size_bytes"`
	Retention      string          `json:"retention"`
	CollectedAt    time.Time       `json:"collected_at"`
	CreatedAt      time.Time       `json:"created_at"`
	Metadata       json.RawMessage `json:"metadata"`
}

func (e EvidenceArtifact) Validate() error {
	if e.TenantID == uuid.Nil || e.ArtifactID == uuid.Nil {
		return ErrNilTenant
	}
	if e.ContentDigest == "" {
		return ErrMissingDigest
	}
	if e.ObjectStoreRef == "" {
		return fmt.Errorf("governance: object_store_ref required")
	}
	if e.SizeBytes < 0 {
		return ErrInvalidIntervalDetail{Reason: "size_bytes negative"}
	}
	if e.Retention != "PERMANENT" {
		return fmt.Errorf("governance: retention must be PERMANENT")
	}
	return nil
}

func (e EvidenceArtifact) IsExpired(at time.Time) bool { return false }

type EvidenceManifest struct {
	TenantID    uuid.UUID       `json:"tenant_id"`
	ManifestID  uuid.UUID       `json:"manifest_id"`
	ArtifactIDs []uuid.UUID     `json:"artifact_ids"`
	RootDigest  string          `json:"root_digest"`
	CollectedAt time.Time       `json:"collected_at"`
	CreatedAt   time.Time       `json:"created_at"`
	Metadata    json.RawMessage `json:"metadata"`
}

func (e EvidenceManifest) Validate() error {
	if e.TenantID == uuid.Nil || e.ManifestID == uuid.Nil {
		return ErrNilTenant
	}
	if e.RootDigest == "" {
		return ErrMissingDigest
	}
	return nil
}

type GovernanceDecisionBundle struct {
	TenantID           uuid.UUID       `json:"tenant_id"`
	BundleID           uuid.UUID       `json:"bundle_id"`
	AuthzDecisionIDs   []uuid.UUID     `json:"authz_decision_ids"`
	LegalEvaluationIDs []uuid.UUID     `json:"legal_evaluation_ids"`
	ObligationIDs      []uuid.UUID     `json:"obligation_ids"`
	EvidenceManifestID *uuid.UUID      `json:"evidence_manifest_id"`
	CanonicalDigest    string          `json:"canonical_digest"`
	ValidUntil         time.Time       `json:"valid_until"`
	CreatedAt          time.Time       `json:"created_at"`
	Metadata           json.RawMessage `json:"metadata"`
}

func (g GovernanceDecisionBundle) Validate() error {
	if g.TenantID == uuid.Nil || g.BundleID == uuid.Nil {
		return ErrNilTenant
	}
	if g.CanonicalDigest == "" {
		return ErrMissingDigest
	}
	if !g.ValidUntil.After(g.CreatedAt) {
		return ErrInvalidIntervalDetail{Reason: "valid_until must be after created_at"}
	}
	return nil
}

func (g GovernanceDecisionBundle) Verify(at time.Time) error {
	if err := g.Validate(); err != nil {
		return err
	}
	if !at.Before(g.ValidUntil) {
		return ErrExpiredEvidence
	}
	return nil
}

func ensureTenant(ctx context.Context, tx dbport.Execer, tenant uuid.UUID) error {
	if tenant == uuid.Nil {
		return ErrNilTenant
	}
	return tenancy.WithTenant(ctx, tx, tenant)
}

func insertPrincipal(ctx context.Context, tx dbport.Tx, p Principal) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, p.TenantID); err != nil {
		return err
	}
	if p.Metadata == nil {
		p.Metadata = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO principal (tenant_id, principal_id, kind, subject, org_scope_id, assurance, authn_method, credential_digest, revocation_epoch, lifecycle, created_at, updated_at, expires_at, metadata) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, p.TenantID, p.PrincipalID, p.Kind, p.Subject, p.OrgScopeID, p.Assurance, p.AuthnMethod, p.CredentialDigest, p.RevocationEpoch, p.Lifecycle, p.CreatedAt, p.UpdatedAt, p.ExpiresAt, p.Metadata)
	return err
}

func InsertPrincipal(ctx context.Context, tx dbport.Tx, p Principal) error {
	return insertPrincipal(ctx, tx, p)
}

func LoadPrincipal(ctx context.Context, q dbport.Querier, tenantID, principalID uuid.UUID) (Principal, error) {
	var p Principal
	var orgScope *uuid.UUID
	var cred *string
	var expires *time.Time
	var meta []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, principal_id, kind, subject, org_scope_id, assurance, authn_method, credential_digest, revocation_epoch, lifecycle, created_at, updated_at, expires_at, metadata FROM principal WHERE tenant_id=$1 AND principal_id=$2`, tenantID, principalID).Scan(&p.TenantID, &p.PrincipalID, &p.Kind, &p.Subject, &orgScope, &p.Assurance, &p.AuthnMethod, &cred, &p.RevocationEpoch, &p.Lifecycle, &p.CreatedAt, &p.UpdatedAt, &expires, &meta)
	if err != nil {
		return Principal{}, err
	}
	p.OrgScopeID = orgScope
	p.CredentialDigest = cred
	p.ExpiresAt = expires
	p.Metadata = meta
	return p, nil
}

func InsertAuthoritySource(ctx context.Context, tx dbport.Tx, a AuthoritySource) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, a.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO authority_source (tenant_id, authority_source_id, kind, display_name, uri, version, valid_interval, content_digest, created_at) VALUES ($1,$2,$3,$4,$5,$6,tstzrange($7::timestamptz,$8::timestamptz,'[)'),$9,$10)`, a.TenantID, a.AuthoritySourceID, a.Kind, a.DisplayName, a.URI, a.Version, a.ValidFrom, a.ValidTo, a.ContentDigest, a.CreatedAt)
	return err
}

func LoadAuthoritySource(ctx context.Context, q dbport.Querier, tenantID, id uuid.UUID) (AuthoritySource, error) {
	var a AuthoritySource
	var validTo *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, authority_source_id, kind, display_name, uri, version, lower(valid_interval), upper(valid_interval), content_digest, created_at FROM authority_source WHERE tenant_id=$1 AND authority_source_id=$2`, tenantID, id).Scan(&a.TenantID, &a.AuthoritySourceID, &a.Kind, &a.DisplayName, &a.URI, &a.Version, &a.ValidFrom, &validTo, &a.ContentDigest, &a.CreatedAt)
	if err != nil {
		return AuthoritySource{}, err
	}
	a.ValidTo = validTo
	return a, nil
}

func InsertAuthorityBinding(ctx context.Context, tx dbport.Tx, b AuthorityBinding) error {
	if err := b.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, b.TenantID); err != nil {
		return err
	}
	if b.Scope == nil {
		b.Scope = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO authority_binding (tenant_id, binding_id, principal_id, authority_source_id, scope, valid_from, valid_to, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, b.TenantID, b.BindingID, b.PrincipalID, b.AuthoritySourceID, b.Scope, b.ValidFrom, b.ValidTo, b.CreatedAt)
	return err
}

func LoadAuthorityBinding(ctx context.Context, q dbport.Querier, tenantID, bindingID uuid.UUID) (AuthorityBinding, error) {
	var b AuthorityBinding
	var scope []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, binding_id, principal_id, authority_source_id, scope, valid_from, valid_to, created_at FROM authority_binding WHERE tenant_id=$1 AND binding_id=$2`, tenantID, bindingID).Scan(&b.TenantID, &b.BindingID, &b.PrincipalID, &b.AuthoritySourceID, &scope, &b.ValidFrom, &b.ValidTo, &b.CreatedAt)
	if err != nil {
		return AuthorityBinding{}, err
	}
	b.Scope = scope
	return b, nil
}

func InsertAuthenticationSession(ctx context.Context, tx dbport.Tx, s AuthenticationSession) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, s.TenantID); err != nil {
		return err
	}
	if s.Metadata == nil {
		s.Metadata = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO authentication_session (tenant_id, session_id, principal_id, issued_at, expires_at, authn_method, assurance, session_digest, ip_hash, metadata, revoked_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, s.TenantID, s.SessionID, s.PrincipalID, s.IssuedAt, s.ExpiresAt, s.AuthnMethod, s.Assurance, s.SessionDigest, s.IPHash, s.Metadata, s.RevokedAt)
	return err
}

func LoadAuthenticationSession(ctx context.Context, q dbport.Querier, tenantID, sessionID uuid.UUID) (AuthenticationSession, error) {
	var s AuthenticationSession
	var meta []byte
	var ip *string
	err := q.QueryRow(ctx, `SELECT tenant_id, session_id, principal_id, issued_at, expires_at, authn_method, assurance, session_digest, ip_hash, metadata, revoked_at FROM authentication_session WHERE tenant_id=$1 AND session_id=$2`, tenantID, sessionID).Scan(&s.TenantID, &s.SessionID, &s.PrincipalID, &s.IssuedAt, &s.ExpiresAt, &s.AuthnMethod, &s.Assurance, &s.SessionDigest, &ip, &meta, &s.RevokedAt)
	if err != nil {
		return AuthenticationSession{}, err
	}
	s.IPHash = ip
	s.Metadata = meta
	return s, nil
}

func InsertDelegationGrant(ctx context.Context, tx dbport.Tx, d DelegationGrant) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, d.TenantID); err != nil {
		return err
	}
	if d.Scope == nil {
		d.Scope = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO delegation_grant (tenant_id, grant_id, delegator_principal_id, delegate_principal_id, delegator_tenant, delegate_tenant, scope, valid_from, valid_to, created_at, revoked_at, revoked_by, reason) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, d.TenantID, d.GrantID, d.DelegatorPrincipalID, d.DelegatePrincipalID, d.DelegatorTenant, d.DelegateTenant, d.Scope, d.ValidFrom, d.ValidTo, d.CreatedAt, d.RevokedAt, d.RevokedBy, d.Reason)
	return err
}

func LoadDelegationGrant(ctx context.Context, q dbport.Querier, tenantID, grantID uuid.UUID) (DelegationGrant, error) {
	var d DelegationGrant
	var scope []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, grant_id, delegator_principal_id, delegate_principal_id, delegator_tenant, delegate_tenant, scope, valid_from, valid_to, created_at, revoked_at, revoked_by, reason FROM delegation_grant WHERE tenant_id=$1 AND grant_id=$2`, tenantID, grantID).Scan(&d.TenantID, &d.GrantID, &d.DelegatorPrincipalID, &d.DelegatePrincipalID, &d.DelegatorTenant, &d.DelegateTenant, &scope, &d.ValidFrom, &d.ValidTo, &d.CreatedAt, &d.RevokedAt, &d.RevokedBy, &d.Reason)
	if err != nil {
		return DelegationGrant{}, err
	}
	d.Scope = scope
	return d, nil
}

func InsertAuthorizationPolicySnapshot(ctx context.Context, tx dbport.Tx, s AuthorizationPolicySnapshot) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, s.TenantID); err != nil {
		return err
	}
	if s.Body == nil {
		s.Body = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO authorization_policy_snapshot (tenant_id, snapshot_id, policy_key, version, content_digest, body, valid_from, valid_to, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, s.TenantID, s.SnapshotID, s.PolicyKey, s.Version, s.ContentDigest, s.Body, s.ValidFrom, s.ValidTo, s.CreatedAt)
	return err
}

func LoadAuthorizationPolicySnapshot(ctx context.Context, q dbport.Querier, tenantID, snapshotID uuid.UUID) (AuthorizationPolicySnapshot, error) {
	var s AuthorizationPolicySnapshot
	var body []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, snapshot_id, policy_key, version, content_digest, body, valid_from, valid_to, created_at FROM authorization_policy_snapshot WHERE tenant_id=$1 AND snapshot_id=$2`, tenantID, snapshotID).Scan(&s.TenantID, &s.SnapshotID, &s.PolicyKey, &s.Version, &s.ContentDigest, &body, &s.ValidFrom, &s.ValidTo, &s.CreatedAt)
	if err != nil {
		return AuthorizationPolicySnapshot{}, err
	}
	s.Body = body
	return s, nil
}

func InsertAuthorizationDecision(ctx context.Context, tx dbport.Tx, d AuthorizationDecision) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, d.TenantID); err != nil {
		return err
	}
	if d.Metadata == nil {
		d.Metadata = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO authorization_decision (tenant_id, decision_id, principal_id, policy_snapshot_id, input_digest, result_digest, evidence_id, result, valid_until, evaluated_at, policy_snapshot_ids, session_id, metadata) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, d.TenantID, d.DecisionID, d.PrincipalID, d.PolicySnapshotID, d.InputDigest, d.ResultDigest, d.EvidenceID, d.Result, d.ValidUntil, d.EvaluatedAt, d.PolicySnapshotIDs, d.SessionID, d.Metadata)
	return err
}

func LoadAuthorizationDecision(ctx context.Context, q dbport.Querier, tenantID, decisionID uuid.UUID) (AuthorizationDecision, error) {
	var d AuthorizationDecision
	var meta []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, decision_id, principal_id, policy_snapshot_id, input_digest, result_digest, evidence_id, result, valid_until, evaluated_at, policy_snapshot_ids, session_id, metadata FROM authorization_decision WHERE tenant_id=$1 AND decision_id=$2`, tenantID, decisionID).Scan(&d.TenantID, &d.DecisionID, &d.PrincipalID, &d.PolicySnapshotID, &d.InputDigest, &d.ResultDigest, &d.EvidenceID, &d.Result, &d.ValidUntil, &d.EvaluatedAt, &d.PolicySnapshotIDs, &d.SessionID, &meta)
	if err != nil {
		return AuthorizationDecision{}, err
	}
	d.Metadata = meta
	return d, nil
}

func InsertDataAccessManifest(ctx context.Context, tx dbport.Tx, m DataAccessManifest) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, m.TenantID); err != nil {
		return err
	}
	if m.Fields == nil {
		m.Fields = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO data_access_manifest (tenant_id, manifest_id, decision_id, principal_id, resource_ref, access_kind, fields, valid_interval, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,tstzrange($8::timestamptz,$9::timestamptz,'[)'),$10)`, m.TenantID, m.ManifestID, m.DecisionID, m.PrincipalID, m.ResourceRef, m.AccessKind, m.Fields, m.ValidFrom, m.ValidTo, m.CreatedAt)
	return err
}

func LoadDataAccessManifest(ctx context.Context, q dbport.Querier, tenantID, manifestID uuid.UUID) (DataAccessManifest, error) {
	var m DataAccessManifest
	var fields []byte
	var validTo *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, manifest_id, decision_id, principal_id, resource_ref, access_kind, fields, lower(valid_interval), upper(valid_interval), created_at FROM data_access_manifest WHERE tenant_id=$1 AND manifest_id=$2`, tenantID, manifestID).Scan(&m.TenantID, &m.ManifestID, &m.DecisionID, &m.PrincipalID, &m.ResourceRef, &m.AccessKind, &fields, &m.ValidFrom, &validTo, &m.CreatedAt)
	if err != nil {
		return DataAccessManifest{}, err
	}
	m.Fields = fields
	m.ValidTo = validTo
	return m, nil
}

func InsertJurisdiction(ctx context.Context, tx dbport.Tx, j Jurisdiction) error {
	if err := j.Validate(); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO jurisdiction (jurisdiction_id, country, state, region, version, valid_interval, created_at) VALUES ($1,$2,$3,$4,$5,tstzrange($6::timestamptz,$7::timestamptz,'[)'),$8)`, j.JurisdictionID, j.Country, j.State, j.Region, j.Version, j.ValidFrom, j.ValidTo, j.CreatedAt)
	return err
}

func LoadJurisdiction(ctx context.Context, q dbport.Querier, jurisdictionID uuid.UUID) (Jurisdiction, error) {
	var j Jurisdiction
	var validTo *time.Time
	err := q.QueryRow(ctx, `SELECT jurisdiction_id, country, state, region, version, lower(valid_interval), upper(valid_interval), created_at FROM jurisdiction WHERE jurisdiction_id=$1`, jurisdictionID).Scan(&j.JurisdictionID, &j.Country, &j.State, &j.Region, &j.Version, &j.ValidFrom, &validTo, &j.CreatedAt)
	if err != nil {
		return Jurisdiction{}, err
	}
	j.ValidTo = validTo
	return j, nil
}

func InsertLegalRulePack(ctx context.Context, tx dbport.Tx, l LegalRulePack) error {
	if err := l.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, l.TenantID); err != nil {
		return err
	}
	if l.Body == nil {
		l.Body = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO legal_rule_pack (tenant_id, pack_id, jurisdiction_id, pack_key, version, valid_interval, content_digest, body, created_at) VALUES ($1,$2,$3,$4,$5,tstzrange($6::timestamptz,$7::timestamptz,'[)'),$8,$9,$10)`, l.TenantID, l.PackID, l.JurisdictionID, l.PackKey, l.Version, l.ValidFrom, l.ValidTo, l.ContentDigest, l.Body, l.CreatedAt)
	return err
}

func LoadLegalRulePack(ctx context.Context, q dbport.Querier, tenantID, packID uuid.UUID) (LegalRulePack, error) {
	var l LegalRulePack
	var body []byte
	var validTo *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, pack_id, jurisdiction_id, pack_key, version, lower(valid_interval), upper(valid_interval), content_digest, body, created_at FROM legal_rule_pack WHERE tenant_id=$1 AND pack_id=$2`, tenantID, packID).Scan(&l.TenantID, &l.PackID, &l.JurisdictionID, &l.PackKey, &l.Version, &l.ValidFrom, &validTo, &l.ContentDigest, &body, &l.CreatedAt)
	if err != nil {
		return LegalRulePack{}, err
	}
	l.Body = body
	l.ValidTo = validTo
	return l, nil
}

func InsertRuleEvaluation(ctx context.Context, tx dbport.Tx, r RuleEvaluation) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, r.TenantID); err != nil {
		return err
	}
	if r.InputSnapshot == nil {
		r.InputSnapshot = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO rule_evaluation (tenant_id, evaluation_id, pack_id, input_digest, result_digest, evidence_id, result, valid_until, evaluated_at, input_snapshot) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, r.TenantID, r.EvaluationID, r.PackID, r.InputDigest, r.ResultDigest, r.EvidenceID, r.Result, r.ValidUntil, r.EvaluatedAt, r.InputSnapshot)
	return err
}

func LoadRuleEvaluation(ctx context.Context, q dbport.Querier, tenantID, evaluationID uuid.UUID) (RuleEvaluation, error) {
	var r RuleEvaluation
	var snap []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, evaluation_id, pack_id, input_digest, result_digest, evidence_id, result, valid_until, evaluated_at, input_snapshot FROM rule_evaluation WHERE tenant_id=$1 AND evaluation_id=$2`, tenantID, evaluationID).Scan(&r.TenantID, &r.EvaluationID, &r.PackID, &r.InputDigest, &r.ResultDigest, &r.EvidenceID, &r.Result, &r.ValidUntil, &r.EvaluatedAt, &snap)
	if err != nil {
		return RuleEvaluation{}, err
	}
	r.InputSnapshot = snap
	return r, nil
}

func InsertObligation(ctx context.Context, tx dbport.Tx, o Obligation) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, o.TenantID); err != nil {
		return err
	}
	if o.Payload == nil {
		o.Payload = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO obligation (tenant_id, obligation_id, evaluation_id, kind, status, due_at, payload, content_digest, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, o.TenantID, o.ObligationID, o.EvaluationID, o.Kind, o.Status, o.DueAt, o.Payload, o.ContentDigest, o.CreatedAt)
	return err
}

func LoadObligation(ctx context.Context, q dbport.Querier, tenantID, obligationID uuid.UUID) (Obligation, error) {
	var o Obligation
	var payload []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, obligation_id, evaluation_id, kind, status, due_at, payload, content_digest, created_at FROM obligation WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, obligationID).Scan(&o.TenantID, &o.ObligationID, &o.EvaluationID, &o.Kind, &o.Status, &o.DueAt, &payload, &o.ContentDigest, &o.CreatedAt)
	if err != nil {
		return Obligation{}, err
	}
	o.Payload = payload
	return o, nil
}

func InsertObligationBinding(ctx context.Context, tx dbport.Tx, b ObligationBinding) error {
	if err := b.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, b.TenantID); err != nil {
		return err
	}
	if b.Metadata == nil {
		b.Metadata = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO obligation_binding (tenant_id, binding_id, obligation_id, principal_id, bound_at, status, metadata) VALUES ($1,$2,$3,$4,$5,$6,$7)`, b.TenantID, b.BindingID, b.ObligationID, b.PrincipalID, b.BoundAt, b.Status, b.Metadata)
	return err
}

func LoadObligationBinding(ctx context.Context, q dbport.Querier, tenantID, bindingID uuid.UUID) (ObligationBinding, error) {
	var b ObligationBinding
	var meta []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, binding_id, obligation_id, principal_id, bound_at, status, metadata FROM obligation_binding WHERE tenant_id=$1 AND binding_id=$2`, tenantID, bindingID).Scan(&b.TenantID, &b.BindingID, &b.ObligationID, &b.PrincipalID, &b.BoundAt, &b.Status, &meta)
	if err != nil {
		return ObligationBinding{}, err
	}
	b.Metadata = meta
	return b, nil
}

func InsertEvidenceArtifact(ctx context.Context, tx dbport.Tx, e EvidenceArtifact) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, e.TenantID); err != nil {
		return err
	}
	if e.Metadata == nil {
		e.Metadata = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO evidence_artifact (tenant_id, artifact_id, kind, content_digest, object_store_ref, size_bytes, retention, collected_at, created_at, metadata) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, e.TenantID, e.ArtifactID, e.Kind, e.ContentDigest, e.ObjectStoreRef, e.SizeBytes, e.Retention, e.CollectedAt, e.CreatedAt, e.Metadata)
	return err
}

func LoadEvidenceArtifact(ctx context.Context, q dbport.Querier, tenantID, artifactID uuid.UUID) (EvidenceArtifact, error) {
	var e EvidenceArtifact
	var meta []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, artifact_id, kind, content_digest, object_store_ref, size_bytes, retention, collected_at, created_at, metadata FROM evidence_artifact WHERE tenant_id=$1 AND artifact_id=$2`, tenantID, artifactID).Scan(&e.TenantID, &e.ArtifactID, &e.Kind, &e.ContentDigest, &e.ObjectStoreRef, &e.SizeBytes, &e.Retention, &e.CollectedAt, &e.CreatedAt, &meta)
	if err != nil {
		return EvidenceArtifact{}, err
	}
	e.Metadata = meta
	return e, nil
}

func InsertEvidenceManifest(ctx context.Context, tx dbport.Tx, e EvidenceManifest) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, e.TenantID); err != nil {
		return err
	}
	if e.Metadata == nil {
		e.Metadata = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO evidence_manifest (tenant_id, manifest_id, artifact_ids, root_digest, collected_at, created_at, metadata) VALUES ($1,$2,$3,$4,$5,$6,$7)`, e.TenantID, e.ManifestID, e.ArtifactIDs, e.RootDigest, e.CollectedAt, e.CreatedAt, e.Metadata)
	return err
}

func LoadEvidenceManifest(ctx context.Context, q dbport.Querier, tenantID, manifestID uuid.UUID) (EvidenceManifest, error) {
	var e EvidenceManifest
	var meta []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, manifest_id, artifact_ids, root_digest, collected_at, created_at, metadata FROM evidence_manifest WHERE tenant_id=$1 AND manifest_id=$2`, tenantID, manifestID).Scan(&e.TenantID, &e.ManifestID, &e.ArtifactIDs, &e.RootDigest, &e.CollectedAt, &e.CreatedAt, &meta)
	if err != nil {
		return EvidenceManifest{}, err
	}
	e.Metadata = meta
	return e, nil
}

func InsertGovernanceDecisionBundle(ctx context.Context, tx dbport.Tx, g GovernanceDecisionBundle) error {
	if err := g.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, g.TenantID); err != nil {
		return err
	}
	if g.Metadata == nil {
		g.Metadata = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO governance_decision_bundle (tenant_id, bundle_id, authz_decision_ids, legal_evaluation_ids, obligation_ids, evidence_manifest_id, canonical_digest, valid_until, created_at, metadata) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, g.TenantID, g.BundleID, g.AuthzDecisionIDs, g.LegalEvaluationIDs, g.ObligationIDs, g.EvidenceManifestID, g.CanonicalDigest, g.ValidUntil, g.CreatedAt, g.Metadata)
	return err
}

func LoadGovernanceDecisionBundle(ctx context.Context, q dbport.Querier, tenantID, bundleID uuid.UUID) (GovernanceDecisionBundle, error) {
	var g GovernanceDecisionBundle
	var meta []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, bundle_id, authz_decision_ids, legal_evaluation_ids, obligation_ids, evidence_manifest_id, canonical_digest, valid_until, created_at, metadata FROM governance_decision_bundle WHERE tenant_id=$1 AND bundle_id=$2`, tenantID, bundleID).Scan(&g.TenantID, &g.BundleID, &g.AuthzDecisionIDs, &g.LegalEvaluationIDs, &g.ObligationIDs, &g.EvidenceManifestID, &g.CanonicalDigest, &g.ValidUntil, &g.CreatedAt, &meta)
	if err != nil {
		return GovernanceDecisionBundle{}, err
	}
	g.Metadata = meta
	return g, nil
}

func VerifyAuthorizationNotExpired(decision AuthorizationDecision, at time.Time, evidenceCollectedAt time.Time) error {
	if err := decision.Verify(at); err != nil {
		return err
	}
	if !evidenceCollectedAt.Before(decision.ValidUntil) {
		return ErrExpiredEvidence
	}
	return nil
}
