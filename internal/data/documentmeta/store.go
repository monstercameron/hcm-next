// Package documentmeta is the tenant-scoped store for the document, template,
// signature and artifact-reference metadata migration 00031 creates (DB-014).
//
// It merges the "documents/templates/signatures and artifact-reference"
// families DB-014's GREEN clause names into one package because they share a
// single lifecycle: a template renders a document version, a signature binds
// the exact rendered bytes of that version, and an artifact reference is the
// governed pointer to those bytes in the object store migration 00010 owns.
// Splitting them would put a foreign key's two ends in two packages without
// buying any independence.
//
// The invariants this package holds, all of them also schema constraints:
//
//   - A signature binds the exact bytes. [InsertSignature] refuses a signed
//     digest that is not the version's rendered digest, and the composite
//     foreign key (tenant, version, rendered digest) refuses it again at the
//     database.
//   - Bytes never enter a row. An artifact reference carries a content id
//     and a retention class; the bytes stay in the governed object store.
//   - Nothing loses classification, version or deadline: the Validate
//     methods reject a zero value for each.
package documentmeta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
)

var (
	// ErrNilTenant is returned when a row omits its tenant or its own id.
	ErrNilTenant = errors.New("documentmeta: tenant or row id is nil")
	// ErrDigestMismatch is returned when a signature claims to cover bytes
	// other than the ones its document version actually rendered.
	ErrDigestMismatch = errors.New("documentmeta: signature does not bind the version's rendered bytes")
	// ErrRawSignatureValue is returned when a signature carries its value
	// instead of a reference into the governed store.
	ErrRawSignatureValue = errors.New("documentmeta: raw signature value rejected, expected a governed reference")
	// ErrMissingDigest is returned when a required digest is absent.
	ErrMissingDigest = errors.New("documentmeta: content digest missing")
	// ErrMissingVersion is returned when a versioned row omits its version.
	ErrMissingVersion = errors.New("documentmeta: version missing or not positive")
	// ErrMissingClassification is returned when a row omits its
	// classification.
	ErrMissingClassification = errors.New("documentmeta: classification missing")
	// ErrMissingDeadline is returned when a signature request omits its
	// deadline.
	ErrMissingDeadline = errors.New("documentmeta: deadline missing")
	// ErrInvalidEnum is returned for a value outside its declared set.
	ErrInvalidEnum = errors.New("documentmeta: value outside its declared set")
	// ErrInvalidInterval is returned for a reversed or empty interval.
	ErrInvalidInterval = errors.New("documentmeta: time interval invalid")
	// ErrUnapprovedPublication is returned when a template publishes before
	// both its legal and accessibility approvals have landed.
	ErrUnapprovedPublication = errors.New("documentmeta: template published without both approvals")
)

// ErrDetail names the field behind one of the sentinels above.
type ErrDetail struct {
	Sentinel error
	Detail   string
}

func (e ErrDetail) Error() string { return fmt.Sprintf("%v: %s", e.Sentinel, e.Detail) }

func (e ErrDetail) Unwrap() error { return e.Sentinel }

func detail(sentinel error, format string, args ...any) error {
	return ErrDetail{Sentinel: sentinel, Detail: fmt.Sprintf(format, args...)}
}

// DocumentTables is the exact set of base tables migration 00031 creates for
// the content half of DB-014, sorted.
var DocumentTables = []string{
	"document",
	"document_artifact_reference",
	"document_template",
	"document_version",
	"signature",
	"signature_request",
}

// signatureValueRefPattern mirrors the schema's
// signature_value_is_reference constraint.
var signatureValueRefPattern = regexp.MustCompile(`^(artifact|kms|provider)://\S+$`)

func ensureTenant(ctx context.Context, tx dbport.Execer, tenantID uuid.UUID) error {
	if tenantID == uuid.Nil {
		return ErrNilTenant
	}
	return tenancy.WithTenant(ctx, tx, tenantID)
}

