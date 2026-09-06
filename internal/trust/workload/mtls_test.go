package workload

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"
)

func testMTLSAuthority(t *testing.T, now time.Time) (*x509.Certificate, ed25519.PrivateKey, *x509.CertPool) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-root"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, public, private)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(root)
	return root, private, pool
}

func testMTLSIssuerVerifier(t *testing.T, now time.Time) (*MTLSIssuer, *MTLSVerifier, *Revocations) {
	t.Helper()
	root, signer, roots := testMTLSAuthority(t, now)
	issuer, err := NewMTLSIssuer(MTLSIssuerConfig{Issuer: "test-root", Certificate: root, Signer: signer, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	revocations := NewRevocations()
	verifier, err := NewMTLSVerifier(MTLSVerifierConfig{Roots: roots, ExpectedTenant: "tenant-a", ExpectedCell: "cell-a", ExpectedService: "worker", Revocations: revocations, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return issuer, verifier, revocations
}

func TestTodo_AUTHN_006(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	issuer, verifier, _ := testMTLSIssuerVerifier(t, now)
	issued, err := issuer.Issue(IdentitySpec{Tenant: "tenant-a", Cell: "cell-a", Service: "worker", Instance: "worker-1", Lifetime: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	got, err := verifier.Verify(issued.Certificate)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Verified() || got.Tenant() != "tenant-a" || got.Cell() != "cell-a" || got.Service() != "worker" || got.Instance() != "worker-1" {
		t.Fatalf("unexpected verified identity: %+v", got)
	}
	if got.Fingerprint() == "" || got.Subject() != "worker/worker-1" {
		t.Fatalf("identity lacks stable attribution: %+v", got)
	}
}

func TestTodo_AUTHN_006_Integration(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	issuer, verifier, _ := testMTLSIssuerVerifier(t, now)
	issued, err := issuer.Issue(IdentitySpec{Tenant: "tenant-a", Cell: "cell-a", Service: "worker", Instance: "worker-1", Lifetime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	state := tlsConnectionState(issued.Certificate)
	identity, err := verifier.VerifyConnection(state)
	if err != nil || identity.Subject() != "worker/worker-1" {
		t.Fatalf("connection verification failed: identity=%+v err=%v", identity, err)
	}
	if got := verifier.TLSConfig(issued.TLSCertificate); got.MinVersion != tls.VersionTLS13 || got.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("TLS config is not mutual and modern: %+v", got)
	}
}

func TestTodo_AUTHN_006_Security(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	issuer, verifier, revocations := testMTLSIssuerVerifier(t, now)
	cases := []IdentitySpec{
		{Tenant: "tenant-b", Cell: "cell-a", Service: "worker", Instance: "worker-1", Lifetime: time.Minute},
		{Tenant: "tenant-a", Cell: "cell-b", Service: "worker", Instance: "worker-1", Lifetime: time.Minute},
		{Tenant: "tenant-a", Cell: "cell-a", Service: "projector", Instance: "projector-1", Lifetime: time.Minute},
	}
	for _, spec := range cases {
		issued, err := issuer.Issue(spec)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := verifier.Verify(issued.Certificate); !errors.Is(err, ErrMTLSNameMismatch) {
			t.Fatalf("mismatched identity accepted: spec=%+v err=%v", spec, err)
		}
	}
	issued, err := issuer.Issue(IdentitySpec{Tenant: "tenant-a", Cell: "cell-a", Service: "worker", Instance: "worker-2", Lifetime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if err := revocations.Revoke(issued.Certificate); err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(issued.Certificate); !errors.Is(err, ErrMTLSRevoked) {
		t.Fatalf("revoked certificate accepted: %v", err)
	}
	if _, err := issuer.Issue(IdentitySpec{Tenant: "tenant-a", Cell: "cell-a", Service: "worker", Lifetime: time.Minute}); !errors.Is(err, ErrMTLSSharedIdentity) {
		t.Fatalf("shared identity was not refused: %v", err)
	}
}

func TestTodo_AUTHN_006_Mutation(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	issuer, verifier, _ := testMTLSIssuerVerifier(t, now)
	issued, err := issuer.Issue(IdentitySpec{Tenant: "tenant-a", Cell: "cell-a", Service: "worker", Instance: "worker-1", Lifetime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	mutated := *issued.Certificate
	mutated.URIs = nil
	if _, err := verifier.Verify(&mutated); !errors.Is(err, ErrMTLSVerification) {
		t.Fatalf("mutated SAN was accepted: %v", err)
	}
	if _, err := issuer.Issue(IdentitySpec{Tenant: "tenant-a", Cell: "cell-a", Service: "worker", Instance: "worker-1", Lifetime: MaxMTLSLifetime + time.Nanosecond}); !errors.Is(err, ErrInvalidMTLSSpec) {
		t.Fatalf("long lifetime was accepted: %v", err)
	}
}

func FuzzTodo_AUTHN_006(f *testing.F) {
	f.Add("tenant-a", "cell-a", "worker", "worker-1", int64(time.Minute))
	f.Fuzz(func(t *testing.T, tenant, cell, service, instance string, lifetimeNanos int64) {
		err := validateIdentitySpec(IdentitySpec{Tenant: tenant, Cell: cell, Service: service, Instance: instance, Lifetime: time.Duration(lifetimeNanos)})
		if err == nil && (strings.ContainsAny(tenant, "/?#") || strings.ContainsAny(cell, "/?#") || strings.ContainsAny(service, "/?#") || strings.ContainsAny(instance, "/?#")) {
			t.Fatal("path separator passed identity validation")
		}
	})
}

func tlsConnectionState(cert *x509.Certificate) tls.ConnectionState {
	return tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{cert}}
}
