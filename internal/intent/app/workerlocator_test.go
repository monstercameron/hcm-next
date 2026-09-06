package app

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/domains/fixtures"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TestCorpusWorkerLocatorResolvesKeysAndIdentifiers proves the corpus locator
// answers both spellings a reference arrives in, and refuses a string that is
// neither -- which is what stops a typo from being proposed as a promotion for
// nobody.
func TestCorpusWorkerLocatorResolvesKeysAndIdentifiers(t *testing.T) {
	ctx := context.Background()
	ref, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("fixtures.WorkerRef: %v", err)
	}

	for name, input := range map[string]string{
		"by corpus key": "omar-reyes",
		"by entity id":  ref.Id,
	} {
		t.Run(name, func(t *testing.T) {
			got, ok, err := corpusWorkerLocator(ctx, fixtures.Tenant, input)
			if err != nil || !ok {
				t.Fatalf("corpusWorkerLocator(%q) = %v, ok=%v, err=%v", input, got, ok, err)
			}
			if got.Ref.Id != ref.Id {
				t.Errorf("resolved id = %q, want %q", got.Ref.Id, ref.Id)
			}
			if got.Key != "omar-reyes" {
				t.Errorf("resolved key = %q, want the corpus key", got.Key)
			}
			if got.Created != nil {
				t.Error("a corpus worker carries a created row")
			}
			if got.Ref.Validate() != nil {
				t.Errorf("resolved reference %+v is not well formed", got.Ref)
			}
		})
	}

	for name, input := range map[string]string{
		"empty":                 "",
		"blank":                 "   ",
		"a created worker key":  "lena-01a0694b",
		"not an identifier":     "who?",
		"an uppercase uuid":     "11111111-1111-4111-8111-11111111111A",
		"a number that is not":  "12345",
		"a name somebody typed": "Omar Reyes",
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			if _, ok, err := corpusWorkerLocator(ctx, fixtures.Tenant, input); ok || err != nil {
				t.Fatalf("corpusWorkerLocator(%q) resolved (ok=%v err=%v)", input, ok, err)
			}
		})
	}
}

// TestCorpusWorkerLocatorAcceptsAWellFormedUnknownIdentifier pins the
// deliberate permissive case: a caller holding a raw entity id still resolves,
// and it is the governed read -- not resolution -- that reports the worker is
// not disclosed. Removing this would turn a disclosure decision into a
// resolution decision, which is exactly the confusion the read model exists to
// prevent.
func TestCorpusWorkerLocatorAcceptsAWellFormedUnknownIdentifier(t *testing.T) {
	unknown := uuid.NewString()
	got, ok, err := corpusWorkerLocator(context.Background(), fixtures.Tenant, unknown)
	if err != nil || !ok {
		t.Fatalf("corpusWorkerLocator(unknown id) = ok=%v err=%v", ok, err)
	}
	if got.Ref.Id != unknown || got.Key != unknown {
		t.Fatalf("resolved %+v, want the identifier itself", got)
	}
	if got.Ref.Kind != people.KindWorker || got.Ref.Tenant != fixtures.Tenant {
		t.Fatalf("resolved reference = %+v, want a worker in the named tenant", got.Ref)
	}
}

// TestNewWorkerLocatorWithoutADatabaseIsTheCorpusLocator proves the
// composition condition: a cell with no execution database has no created
// population, and resolution behaves exactly as it did before one existed.
func TestNewWorkerLocatorWithoutADatabaseIsTheCorpusLocator(t *testing.T) {
	ctx := context.Background()
	for name, locate := range map[string]WorkerLocator{
		"no database":   newWorkerLocator(nil, func(values.TenantId) uuid.UUID { return uuid.New() }),
		"no tenant map": newWorkerLocator(nil, nil),
	} {
		t.Run(name, func(t *testing.T) {
			got, ok, err := locate(ctx, fixtures.Tenant, "omar-reyes")
			if err != nil || !ok {
				t.Fatalf("locate(corpus key) = ok=%v err=%v", ok, err)
			}
			if got.Key != "omar-reyes" || got.Created != nil {
				t.Fatalf("resolved %+v, want the corpus worker", got)
			}
			if _, ok, _ := locate(ctx, fixtures.Tenant, "lena-01a0694b"); ok {
				t.Fatal("a created worker key resolved on a cell with no created population")
			}
		})
	}
}

// TestLocateCorpusWorkerExactHasNoPermissiveFallback proves the split the
// layered locator depends on: the exact pass must not claim a reference the
// created population might own, or a created worker's key would be answered as
// an unknown entity id before the database was ever asked.
func TestLocateCorpusWorkerExactHasNoPermissiveFallback(t *testing.T) {
	unknown := uuid.NewString()
	if _, ok, err := locateCorpusWorkerExact(fixtures.Tenant, unknown); ok || err != nil {
		t.Fatalf("locateCorpusWorkerExact(unknown id) resolved (ok=%v err=%v)", ok, err)
	}
	if _, ok, err := locateCorpusWorkerExact(fixtures.Tenant, "lena-01a0694b"); ok || err != nil {
		t.Fatalf("locateCorpusWorkerExact(created key) resolved (ok=%v err=%v)", ok, err)
	}
	got, ok, err := locateCorpusWorkerExact(fixtures.Tenant, "omar-reyes")
	if err != nil || !ok || got.Key != "omar-reyes" {
		t.Fatalf("locateCorpusWorkerExact(corpus key) = %+v ok=%v err=%v", got, ok, err)
	}
}

// TestWorkspaceWorkerStillResolvesTheCorpus guards the one caller left on the
// corpus-only helper: the stored-intent summary projection, which has no
// context or database and whose worker id is overwritten from the intent's own
// EMPLOYMENT subject anyway.
func TestWorkspaceWorkerStillResolvesTheCorpus(t *testing.T) {
	got, ok := workspaceWorker("omar-reyes")
	if !ok {
		t.Fatal("workspaceWorker no longer resolves a corpus key")
	}
	if got.Tenant != fixtures.Tenant || got.Kind != people.KindWorker {
		t.Fatalf("workspaceWorker resolved %+v", got)
	}
	if _, ok := workspaceWorker("lena-01a0694b"); ok {
		t.Fatal("workspaceWorker claimed a created worker key it cannot resolve")
	}
}
