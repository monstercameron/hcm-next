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

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
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

type badCertificateIssuer struct {
	*certIssuer
	der []byte
}

func (i badCertificateIssuer) IssueCertificate(custody.Context, custody.Handle, CertificateRequest) (IssuedCertificate, custody.Receipt, error) {
	return IssuedCertificate{DER: i.der}, custody.Receipt{}, nil
}

func TestHierarchy_PublicAPIs_ValidationIssuanceRotationAndCopies(t *testing.T) {
	h, ctx, root, intermediate, leaf, issuer := hierarchyFixture(t)
	if _, err := NewHierarchy(root, nil); !errors.Is(err, ErrInvalidHierarchy) {
		t.Fatalf("nil issuer err=%v", err)
	}
	for name, mutate := range map[string]func(*Authority){
		"id":          func(a *Authority) { a.ID = "" },
		"kind":        func(a *Authority) { a.Kind = LeafAuthority },
		"certificate": func(a *Authority) { a.Certificate = nil },
		"handle":      func(a *Authority) { a.Handle.Kind = custody.Secret },
		"path":        func(a *Authority) { a.Constraints.MaxPathLen = -2 },
	} {
		t.Run("invalid root "+name, func(t *testing.T) {
			bad := root
			mutate(&bad)
			if _, err := NewHierarchy(bad, issuer); err == nil || !errors.Is(err, ErrInvalidHierarchy) && !errors.Is(err, ErrPathConstraint) {
				t.Fatalf("root=%+v err=%v", bad, err)
			}
		})
	}
	if _, _, err := IssueRoot(ctx, nil, CertificateRequest{}, root.Handle); !errors.Is(err, ErrInvalidHierarchy) {
		t.Fatalf("nil root issuer err=%v", err)
	}
	badHandle := root.Handle
	badHandle.Kind = custody.Secret
	if _, _, err := IssueRoot(ctx, issuer, CertificateRequest{}, badHandle); !errors.Is(err, ErrInvalidHierarchy) {
		t.Fatalf("wrong root handle err=%v", err)
	}
	rootKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	issuedRoot, receipt, err := IssueRoot(ctx, issuer, CertificateRequest{ID: "root-copy", Template: &x509.Certificate{Subject: pkix.Name{CommonName: "root-copy"}, NotBefore: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}, PublicKey: &rootKey.PublicKey, Constraints: PathConstraints{MaxPathLen: 1}}, root.Handle)
	if err != nil || receipt.ID == "" || issuedRoot.ID != "root-copy" || issuedRoot.Handle.ID != "root-copy" {
		t.Fatalf("IssueRoot authority=%+v receipt=%+v err=%v", issuedRoot, receipt, err)
	}
	if _, _, err := IssueRoot(ctx, badCertificateIssuer{certIssuer: issuer, der: []byte("bad")}, CertificateRequest{ID: "bad", Template: &x509.Certificate{}, PublicKey: rootKey.Public()}, root.Handle); !errors.Is(err, ErrInvalidHierarchy) {
		t.Fatalf("invalid issued root err=%v", err)
	}
	if _, _, err := h.IssueIntermediate(custody.Context{}, "root", CertificateRequest{}); err == nil {
		t.Fatal("invalid issuance context accepted")
	}
	if _, _, err := h.IssueIntermediate(ctx, "missing", CertificateRequest{}); !errors.Is(err, ErrUnknownAuthority) {
		t.Fatalf("unknown intermediate parent err=%v", err)
	}
	intKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	newIntermediate, _, err := h.IssueIntermediate(ctx, root.ID, CertificateRequest{ID: "int-issued", Template: &x509.Certificate{Subject: pkix.Name{CommonName: "int-issued"}, NotBefore: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC), IsCA: true, BasicConstraintsValid: true, MaxPathLen: 0, KeyUsage: x509.KeyUsageCertSign}, PublicKey: &intKey.PublicKey, Constraints: PathConstraints{MaxPathLen: 0, PermittedDNSDomains: []string{".example.com"}}})
	if err != nil || newIntermediate.ID != "int-issued" {
		t.Fatalf("IssueIntermediate=%+v err=%v", newIntermediate, err)
	}
	if _, _, err := h.IssueLeaf(ctx, root.ID, CertificateRequest{}); !errors.Is(err, ErrInvalidHierarchy) {
		t.Fatalf("root accepted as leaf issuer err=%v", err)
	}
	rootSigner, ok := issuer.keys[root.Handle].(*ecdsa.PrivateKey)
	if !ok {
		t.Fatal("fixture root signer is not an ecdsa key")
	}
	compatibleHandle := root.Handle
	compatibleHandle.ID = "int-compatible"
	compatibleTemplate := &x509.Certificate{SerialNumber: big.NewInt(100), Subject: pkix.Name{CommonName: "int-compatible"}, NotBefore: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC), IsCA: true, BasicConstraintsValid: true, MaxPathLen: 0, KeyUsage: x509.KeyUsageCertSign}
	compatibleDER, err := x509.CreateCertificate(rand.Reader, compatibleTemplate, root.Certificate, &rootSigner.PublicKey, rootSigner)
	if err != nil {
		t.Fatal(err)
	}
	compatibleCert, _ := x509.ParseCertificate(compatibleDER)
	issuer.keys[compatibleHandle] = rootSigner
	issuer.certs[compatibleHandle] = compatibleCert
	compatible := Authority{ID: compatibleHandle.ID, Kind: IntermediateAuthority, Handle: compatibleHandle, Certificate: compatibleCert, Constraints: PathConstraints{MaxPathLen: 0, PermittedDNSDomains: []string{".example.com"}}}
	if err := h.AddIntermediate(ctx, root.ID, compatible); err != nil {
		t.Fatalf("add compatible intermediate: %v", err)
	}
	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	newLeaf, _, err := h.IssueLeaf(ctx, compatible.ID, CertificateRequest{ID: "leaf-issued", Template: &x509.Certificate{Subject: pkix.Name{CommonName: "issued.example.com"}, DNSNames: []string{"issued.example.com"}, NotBefore: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), KeyUsage: x509.KeyUsageDigitalSignature}, PublicKey: &leafKey.PublicKey})
	if err != nil || newLeaf.IssuerID != compatible.ID {
		t.Fatalf("IssueLeaf=%+v err=%v", newLeaf, err)
	}
	if _, _, err := h.IssueLeaf(ctx, "missing", CertificateRequest{}); !errors.Is(err, ErrUnknownAuthority) {
		t.Fatalf("unknown leaf issuer err=%v", err)
	}
	if err := h.AddIntermediate(ctx, root.ID, newIntermediate); err == nil || !errors.Is(err, ErrInvalidHierarchy) {
		t.Fatalf("duplicate intermediate err=%v", err)
	}
	if err := h.AddLeaf(ctx, Leaf{ID: "", IssuerID: intermediate.ID, Certificate: leaf}); !errors.Is(err, ErrInvalidHierarchy) {
		t.Fatalf("incomplete leaf err=%v", err)
	}
	if err := h.AddLeaf(ctx, Leaf{ID: "bad-signature", IssuerID: intermediate.ID, Certificate: root.Certificate}); !errors.Is(err, ErrInvalidHierarchy) {
		t.Fatalf("bad leaf signature err=%v", err)
	}
	if err := h.AddLeaf(ctx, Leaf{ID: "unknown-issuer", IssuerID: "missing", Certificate: leaf}); !errors.Is(err, ErrUnknownAuthority) {
		t.Fatalf("unknown leaf authority err=%v", err)
	}
	if err := h.AddLeaf(ctx, Leaf{ID: "leaf-a", IssuerID: intermediate.ID, Certificate: leaf}); err != nil {
		t.Fatalf("first leaf-a registration err=%v", err)
	}
	if err := h.AddLeaf(ctx, Leaf{ID: "leaf-a", IssuerID: intermediate.ID, Certificate: leaf}); !errors.Is(err, ErrInvalidHierarchy) {
		t.Fatalf("duplicate leaf err=%v", err)
	}

	replacementKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	issued, _, err := issuer.IssueCertificate(ctx, root.Handle, CertificateRequest{ID: "int-replacement", Template: &x509.Certificate{Subject: pkix.Name{CommonName: "int-replacement"}, NotBefore: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}, PublicKey: &replacementKey.PublicKey})
	if err != nil {
		t.Fatal(err)
	}
	replacementCert, _ := x509.ParseCertificate(issued.DER)
	replacement := Authority{ID: "int-replacement", Kind: IntermediateAuthority, Handle: issued.Handle, Certificate: replacementCert}
	if err := h.RotateIntermediate(ctx, "missing", replacement, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, ErrUnknownAuthority) {
		t.Fatalf("unknown rotation source err=%v", err)
	}
	if err := h.RotateIntermediate(ctx, intermediate.ID, replacement, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("successful rotation err=%v", err)
	}
	if len(h.Bundles()) != 4 {
		t.Fatalf("bundle count=%d, want 4", len(h.Bundles()))
	}
	if got := h.Bundles(); len(got) == 2 {
		got[0] = nil
		if len(h.Bundles()) != 2 || h.Bundles()[0] == nil {
			t.Fatal("Bundles did not return a slice copy")
		}
	}
	if err := h.RevokeIntermediate("missing", "reason", time.Now()); !errors.Is(err, ErrUnknownAuthority) {
		t.Fatalf("unknown revoke err=%v", err)
	}
	if err := h.RevokeIntermediate(intermediate.ID, " ", time.Now()); !errors.Is(err, ErrInvalidHierarchy) {
		t.Fatalf("blank revoke reason err=%v", err)
	}
}
