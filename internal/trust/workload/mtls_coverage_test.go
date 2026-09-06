package workload

import (
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"math/big"
	"net/url"
	"testing"
	"time"
)

func TestMTLS_PublicIdentitySurfaceAndTLSConfigs(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	issuer, verifier, _ := testMTLSIssuerVerifier(t, now)
	issued, err := issuer.Issue(IdentitySpec{Tenant: "tenant-a", Cell: "cell-a", Service: "worker", Instance: "worker-1", Lifetime: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	id := issued.Identity
	if id.Tenant() != "tenant-a" || id.Cell() != "cell-a" || id.Service() != "worker" || id.Instance() != "worker-1" || id.Issuer() != "test-root" || id.Serial() == "" || id.Fingerprint() == "" {
		t.Fatalf("issued identity accessors = %+v", id)
	}
	if !id.IssuedAt().Equal(issued.Certificate.NotBefore.UTC()) || !id.ExpiresAt().Equal(issued.Certificate.NotAfter.UTC()) || !id.Verified() || id.Subject() != "worker/worker-1" {
		t.Fatalf("issued identity time/status/subject incorrect: %+v", id)
	}
	if !id.ValidAt(now) || id.ValidAt(id.IssuedAt().Add(-time.Nanosecond)) || id.ValidAt(id.ExpiresAt()) {
		t.Fatal("mTLS identity validity boundaries are not [issued, expires)")
	}
	workloadID := id.AsWorkloadIdentity()
	if workloadID.Role() != RoleWorker || workloadID.Subject() != id.Subject() || workloadID.KeyID() != id.Serial() || !workloadID.ValidAt(now) {
		t.Fatalf("AsWorkloadIdentity lost verified identity: %+v", workloadID)
	}
	serverConfig := verifier.TLSConfig(issued.TLSCertificate)
	if serverConfig.MinVersion != tls.VersionTLS13 || serverConfig.ClientAuth != tls.RequireAndVerifyClientCert || len(serverConfig.Certificates) != 1 || serverConfig.ClientCAs == nil {
		t.Fatalf("TLSConfig = %+v", serverConfig)
	}
	clientConfig := verifier.ClientTLSConfig(issued.TLSCertificate, "worker.internal")
	if clientConfig.MinVersion != tls.VersionTLS13 || clientConfig.ServerName != "worker.internal" || len(clientConfig.Certificates) != 1 || clientConfig.RootCAs == nil {
		t.Fatalf("ClientTLSConfig = %+v", clientConfig)
	}
	if err := clientConfig.VerifyConnection(tlsConnectionState(issued.Certificate)); err != nil {
		t.Fatalf("ClientTLSConfig VerifyConnection = %v", err)
	}
}

func TestMTLS_ConstructorAndSpecValidation(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	root, signer, roots := testMTLSAuthority(t, now)
	badRoot := *root
	badRoot.IsCA = false
	cases := []struct {
		name string
		cfg  MTLSIssuerConfig
	}{
		{"blank issuer", MTLSIssuerConfig{Certificate: root, Signer: signer}},
		{"missing certificate", MTLSIssuerConfig{Issuer: "root", Signer: signer}},
		{"missing signer", MTLSIssuerConfig{Issuer: "root", Certificate: root}},
		{"non-ca certificate", MTLSIssuerConfig{Issuer: "root", Certificate: &badRoot, Signer: signer}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewMTLSIssuer(tc.cfg); !errors.Is(err, ErrInvalidMTLSIssuer) {
				t.Fatalf("NewMTLSIssuer = %v, want ErrInvalidMTLSIssuer", err)
			}
		})
	}
	issuer, err := NewMTLSIssuer(MTLSIssuerConfig{Issuer: "root", Certificate: root, Signer: signer})
	if err != nil {
		t.Fatal(err)
	}
	var nilIssuer *MTLSIssuer
	if _, err := nilIssuer.Issue(IdentitySpec{}); !errors.Is(err, ErrInvalidMTLSIssuer) {
		t.Fatalf("nil issuer Issue = %v", err)
	}
	invalidSpecs := []IdentitySpec{
		{Cell: "cell", Service: "worker", Instance: "one", Lifetime: time.Minute},
		{Tenant: " tenant", Cell: "cell", Service: "worker", Instance: "one", Lifetime: time.Minute},
		{Tenant: "tenant", Cell: "cell/x", Service: "worker", Instance: "one", Lifetime: time.Minute},
		{Tenant: "tenant", Cell: "cell", Service: "worker?", Instance: "one", Lifetime: time.Minute},
		{Tenant: "tenant", Cell: "cell", Service: "worker", Instance: "one/two", Lifetime: time.Minute},
		{Tenant: "tenant", Cell: "cell", Service: "worker", Instance: "one", Lifetime: 0},
		{Tenant: "tenant", Cell: "cell", Service: "worker", Instance: "one", Lifetime: MaxMTLSLifetime + time.Nanosecond},
	}
	for i, spec := range invalidSpecs {
		if _, err := issuer.Issue(spec); !errors.Is(err, ErrInvalidMTLSSpec) {
			t.Errorf("invalid spec %d returned %v", i, err)
		}
	}
	verifierCases := []MTLSVerifierConfig{
		{}, {Roots: roots}, {Roots: roots, ExpectedTenant: "tenant"}, {Roots: roots, ExpectedTenant: "tenant", ExpectedCell: "cell"},
	}
	for i, cfg := range verifierCases {
		if _, err := NewMTLSVerifier(cfg); !errors.Is(err, ErrMTLSVerification) {
			t.Errorf("invalid verifier config %d = %v", i, err)
		}
	}
	if _, err := NewMTLSVerifier(MTLSVerifierConfig{Roots: roots, ExpectedTenant: "tenant", ExpectedCell: "cell", ExpectedService: "worker"}); err != nil {
		t.Fatalf("valid verifier config = %v", err)
	}
}

func TestMTLS_VerifyFailureBranchesAndRevocations(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	issuer, verifier, revocations := testMTLSIssuerVerifier(t, now)
	issued, err := issuer.Issue(IdentitySpec{Tenant: "tenant-a", Cell: "cell-a", Service: "worker", Instance: "worker-1", Lifetime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	var nilVerifier *MTLSVerifier
	if _, err := nilVerifier.Verify(issued.Certificate); !errors.Is(err, ErrMTLSNoPeer) {
		t.Fatalf("nil verifier = %v", err)
	}
	if _, err := verifier.Verify(nil); !errors.Is(err, ErrMTLSNoPeer) {
		t.Fatalf("nil cert = %v", err)
	}
	if _, err := verifier.VerifyConnection(tls.ConnectionState{}); !errors.Is(err, ErrMTLSNoPeer) {
		t.Fatalf("no peer connection = %v", err)
	}
	if err := (*Revocations)(nil).Revoke(issued.Certificate); !errors.Is(err, ErrMTLSRevoked) {
		t.Fatalf("nil revocation set = %v", err)
	}
	if err := revocations.Revoke(nil); !errors.Is(err, ErrMTLSRevoked) {
		t.Fatalf("nil cert revoke = %v", err)
	}
	if err := revocations.Revoke(&x509.Certificate{}); !errors.Is(err, ErrMTLSRevoked) {
		t.Fatalf("serial-less cert revoke = %v", err)
	}
	if err := revocations.Revoke(issued.Certificate); err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(issued.Certificate); !errors.Is(err, ErrMTLSRevoked) {
		t.Fatalf("revoked cert = %v", err)
	}

	issued, err = issuer.Issue(IdentitySpec{Tenant: "tenant-a", Cell: "cell-a", Service: "worker", Instance: "worker-2", Lifetime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	long := *issued.Certificate
	long.NotAfter = long.NotBefore.Add(MaxMTLSLifetime + time.Second)
	if _, err := verifier.Verify(&long); !errors.Is(err, ErrMTLSVerification) {
		t.Fatalf("long certificate = %v", err)
	}
	expiredVerifier, err := NewMTLSVerifier(MTLSVerifierConfig{Roots: verifier.roots, ExpectedTenant: "tenant-a", ExpectedCell: "cell-a", ExpectedService: "worker", Now: func() time.Time { return now.Add(2 * time.Minute) }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := expiredVerifier.Verify(issued.Certificate); !errors.Is(err, ErrMTLSVerification) {
		t.Fatalf("expired certificate = %v", err)
	}
	notYetVerifier, err := NewMTLSVerifier(MTLSVerifierConfig{Roots: verifier.roots, ExpectedTenant: "tenant-a", ExpectedCell: "cell-a", ExpectedService: "worker", Now: func() time.Time { return now.Add(-2 * time.Minute) }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := notYetVerifier.Verify(issued.Certificate); !errors.Is(err, ErrMTLSVerification) {
		t.Fatalf("not-yet-valid certificate = %v", err)
	}
	badRoots := x509.NewCertPool()
	chainVerifier, err := NewMTLSVerifier(MTLSVerifierConfig{Roots: badRoots, ExpectedTenant: "tenant-a", ExpectedCell: "cell-a", ExpectedService: "worker", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chainVerifier.Verify(issued.Certificate); !errors.Is(err, ErrMTLSVerification) {
		t.Fatalf("untrusted chain = %v", err)
	}
	for _, cert := range []*x509.Certificate{{}, {URIs: []*url.URL{{Scheme: "https", Host: "wrong"}}}} {
		if _, err := parseIdentity(cert); !errors.Is(err, ErrMTLSVerification) {
			t.Errorf("parseIdentity(%+v) = %v", cert, err)
		}
	}
	mutated := *issued.Certificate
	mutated.URIs = []*url.URL{{Scheme: spiffeScheme, Host: spiffeAuthority, Path: "/workload/tenant-a/cell-a/worker"}}
	if _, err := parseIdentity(&mutated); !errors.Is(err, ErrMTLSVerification) {
		t.Fatalf("malformed URI path = %v", err)
	}
	if _, err := verifier.Verify(&mutated); !errors.Is(err, ErrMTLSVerification) {
		t.Fatalf("malformed URI certificate = %v", err)
	}
}

func TestMTLS_RevocationAndIdentityHelpers(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	issuer, verifier, revocations := testMTLSIssuerVerifier(t, now)
	issued, err := issuer.Issue(IdentitySpec{Tenant: "tenant-a", Cell: "cell-a", Service: "worker", Instance: "worker-1", Lifetime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if revocations.revoked(issued.Certificate) {
		t.Fatal("new certificate was already revoked")
	}
	fromCert := identityFromCertificate(issued.Certificate, "issuer-a", false)
	if fromCert.Verified() || fromCert.Issuer() != "issuer-a" || fromCert.Subject() != "worker/worker-1" {
		t.Fatalf("identityFromCertificate = %+v", fromCert)
	}
	if _, err := verifier.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{issued.Certificate}}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewMTLSIssuer(MTLSIssuerConfig{Issuer: "root", Certificate: issued.Certificate, Signer: ed25519.PrivateKey(make([]byte, ed25519.PrivateKeySize))}); !errors.Is(err, ErrInvalidMTLSIssuer) {
		t.Fatalf("leaf used as issuer = %v", err)
	}
	if _, err := randomSerial(); err != nil {
		t.Fatal(err)
	}
	if publicKey(issued.TLSCertificate.PrivateKey.(ed25519.PrivateKey)) == nil {
		t.Fatal("publicKey returned nil")
	}
	if serial := big.NewInt(7); serial.Sign() <= 0 {
		t.Fatal("test serial was not positive")
	}
}
