package workeridstore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/workerids"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestStorePersistsPolicyWithCASAndReservesConcurrently(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-worker-id','Worker IDs','ACTIVE',$3)`, tenantID, "worker-id-test", time.Now().UTC())
	store := New(db.Conn, func(values.TenantId) uuid.UUID { return tenantID })
	tenant := values.TenantId("worker-id-test")
	policy := workerids.DefaultPolicy()
	policy.Prefix, policy.SequenceDigits, policy.StartAt, policy.NextSequence = "NW", 5, 98, 98
	policy.ExcludedRanges = "100-104"
	saved, err := store.Save(context.Background(), tenant, "org:north", "admin", policy)
	if err != nil || saved.Version != 1 {
		t.Fatalf("save: %+v err=%v", saved, err)
	}
	if _, err := store.Save(context.Background(), tenant, "org:north", "admin", policy); !errors.Is(err, workerids.ErrVersionConflict) {
		t.Fatalf("stale save err=%v", err)
	}

	const count = 24
	concurrentStores := make([]*Store, count)
	for i := range concurrentStores {
		concurrentStores[i] = New(db.NewConn(t), func(values.TenantId) uuid.UUID { return tenantID })
	}
	ids := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		workerStore := concurrentStores[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, reserveErr := workerStore.Reserve(context.Background(), tenant, "org:north", "hiring", workerids.FormatContext{At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
			if reserveErr != nil {
				errs <- reserveErr
				return
			}
			ids <- id
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Errorf("reserve: %v", err)
	}
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Errorf("duplicate reservation %q", id)
		}
		seen[id] = true
		if id == "NW-00100" {
			t.Error("excluded sequence was issued")
		}
	}
	if len(seen) != count {
		t.Fatalf("reserved %d unique ids, want %d", len(seen), count)
	}
	loaded, err := store.Load(context.Background(), tenant, "org:north")
	if err != nil || loaded.IssuedCount != count {
		t.Fatalf("load: %+v err=%v", loaded, err)
	}
}
