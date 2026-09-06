package bundle

import (
	"crypto"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/custody"
)

var (
	ErrInvalidHierarchy  = errors.New("bundle: invalid CA hierarchy")
	ErrUnknownAuthority  = errors.New("bundle: unknown authority")
	ErrIssuerUnavailable = errors.New("bundle: certificate issuer unavailable")
	ErrPathConstraint    = errors.New("bundle: declared path constraint refused certificate")
)

// AuthorityKind identifies a CA hierarchy generation.
type AuthorityKind string

const (
	RootAuthority         AuthorityKind = "root"
	IntermediateAuthority AuthorityKind = "intermediate"
	LeafAuthority         AuthorityKind = "leaf"
)

// PathConstraints are the constraints declared by an authority in addition
// to the constraints encoded in its x509 certificate. MaxPathLen < 0 means
// no additional declaration. PermittedDNSDomains, when non-empty, restrict
// leaf DNS names to those suffixes.
type PathConstraints struct {
	MaxPathLen          int
	PermittedDNSDomains []string
}

// Authority is an opaque custody reference paired with its public
// certificate. Private signing material never enters this package.
type Authority struct {
	ID          string
	Kind        AuthorityKind
	Handle      custody.Handle
	Certificate *x509.Certificate
	Constraints PathConstraints
}

// Leaf is a leaf certificate registered under an intermediate authority.
type Leaf struct {
	ID          string
	IssuerID    string
	Certificate *x509.Certificate
}

// CertificateRequest is passed unchanged to the certificate custody issuer.
// The issuer owns signing keys and returns only a DER certificate and receipt.
type CertificateRequest struct {
	ID          string
	Template    *x509.Certificate
	PublicKey   crypto.PublicKey
	Constraints PathConstraints
}

// IssuedCertificate is the non-secret result of custody-backed issuance.
type IssuedCertificate struct {
	Handle custody.Handle
	DER    []byte
}

// CertificateCustody combines TRUST-026's metadata-only certificate custody
// lifecycle with the narrow custody-backed issuance operation required by a
// CA hierarchy. Implementations retain all private signing material.
type CertificateCustody interface {
	custody.CertificateCustody
	IssueCertificate(ctx custody.Context, signer custody.Handle, request CertificateRequest) (IssuedCertificate, custody.Receipt, error)
}

type hierarchyState struct {
	root          Authority
	intermediates map[string]Authority
	leaves        map[string]Leaf
	bundles       []*Bundle
	revocations   *RevocationList
	nextVersion   int
}

// Hierarchy manages a root, versioned intermediate authorities, and leaves.
// Trust is evaluated through the existing overlap-aware Bundle Verifier, so
// the old and new intermediate are accepted only during their overlapping
// bundle windows.
type Hierarchy struct {
	issuer CertificateCustody
	mu     sync.RWMutex
	state  hierarchyState
}

// NewHierarchy creates a hierarchy with one validated root authority.
func NewHierarchy(root Authority, issuer CertificateCustody) (*Hierarchy, error) {
	if issuer == nil {
		return nil, fmt.Errorf("%w: certificate custody issuer is required", ErrInvalidHierarchy)
	}
	if err := validateAuthority(root, RootAuthority); err != nil {
		return nil, err
	}
	return &Hierarchy{issuer: issuer, state: hierarchyState{root: root, intermediates: make(map[string]Authority), leaves: make(map[string]Leaf), revocations: NewRevocationList(), nextVersion: 1}}, nil
}

// IssueRoot performs root issuance through certificate custody. The returned
// authority can be supplied to NewHierarchy.
func IssueRoot(ctx custody.Context, issuer CertificateCustody, request CertificateRequest, handle custody.Handle) (Authority, custody.Receipt, error) {
	if issuer == nil || handle.Kind != custody.Certificate {
		return Authority{}, custody.Receipt{}, fmt.Errorf("%w: invalid root issuer or certificate handle", ErrInvalidHierarchy)
	}
	issued, receipt, err := issuer.IssueCertificate(ctx, handle, request)
	if err != nil {
		return Authority{}, custody.Receipt{}, fmt.Errorf("%w: %v", ErrIssuerUnavailable, err)
	}
	cert, err := x509.ParseCertificate(issued.DER)
	if err != nil {
		return Authority{}, custody.Receipt{}, fmt.Errorf("%w: issued root is not a certificate", ErrInvalidHierarchy)
	}
	root := Authority{ID: request.ID, Kind: RootAuthority, Handle: issued.Handle, Certificate: cert, Constraints: request.Constraints}
	if root.Handle == (custody.Handle{}) {
		root.Handle = handle
	}
	if err := validateAuthority(root, RootAuthority); err != nil {
		return Authority{}, custody.Receipt{}, err
	}
	return root, receipt, nil
}

