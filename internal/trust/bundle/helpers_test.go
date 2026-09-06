package bundle

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"
)

// testCA is a generated certificate authority used only to build fixtures
// for these tests. It is never used as production key material.
type testCA struct {
	key  *ecdsa.PrivateKey
	cert *x509.Certificate
	der  []byte
}

func genKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func serial(t *testing.T) *big.Int {
	t.Helper()
	s, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate serial: %v", err)
	}
	return s
}

// genRoot creates a self-signed root CA certificate valid over [nb, na).
func genRoot(t *testing.T, cn string, nb, na time.Time) testCA {
	t.Helper()
	key := genKey(t)
	tmpl := &x509.Certificate{
		SerialNumber:          serial(t),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             nb,
		NotAfter:              na,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse root: %v", err)
	}
	return testCA{key: key, cert: cert, der: der}
}

// genIntermediate creates an intermediate CA certificate signed by parent,
// valid over [nb, na).
func genIntermediate(t *testing.T, parent testCA, cn string, nb, na time.Time) testCA {
	t.Helper()
	key := genKey(t)
	tmpl := &x509.Certificate{
		SerialNumber:          serial(t),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             nb,
		NotAfter:              na,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent.cert, &key.PublicKey, parent.key)
	if err != nil {
		t.Fatalf("create intermediate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse intermediate: %v", err)
	}
	return testCA{key: key, cert: cert, der: der}
}

// genLeaf creates an end-entity certificate signed by parent, valid over
// [nb, na), suitable for server-auth chain verification.
func genLeaf(t *testing.T, parent testCA, cn string, nb, na time.Time) (der []byte, cert *x509.Certificate) {
	t.Helper()
	key := genKey(t)
	tmpl := &x509.Certificate{
		SerialNumber: serial(t),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    nb,
		NotAfter:     na,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	d, err := x509.CreateCertificate(rand.Reader, tmpl, parent.cert, &key.PublicKey, parent.key)
	if err != nil {
		t.Fatalf("create leaf: %v", err)
	}
	c, err := x509.ParseCertificate(d)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	return d, c
}

func mustPin(t *testing.T, ca testCA) PinnedCert {
	t.Helper()
	p, err := NewPinnedCert(ca.der)
	if err != nil {
		t.Fatalf("pin: %v", err)
	}
	return p
}