func object(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func oneOf(value string, allowed ...string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// document_template
// ---------------------------------------------------------------------------

// DocumentTemplate is one versioned, approved rendering contract.
type DocumentTemplate struct {
	TenantID              uuid.UUID       `json:"tenant_id"`
	TemplateID            uuid.UUID       `json:"template_id"`
	TemplateKey           string          `json:"template_key"`
	TemplateVersion       int64           `json:"template_version"`
	Purpose               string          `json:"purpose"`
	SourceLocale          string          `json:"source_locale"`
	ParameterSchema       json.RawMessage `json:"parameter_schema"`
	ContentDigest         string          `json:"content_digest"`
	LegalApproval         string          `json:"legal_approval"`
	AccessibilityApproval string          `json:"accessibility_approval"`
	EffectiveFrom         time.Time       `json:"effective_from"`
	EffectiveTo           *time.Time      `json:"effective_to"`
	PublicationState      string          `json:"publication_state"`
}

func (t DocumentTemplate) Validate() error {
	if t.TenantID == uuid.Nil || t.TemplateID == uuid.Nil {
		return ErrNilTenant
	}
	if t.TemplateVersion < 1 {
		return detail(ErrMissingVersion, "document_template.template_version")
	}
	if t.ContentDigest == "" {
		return detail(ErrMissingDigest, "document_template.content_digest")
	}
	if !oneOf(t.LegalApproval, "PENDING", "APPROVED", "REJECTED") || !oneOf(t.AccessibilityApproval, "PENDING", "APPROVED", "REJECTED") {
		return detail(ErrInvalidEnum, "document_template approvals legal=%q accessibility=%q", t.LegalApproval, t.AccessibilityApproval)
	}
	if !oneOf(t.PublicationState, "DRAFT", "PUBLISHED", "RETIRED") {
		return detail(ErrInvalidEnum, "document_template.publication_state=%q", t.PublicationState)
	}
	if t.PublicationState == "PUBLISHED" && (t.LegalApproval != "APPROVED" || t.AccessibilityApproval != "APPROVED") {
		return detail(ErrUnapprovedPublication, "legal=%s accessibility=%s", t.LegalApproval, t.AccessibilityApproval)
	}
	if t.EffectiveTo != nil && !t.EffectiveTo.After(t.EffectiveFrom) {
		return detail(ErrInvalidInterval, "document_template effective interval is not half-open")
	}
	return nil
}

func InsertDocumentTemplate(ctx context.Context, tx dbport.Tx, t DocumentTemplate) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, t.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO document_template (tenant_id, template_id, template_key, template_version, purpose, source_locale, parameter_schema, content_digest, legal_approval, accessibility_approval, effective_from, effective_to, publication_state) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		t.TenantID, t.TemplateID, t.TemplateKey, t.TemplateVersion, t.Purpose, t.SourceLocale, object(t.ParameterSchema), t.ContentDigest, t.LegalApproval, t.AccessibilityApproval, t.EffectiveFrom, t.EffectiveTo, t.PublicationState)
	return err
}

func LoadDocumentTemplate(ctx context.Context, q dbport.Querier, tenantID, templateID uuid.UUID) (DocumentTemplate, error) {
	var t DocumentTemplate
	var schema []byte
	var to *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, template_id, template_key, template_version, purpose, source_locale, parameter_schema, content_digest, legal_approval, accessibility_approval, effective_from, effective_to, publication_state FROM document_template WHERE tenant_id=$1 AND template_id=$2`, tenantID, templateID).
		Scan(&t.TenantID, &t.TemplateID, &t.TemplateKey, &t.TemplateVersion, &t.Purpose, &t.SourceLocale, &schema, &t.ContentDigest, &t.LegalApproval, &t.AccessibilityApproval, &t.EffectiveFrom, &to, &t.PublicationState)
	if err != nil {
		return DocumentTemplate{}, err
	}
	t.ParameterSchema = schema
	t.EffectiveTo = to
	return t, nil
}

// ---------------------------------------------------------------------------
// document and document_version
// ---------------------------------------------------------------------------

// Document is the logical record; its bytes live in versions.
type Document struct {
	TenantID             uuid.UUID  `json:"tenant_id"`
	DocumentID           uuid.UUID  `json:"document_id"`
	DocumentType         string     `json:"document_type"`
	OwnerRef             string     `json:"owner_ref"`
	SubjectRef           string     `json:"subject_ref"`
	CurrentVersionID     *uuid.UUID `json:"current_version_id"`
	Classification       string     `json:"classification"`
	Compartment          string     `json:"compartment"`
	RecordDeclarationRef string     `json:"record_declaration_ref"`
	Lifecycle            string     `json:"lifecycle"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

func (d Document) Validate() error {
	if d.TenantID == uuid.Nil || d.DocumentID == uuid.Nil {
		return ErrNilTenant
	}
	if d.Classification == "" {
		return detail(ErrMissingClassification, "document.classification")
	}
	if !oneOf(d.Lifecycle, "DRAFT", "ACTIVE", "SUPERSEDED", "ARCHIVED", "DISPOSED") {
		return detail(ErrInvalidEnum, "document.lifecycle=%q", d.Lifecycle)
	}
	return nil
}

func InsertDocument(ctx context.Context, tx dbport.Tx, d Document) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, d.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO document (tenant_id, document_id, document_type, owner_ref, subject_ref, current_version_id, classification, compartment, record_declaration_ref, lifecycle, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		d.TenantID, d.DocumentID, d.DocumentType, d.OwnerRef, d.SubjectRef, d.CurrentVersionID, d.Classification, d.Compartment, d.RecordDeclarationRef, d.Lifecycle, d.CreatedAt, d.UpdatedAt)
	return err
}