// IssueIntermediate issues and registers an intermediate signed by parentID.
func (h *Hierarchy) IssueIntermediate(ctx custody.Context, parentID string, request CertificateRequest) (Authority, custody.Receipt, error) {
	parent, err := h.authority(parentID)
	if err != nil {
		return Authority{}, custody.Receipt{}, err
	}
	issued, receipt, err := h.issue(ctx, parent.Handle, request)
	if err != nil {
		return Authority{}, custody.Receipt{}, err
	}
	cert, err := x509.ParseCertificate(issued.DER)
	if err != nil {
		return Authority{}, custody.Receipt{}, fmt.Errorf("%w: issued intermediate parse failed", ErrInvalidHierarchy)
	}
	intermediate := Authority{ID: request.ID, Kind: IntermediateAuthority, Handle: issued.Handle, Certificate: cert, Constraints: request.Constraints}
	if intermediate.Handle == (custody.Handle{}) {
		intermediate.Handle = parent.Handle
		intermediate.Handle.ID = request.ID
	}
	if err := h.AddIntermediate(ctx, parentID, intermediate); err != nil {
		return Authority{}, custody.Receipt{}, err
	}
	return intermediate, receipt, nil
}

// IssueLeaf issues and registers a leaf signed by intermediateID.
func (h *Hierarchy) IssueLeaf(ctx custody.Context, intermediateID string, request CertificateRequest) (Leaf, custody.Receipt, error) {
	intermediate, err := h.authority(intermediateID)
	if err != nil {
		return Leaf{}, custody.Receipt{}, err
	}
	issued, receipt, err := h.issue(ctx, intermediate.Handle, request)
	if err != nil {
		return Leaf{}, custody.Receipt{}, err
	}
	cert, err := x509.ParseCertificate(issued.DER)
	if err != nil {
		return Leaf{}, custody.Receipt{}, fmt.Errorf("%w: issued leaf parse failed", ErrInvalidHierarchy)
	}
	leaf := Leaf{ID: request.ID, IssuerID: intermediateID, Certificate: cert}
	if err := h.AddLeaf(ctx, leaf); err != nil {
		return Leaf{}, custody.Receipt{}, err
	}
	return leaf, receipt, nil
}

func (h *Hierarchy) issue(ctx custody.Context, signer custody.Handle, request CertificateRequest) (IssuedCertificate, custody.Receipt, error) {
	if err := ctx.Validate(); err != nil {
		return IssuedCertificate{}, custody.Receipt{}, err
	}
	if request.Template == nil || strings.TrimSpace(request.ID) == "" {
		return IssuedCertificate{}, custody.Receipt{}, fmt.Errorf("%w: request id and template are required", ErrInvalidHierarchy)
	}
	issued, receipt, err := h.issuer.IssueCertificate(ctx, signer, request)
	if err != nil {
		return IssuedCertificate{}, custody.Receipt{}, fmt.Errorf("%w: %v", ErrIssuerUnavailable, err)
	}
	return issued, receipt, nil
}

