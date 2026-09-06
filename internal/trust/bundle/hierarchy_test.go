package bundle

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/custody"
)

type certIssuer struct {
	keys   map[custody.Handle]crypto.Signer
	certs  map[custody.Handle]*x509.Certificate
	serial int64
}

func newCertIssuer() *certIssuer {
	return &certIssuer{keys: make(map[custody.Handle]crypto.Signer), certs: make(map[custody.Handle]*x509.Certificate), serial: 1}
}
func (i *certIssuer) Get(_ custody.Context, h custody.Handle) (custody.Lifecycle, error) {
	if _, ok := i.keys[h]; !ok {
		return custody.Lifecycle{}, custody.ErrObjectNotFound
	}
	return custody.Lifecycle{Handle: h, Status: custody.StatusActive}, nil
}
func (i *certIssuer) Rotate(custody.Context, custody.Handle) (custody.Lifecycle, custody.Receipt, error) {
	return custody.Lifecycle{}, custody.Receipt{}, errors.New("unused")
}
func (i *certIssuer) Revoke(custody.Context, custody.Handle, string) (custody.Receipt, error) {
	return custody.Receipt{}, errors.New("unused")
}
func (i *certIssuer) Attest(_ custody.Context, h custody.Handle) (custody.Attestation, error) {
	if _, ok := i.keys[h]; !ok {
		return custody.Attestation{}, custody.ErrObjectNotFound
	}
	return custody.Attestation{Handle: h, Digest: "attested"}, nil
}
func (i *certIssuer) IssueCertificate(_ custody.Context, signer custody.Handle, request CertificateRequest) (IssuedCertificate, custody.Receipt, error) {
	key, ok := i.keys[signer]
	if !ok {
		return IssuedCertificate{}, custody.Receipt{}, custody.ErrObjectNotFound
	}
	parent := i.certs[signer]
	template := *request.Template
	template.SerialNumber = big.NewInt(i.serial)
	i.serial++
	der, err := x509.CreateCertificate(rand.Reader, &template, parent, request.PublicKey, key)
	if err != nil {
		return IssuedCertificate{}, custody.Receipt{}, err
	}
	h := signer
	h.ID = request.ID
	h.Version = "v1"
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return IssuedCertificate{}, custody.Receipt{}, err
	}
	i.keys[h] = key
	i.certs[h] = cert
	return IssuedCertificate{Handle: h, DER: der}, custody.Receipt{ID: "issue", Handle: h, Operation: custody.Sign}, nil
}

func hierarchyFixture(t *testing.T) (*Hierarchy, custody.Context, Authority, Authority, *x509.Certificate, *certIssuer) {
	t.Helper()
	issuer := newCertIssuer()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	later := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx := custody.Context{RequestContext: custody.RequestContext{Workload: "ca-worker", Tenant: "tenant-a", Region: "us-east", Purpose: "certificate-issuance", Destination: "ca"}}
	rootKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rootHandle := custody.Handle{ID: "root", Kind: custody.Certificate, Version: "v1", Tenant: "tenant-a", Region: "us-east"}
	rootTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "root"}, NotBefore: now, NotAfter: later, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	rootCert, _ := x509.ParseCertificate(rootDER)
	issuer.keys[rootHandle] = rootKey
	issuer.certs[rootHandle] = rootCert
	root := Authority{ID: "root", Kind: RootAuthority, Handle: rootHandle, Certificate: rootCert, Constraints: PathConstraints{MaxPathLen: 2, PermittedDNSDomains: []string{".example.com"}}}
	h, err := NewHierarchy(root, issuer)
	if err != nil {
		t.Fatal(err)
	}
	intKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	intTemplate := &x509.Certificate{Subject: pkix.Name{CommonName: "int-a"}, NotBefore: now, NotAfter: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), IsCA: true, BasicConstraintsValid: true, MaxPathLen: 0, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	issued, _, err := issuer.IssueCertificate(ctx, rootHandle, CertificateRequest{ID: "int-a", Template: intTemplate, PublicKey: &intKey.PublicKey, Constraints: PathConstraints{MaxPathLen: 0, PermittedDNSDomains: []string{".example.com"}}})
	if err != nil {
		t.Fatal(err)
	}
	intCert, _ := x509.ParseCertificate(issued.DER)
	intermediate := Authority{ID: "int-a", Kind: IntermediateAuthority, Handle: issued.Handle, Certificate: intCert, Constraints: PathConstraints{MaxPathLen: 0, PermittedDNSDomains: []string{".example.com"}}}
	if err := h.AddIntermediate(ctx, "root", intermediate); err != nil {
		t.Fatal(err)
	}
	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafTemplate := &x509.Certificate{Subject: pkix.Name{CommonName: "leaf.example.com"}, DNSNames: []string{"leaf.example.com"}, NotBefore: now, NotAfter: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, intCert, &leafKey.PublicKey, intKey)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(leafDER)
	return h, ctx, root, intermediate, leaf, issuer
}

