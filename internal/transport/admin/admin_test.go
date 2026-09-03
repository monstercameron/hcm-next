package admin_test

import (
	"context"
	"testing"
	"time"

	adminv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/admin/v1"
	"github.com/monstercameron/hcm-next/internal/domains/fixtures"
	"github.com/monstercameron/hcm-next/internal/transport/admin"
	"github.com/monstercameron/hcm-next/internal/transport/envelope"
)

// TestTodo_ADMIN_001 is the ADMIN-001 primary test: AdminService, native
// gRPC in this test, projects the identical trusted-request boundary every
// other service on the shared *grpc.Server uses (the same
// internal/transport/grpcserver.UnaryInterceptor chain: deadline capping,
// admission, strict validation, canonical envelope.Error projection,
// correlation and evidence IDs) plus one additional, distinct predicate -
// the caller must carry [admin.OperatorRole] - that an ordinary user
// credential never satisfies.
func TestTodo_ADMIN_001(t *testing.T) {
	wf, err := fixtures.NewMemoryWorkerFacts()
	if err != nil {
		t.Fatalf("NewMemoryWorkerFacts: %v", err)
	}
	conn, cleanup := startTestServer(t, admin.Dependencies{
		Intent:             &fakeIntentHandler{},
		WorkerFacts:        wf,
		TransactionHistory: fakeTransactionHistory{},
	})
	defer cleanup()
	client := dialAdminClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("operator token succeeds and carries an evidence-bearing response", func(t *testing.T) {
		resp, err := client.GetReleaseManifest(withToken(ctx, fixtureOperatorToken), &adminv1.GetReleaseManifestRequest{})
		if err != nil {
			t.Fatalf("GetReleaseManifest: %v", err)
		}
		if resp.GetManifestDigest() == "" {
			t.Fatal("expected a non-empty manifest digest")
		}
	})

	t.Run("no credential is rejected before any capability runs", func(t *testing.T) {
		_, err := client.GetReleaseManifest(ctx, &adminv1.GetReleaseManifestRequest{})
		assertOwnedCode(t, err, envelope.CodeUnauthenticated)
	})

	t.Run("an authenticated ordinary-user credential is not enough", func(t *testing.T) {
		_, err := client.GetReleaseManifest(withToken(ctx, fixtureOrdinaryToken), &adminv1.GetReleaseManifestRequest{})
		owned := assertOwnedCode(t, err, envelope.CodePermissionDenied)
		// ReasonRef itself is server-internal telemetry and is deliberately
		// not part of the wire projection (envelope.Error.Detail); what must
		// cross the wire identically on every channel is the owned code, the
		// evidence reference and the correlation id.
		if owned.EvidenceRef().ID == "" {
			t.Fatal("a permission-denied result must still carry the authentication evidence reference")
		}
		if owned.CorrelationID() == "" {
			t.Fatal("a permission-denied result must carry a correlation id")
		}
	})

	t.Run("an unrecognized credential is rejected the same way a missing one is", func(t *testing.T) {
		_, err := client.GetReleaseManifest(withToken(ctx, fixtureUnknownToken), &adminv1.GetReleaseManifestRequest{})
		assertOwnedCode(t, err, envelope.CodeUnauthenticated)
	})

	t.Run("every method requires the operator role, not just one", func(t *testing.T) {
		cases := []struct {
			name string
			call func() error
		}{
			{"ListIntents", func() error {
				_, err := client.ListIntents(withToken(ctx, fixtureOrdinaryToken), &adminv1.ListIntentsRequest{})
				return err
			}},
			{"ListCapabilityProfiles", func() error {
				_, err := client.ListCapabilityProfiles(withToken(ctx, fixtureOrdinaryToken), &adminv1.ListCapabilityProfilesRequest{})
				return err
			}},
			{"ExplainTransaction", func() error {
				_, err := client.ExplainTransaction(withToken(ctx, fixtureOrdinaryToken), &adminv1.ExplainTransactionRequest{Transaction: &adminv1.TransactionRef{Id: fixtureTransactionID}})
				return err
			}},
			{"GetWorkerState", func() error {
				_, err := client.GetWorkerState(withToken(ctx, fixtureOrdinaryToken), &adminv1.GetWorkerStateRequest{WorkerId: "jane-doe"})
				return err
			}},
			{"GetWorkflowInstance", func() error {
				_, err := client.GetWorkflowInstance(withToken(ctx, fixtureOrdinaryToken), &adminv1.GetWorkflowInstanceRequest{InstanceId: fixtureUnknownWorkerID})
				return err
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				assertOwnedCode(t, tc.call(), envelope.CodePermissionDenied)
			})
		}
	})
}

// TestTodo_ADMIN_001_Golden pins the served shape of one method (the field
// names and enum spellings a CLI or console will parse) so a later change to
// that shape is a reviewed diff, not a silent break.
func TestTodo_ADMIN_001_Golden(t *testing.T) {
	conn, cleanup := startTestServer(t, admin.Dependencies{})
	defer cleanup()
	client := dialAdminClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.ListCapabilityProfiles(withToken(ctx, fixtureOperatorToken), &adminv1.ListCapabilityProfilesRequest{})
	if err != nil {
		t.Fatalf("ListCapabilityProfiles: %v", err)
	}
	var found *adminv1.CapabilityProfile
	for _, c := range resp.GetCapabilities() {
		if c.GetCapabilityId() == "hcmnext.people.promote_worker" {
			found = c
			break
		}
	}
	if found == nil {
		t.Fatal("hcmnext.people.promote_worker is missing from ListCapabilityProfiles")
	}
	if found.GetVersion() != 1 {
		t.Fatalf("version = %d, want 1", found.GetVersion())
	}
	if found.GetStatus() != "ACTIVE" {
		t.Fatalf("status = %q, want ACTIVE", found.GetStatus())
	}
	if found.GetDigest() == "" {
		t.Fatal("expected a non-empty content digest")
	}
}

// assertOwnedCode decodes err as an *envelope.Error (the same decode a real
// generated client uses, internal/transport/clients.DecodeGRPCError) and
// fails the test unless its code matches want.
func assertOwnedCode(t *testing.T, err error, want envelope.Code) *envelope.Error {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	owned, ok := envelope.FromGRPC(err)
	if !ok {
		t.Fatalf("error %v did not decode as an owned envelope.Error", err)
	}
	if owned.Code() != want {
		t.Fatalf("Code() = %s, want %s (reason=%s message=%s)", owned.Code(), want, owned.ReasonRef(), owned.Message())
	}
	return owned
}