// AddIntermediate validates custody metadata, parent signature, CA status,
// and declared path constraints, then publishes a new trust bundle version.
func (h *Hierarchy) AddIntermediate(ctx custody.Context, parentID string, intermediate Authority) error {
	if err := h.validateCustody(ctx, intermediate.Handle); err != nil {
		return err
	}
	if err := validateAuthority(intermediate, IntermediateAuthority); err != nil {
		return err
	}
	parent, err := h.authority(parentID)
	if err != nil {
		return err
	}
	if err := intermediate.Certificate.CheckSignatureFrom(parent.Certificate); err != nil {
		return fmt.Errorf("%w: intermediate signature: %v", ErrInvalidHierarchy, err)
	}
	if parent.Constraints.MaxPathLen == 0 {
		return fmt.Errorf("%w: parent max path length is zero", ErrPathConstraint)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.state.intermediates[intermediate.ID]; exists {
		return fmt.Errorf("%w: duplicate intermediate %s", ErrInvalidHierarchy, intermediate.ID)
	}
	h.state.intermediates[intermediate.ID] = intermediate
	if err := h.publishBundleLocked(intermediate.Certificate.NotBefore, earliest(h.state.root.Certificate.NotAfter, intermediate.Certificate.NotAfter)); err != nil {
		delete(h.state.intermediates, intermediate.ID)
		return err
	}
	return nil
}

// AddLeaf validates and registers a leaf against its declared intermediate.
func (h *Hierarchy) AddLeaf(ctx custody.Context, leaf Leaf) error {
	if leaf.Certificate == nil || strings.TrimSpace(leaf.ID) == "" || strings.TrimSpace(leaf.IssuerID) == "" {
		return fmt.Errorf("%w: incomplete leaf", ErrInvalidHierarchy)
	}
	intermediate, err := h.authority(leaf.IssuerID)
	if err != nil {
		return err
	}
	if err := h.validateCustody(ctx, intermediate.Handle); err != nil {
		return err
	}
	if err := leaf.Certificate.CheckSignatureFrom(intermediate.Certificate); err != nil {
		return fmt.Errorf("%w: leaf signature: %v", ErrInvalidHierarchy, err)
	}
	if err := checkDNSConstraints(intermediate.Constraints, leaf.Certificate); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.state.leaves[leaf.ID]; exists {
		return fmt.Errorf("%w: duplicate leaf %s", ErrInvalidHierarchy, leaf.ID)
	}
	h.state.leaves[leaf.ID] = leaf
	return nil
}

// RotateIntermediate publishes a bundle with replacement while retaining the
// retiring bundle. Leaves under either intermediate validate in the overlap.
func (h *Hierarchy) RotateIntermediate(ctx custody.Context, oldID string, replacement Authority, overlapStart, expiresAt time.Time) error {
	if err := h.validateCustody(ctx, replacement.Handle); err != nil {
		return err
	}
	if err := validateAuthority(replacement, IntermediateAuthority); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	old, ok := h.state.intermediates[oldID]
	if !ok {
		return ErrUnknownAuthority
	}
	if err := replacement.Certificate.CheckSignatureFrom(h.state.root.Certificate); err != nil {
		return fmt.Errorf("%w: replacement signature: %v", ErrInvalidHierarchy, err)
	}
	if !overlapStart.Before(old.Certificate.NotAfter) {
		return ErrRotationNoOverlap
	}
	roots := []PinnedCert{mustPinCertificate(h.state.root.Certificate)}
	intermediates := []PinnedCert{mustPinCertificate(replacement.Certificate)}
	oldBundle := h.latestBundleLocked()
	if oldBundle == nil {
		return fmt.Errorf("%w: no retiring bundle", ErrInvalidHierarchy)
	}
	next, err := Rotate(oldBundle, roots, intermediates, overlapStart, expiresAt)
	if err != nil {
		return err
	}
	h.state.intermediates[replacement.ID] = replacement
	h.state.bundles = append(h.state.bundles, next)
	_ = old
	return nil
}

// RevokeIntermediate records a digest-based issuer revocation that survives
// bundle rollback and immediately affects Verify.
func (h *Hierarchy) RevokeIntermediate(id, reason string, at time.Time) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	intermediate, ok := h.state.intermediates[id]
	if !ok {
		return ErrUnknownAuthority
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: revocation reason required", ErrInvalidHierarchy)
	}
	h.state.revocations.Revoke(DigestOf(intermediate.Certificate.Raw), at)
	return nil
}

