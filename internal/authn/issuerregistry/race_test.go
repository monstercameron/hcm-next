package issuerregistry_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/authn/issuerregistry"
)

// TestTodo_AUTHN_001_Race is this todo's RACE matrix test. [issuerregistry.MemoryStore]
// holds a sync.RWMutex; this test drives concurrent Publish, Activate,
// Suspend, Lookup and Resolver calls against one shared store to prove
// nothing panics or corrupts shared state, and that the store lands in a
// consistent, individually-observable final state. It is a race
// *coverage* test -- this environment does not run go test -race on
// windows/arm64 -- and is what tools/policy/racepolicy checks exists for
// any package that starts goroutines or imports sync/sync-atomic; go.mod's
// own CI job races it for real on linux/amd64.
func TestTodo_AUTHN_001_Race(t *testing.T) {
	store := issuerregistry.NewMemoryStore()
	const issuers = 6

	var wg sync.WaitGroup
	for n := 0; n < issuers; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			i := validIssuer(t)
			i.Tenant = tenantAcme
			i.IssuerURL = raceIssuerURL(n)
			published, err := issuerregistry.Publish(store, i)
			if err != nil {
				t.Errorf("Publish issuer %d: %v", n, err)
				return
			}
			if _, err := issuerregistry.Activate(store, published.Ref(), evidence("bob-approver", baseTime.Add(time.Minute))); err != nil {
				t.Errorf("Activate issuer %d: %v", n, err)
			}
		}(n)
	}
	wg.Wait()

	resolver, err := issuerregistry.NewResolver(store, nil)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	// Concurrent readers (Lookup, ResolveIssuerKeys, ListEvents) racing a
	// concurrent writer (Suspend on one issuer, repeatedly) over the same
	// shared store.
	var wg2 sync.WaitGroup
	stop := make(chan struct{})
	for r := 0; r < 8; r++ {
		wg2.Add(1)
		go func(r int) {
			defer wg2.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				n := r % issuers
				_, _ = issuerregistry.Lookup(store, tenantAcme, raceIssuerURL(n))
				_, _ = resolver.ResolveIssuerKeys(context.Background(), raceIssuerURL(n))
				_, _ = store.ListEvents(tenantAcme, raceIssuerURL(n))
			}
		}(r)
	}

	ref := issuerregistry.Ref{Tenant: tenantAcme, IssuerURL: raceIssuerURL(0), Revision: 1}
	for i := 0; i < 20; i++ {
		if _, err := issuerregistry.Suspend(store, ref, evidence("bob-approver", baseTime.Add(time.Duration(i+2)*time.Minute))); err == nil {
			if _, err := issuerregistry.Activate(store, ref, evidence("carol-approver", baseTime.Add(time.Duration(i+2)*time.Minute+time.Second))); err != nil {
				t.Errorf("re-Activate after Suspend %d: %v", i, err)
			}
		}
	}
	close(stop)
	wg2.Wait()

	for n := 0; n < issuers; n++ {
		if _, err := issuerregistry.Lookup(store, tenantAcme, raceIssuerURL(n)); err != nil {
			t.Errorf("final Lookup issuer %d: %v", n, err)
		}
	}
}

func raceIssuerURL(n int) string {
	return "https://login.race-" + string(rune('a'+n)) + ".invalid/"
}
