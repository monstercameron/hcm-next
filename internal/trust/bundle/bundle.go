// Package bundle implements the trust-bundle lifecycle: versioned sets of
// pinned root and intermediate certificate authorities, each with its own
// activation window and expiry, rotated with an overlap period so a new
// bundle version is trusted before the retiring one stops being trusted, and
// a [Verifier] that only ever validates a peer certificate against a bundle
// version that is both pinned (by digest, not by subject name) and active at
// the instant of verification.
//
// This package uses only the standard library's crypto/x509: no new
// dependency is introduced, and nothing here branches on a specific key
// algorithm -- a [PinnedCert] is opaque past its parsed *x509.Certificate,
// so migrating a certificate authority from one algorithm to another changes
// no API here.
package bundle

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Digest is a hex-encoded SHA-256 digest over one certificate's raw DER
// bytes. A bundle trusts a certificate by this digest, never by subject
// name or issuer alone: a same-subject certificate signed by a different
// key has a different digest and is not the pinned certificate.
type Digest string

// DigestOf returns the pin digest for a certificate's raw DER bytes.
func DigestOf(der []byte) Digest {
	sum := sha256.Sum256(der)
	return Digest(hex.EncodeToString(sum[:]))
}

// PinnedCert is one root or intermediate certificate pinned into a bundle.
type PinnedCert struct {
	Digest Digest
	Cert   *x509.Certificate
}

// NewPinnedCert parses der and pins it by its own digest, so the resulting
// [PinnedCert] can never be constructed with a digest that does not match
// the certificate it carries.
func NewPinnedCert(der []byte) (PinnedCert, error) {
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return PinnedCert{}, fmt.Errorf("%w: parse certificate: %v", ErrInvalidBundle, err)
	}
	return PinnedCert{Digest: DigestOf(cert.Raw), Cert: cert}, nil
}

// Errors returned while building, rotating, or verifying against a trust
// bundle. All are matchable with errors.Is.
var (
	ErrInvalidBundle     = errors.New("bundle: invalid trust bundle")
	ErrDigestMismatch    = errors.New("bundle: pinned digest does not match the certificate's own digest")
	ErrRotationNoOverlap = errors.New("bundle: rotation window does not overlap the retiring bundle's validity")
	ErrNoActiveBundle    = errors.New("bundle: no active, pinned trust bundle is available at this instant")
	ErrChainInvalid      = errors.New("bundle: certificate chain does not verify against any active bundle")
	ErrChainRevoked      = errors.New("bundle: certificate chain includes a revoked issuer")
)

// Status is the closed set of lifecycle states a [Bundle] can be in at a
// given instant, computed from its own activation window and revocation
// state -- never stored, always derived, so it can never drift from the
// underlying facts.
type Status string

// Bundle lifecycle states.
const (
	StatusDraft   Status = "DRAFT"
	StatusActive  Status = "ACTIVE"
	StatusExpired Status = "EXPIRED"
	StatusRevoked Status = "REVOKED"
)

// Bundle is one version of a trust bundle: a pinned set of roots and
// intermediates with an activation window. Every field describing the pin
// set and window is immutable after [New] returns; only revocation state
// changes over the bundle's life, and revocation is permanent.
type Bundle struct {
	Version       int
	Purpose       string
	Roots         []PinnedCert
	Intermediates []PinnedCert
	ActivatesAt   time.Time
	ExpiresAt     time.Time

	revokedAt     *time.Time
	revokedReason string
}

// New validates and freezes a trust bundle version. It refuses: a version
// below 1, an empty purpose, no pinned roots, an activation window that does
// not strictly precede expiry, any pin whose digest does not match its own
// certificate, and any root or intermediate that is not itself a CA
// certificate.
func New(version int, purpose string, roots, intermediates []PinnedCert, activatesAt, expiresAt time.Time) (*Bundle, error) {
	if version < 1 {
		return nil, fmt.Errorf("%w: version must be >= 1, got %d", ErrInvalidBundle, version)
	}
	if strings.TrimSpace(purpose) == "" {
		return nil, fmt.Errorf("%w: purpose is required", ErrInvalidBundle)
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("%w: at least one pinned root is required", ErrInvalidBundle)
	}
	if !activatesAt.Before(expiresAt) {
		return nil, fmt.Errorf("%w: activation window must strictly precede expiry", ErrInvalidBundle)
	}
	for _, r := range roots {
		if err := validatePin(r, true); err != nil {
			return nil, err
		}
	}
	for _, i := range intermediates {
		if err := validatePin(i, false); err != nil {
			return nil, err
		}
	}
	return &Bundle{
		Version:       version,
		Purpose:       purpose,
		Roots:         append([]PinnedCert(nil), roots...),
		Intermediates: append([]PinnedCert(nil), intermediates...),
		ActivatesAt:   activatesAt.UTC(),
		ExpiresAt:     expiresAt.UTC(),
	}, nil
}