// Verify validates a leaf through active overlapping trust bundles and
// refuses chains crossing revoked or expired intermediates.
func (h *Hierarchy) Verify(leaf *x509.Certificate, extraIntermediates []*x509.Certificate, now time.Time, keyUsages []x509.ExtKeyUsage) ([]*x509.Certificate, error) {
	h.mu.RLock()
	bundles := append([]*Bundle(nil), h.state.bundles...)
	list := h.state.revocations
	constraints := make(map[Digest]PathConstraints, len(h.state.intermediates))
	for _, intermediate := range h.state.intermediates {
		constraints[DigestOf(intermediate.Certificate.Raw)] = intermediate.Constraints
	}
	h.mu.RUnlock()
	chain, err := NewVerifier(bundles...).WithRevocationList(list).Verify(leaf, extraIntermediates, now, keyUsages)
	if err != nil {
		return nil, err
	}
	for _, cert := range chain {
		if c, ok := constraints[DigestOf(cert.Raw)]; ok {
			if err := checkDNSConstraints(c, leaf); err != nil {
				return nil, err
			}
		}
	}
	return chain, nil
}

// Bundles returns a copy of all published versions, including the retiring
// version needed for overlap evidence.
func (h *Hierarchy) Bundles() []*Bundle {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]*Bundle(nil), h.state.bundles...)
}

func (h *Hierarchy) validateCustody(ctx custody.Context, handle custody.Handle) error {
	if err := ctx.Validate(); err != nil {
		return err
	}
	if err := handle.Validate(); err != nil {
		return err
	}
	if handle.Kind != custody.Certificate || handle.Tenant != ctx.Tenant || handle.Region != ctx.Region {
		return fmt.Errorf("%w: certificate handle is outside custody scope", ErrInvalidHierarchy)
	}
	if _, err := h.issuer.Get(ctx, handle); err != nil {
		return fmt.Errorf("%w: %v", ErrIssuerUnavailable, err)
	}
	return nil
}

func (h *Hierarchy) authority(id string) (Authority, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if id == h.state.root.ID {
		return h.state.root, nil
	}
	if a, ok := h.state.intermediates[id]; ok {
		return a, nil
	}
	return Authority{}, ErrUnknownAuthority
}

func (h *Hierarchy) publishBundleLocked(activates, expires time.Time) error {
	roots := []PinnedCert{mustPinCertificate(h.state.root.Certificate)}
	intermediates := make([]PinnedCert, 0, len(h.state.intermediates))
	for _, a := range h.state.intermediates {
		intermediates = append(intermediates, mustPinCertificate(a.Certificate))
	}
	b, err := New(h.state.nextVersion, "ca-hierarchy", roots, intermediates, activates.UTC(), expires.UTC())
	if err != nil {
		return err
	}
	h.state.nextVersion++
	h.state.bundles = append(h.state.bundles, b)
	return nil
}

func (h *Hierarchy) latestBundleLocked() *Bundle {
	if len(h.state.bundles) == 0 {
		return nil
	}
	return h.state.bundles[len(h.state.bundles)-1]
}

func validateAuthority(a Authority, kind AuthorityKind) error {
	if strings.TrimSpace(a.ID) == "" || a.Kind != kind || a.Certificate == nil || a.Handle.Kind != custody.Certificate {
		return fmt.Errorf("%w: incomplete %s authority", ErrInvalidHierarchy, kind)
	}
	if !a.Certificate.IsCA || !a.Certificate.BasicConstraintsValid {
		return fmt.Errorf("%w: %s is not a valid CA", ErrInvalidHierarchy, kind)
	}
	if a.Constraints.MaxPathLen < -1 {
		return fmt.Errorf("%w: negative max path length", ErrPathConstraint)
	}
	return nil
}

func checkDNSConstraints(constraints PathConstraints, leaf *x509.Certificate) error {
	if leaf == nil || len(constraints.PermittedDNSDomains) == 0 {
		return nil
	}
	for _, name := range leaf.DNSNames {
		allowed := false
		for _, suffix := range constraints.PermittedDNSDomains {
			if strings.HasSuffix(name, suffix) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("%w: leaf DNS name %q is outside permitted domains", ErrPathConstraint, name)
		}
	}
	return nil
}

func mustPinCertificate(cert *x509.Certificate) PinnedCert {
	return PinnedCert{Digest: DigestOf(cert.Raw), Cert: cert}
}

func earliest(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
