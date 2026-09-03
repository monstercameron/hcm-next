package admin_test

import (
	"context"
	"testing"
	"time"

	adminv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/admin/v1"
	"github.com/monstercameron/hcm-next/internal/transport/admin"
	"github.com/monstercameron/hcm-next/internal/transport/envelope"
)

// TestTodo_ADMIN_001_Fault proves that a method whose backing port is not
// configured returns a typed UNAVAILABLE rather than panicking the process
// or falling through to a raw nil-pointer failure. This is the FAULT case
// SVC-011/ADMIN-001 requires: the modular application can host AdminService
// before every read port is wired, and an unconfigured method degrades
// safely for exactly the methods that depend on it, while the
// self-contained methods (GetReleaseManifest, ListCapabilityProfiles) keep
// working regardless.
func TestTodo_ADMIN_001_Fault(t *testing.T) {
	// Every optional port left nil.
	conn, cleanup := startTestServer(t, admin.Dependencies{})
	defer cleanup()
	client := dialAdminClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	authed := withToken(ctx, fixtureOperatorToken)

	t.Run("ListIntents without an intent port", func(t *testing.T) {
		_, err := client.ListIntents(authed, &adminv1.ListIntentsRequest{})
		assertOwnedCode(t, err, envelope.CodeUnavailable)
	})

	t.Run("ExplainTransaction without a transaction history port", func(t *testing.T) {
		_, err := client.ExplainTransaction(authed, &adminv1.ExplainTransactionRequest{
			Transaction: &adminv1.TransactionRef{Id: fixtureTransactionID},
		})
		assertOwnedCode(t, err, envelope.CodeUnavailable)
	})

	t.Run("GetWorkerState without a worker facts port", func(t *testing.T) {
		_, err := client.GetWorkerState(authed, &adminv1.GetWorkerStateRequest{WorkerId: "11111111-1111-4111-8111-111111111111"})
		assertOwnedCode(t, err, envelope.CodeUnavailable)
	})

	t.Run("GetReleaseManifest keeps working with no ports configured", func(t *testing.T) {
		resp, err := client.GetReleaseManifest(authed, &adminv1.GetReleaseManifestRequest{})
		if err != nil {
			t.Fatalf("GetReleaseManifest: %v", err)
		}
		if resp.GetManifestDigest() == "" {
			t.Fatal("expected a non-empty manifest digest")
		}
	})

	t.Run("ListCapabilityProfiles keeps working with no ports configured", func(t *testing.T) {
		resp, err := client.ListCapabilityProfiles(authed, &adminv1.ListCapabilityProfilesRequest{})
		if err != nil {
			t.Fatalf("ListCapabilityProfiles: %v", err)
		}
		if len(resp.GetCapabilities()) == 0 {
			t.Fatal("expected at least one registered capability")
		}
	})
}