func validatePin(p PinnedCert, requireCA bool) error {
	if p.Cert == nil {
		return fmt.Errorf("%w: pinned entry has no parsed certificate", ErrInvalidBundle)
	}
	if DigestOf(p.Cert.Raw) != p.Digest {
		return ErrDigestMismatch
	}
	if requireCA && !(p.Cert.IsCA && p.Cert.BasicConstraintsValid) {
		return fmt.Errorf("%w: pinned root is not a valid CA certificate", ErrInvalidBundle)
	}
	if !requireCA && !(p.Cert.IsCA && p.Cert.BasicConstraintsValid) {
		return fmt.Errorf("%w: pinned intermediate is not a valid CA certificate", ErrInvalidBundle)
	}
	return nil
}

// valid re-checks every pin in the bundle against its own recorded digest.
// This is defense in depth beyond [New]: a [Bundle] built by struct literal
// rather than [New] -- for example, one reconstructed from storage -- is
// still refused by [Verifier] if any pin has been tampered with or is
// otherwise inconsistent.
func (b *Bundle) valid() bool {
	if b == nil || len(b.Roots) == 0 || !b.ActivatesAt.Before(b.ExpiresAt) {
		return false
	}
	for _, r := range b.Roots {
		if validatePin(r, true) != nil {
			return false
		}
	}
	for _, i := range b.Intermediates {
		if validatePin(i, false) != nil {
			return false
		}
	}
	return true
}

// StatusAt derives the bundle's lifecycle state at now. Revocation is
// checked first and is permanent: a revoked bundle is never reported as
// active again, even if now precedes the revocation instant is impossible
// by construction (revocation cannot be backdated past now at the time it
// was recorded), and even if the bundle is later "rolled back" into use by a
// caller that still holds a reference to it.
func (b *Bundle) StatusAt(now time.Time) Status {
	if b.revokedAt != nil {
		return StatusRevoked
	}
	if !now.Before(b.ExpiresAt) {
		return StatusExpired
	}
	if now.Before(b.ActivatesAt) {
		return StatusDraft
	}
	return StatusActive
}

// Revoke ends the bundle's trust immediately and permanently. A revoked
// bundle can never be un-revoked; rolling back to an older bundle object
// that has already been revoked does not restore its trust.
func (b *Bundle) Revoke(reason string, now time.Time) error {
	if b.revokedAt != nil {
		return fmt.Errorf("%w: bundle version %d already revoked", ErrInvalidBundle, b.Version)
	}
	t := now.UTC()
	b.revokedAt = &t
	b.revokedReason = reason
	return nil
}

// Revocation reports the revocation instant and reason, and whether the
// bundle has been revoked at all.
func (b *Bundle) Revocation() (at time.Time, reason string, revoked bool) {
	if b.revokedAt == nil {
		return time.Time{}, "", false
	}
	return *b.revokedAt, b.revokedReason, true
}

// RootPool returns an *x509.CertPool containing every pinned root.
func (b *Bundle) RootPool() *x509.CertPool {
	pool := x509.NewCertPool()
	for _, r := range b.Roots {
		pool.AddCert(r.Cert)
	}
	return pool
}

// IntermediatePool returns an *x509.CertPool containing every pinned
// intermediate.
func (b *Bundle) IntermediatePool() *x509.CertPool {
	pool := x509.NewCertPool()
	for _, i := range b.Intermediates {
		pool.AddCert(i.Cert)
	}
	return pool
}

// Rotate produces the next bundle version, retiring old. The new version's
// ActivatesAt must strictly precede old's ExpiresAt: this is what an overlap
// window is -- there is always an instant at which both the retiring and the
// incoming bundle version are simultaneously [StatusActive], so in-flight
// verification traffic never sees a gap with zero active bundle.
func Rotate(old *Bundle, roots, intermediates []PinnedCert, overlapStart, newExpiresAt time.Time) (*Bundle, error) {
	if old == nil {
		return nil, fmt.Errorf("%w: no bundle to rotate from", ErrInvalidBundle)
	}
	next, err := New(old.Version+1, old.Purpose, roots, intermediates, overlapStart, newExpiresAt)
	if err != nil {
		return nil, err
	}
	if !overlapStart.Before(old.ExpiresAt) {
		return nil, ErrRotationNoOverlap
	}
	return next, nil
}

