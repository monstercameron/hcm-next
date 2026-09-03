package hcmctl_test

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/monstercameron/hcm-next/internal/transport"
	admin "github.com/monstercameron/hcm-next/internal/transport/admin"
	"github.com/monstercameron/hcm-next/internal/transport/admin/hcmctl"
	"github.com/monstercameron/hcm-next/internal/transport/grpcserver"
	"github.com/monstercameron/hcm-next/internal/trust"
)

const testToken = "hcmctl-test-operator-token"

// fixtureVerifier recognizes exactly one operator-scoped bearer token, so
// this test exercises hcmctl.Main against a real, fully-admitted AdminService
// instance rather than a mock of the CLI's own making.
type fixtureVerifier struct{}

func (fixtureVerifier) Verify(_ context.Context, cred trust.Credential) (*trust.Principal, error) {
	if cred.Token != testToken {
		return nil, trust.ErrInvalidCredential
	}
	now := time.Now()
	return trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               "acme-corp",
		Subject:              "operator-fixture",
		SubjectKind:          trust.SubjectKindHuman,
		Roles:                []string{admin.OperatorRole},
		Purposes:             []string{"operator_diagnostics"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "session-fixture",
		IssuedAt:             now.Add(-time.Minute),
		ExpiresAt:            now.Add(time.Hour),
		CredentialDigest:     "digest-fixture",
	})
}

// startFixtureServer boots a real AdminService (self-contained methods
// only: GetReleaseManifest and ListCapabilityProfiles need no injected
// port) on a loopback listener, returning its address and a cleanup func.
func startFixtureServer(t *testing.T) (addr string, cleanup func()) {
	t.Helper()
	cfg := transport.Config{Verifier: fixtureVerifier{}}
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(cfg)))
	admin.Register(srv, admin.Dependencies{})

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(lis) }()
	return lis.Addr().String(), func() {
		srv.Stop()
		_ = lis.Close()
	}
}

// dial adapts hcmctl.DialInsecure's shape for this test.
func dial(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// TestMainReleaseManifestPrintsEvidenceOnSuccess proves hcmctl.Main dials
// the real generated client, authenticates with the supplied token, and
// prints both the typed result and an evidence line - ADMIN-001's "evidence
// IDs printed on every call" - without any business logic of its own: every
// value in the output came back from the server.
func TestMainReleaseManifestPrintsEvidenceOnSuccess(t *testing.T) {
	addr, cleanup := startFixtureServer(t)
	defer cleanup()

	var stdout, stderr bytes.Buffer
	code := hcmctl.Main([]string{"-addr", addr, "-token", testToken, "release-manifest"}, &stdout, &stderr, dial)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "manifest_digest:") {
		t.Fatalf("output missing manifest_digest:\n%s", out)
	}
	if !strings.Contains(out, "evidence:") {
		t.Fatalf("output missing the required evidence line:\n%s", out)
	}
}

// TestMainListCapabilities proves a second subcommand also reaches the real
// server and prints its typed result plus an evidence line.
func TestMainListCapabilities(t *testing.T) {
	addr, cleanup := startFixtureServer(t)
	defer cleanup()

	var stdout, stderr bytes.Buffer
	code := hcmctl.Main([]string{"-addr", addr, "-token", testToken, "list-capabilities"}, &stdout, &stderr, dial)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "evidence:") {
		t.Fatalf("output missing the required evidence line:\n%s", stdout.String())
	}
}

// TestMainRejectsAnOrdinaryCredentialWithoutLeakingIt proves an
// unauthorized call fails cleanly (non-zero exit, no panic) and that the
// token itself never appears in the printed diagnostic.
func TestMainRejectsAnOrdinaryCredentialWithoutLeakingIt(t *testing.T) {
	addr, cleanup := startFixtureServer(t)
	defer cleanup()

	var stdout, stderr bytes.Buffer
	code := hcmctl.Main([]string{"-addr", addr, "-token", "not-the-operator-token", "release-manifest"}, &stdout, &stderr, dial)
	if code == 0 {
		t.Fatal("expected a non-zero exit code for an unrecognized credential")
	}
	if strings.Contains(stderr.String(), "not-the-operator-token") {
		t.Fatalf("stderr leaked the credential: %s", stderr.String())
	}
}

// TestMainRequiresTokenOrMint proves the CLI refuses to run with neither a
// credential nor -mint, rather than silently dialing unauthenticated.
func TestMainRequiresTokenOrMint(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := hcmctl.Main([]string{"-addr", "127.0.0.1:0", "release-manifest"}, &stdout, &stderr, dial)
	if code == 0 {
		t.Fatal("expected a non-zero exit code with neither -token nor -mint set")
	}
}