func LoadDocument(ctx context.Context, q dbport.Querier, tenantID, documentID uuid.UUID) (Document, error) {
	var d Document
	var current *uuid.UUID
	err := q.QueryRow(ctx, `SELECT tenant_id, document_id, document_type, owner_ref, subject_ref, current_version_id, classification, compartment, record_declaration_ref, lifecycle, created_at, updated_at FROM document WHERE tenant_id=$1 AND document_id=$2`, tenantID, documentID).
		Scan(&d.TenantID, &d.DocumentID, &d.DocumentType, &d.OwnerRef, &d.SubjectRef, &current, &d.Classification, &d.Compartment, &d.RecordDeclarationRef, &d.Lifecycle, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return Document{}, err
	}
	d.CurrentVersionID = current
	return d, nil
}

// SetCurrentVersion points a document at one of its own sealed versions.
func SetCurrentVersion(ctx context.Context, tx dbport.Tx, tenantID, documentID, versionID uuid.UUID, at time.Time) error {
	if tenantID == uuid.Nil || documentID == uuid.Nil || versionID == uuid.Nil {
		return ErrNilTenant
	}
	if err := ensureTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	var owner uuid.UUID
	var status string
	if err := tx.QueryRow(ctx, `SELECT document_id, status FROM document_version WHERE tenant_id=$1 AND version_id=$2`, tenantID, versionID).Scan(&owner, &status); err != nil {
		return err
	}
	if owner != documentID {
		return detail(ErrInvalidEnum, "version %s belongs to document %s", versionID, owner)
	}
	if status != "SEALED" {
		return detail(ErrInvalidEnum, "version %s is %s, not SEALED", versionID, status)
	}
	affected, err := tx.Exec(ctx, `UPDATE document SET current_version_id=$3, lifecycle='ACTIVE', updated_at=$4 WHERE tenant_id=$1 AND document_id=$2`, tenantID, documentID, versionID, at)
	if err != nil {
		return err
	}
	if affected == 0 {
		return dbport.ErrNoRows
	}
	return nil
}

// DocumentVersion is one immutable rendering of a document.
type DocumentVersion struct {
	TenantID                uuid.UUID  `json:"tenant_id"`
	VersionID               uuid.UUID  `json:"version_id"`
	DocumentID              uuid.UUID  `json:"document_id"`
	VersionNumber           int64      `json:"version_number"`
	TemplateID              *uuid.UUID `json:"template_id"`
	TemplateVersion         *int64     `json:"template_version"`
	RenderVersion           int64      `json:"render_version"`
	CanonicalizationVersion int64      `json:"canonicalization_version"`
	SourceArtifactDigest    string     `json:"source_artifact_digest"`
	RenderedArtifactDigest  string     `json:"rendered_artifact_digest"`
	Locale                  string     `json:"locale"`
	JurisdictionRef         string     `json:"jurisdiction_ref"`
	Classification          string     `json:"classification"`
	SupersedesVersionID     *uuid.UUID `json:"supersedes_version_id"`
	CreatedAt               time.Time  `json:"created_at"`
	SealedAt                *time.Time `json:"sealed_at"`
	Status                  string     `json:"status"`
}

