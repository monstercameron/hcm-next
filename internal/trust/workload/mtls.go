package workload

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// MaxMTLSLifetime keeps a workload certificate inside the same short-lived
	// identity boundary as the signed workload credential.
	MaxMTLSLifetime = MaxIdentityLifetime
	spiffeScheme    = "spiffe"
	spiffeAuthority = "hcm-next"
)

var (
	ErrInvalidMTLSIssuer  = errors.New("workload: invalid mTLS issuer")
	ErrInvalidMTLSSpec    = errors.New("workload: invalid mTLS identity specification")
	ErrMTLSVerification   = errors.New("workload: mTLS certificate verification failed")
	ErrMTLSNameMismatch   = errors.New("workload: mTLS identity does not match the expected name")
	ErrMTLSRevoked        = errors.New("workload: mTLS certificate is revoked")
	ErrMTLSNoPeer         = errors.New("workload: mTLS connection has no peer certificate")
	ErrMTLSSharedIdentity = errors.New("workload: mTLS identity must name a unique workload instance")
)

// IdentitySpec identifies one workload instance. The instance component is
// mandatory: a service-wide certificate would be a shared identity and would
// make service attribution and revocation ambiguous.
type IdentitySpec struct {
	Tenant   string
	Cell     string
	Service  string
	Instance string
	Lifetime time.Duration
}

// MTLSIssuerConfig supplies the CA certificate and signer. The signer stays
// with the issuer and is used only to create a short-lived leaf certificate.
type MTLSIssuerConfig struct {
	Issuer      string
	Certificate *x509.Certificate
	Signer      crypto.Signer
	Now         func() time.Time
}

// MTLSIssuer issues client/server workload certificates with a structured
// SPIFFE URI SAN. No private key is serialized by this package.
type MTLSIssuer struct {
	issuer      string
	certificate *x509.Certificate
	signer      crypto.Signer
	now         func() time.Time
}

// IssuedIdentity is the in-memory result needed to configure a TLS client or
// server. Private key bytes never enter the returned structure; the key is a
// crypto.Signer held by tls.Certificate in the caller's process.
type IssuedIdentity struct {
	Certificate    *x509.Certificate
	TLSCertificate tls.Certificate
	Identity       MTLSIdentity
}

// MTLSIdentity is the verified identity extracted from a peer certificate.
type MTLSIdentity struct {
	tenant      string
	cell        string
	service     string
	instance    string
	issuer      string
	serial      string
	fingerprint string
	issuedAt    time.Time
	expiresAt   time.Time
	verified    bool
}

func (i MTLSIdentity) Tenant() string       { return i.tenant }
func (i MTLSIdentity) Cell() string         { return i.cell }
func (i MTLSIdentity) Service() string      { return i.service }
func (i MTLSIdentity) Instance() string     { return i.instance }
func (i MTLSIdentity) Issuer() string       { return i.issuer }
func (i MTLSIdentity) Serial() string       { return i.serial }
func (i MTLSIdentity) Fingerprint() string  { return i.fingerprint }
func (i MTLSIdentity) IssuedAt() time.Time  { return i.issuedAt }
func (i MTLSIdentity) ExpiresAt() time.Time { return i.expiresAt }
func (i MTLSIdentity) Verified() bool       { return i.verified }

// Subject returns the unique service instance name used by custody and
// service authorization contexts.
func (i MTLSIdentity) Subject() string { return i.service + "/" + i.instance }

// ValidAt reports whether the verified identity is live at at.
func (i MTLSIdentity) ValidAt(at time.Time) bool {
	return i.verified && !at.Before(i.issuedAt) && at.Before(i.expiresAt)
}

// AsWorkloadIdentity adapts a verified mTLS identity to the existing service
// authorization matrix when the certificate service is a declared process
// role. A non-process service remains valid for mTLS but is not upgraded to a
// process-role authorization decision.
func (i MTLSIdentity) AsWorkloadIdentity() Identity {
	role := ProcessRole(i.service)
	return Identity{issuer: i.issuer, subject: i.Subject(), role: role, cell: i.cell, keyID: i.serial, issuedAt: i.issuedAt, expiresAt: i.expiresAt, fingerprint: i.fingerprint}
}

