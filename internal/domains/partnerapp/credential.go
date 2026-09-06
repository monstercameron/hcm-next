package partnerapp

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Application workload credentials are opaque references. This package never
// stores, returns, or derives a secret, token, key, or certificate value.
const MaxApplicationCredentialLifetime = 15 * time.Minute

var (
	ErrInvalidCredential        = errors.New("partnerapp: invalid application credential")
	ErrCredentialExpired        = errors.New("partnerapp: application credential is expired or not yet valid")
	ErrCredentialRevoked        = errors.New("partnerapp: application credential is revoked")
	ErrCredentialUnknown        = errors.New("partnerapp: application credential is unknown")
	ErrCredentialTampered       = errors.New("partnerapp: application credential does not match its issued reference")
	ErrCredentialScope          = errors.New("partnerapp: application credential exceeds installation scope")
	ErrCredentialBinding        = errors.New("partnerapp: application credential binding does not match installation")
	ErrCredentialSender         = errors.New("partnerapp: application credential sender does not match")
	ErrCredentialDestination    = errors.New("partnerapp: application credential destination does not match")
	ErrCredentialRotation       = errors.New("partnerapp: application credential cannot be rotated")
	ErrCredentialAlreadyRevoked = errors.New("partnerapp: application credential is already revoked")
)

// ApplicationWorkloadIdentity is an opaque, short-lived reference issued for
// one active installation. It carries only authorization coordinates and
// digests; it never carries credential material.
type ApplicationWorkloadIdentity struct {
	ID                   string
	InstallationID       string
	InstallationRevision uint64
	InstallationDigest   string
	ApplicationID        string
	ApplicationVersion   string
	ApplicationRevision  uint64
	ApplicationDigest    string
	Tenant               string
	WorkloadIdentity     string
	Sender               string
	Destination          string
	Purpose              string
	Capabilities         []string
	IssuedAt             time.Time
	ExpiresAt            time.Time
	Rotation             uint64
}

// ApplicationCredential and WorkloadCredential are vocabulary aliases for
// callers that use the credential rather than identity term.
type ApplicationCredential = ApplicationWorkloadIdentity
type WorkloadCredential = ApplicationWorkloadIdentity

// CredentialIssueRequest specifies the exact sender, destination and
// installation subset for a new reference. Empty values are not wildcards.
type CredentialIssueRequest struct {
	ID               string
	WorkloadIdentity string
	Sender           string
	Destination      string
	Purpose          string
	Capabilities     []string
	IssuedAt         time.Time
	Lifetime         time.Duration
}

// ApplicationCredentialRequest and WorkloadCredentialRequest are aliases for
// callers that use the credential vocabulary.
type ApplicationCredentialRequest = CredentialIssueRequest
type WorkloadCredentialRequest = CredentialIssueRequest

// CredentialUseRequest is the caller-supplied context checked against the
// immutable identity. Sender and destination are both exact, non-wildcard
// bindings, even when the same identity is used more than once.
type CredentialUseRequest struct {
	Sender       string
	Destination  string
	Purpose      string
	Capabilities []string
}

// CredentialEventKind identifies an evidence record without exposing any
// credential material.
type CredentialEventKind string

const (
	CredentialIssued  CredentialEventKind = "ISSUED"
	CredentialRotated CredentialEventKind = "ROTATED"
	CredentialRevoked CredentialEventKind = "REVOKED"
	CredentialChecked CredentialEventKind = "CHECKED"
)

// CredentialEvidence is a redaction-safe operation record. It contains
// references, coordinates, digests, and outcomes only.
type CredentialEvidence struct {
	Kind               CredentialEventKind
	CredentialID       string
	InstallationID     string
	InstallationDigest string
	ApplicationID      string
	ApplicationDigest  string
	Tenant             string
	WorkloadIdentity   string
	Sender             string
	Destination        string
	Purpose            string
	Rotation           uint64
	At                 time.Time
	Outcome            string
	Reason             string
	EvidenceDigest     string
}