// RevocationList is an independent, monotonic record of compromised issuer
// digests. It exists separately from any single [Bundle] version precisely
// so that a rollback to an older bundle -- one built before an issuer was
// known to be compromised -- cannot reintroduce trust in that issuer: the
// list is checked by [Verifier] against every resolved chain regardless of
// which bundle version produced it.
type RevocationList struct {
	revoked map[Digest]time.Time
}

// NewRevocationList returns an empty revocation list.
func NewRevocationList() *RevocationList {
	return &RevocationList{revoked: make(map[Digest]time.Time)}
}

// Revoke marks d as compromised as of at. Revoking an already-revoked digest
// keeps the earlier revocation instant: revocation cannot be pushed later.
func (l *RevocationList) Revoke(d Digest, at time.Time) {
	at = at.UTC()
	if existing, ok := l.revoked[d]; ok && existing.Before(at) {
		return
	}
	l.revoked[d] = at
}

// IsRevoked reports whether d was revoked at or before at.
func (l *RevocationList) IsRevoked(d Digest, at time.Time) bool {
	t, ok := l.revoked[d]
	return ok && !at.Before(t)
}

// Verifier verifies a leaf certificate against a set of trust bundle
// versions and an optional [RevocationList]. It refuses to trust any bundle
// whose pins do not withstand re-validation and any bundle that is not
// [StatusActive] at the instant of verification -- an expired, not-yet-
// activated, or revoked bundle version is never used, even if a caller
// passes it in explicitly.
type Verifier struct {
	bundles     []*Bundle
	revocations *RevocationList
}

// NewVerifier builds a verifier over the given bundle versions. Passing more
// than one version is how a rotation overlap window is expressed: both the
// retiring and incoming versions are considered, and whichever is
// [StatusActive] at verification time is used.
func NewVerifier(bundles ...*Bundle) *Verifier {
	return &Verifier{bundles: append([]*Bundle(nil), bundles...)}
}

// WithRevocationList attaches an independent issuer revocation list and
// returns the receiver for chaining.
func (v *Verifier) WithRevocationList(l *RevocationList) *Verifier {
	v.revocations = l
	return v
}

// Verify checks leaf against every active, pinned bundle version this
// verifier holds, in order, and returns the first chain that both verifies
// under stdlib x509 rules and contains no issuer present on the attached
// revocation list. It returns [ErrNoActiveBundle] when no bundle is active
// at now at all, and [ErrChainInvalid] when at least one bundle is active
// but none validates leaf.
func (v *Verifier) Verify(leaf *x509.Certificate, extraIntermediates []*x509.Certificate, now time.Time, keyUsages []x509.ExtKeyUsage) ([]*x509.Certificate, error) {
	if leaf == nil {
		return nil, fmt.Errorf("%w: no leaf certificate presented", ErrChainInvalid)
	}
	activeFound := false
	var lastErr error
	for _, b := range v.bundles {
		if b == nil || !b.valid() || b.StatusAt(now) != StatusActive {
			continue
		}
		activeFound = true

		pool := x509.NewCertPool()
		for _, ic := range extraIntermediates {
			pool.AddCert(ic)
		}
		for _, ic := range b.Intermediates {
			pool.AddCert(ic.Cert)
		}
		chains, err := leaf.Verify(x509.VerifyOptions{
			Roots:         b.RootPool(),
			Intermediates: pool,
			CurrentTime:   now,
			KeyUsages:     keyUsages,
		})
		if err != nil || len(chains) == 0 {
			if err != nil {
				lastErr = err
			}
			continue
		}

		chain := chains[0]
		if v.revocations != nil && v.chainRevoked(chain, now) {
			lastErr = ErrChainRevoked
			continue
		}
		return chain, nil
	}

	if !activeFound {
		return nil, ErrNoActiveBundle
	}
	if lastErr == nil {
		lastErr = ErrChainInvalid
	}
	if errors.Is(lastErr, ErrChainRevoked) {
		return nil, ErrChainRevoked
	}
	return nil, fmt.Errorf("%w: %v", ErrChainInvalid, lastErr)
}

func (v *Verifier) chainRevoked(chain []*x509.Certificate, now time.Time) bool {
	for _, c := range chain {
		if v.revocations.IsRevoked(DigestOf(c.Raw), now) {
			return true
		}
	}
	return false
}
