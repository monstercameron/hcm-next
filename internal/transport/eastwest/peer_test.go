package eastwest

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

func eastwestPeer(t *testing.T, service, cell string, now time.Time) workload.MTLSIdentity {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootTemplate := &x509.Certificate{SerialNumber: big.NewInt(17), Subject: pkix.Name{CommonName: "eastwest-root"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
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
	issuer, err := workload.NewMTLSIssuer(workload.MTLSIssuerConfig{Issuer: "eastwest-root", Certificate: root, Signer: private, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := issuer.Issue(workload.IdentitySpec{Tenant: "tenant-a", Cell: cell, Service: service, Instance: service + "-1", Lifetime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := workload.NewMTLSVerifier(workload.MTLSVerifierConfig{Roots: pool, ExpectedTenant: "tenant-a", ExpectedCell: cell, ExpectedService: service, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := verifier.Verify(issued.Certificate)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func TestTodo_EDGE_006_MTLSAndServiceAuthZ(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	policy, err := CompileManifest(repoManifest(t))
	if err != nil {
		t.Fatal(err)
	}
	peer := eastwestPeer(t, "worker", "cell-a", now)
	if err := policy.AllowsPeer(peer, "postgres-store", "READ", "cell-a", now); err != nil {
		t.Fatalf("verified worker read was denied: %v", err)
	}
	if err := policy.AllowsPeer(peer, "postgres-store", "WRITE", "cell-a", now); err != nil {
		t.Fatalf("verified worker write was denied: %v", err)
	}
	for _, tc := range []struct {
		name   string
		peer   workload.MTLSIdentity
		target string
		method string
		cell   string
		want   error
	}{
		{"wrong cell", peer, "postgres-store", "READ", "cell-b", ErrPeerCell},
		{"unauthorized method", peer, "postgres-store", "DELETE", "cell-a", ErrPeerMethod},
		{"unknown target", peer, "unknown-service", "READ", "cell-a", ErrPeerWorkload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := policy.AllowsPeer(tc.peer, tc.target, tc.method, tc.cell, now); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want %v", err, tc.want)
			}
		})
	}
}