func (v DocumentVersion) Validate() error {
	if v.TenantID == uuid.Nil || v.VersionID == uuid.Nil || v.DocumentID == uuid.Nil {
		return ErrNilTenant
	}
	if v.VersionNumber < 1 || v.RenderVersion < 1 || v.CanonicalizationVersion < 1 {
		return detail(ErrMissingVersion, "document_version version/render/canonicalization")
	}
	if v.SourceArtifactDigest == "" || v.RenderedArtifactDigest == "" {
		return detail(ErrMissingDigest, "document_version source/rendered artifact digest")
	}
	if v.Classification == "" {
		return detail(ErrMissingClassification, "document_version.classification")
	}
	if !oneOf(v.Status, "DRAFT", "SEALED", "SUPERSEDED", "VOID") {
		return detail(ErrInvalidEnum, "document_version.status=%q", v.Status)
	}
	if v.Status == "SEALED" && v.SealedAt == nil {
		return detail(ErrInvalidInterval, "document_version sealed without a sealed_at")
	}
	if (v.TemplateID == nil) != (v.TemplateVersion == nil) {
		return detail(ErrMissingVersion, "document_version names a template without its version, or the reverse")
	}
	if v.SupersedesVersionID != nil && *v.SupersedesVersionID == v.VersionID {
		return detail(ErrInvalidEnum, "document_version supersedes itself")
	}
	return nil
}

func InsertDocumentVersion(ctx context.Context, tx dbport.Tx, v DocumentVersion) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, v.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO document_version (tenant_id, version_id, document_id, version_number, template_id, template_version, render_version, canonicalization_version, source_artifact_digest, rendered_artifact_digest, locale, jurisdiction_ref, classification, supersedes_version_id, created_at, sealed_at, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		v.TenantID, v.VersionID, v.DocumentID, v.VersionNumber, v.TemplateID, v.TemplateVersion, v.RenderVersion, v.CanonicalizationVersion, v.SourceArtifactDigest, v.RenderedArtifactDigest, v.Locale, v.JurisdictionRef, v.Classification, v.SupersedesVersionID, v.CreatedAt, v.SealedAt, v.Status)
	return err
}

