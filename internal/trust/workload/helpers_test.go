package workload_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

const (
	authorityName = "hcm-next-workload-authority"
	cellPrimary   = "cell-us-east-1a"
	cellOther     = "cell-us-west-2a"
)

// baseTime is the fixed instant every test in this package treats as "now".
var baseTime = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// fatalHelper is the subset of *testing.T and *testing.F this file's setup
// helpers need. *testing.F deliberately does not implement testing.TB, so a
// helper shared between an ordinary test and a fuzz target's seed-corpus
// setup is typed against this narrower interface instead.
type fatalHelper interface {
	Helper()
	Fatalf(format string, args ...any)
}

// testAuthority bundles one Ed25519 keypair, the [workload.Issuer] that
// signs with it and the [workload.KeySource] a [workload.Verifier] resolves
// it through.
type testAuthority struct {
	keyID   string
	public  ed25519.PublicKey
	private ed25519.PrivateKey
	issuer  *workload.Issuer
	source  *workload.StaticKeySource
}

func newTestAuthority(t fatalHelper, at time.Time) *testAuthority {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	issuer, err := workload.NewIssuer(workload.IssuerConfig{
		Name:    authorityName,
		KeyID:   "key-1",
		Private: priv,
		Now:     func() time.Time { return at },
	})
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	source := workload.NewStaticKeySource().WithKey(authorityName, "key-1", pub)
	return &testAuthority{keyID: "key-1", public: pub, private: priv, issuer: issuer, source: source}
}

func newVerifier(t *testing.T, keys workload.KeySource, cell string, at time.Time) *workload.Verifier {
	t.Helper()
	v, err := workload.NewVerifier(workload.VerifierConfig{
		Keys: keys,
		Cell: cell,
		Now:  func() time.Time { return at },
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return v
}

func validSpec(role workload.ProcessRole) workload.IssueSpec {
	return workload.IssueSpec{
		Subject:  "instance-" + string(role) + "-1",
		Role:     role,
		Cell:     cellPrimary,
		Lifetime: 5 * time.Minute,
	}
}