// Explain returns a bounded summary that excludes credential material and
// does not disclose the capability list.
func (e CredentialEvidence) Explain() string {
	return fmt.Sprintf("partnerapp credential kind=%s id=%s installation=%s outcome=%s reason=%s evidence=%s", e.Kind, e.CredentialID, e.InstallationID, e.Outcome, e.Reason, e.EvidenceDigest)
}

type credentialRecord struct {
	identity ApplicationWorkloadIdentity
	revoked  bool
}

// WorkloadIdentityManager is a pure in-memory issuer and verifier for
// application workload identity references. It is an adapter for tests and
// domain composition; durable authority remains outside this package.
type WorkloadIdentityManager struct {
	mu      sync.Mutex
	now     func() time.Time
	records map[string]credentialRecord
	events  []CredentialEvidence
}

// ApplicationCredentialIssuer and CredentialManager are descriptive aliases
// for the same semantic owner.
type ApplicationCredentialIssuer = WorkloadIdentityManager
type CredentialManager = WorkloadIdentityManager

// NewWorkloadIdentityManager creates an issuer using now for all validity and
// evidence decisions. A nil clock uses UTC wall time.
func NewWorkloadIdentityManager(now func() time.Time) *WorkloadIdentityManager {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &WorkloadIdentityManager{now: now, records: make(map[string]credentialRecord)}
}

// NewApplicationCredentialIssuer is the application-identity constructor.
func NewApplicationCredentialIssuer(now func() time.Time) *WorkloadIdentityManager {
	return NewWorkloadIdentityManager(now)
}

// NewCredentialManager is a concise constructor alias.
func NewCredentialManager(now func() time.Time) *WorkloadIdentityManager {
	return NewWorkloadIdentityManager(now)
}