func LoadDocumentVersion(ctx context.Context, q dbport.Querier, tenantID, versionID uuid.UUID) (DocumentVersion, error) {
	var v DocumentVersion
	var template, supersedes *uuid.UUID
	var templateVersion *int64
	var sealed *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, version_id, document_id, version_number, template_id, template_version, render_version, canonicalization_version, source_artifact_digest, rendered_artifact_digest, locale, jurisdiction_ref, classification, supersedes_version_id, created_at, sealed_at, status FROM document_version WHERE tenant_id=$1 AND version_id=$2`, tenantID, versionID).
		Scan(&v.TenantID, &v.VersionID, &v.DocumentID, &v.VersionNumber, &template, &templateVersion, &v.RenderVersion, &v.CanonicalizationVersion, &v.SourceArtifactDigest, &v.RenderedArtifactDigest, &v.Locale, &v.JurisdictionRef, &v.Classification, &supersedes, &v.CreatedAt, &sealed, &v.Status)
	if err != nil {
		return DocumentVersion{}, err
	}
	v.TemplateID = template
	v.TemplateVersion = templateVersion
	v.SupersedesVersionID = supersedes
	v.SealedAt = sealed
	return v, nil
}

// ---------------------------------------------------------------------------
// signature_request and signature
// ---------------------------------------------------------------------------

// SignatureRequest is one signing ceremony over a document version.
type SignatureRequest struct {
	TenantID           uuid.UUID       `json:"tenant_id"`
	RequestID          uuid.UUID       `json:"request_id"`
	VersionID          uuid.UUID       `json:"version_id"`
	AssuranceMode      string          `json:"assurance_mode"`
	SignerRequirements json.RawMessage `json:"signer_requirements"`
	Provider           string          `json:"provider"`
	DeadlineAt         time.Time       `json:"deadline_at"`
	CreatedAt          time.Time       `json:"created_at"`
	Status             string          `json:"status"`
}

func (r SignatureRequest) Validate() error {
	if r.TenantID == uuid.Nil || r.RequestID == uuid.Nil || r.VersionID == uuid.Nil {
		return ErrNilTenant
	}
	if !oneOf(r.AssuranceMode, "NATIVE_EVIDENCE", "EXTERNAL_PROVIDER", "MANUAL_GATE") {
		return detail(ErrInvalidEnum, "signature_request.assurance_mode=%q", r.AssuranceMode)
	}
	if !oneOf(r.Status, "OPEN", "COMPLETED", "DECLINED", "EXPIRED", "WITHDRAWN") {
		return detail(ErrInvalidEnum, "signature_request.status=%q", r.Status)
	}
	if r.DeadlineAt.IsZero() || !r.DeadlineAt.After(r.CreatedAt) {
		return detail(ErrMissingDeadline, "signature_request.deadline_at")
	}
	if r.AssuranceMode == "EXTERNAL_PROVIDER" && r.Provider == "" {
		return detail(ErrInvalidEnum, "signature_request external ceremony without a provider")
	}
	return nil
}

func InsertSignatureRequest(ctx context.Context, tx dbport.Tx, r SignatureRequest) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, r.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO signature_request (tenant_id, request_id, version_id, assurance_mode, signer_requirements, provider, deadline_at, created_at, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		r.TenantID, r.RequestID, r.VersionID, r.AssuranceMode, object(r.SignerRequirements), r.Provider, r.DeadlineAt, r.CreatedAt, r.Status)
	return err
}

func LoadSignatureRequest(ctx context.Context, q dbport.Querier, tenantID, requestID uuid.UUID) (SignatureRequest, error) {
	var r SignatureRequest
	var requirements []byte
	err := q.QueryRow(ctx, `SELECT tenant_id, request_id, version_id, assurance_mode, signer_requirements, provider, deadline_at, created_at, status FROM signature_request WHERE tenant_id=$1 AND request_id=$2`, tenantID, requestID).
		Scan(&r.TenantID, &r.RequestID, &r.VersionID, &r.AssuranceMode, &requirements, &r.Provider, &r.DeadlineAt, &r.CreatedAt, &r.Status)
	if err != nil {
		return SignatureRequest{}, err
	}
	r.SignerRequirements = requirements
	return r, nil
}

// Signature is one signer's immutable act over the exact rendered bytes of a
// document version.
type Signature struct {
	TenantID          uuid.UUID `json:"tenant_id"`
	SignatureID       uuid.UUID `json:"signature_id"`
	RequestID         uuid.UUID `json:"request_id"`
	VersionID         uuid.UUID `json:"version_id"`
	SignerRef         string    `json:"signer_ref"`
	SignedDigest      string    `json:"signed_digest"`
	SignatureValueRef string    `json:"signature_value_ref"`
	CertificateRef    string    `json:"certificate_ref"`
	IdentityAssurance string    `json:"identity_assurance"`
	SignedAt          time.Time `json:"signed_at"`
	ValidityState     string    `json:"validity_state"`
}

func (s Signature) Validate() error {
	if s.TenantID == uuid.Nil || s.SignatureID == uuid.Nil || s.RequestID == uuid.Nil || s.VersionID == uuid.Nil {
		return ErrNilTenant
	}
	if s.SignedDigest == "" {
		return detail(ErrMissingDigest, "signature.signed_digest")
	}
	if !signatureValueRefPattern.MatchString(s.SignatureValueRef) {
		return detail(ErrRawSignatureValue, "signature.signature_value_ref=%q is not an artifact://, kms:// or provider:// reference", s.SignatureValueRef)
	}
	if !oneOf(s.IdentityAssurance, "IAL1", "IAL2", "IAL3") {
		return detail(ErrInvalidEnum, "signature.identity_assurance=%q", s.IdentityAssurance)
	}
	if !oneOf(s.ValidityState, "VALID", "REVOKED", "DISPUTED", "EXPIRED") {
		return detail(ErrInvalidEnum, "signature.validity_state=%q", s.ValidityState)
	}
	return nil
}

// InsertSignature refuses a signature whose signed digest is not the exact
// rendered digest of the version it names. The schema's composite foreign key
// enforces the same binding, so a raw SQL path cannot route around this.
func InsertSignature(ctx context.Context, tx dbport.Tx, s Signature) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, s.TenantID); err != nil {
		return err
	}
	var rendered string
	if err := tx.QueryRow(ctx, `SELECT rendered_artifact_digest FROM document_version WHERE tenant_id=$1 AND version_id=$2`, s.TenantID, s.VersionID).Scan(&rendered); err != nil {
		return err
	}
	if rendered != s.SignedDigest {
		return detail(ErrDigestMismatch, "signed %s, version renders %s", s.SignedDigest, rendered)
	}
	_, err := tx.Exec(ctx, `INSERT INTO signature (tenant_id, signature_id, request_id, version_id, signer_ref, signed_digest, signature_value_ref, certificate_ref, identity_assurance, signed_at, validity_state) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		s.TenantID, s.SignatureID, s.RequestID, s.VersionID, s.SignerRef, s.SignedDigest, s.SignatureValueRef, s.CertificateRef, s.IdentityAssurance, s.SignedAt, s.ValidityState)
	return err
}

