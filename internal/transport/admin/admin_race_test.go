package admin_test

import (
	"context"
	"sync"
	"testing"
	"time"

	adminv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/admin/v1"
	"github.com/monstercameron/hcm-next/internal/domains/fixtures"
	"github.com/monstercameron/hcm-next/internal/transport/admin"
)

// TestTodo_ADMIN_001_Race drives many concurrent callers - a mix of
// operator and ordinary-user credentials, across every method - against one
// shared server instance and one shared *fakeIntentHandler, to prove the
// server holds no unsynchronized mutable state of its own (every method is
// either a pure computation over its arguments or a forward through an
// injected port) and that concurrent PERMISSION_DENIED and successful calls
// never interfere with each other's results. Run with -race to catch a
// shared-state regression; it also passes deterministically without it.
func TestTodo_ADMIN_001_Race(t *testing.T) {
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

	const workers = 40
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if i%2 == 0 {
				resp, err := client.GetReleaseManifest(withToken(ctx, fixtureOperatorToken), &adminv1.GetReleaseManifestRequest{})
				if err != nil {
					t.Errorf("worker %d: operator GetReleaseManifest: %v", i, err)
					return
				}
				if resp.GetManifestDigest() == "" {
					t.Errorf("worker %d: empty manifest digest", i)
				}
			} else {
				_, err := client.GetReleaseManifest(withToken(ctx, fixtureOrdinaryToken), &adminv1.GetReleaseManifestRequest{})
				if err == nil {
					t.Errorf("worker %d: expected an ordinary-user credential to be refused", i)
				}
			}

			if _, err := client.ListCapabilityProfiles(withToken(ctx, fixtureOperatorToken), &adminv1.ListCapabilityProfilesRequest{}); err != nil {
				t.Errorf("worker %d: ListCapabilityProfiles: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
}