func TestTodo_TRUST_030(t *testing.T) {
	h, ctx, _, intermediate, leaf, _ := hierarchyFixture(t)
	chain, err := h.Verify(leaf, nil, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	if err != nil || len(chain) != 3 {
		t.Fatalf("Verify = %v, chain length %d", err, len(chain))
	}
	if err := h.AddLeaf(ctx, Leaf{ID: "leaf-a", IssuerID: intermediate.ID, Certificate: leaf}); err != nil {
		t.Fatal(err)
	}
	if err := h.RevokeIntermediate(intermediate.ID, "compromised", time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Verify(leaf, nil, time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC), nil); !errors.Is(err, ErrChainRevoked) {
		t.Fatalf("revoked intermediate Verify = %v", err)
	}
}

func TestTodo_TRUST_030_Security(t *testing.T) {
	h, ctx, _, intermediate, leaf, _ := hierarchyFixture(t)
	bad := *leaf
	bad.DNSNames = []string{"attacker.invalid"}
	if _, err := h.Verify(&bad, nil, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), nil); err == nil {
		t.Fatal("wrong SAN accepted")
	}
	if err := h.RevokeIntermediate(intermediate.ID, "emergency", time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Verify(leaf, nil, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), nil); !errors.Is(err, ErrChainRevoked) {
		t.Fatalf("revoked intermediate accepted: %v", err)
	}
	_ = ctx
}

func TestTodo_TRUST_030_Mutation(t *testing.T) {
	h, ctx, root, intermediate, _, issuer := hierarchyFixture(t)
	newKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	newHandle := custody.Handle{ID: "int-b", Kind: custody.Certificate, Version: "v1", Tenant: "tenant-a", Region: "us-east"}
	issued, _, err := issuer.IssueCertificate(ctx, root.Handle, CertificateRequest{ID: "int-b", Template: &x509.Certificate{Subject: pkix.Name{CommonName: "int-b"}, NotBefore: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}, PublicKey: &newKey.PublicKey})
	if err != nil {
		t.Fatal(err)
	}
	newCert, _ := x509.ParseCertificate(issued.DER)
	newAuthority := Authority{ID: "int-b", Kind: IntermediateAuthority, Handle: newHandle, Certificate: newCert}
	if err := h.RotateIntermediate(ctx, intermediate.ID, newAuthority, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, ErrRotationNoOverlap) {
		t.Fatalf("non-overlap rotation = %v", err)
	}
}

func FuzzTodo_TRUST_030(f *testing.F) {
	f.Add(int64(0))
	f.Add(int64(90 * 24 * time.Hour))
	f.Fuzz(func(t *testing.T, offset int64) {
		h, _, _, _, leaf, _ := hierarchyFixture(t)
		now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(offset % int64(365*24*time.Hour)))
		_, _ = h.Verify(leaf, nil, now, nil)
	})
}
