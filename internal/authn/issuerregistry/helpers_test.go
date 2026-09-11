package issuerregistry_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"sync"
	"testing"
	"time"
)

// testMustRSAKey is generated once per test binary run (RSA key generation
// is slow enough that regenerating it per test would noticeably slow this
// package's suite down) and reused read-only by every test that needs a
// syntactically valid pinned key.
var (
	rsaKeyOnce sync.Once
	rsaKey     *rsa.PrivateKey
)

func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	rsaKeyOnce.Do(func() {
		var err error
		rsaKey, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate RSA key: %v", err)
		}
	})
	return rsaKey
}

func testRSAPublicKeyDER(t *testing.T) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&testRSAKey(t).PublicKey)
	if err != nil {
		t.Fatalf("marshal RSA public key: %v", err)
	}
	return der
}

// testSelfSignedCA builds a minimal, valid self-signed CA certificate over
// an RSA key, for tests that exercise the [issuerregistry.JWKSSourcePinnedBundle]
// path through a real internal/trust/bundle.Bundle rather than a stub.
func testSelfSignedCA(t *testing.T, serial int64, notBefore, notAfter time.Time) (der []byte, key *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		Subject:               pkix.Name{CommonName: "issuerregistry test CA"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err = x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}
	return der, key
}