// NewMTLSIssuer validates the CA inputs and returns an issuer.
func NewMTLSIssuer(cfg MTLSIssuerConfig) (*MTLSIssuer, error) {
	if strings.TrimSpace(cfg.Issuer) == "" || cfg.Certificate == nil || cfg.Signer == nil || !cfg.Certificate.IsCA {
		return nil, ErrInvalidMTLSIssuer
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &MTLSIssuer{issuer: cfg.Issuer, certificate: cfg.Certificate, signer: cfg.Signer, now: cfg.Now}, nil
}

// Issue creates a short-lived certificate with client and server EKUs so the
// same verified workload identity can be used at either side of a mTLS hop.
func (i *MTLSIssuer) Issue(spec IdentitySpec) (IssuedIdentity, error) {
	if i == nil || i.certificate == nil || i.signer == nil {
		return IssuedIdentity{}, ErrInvalidMTLSIssuer
	}
	if err := validateIdentitySpec(spec); err != nil {
		return IssuedIdentity{}, err
	}
	now := i.now().UTC().Truncate(time.Second)
	serial, err := randomSerial()
	if err != nil {
		return IssuedIdentity{}, err
	}
	key, err := newSigner()
	if err != nil {
		return IssuedIdentity{}, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "hcm-next-workload"},
		NotBefore:             now,
		NotAfter:              now.Add(spec.Lifetime),
		BasicConstraintsValid: true,
		DNSNames:              []string{spec.Service},
		URIs:                  []*url.URL{identityURI(spec)},
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, i.certificate, publicKey(key), i.signer)
	if err != nil {
		return IssuedIdentity{}, fmt.Errorf("%w: create certificate: %v", ErrInvalidMTLSIssuer, err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return IssuedIdentity{}, fmt.Errorf("%w: parse issued certificate: %v", ErrInvalidMTLSIssuer, err)
	}
	tlsCert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: cert}
	identity := identityFromCertificate(cert, i.issuer, true)
	return IssuedIdentity{Certificate: cert, TLSCertificate: tlsCert, Identity: identity}, nil
}

func validateIdentitySpec(spec IdentitySpec) error {
	for name, value := range map[string]string{"tenant": spec.Tenant, "cell": spec.Cell, "service": spec.Service, "instance": spec.Instance} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "/?#") {
			if name == "instance" {
				return fmt.Errorf("%w: %s: %w", ErrInvalidMTLSSpec, name, ErrMTLSSharedIdentity)
			}
			return fmt.Errorf("%w: %s is empty, padded, or contains a path separator", ErrInvalidMTLSSpec, name)
		}
	}
	if spec.Lifetime <= 0 || spec.Lifetime > MaxMTLSLifetime {
		return fmt.Errorf("%w: lifetime must be positive and at most %s", ErrInvalidMTLSSpec, MaxMTLSLifetime)
	}
	return nil
}

func identityURI(spec IdentitySpec) *url.URL {
	return &url.URL{Scheme: spiffeScheme, Host: spiffeAuthority, Path: "/workload/" + spec.Tenant + "/" + spec.Cell + "/" + spec.Service + "/" + spec.Instance}
}

func newSigner() (crypto.Signer, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	_ = public
	return private, err
}

func publicKey(key crypto.Signer) crypto.PublicKey { return key.Public() }

func randomSerial() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return nil, fmt.Errorf("workload: generate certificate serial: %w", err)
	}
	return serial, nil
}

// Revocations is a process-local monotonic certificate revocation set. A
// production adapter can populate it from the signed revocation stream.
type Revocations struct {
	mu           sync.RWMutex
	serials      map[string]struct{}
	fingerprints map[string]struct{}
}

func NewRevocations() *Revocations {
	return &Revocations{serials: make(map[string]struct{}), fingerprints: make(map[string]struct{})}
}

func (r *Revocations) Revoke(cert *x509.Certificate) error {
	if r == nil || cert == nil || cert.SerialNumber == nil {
		return ErrMTLSRevoked
	}
	sum := sha256.Sum256(cert.Raw)
	r.mu.Lock()
	r.serials[cert.SerialNumber.String()] = struct{}{}
	r.fingerprints[hex.EncodeToString(sum[:])] = struct{}{}
	r.mu.Unlock()
	return nil
}

func (r *Revocations) revoked(cert *x509.Certificate) bool {
	if r == nil || cert == nil || cert.SerialNumber == nil {
		return false
	}
	sum := sha256.Sum256(cert.Raw)
	r.mu.RLock()
	_, serial := r.serials[cert.SerialNumber.String()]
	_, fingerprint := r.fingerprints[hex.EncodeToString(sum[:])]
	r.mu.RUnlock()
	return serial || fingerprint
}

// MTLSVerifier validates the certificate chain, EKU, structured SAN, time
// window, expected tenant/cell/service, and revocation state.
type MTLSVerifier struct {
	roots           *x509.CertPool
	expectedTenant  string
	expectedCell    string
	expectedService string
	revocations     *Revocations
	now             func() time.Time
	clockSkew       time.Duration
}

type MTLSVerifierConfig struct {
	Roots           *x509.CertPool
	ExpectedTenant  string
	ExpectedCell    string
	ExpectedService string
	Revocations     *Revocations
	Now             func() time.Time
	ClockSkew       time.Duration
}

