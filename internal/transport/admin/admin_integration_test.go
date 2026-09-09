package admin_test

import (
	"context"
	"testing"
	"time"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/admin"
)

// TestTodo_ADMIN_001_Integration starts the admin server in-process on a
// loopback listener (matching SVC-011's GREEN: "Integration tests may start
// the admin server in-process on a loopback listener") and drives every
// method end to end over a real network connection with the generated Go
// gRPC client: ListIntents (forwarding to a fake transport.IntentHandler),
// GetReleaseManifest and ListCapabilityProfiles (self-contained), and the
// two governed passthroughs ExplainTransaction and GetWorkerState against
// real internal/domains fixtures.
func TestTodo_ADMIN_001_Integration(t *testing.T) {
	wf, err := fixtures.NewMemoryWorkerFacts()
	if err != nil {
		t.Fatalf("NewMemoryWorkerFacts: %v", err)
	}
	intentHandler := &fakeIntentHandler{}
	conn, cleanup := startTestServer(t, admin.Dependencies{
		Intent:             intentHandler,
		WorkerFacts:        wf,
		TransactionHistory: fakeTransactionHistory{},
	})
	defer cleanup()
	client := dialAdminClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	authed := withToken(ctx, fixtureOperatorToken)

	t.Run("ListIntents forwards to the reused intent handler port", func(t *testing.T) {
		if _, err := client.ListIntents(authed, &adminv1.ListIntentsRequest{}); err != nil {
			t.Fatalf("ListIntents: %v", err)
		}
		if intentHandler.calls != 1 {
			t.Fatalf("intent handler called %d times, want 1", intentHandler.calls)
		}
	})

	t.Run("GetReleaseManifest renders a non-empty, self-consistent document", func(t *testing.T) {
		resp, err := client.GetReleaseManifest(authed, &adminv1.GetReleaseManifestRequest{})
		if err != nil {
			t.Fatalf("GetReleaseManifest: %v", err)
		}
		if len(resp.GetEndpoints()) == 0 {
			t.Fatal("expected at least one endpoint in the discovery document")
		}
		if len(resp.GetCapabilities()) == 0 {
			t.Fatal("expected at least one capability in the discovery document")
		}
	})

	t.Run("ListCapabilityProfiles lists the compiled-in registry", func(t *testing.T) {
		resp, err := client.ListCapabilityProfiles(authed, &adminv1.ListCapabilityProfilesRequest{})
		if err != nil {
			t.Fatalf("ListCapabilityProfiles: %v", err)
		}
		if len(resp.GetCapabilities()) == 0 {
			t.Fatal("expected at least one registered capability")
		}
	})

	t.Run("ExplainTransaction discloses a known transaction's REQUEST section", func(t *testing.T) {
		resp, err := client.ExplainTransaction(authed, &adminv1.ExplainTransactionRequest{
			Transaction: &adminv1.TransactionRef{Id: fixtureTransactionID},
			Sections:    []string{"REQUEST"},
		})
		if err != nil {
			t.Fatalf("ExplainTransaction: %v", err)
		}
		if !resp.GetDisclosed() || resp.GetDisclosure() != "FULL" {
			t.Fatalf("disclosed=%v disclosure=%q, want disclosed=true disclosure=FULL", resp.GetDisclosed(), resp.GetDisclosure())
		}
		if len(resp.GetSections()) != 1 || resp.GetSections()[0].GetSection() != "REQUEST" {
			t.Fatalf("sections = %+v, want exactly one REQUEST section", resp.GetSections())
		}
		if resp.GetEvidenceRef().GetEvidenceId() == "" {
			t.Fatal("expected a non-empty evidence id")
		}
	})

	t.Run("ExplainTransaction reports an unknown transaction as ABSENT under the full-disclosure operator profile", func(t *testing.T) {
		// operatorTransactionAuthorization grants TransactionDisclosable
		// unconditionally, so an unknown transaction id surfaces as an
		// authorized-but-absent read, not WITHHELD; WITHHELD is reserved for a
		// caller whose authorization decision denies knowing the transaction
		// exists at all, which never happens under this profile.
		resp, err := client.ExplainTransaction(authed, &adminv1.ExplainTransactionRequest{
			Transaction: &adminv1.TransactionRef{Id: fixtureUnknownTransactionID},
			Sections:    []string{"REQUEST"},
		})
		if err != nil {
			t.Fatalf("ExplainTransaction: %v", err)
		}
		if !resp.GetDisclosed() || resp.GetPresence() != "ABSENT" {
			t.Fatalf("disclosed=%v presence=%q, want disclosed=true presence=ABSENT", resp.GetDisclosed(), resp.GetPresence())
		}
	})

	t.Run("GetWorkerState discloses a known worker's fields", func(t *testing.T) {
		ref, err := fixtures.WorkerRef("jane-doe")
		if err != nil {
			t.Fatalf("WorkerRef: %v", err)
		}
		resp, err := client.GetWorkerState(authed, &adminv1.GetWorkerStateRequest{
			WorkerId:    ref.Id,
			EffectiveOn: "2026-06-01",
			Fields:      []string{"worker.worker_number", "person.legal_name"},
		})
		if err != nil {
			t.Fatalf("GetWorkerState: %v", err)
		}
		if len(resp.GetFields()) != 2 {
			t.Fatalf("fields = %+v, want exactly 2", resp.GetFields())
		}
		for _, f := range resp.GetFields() {
			if f.GetAccess() != "AUTHORIZED" {
				t.Fatalf("field %s access = %q, want AUTHORIZED under the operator profile", f.GetField(), f.GetAccess())
			}
		}
	})

	t.Run("GetWorkerState reports an unknown worker as ABSENT under the full-disclosure operator profile", func(t *testing.T) {
		// The operator profile grants SubjectDisclosable unconditionally (see
		// operatorWorkerAuthorization), so a worker that does not exist is
		// reported as an authorized-but-absent subject, not withheld: WITHHELD
		// is reserved for a caller whose authorization decision denies knowing
		// the subject exists at all, which never happens under this profile.
		resp, err := client.GetWorkerState(authed, &adminv1.GetWorkerStateRequest{
			WorkerId:    fixtureUnknownWorkerID,
			EffectiveOn: "2026-06-01",
			Fields:      []string{"worker.worker_number"},
		})
		if err != nil {
			t.Fatalf("GetWorkerState: %v", err)
		}
		if !resp.GetDisclosed() || resp.GetPresence() != "ABSENT" {
			t.Fatalf("disclosed=%v presence=%q, want disclosed=true presence=ABSENT", resp.GetDisclosed(), resp.GetPresence())
		}
	})
}