// Issue creates a reference bound to the exact active installation revision.
// The requested purpose and capabilities must be no broader than the
// installation grant, and sender/destination are exact values.
func (m *WorkloadIdentityManager) Issue(installation Installation, req CredentialIssueRequest) (ApplicationWorkloadIdentity, CredentialEvidence, error) {
	if m == nil {
		return ApplicationWorkloadIdentity{}, CredentialEvidence{}, ErrCredentialUnknown
	}
	if req.IssuedAt.IsZero() {
		req.IssuedAt = m.now().UTC()
	}
	if err := validateCredentialIssue(installation, req); err != nil {
		return ApplicationWorkloadIdentity{}, CredentialEvidence{}, err
	}
	identity, err := newIdentity(installation, req, 1)
	if err != nil {
		return ApplicationWorkloadIdentity{}, CredentialEvidence{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.records[identity.ID]; exists {
		return ApplicationWorkloadIdentity{}, CredentialEvidence{}, fmt.Errorf("%w: duplicate id", ErrInvalidCredential)
	}
	m.records[identity.ID] = credentialRecord{identity: cloneIdentity(identity)}
	evidence := m.evidenceLocked(CredentialIssued, identity, "issued", "")
	m.events = append(m.events, evidence)
	return cloneIdentity(identity), evidence, nil
}

// Rotate issues a new reference for the same active installation and marks
// the previous reference unusable. The replacement may narrow purpose or
// capabilities, but may not widen the installation grant.
func (m *WorkloadIdentityManager) Rotate(previous ApplicationWorkloadIdentity, installation Installation, req CredentialIssueRequest) (ApplicationWorkloadIdentity, CredentialEvidence, error) {
	if m == nil {
		return ApplicationWorkloadIdentity{}, CredentialEvidence{}, ErrCredentialUnknown
	}
	if req.IssuedAt.IsZero() {
		req.IssuedAt = m.now().UTC()
	}
	if err := validateCredentialIssue(installation, req); err != nil {
		return ApplicationWorkloadIdentity{}, CredentialEvidence{}, err
	}
	m.mu.Lock()
	record, ok := m.records[previous.ID]
	if !ok {
		m.mu.Unlock()
		return ApplicationWorkloadIdentity{}, CredentialEvidence{}, ErrCredentialUnknown
	}
	if record.revoked {
		m.mu.Unlock()
		return ApplicationWorkloadIdentity{}, CredentialEvidence{}, ErrCredentialRevoked
	}
	if !sameIdentity(record.identity, previous) {
		m.mu.Unlock()
		return ApplicationWorkloadIdentity{}, CredentialEvidence{}, ErrCredentialTampered
	}
	if err := identityBinding(record.identity, installation); err != nil {
		m.mu.Unlock()
		return ApplicationWorkloadIdentity{}, CredentialEvidence{}, err
	}
	identity, err := newIdentity(installation, req, previous.Rotation+1)
	if err != nil {
		m.mu.Unlock()
		return ApplicationWorkloadIdentity{}, CredentialEvidence{}, err
	}
	if _, exists := m.records[identity.ID]; exists {
		m.mu.Unlock()
		return ApplicationWorkloadIdentity{}, CredentialEvidence{}, fmt.Errorf("%w: duplicate id", ErrInvalidCredential)
	}
	record.revoked = true
	m.records[previous.ID] = record
	m.records[identity.ID] = credentialRecord{identity: cloneIdentity(identity)}
	evidence := m.evidenceLocked(CredentialRotated, identity, "rotated", previous.ID)
	m.events = append(m.events, evidence)
	m.mu.Unlock()
	return cloneIdentity(identity), evidence, nil
}

// Revoke immediately fences one issued reference. The reference and its
// evidence remain available for audit, but Check can no longer authorize it.
func (m *WorkloadIdentityManager) Revoke(identity ApplicationWorkloadIdentity, reason string) (CredentialEvidence, error) {
	if m == nil {
		return CredentialEvidence{}, ErrCredentialUnknown
	}
	if strings.TrimSpace(reason) == "" || strings.TrimSpace(reason) != reason {
		return CredentialEvidence{}, fmt.Errorf("%w: revoke reason is required", ErrInvalidCredential)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[identity.ID]
	if !ok {
		return CredentialEvidence{}, ErrCredentialUnknown
	}
	if !sameIdentity(record.identity, identity) {
		return CredentialEvidence{}, ErrCredentialTampered
	}
	if record.revoked {
		return CredentialEvidence{}, ErrCredentialAlreadyRevoked
	}
	record.revoked = true
	m.records[identity.ID] = record
	evidence := m.evidenceLocked(CredentialRevoked, identity, "revoked", reason)
	m.events = append(m.events, evidence)
	return evidence, nil
}

// Check verifies the identity against its issuing record, current
// installation, current time, and exact sender/destination request.
func (m *WorkloadIdentityManager) Check(identity ApplicationWorkloadIdentity, installation Installation, req CredentialUseRequest) (CredentialEvidence, error) {
	if m == nil {
		return CredentialEvidence{}, ErrCredentialUnknown
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[identity.ID]
	if !ok {
		return m.deniedLocked(identity, "unknown_credential", ErrCredentialUnknown)
	}
	if !sameIdentity(record.identity, identity) {
		return m.deniedLocked(record.identity, "tampered", ErrCredentialTampered)
	}
	if record.revoked {
		return m.deniedLocked(record.identity, "revoked", ErrCredentialRevoked)
	}
	if err := identityBinding(record.identity, installation); err != nil {
		return m.deniedLocked(record.identity, "installation_binding", err)
	}
	now := m.now().UTC()
	if now.Before(record.identity.IssuedAt) || !now.Before(record.identity.ExpiresAt) {
		return m.deniedLocked(record.identity, "expired", ErrCredentialExpired)
	}
	if req.Sender != record.identity.Sender {
		return m.deniedLocked(record.identity, "sender_mismatch", ErrCredentialSender)
	}
	if req.Destination != record.identity.Destination {
		return m.deniedLocked(record.identity, "destination_mismatch", ErrCredentialDestination)
	}
	if req.Purpose != record.identity.Purpose || !subset(req.Capabilities, record.identity.Capabilities) {
		return m.deniedLocked(record.identity, "credential_scope", ErrCredentialScope)
	}
	evidence := m.evidenceLocked(CredentialChecked, record.identity, "authorized", "")
	m.events = append(m.events, evidence)
	return evidence, nil
}

// Authorize is the value-oriented alias for Check.
func (m *WorkloadIdentityManager) Authorize(identity ApplicationWorkloadIdentity, installation Installation, req CredentialUseRequest) (CredentialEvidence, error) {
	return m.Check(identity, installation, req)
}

// ValidateAt checks only the immutable identity and installation binding at a
// caller-supplied instant. It is useful to gateways that already have the
// sender and destination in a separate request context.
func (m *WorkloadIdentityManager) ValidateAt(identity ApplicationWorkloadIdentity, installation Installation, at time.Time) error {
	if m == nil {
		return ErrCredentialUnknown
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[identity.ID]
	if !ok {
		return ErrCredentialUnknown
	}
	if !sameIdentity(record.identity, identity) {
		return ErrCredentialTampered
	}
	if record.revoked {
		return ErrCredentialRevoked
	}
	if err := identityBinding(record.identity, installation); err != nil {
		return err
	}
	if at.Before(identity.IssuedAt) || !at.Before(identity.ExpiresAt) {
		return ErrCredentialExpired
	}
	return nil
}

// Events returns a copy of redaction-safe issuance, rotation, revocation and
// authorization evidence.
func (m *WorkloadIdentityManager) Events() []CredentialEvidence {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]CredentialEvidence(nil), m.events...)
}

func validateCredentialIssue(installation Installation, req CredentialIssueRequest) error {
	if err := installation.Verify(); err != nil {
		return fmt.Errorf("%w: installation: %v", ErrCredentialBinding, err)
	}
	if installation.State != GrantActive {
		return fmt.Errorf("%w: installation is not active", ErrCredentialBinding)
	}
	if req.ID != "" && !exactRef(req.ID) {
		return fmt.Errorf("%w: credential id must be exact", ErrInvalidCredential)
	}
	for label, value := range map[string]string{
		"workload identity": req.WorkloadIdentity,
		"sender":            req.Sender, "destination": req.Destination, "purpose": req.Purpose,
	} {
		if !exactRef(value) {
			return fmt.Errorf("%w: %s is required and exact", ErrInvalidCredential, label)
		}
	}
	if req.Sender != req.WorkloadIdentity {
		return fmt.Errorf("%w: sender must equal workload identity", ErrCredentialSender)
	}
	if req.Purpose != installation.Purpose {
		return fmt.Errorf("%w: purpose is not granted by installation", ErrCredentialScope)
	}
	if err := validateExactSet(req.Capabilities, "credential capability", ErrInvalidCredential); err != nil {
		return err
	}
	if !subset(req.Capabilities, installation.Capabilities) {
		return fmt.Errorf("%w: capabilities are not granted by installation", ErrCredentialScope)
	}
	if req.Lifetime <= 0 || req.Lifetime > MaxApplicationCredentialLifetime {
		return fmt.Errorf("%w: lifetime must be positive and must not exceed %s", ErrInvalidCredential, MaxApplicationCredentialLifetime)
	}
	return nil
}

func newIdentity(installation Installation, req CredentialIssueRequest, rotation uint64) (ApplicationWorkloadIdentity, error) {
	if req.ID == "" {
		id, err := newCredentialID()
		if err != nil {
			return ApplicationWorkloadIdentity{}, err
		}
		req.ID = id
	}
	if req.IssuedAt.IsZero() {
		return ApplicationWorkloadIdentity{}, fmt.Errorf("%w: issued-at must be supplied by the manager", ErrInvalidCredential)
	}
	identity := ApplicationWorkloadIdentity{
		ID: req.ID, InstallationID: installation.InstallationID, InstallationRevision: installation.Revision, InstallationDigest: installation.Digest,
		ApplicationID: installation.VersionBinding.ApplicationID, ApplicationVersion: installation.VersionBinding.Version, ApplicationRevision: installation.VersionBinding.Revision, ApplicationDigest: installation.VersionBinding.Digest,
		Tenant: installation.TenantScope, WorkloadIdentity: req.WorkloadIdentity, Sender: req.Sender, Destination: req.Destination, Purpose: req.Purpose,
		Capabilities: sortedCopy(req.Capabilities), IssuedAt: req.IssuedAt.UTC(), ExpiresAt: req.IssuedAt.UTC().Add(req.Lifetime), Rotation: rotation,
	}
	if identity.ExpiresAt.Sub(identity.IssuedAt) > MaxApplicationCredentialLifetime {
		return ApplicationWorkloadIdentity{}, ErrInvalidCredential
	}
	return identity, nil
}

func newCredentialID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("%w: generate opaque credential reference: %v", ErrInvalidCredential, err)
	}
	return "credential:" + hex.EncodeToString(raw[:]), nil
}

func identityBinding(identity ApplicationWorkloadIdentity, installation Installation) error {
	if err := installation.Verify(); err != nil {
		return fmt.Errorf("%w: installation: %v", ErrCredentialBinding, err)
	}
	if installation.State != GrantActive {
		return fmt.Errorf("%w: installation is not active", ErrCredentialBinding)
	}
	if identity.InstallationID != installation.InstallationID || identity.InstallationRevision != installation.Revision || identity.InstallationDigest != installation.Digest ||
		identity.ApplicationID != installation.VersionBinding.ApplicationID || identity.ApplicationVersion != installation.VersionBinding.Version || identity.ApplicationRevision != installation.VersionBinding.Revision || identity.ApplicationDigest != installation.VersionBinding.Digest || identity.Tenant != installation.TenantScope {
		return ErrCredentialBinding
	}
	if identity.Purpose != installation.Purpose || !subset(identity.Capabilities, installation.Capabilities) {
		return ErrCredentialScope
	}
	return nil
}

func (m *WorkloadIdentityManager) evidenceLocked(kind CredentialEventKind, identity ApplicationWorkloadIdentity, outcome, reason string) CredentialEvidence {
	now := m.now().UTC()
	evidence := CredentialEvidence{Kind: kind, CredentialID: identity.ID, InstallationID: identity.InstallationID, InstallationDigest: identity.InstallationDigest, ApplicationID: identity.ApplicationID, ApplicationDigest: identity.ApplicationDigest, Tenant: identity.Tenant, WorkloadIdentity: identity.WorkloadIdentity, Sender: identity.Sender, Destination: identity.Destination, Purpose: identity.Purpose, Rotation: identity.Rotation, At: now, Outcome: outcome, Reason: reason}
	evidence.EvidenceDigest = credentialEvidenceDigest(evidence)
	return evidence
}

func (m *WorkloadIdentityManager) deniedLocked(identity ApplicationWorkloadIdentity, reason string, err error) (CredentialEvidence, error) {
	evidence := m.evidenceLocked(CredentialChecked, identity, "denied", reason)
	m.events = append(m.events, evidence)
	return evidence, err
}

func credentialEvidenceDigest(e CredentialEvidence) string {
	value := fmt.Sprintf("partnerapp.credential.evidence/v1\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%s\x00%s\x00%s", e.Kind, e.CredentialID, e.InstallationID, e.InstallationDigest, e.ApplicationID, e.ApplicationDigest, e.Tenant, e.Rotation, e.At.UTC().Format(time.RFC3339Nano), e.Outcome, e.Reason)
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func sameIdentity(left, right ApplicationWorkloadIdentity) bool {
	if left.ID != right.ID || left.InstallationID != right.InstallationID || left.InstallationRevision != right.InstallationRevision || left.InstallationDigest != right.InstallationDigest || left.ApplicationID != right.ApplicationID || left.ApplicationVersion != right.ApplicationVersion || left.ApplicationRevision != right.ApplicationRevision || left.ApplicationDigest != right.ApplicationDigest || left.Tenant != right.Tenant || left.WorkloadIdentity != right.WorkloadIdentity || left.Sender != right.Sender || left.Destination != right.Destination || left.Purpose != right.Purpose || left.Rotation != right.Rotation || !left.IssuedAt.Equal(right.IssuedAt) || !left.ExpiresAt.Equal(right.ExpiresAt) || len(left.Capabilities) != len(right.Capabilities) {
		return false
	}
	for i := range left.Capabilities {
		if left.Capabilities[i] != right.Capabilities[i] {
			return false
		}
	}
	return true
}

func cloneIdentity(identity ApplicationWorkloadIdentity) ApplicationWorkloadIdentity {
	identity.Capabilities = append([]string(nil), identity.Capabilities...)
	return identity
}