func LoadSignature(ctx context.Context, q dbport.Querier, tenantID, signatureID uuid.UUID) (Signature, error) {
	var s Signature
	err := q.QueryRow(ctx, `SELECT tenant_id, signature_id, request_id, version_id, signer_ref, signed_digest, signature_value_ref, certificate_ref, identity_assurance, signed_at, validity_state FROM signature WHERE tenant_id=$1 AND signature_id=$2`, tenantID, signatureID).
		Scan(&s.TenantID, &s.SignatureID, &s.RequestID, &s.VersionID, &s.SignerRef, &s.SignedDigest, &s.SignatureValueRef, &s.CertificateRef, &s.IdentityAssurance, &s.SignedAt, &s.ValidityState)
	if err != nil {
		return Signature{}, err
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// document_artifact_reference
// ---------------------------------------------------------------------------

// ArtifactReference is the governed pointer from document metadata to
// content-addressed bytes in the object store. It never carries bytes.
type ArtifactReference struct {
	TenantID        uuid.UUID  `json:"tenant_id"`
	ReferenceID     uuid.UUID  `json:"reference_id"`
	DocumentID      uuid.UUID  `json:"document_id"`
	VersionID       *uuid.UUID `json:"version_id"`
	ContentID       string     `json:"content_id"`
	DigestAlgorithm string     `json:"digest_algorithm"`
	Purpose         string     `json:"purpose"`
	Classification  string     `json:"classification"`
	RetentionClass  string     `json:"retention_class"`
	HoldState       string     `json:"hold_state"`
	ReferencedAt    time.Time  `json:"referenced_at"`
}

func (a ArtifactReference) Validate() error {
	if a.TenantID == uuid.Nil || a.ReferenceID == uuid.Nil || a.DocumentID == uuid.Nil {
		return ErrNilTenant
	}
	if a.ContentID == "" {
		return detail(ErrMissingDigest, "document_artifact_reference.content_id")
	}
	if a.Classification == "" {
		return detail(ErrMissingClassification, "document_artifact_reference.classification")
	}
	if !oneOf(a.Purpose, "SOURCE", "RENDERED", "ATTACHMENT", "REDACTION", "EVIDENCE", "THUMBNAIL") {
		return detail(ErrInvalidEnum, "document_artifact_reference.purpose=%q", a.Purpose)
	}
	if !oneOf(a.RetentionClass, "PERMANENT", "OPERATIONAL", "REBUILDABLE") {
		return detail(ErrInvalidEnum, "document_artifact_reference.retention_class=%q", a.RetentionClass)
	}
	if !oneOf(a.HoldState, "NONE", "HELD", "RELEASED") {
		return detail(ErrInvalidEnum, "document_artifact_reference.hold_state=%q", a.HoldState)
	}
	return nil
}

func InsertArtifactReference(ctx context.Context, tx dbport.Tx, a ArtifactReference) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, a.TenantID); err != nil {
		return err
	}
	if a.DigestAlgorithm == "" {
		a.DigestAlgorithm = "sha256"
	}
	_, err := tx.Exec(ctx, `INSERT INTO document_artifact_reference (tenant_id, reference_id, document_id, version_id, content_id, digest_algorithm, purpose, classification, retention_class, hold_state, referenced_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		a.TenantID, a.ReferenceID, a.DocumentID, a.VersionID, a.ContentID, a.DigestAlgorithm, a.Purpose, a.Classification, a.RetentionClass, a.HoldState, a.ReferencedAt)
	return err
}

func LoadArtifactReference(ctx context.Context, q dbport.Querier, tenantID, referenceID uuid.UUID) (ArtifactReference, error) {
	var a ArtifactReference
	var version *uuid.UUID
	err := q.QueryRow(ctx, `SELECT tenant_id, reference_id, document_id, version_id, content_id, digest_algorithm, purpose, classification, retention_class, hold_state, referenced_at FROM document_artifact_reference WHERE tenant_id=$1 AND reference_id=$2`, tenantID, referenceID).
		Scan(&a.TenantID, &a.ReferenceID, &a.DocumentID, &version, &a.ContentID, &a.DigestAlgorithm, &a.Purpose, &a.Classification, &a.RetentionClass, &a.HoldState, &a.ReferencedAt)
	if err != nil {
		return ArtifactReference{}, err
	}
	a.VersionID = version
	return a, nil
}