func NewMTLSVerifier(cfg MTLSVerifierConfig) (*MTLSVerifier, error) {
	if cfg.Roots == nil || strings.TrimSpace(cfg.ExpectedTenant) == "" || strings.TrimSpace(cfg.ExpectedCell) == "" || strings.TrimSpace(cfg.ExpectedService) == "" {
		return nil, fmt.Errorf("%w: roots and expected tenant, cell, and service are required", ErrMTLSVerification)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &MTLSVerifier{roots: cfg.Roots, expectedTenant: cfg.ExpectedTenant, expectedCell: cfg.ExpectedCell, expectedService: cfg.ExpectedService, revocations: cfg.Revocations, now: cfg.Now, clockSkew: cfg.ClockSkew}, nil
}

func (v *MTLSVerifier) Verify(cert *x509.Certificate) (MTLSIdentity, error) {
	if v == nil || cert == nil {
		return MTLSIdentity{}, ErrMTLSNoPeer
	}
	now := v.now().UTC()
	if v.revocations.revoked(cert) {
		return MTLSIdentity{}, ErrMTLSRevoked
	}
	if cert.NotAfter.Sub(cert.NotBefore) > MaxMTLSLifetime {
		return MTLSIdentity{}, fmt.Errorf("%w: certificate lifetime exceeds %s", ErrMTLSVerification, MaxMTLSLifetime)
	}
	if now.Before(cert.NotBefore.Add(-v.clockSkew)) || !now.Before(cert.NotAfter.Add(v.clockSkew)) {
		return MTLSIdentity{}, fmt.Errorf("%w: certificate is expired or not yet valid", ErrMTLSVerification)
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: v.roots, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return MTLSIdentity{}, fmt.Errorf("%w: chain: %v", ErrMTLSVerification, err)
	}
	identity, err := parseIdentity(cert)
	if err != nil {
		return MTLSIdentity{}, err
	}
	if identity.tenant != v.expectedTenant || identity.cell != v.expectedCell || identity.service != v.expectedService {
		return MTLSIdentity{}, fmt.Errorf("%w: got tenant=%s cell=%s service=%s", ErrMTLSNameMismatch, identity.tenant, identity.cell, identity.service)
	}
	identity.issuer = cert.Issuer.String()
	identity.verified = true
	return identity, nil
}

func (v *MTLSVerifier) VerifyConnection(state tls.ConnectionState) (MTLSIdentity, error) {
	if len(state.PeerCertificates) == 0 {
		return MTLSIdentity{}, ErrMTLSNoPeer
	}
	return v.Verify(state.PeerCertificates[0])
}

// TLSConfig returns a server-side mTLS configuration. The returned config
// requires a client certificate and repeats the semantic identity checks
// after the standard chain verification.
func (v *MTLSVerifier) TLSConfig(server tls.Certificate) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{server}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: v.roots, VerifyConnection: func(state tls.ConnectionState) error {
		_, err := v.VerifyConnection(state)
		return err
	}}
}

// ClientTLSConfig returns a client-side mTLS configuration for a workload
// certificate. ServerName is supplied by the caller because endpoint naming
// is a separate discovery and policy decision.
func (v *MTLSVerifier) ClientTLSConfig(client tls.Certificate, serverName string) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{client}, RootCAs: v.roots, ServerName: serverName, VerifyConnection: func(state tls.ConnectionState) error {
		_, err := v.VerifyConnection(state)
		return err
	}}
}

func parseIdentity(cert *x509.Certificate) (MTLSIdentity, error) {
	if len(cert.URIs) != 1 || cert.URIs[0].Scheme != spiffeScheme || cert.URIs[0].Host != spiffeAuthority {
		return MTLSIdentity{}, fmt.Errorf("%w: expected one %s://%s URI SAN", ErrMTLSVerification, spiffeScheme, spiffeAuthority)
	}
	parts := strings.Split(strings.TrimPrefix(cert.URIs[0].Path, "/"), "/")
	if len(parts) != 5 || parts[0] != "workload" || parts[1] == "" || parts[2] == "" || parts[3] == "" || parts[4] == "" {
		return MTLSIdentity{}, fmt.Errorf("%w: malformed workload URI SAN", ErrMTLSVerification)
	}
	sum := sha256.Sum256(cert.Raw)
	return MTLSIdentity{tenant: parts[1], cell: parts[2], service: parts[3], instance: parts[4], serial: cert.SerialNumber.String(), fingerprint: hex.EncodeToString(sum[:]), issuedAt: cert.NotBefore.UTC(), expiresAt: cert.NotAfter.UTC()}, nil
}

func identityFromCertificate(cert *x509.Certificate, issuer string, verified bool) MTLSIdentity {
	identity, _ := parseIdentity(cert)
	identity.issuer = issuer
	identity.verified = verified
	return identity
}
