package seed_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/aggregates"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
)

// TestTodo_DB_019_Conformance proves the re-point actually landed: the
// worker fixtures Seed loads through aggregates.LoadFixtures read back
// through aggregates.PeopleStore.CurrentWorker, and the pay-band fixtures
// read back through aggregates.CompensationStore.CurrentCompensationBand --
// the same adapter every other data-plane caller uses, not a
// definition_version body only this package knows how to interpret.
func TestTodo_DB_019_Conformance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "seed-conformance-tenant")

	summary := runSeed(t, ctx, db, tenantID)
	if summary.Aggregates == nil {
		t.Fatal("seed did not report the aggregate fixtures it loaded")
	}
	if len(summary.Aggregates.WorkerID) == 0 {
		t.Fatal("seed loaded no workers")
	}
	if len(summary.Aggregates.BandID) == 0 {
		t.Fatal("seed loaded no compensation bands")
	}

	// Safely after every fixture record's effective_from (see
	// internal/data/seed/testdata/workers.json) and after the pay-band
	// catalog's own load-time effective_from (aggregates.LoadFixtures stamps
	// every band with time.Now() when it runs, inside this test's own
	// runSeed call above).
	businessAt := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

	people := aggregates.PeopleStore{}
	for key, workerID := range summary.Aggregates.WorkerID {
		worker, err := people.CurrentWorker(ctx, db.Conn, tenantID, workerID, businessAt)
		if err != nil {
			t.Fatalf("CurrentWorker %s (%s): %v", key, workerID, err)
		}
		wantPersonRef, ok := summary.Aggregates.PersonID[key]
		if !ok {
			t.Fatalf("worker %s has no matching person id in the loaded fixtures", key)
		}
		if worker.PersonRef != wantPersonRef {
			t.Fatalf("worker %s person_ref = %s, want %s", key, worker.PersonRef, wantPersonRef)
		}
		if worker.WorkerNumber == "" {
			t.Fatalf("worker %s read back with an empty worker_number", key)
		}
	}

	comp := aggregates.CompensationStore{}
	for bandKey, bandID := range summary.Aggregates.BandID {
		band, err := comp.CurrentCompensationBand(ctx, db.Conn, tenantID, bandID, businessAt)
		if err != nil {
			t.Fatalf("CurrentCompensationBand %s (%s): %v", bandKey, bandID, err)
		}
		if band.JobCode == "" {
			t.Fatalf("band %s read back with an empty job_code", bandKey)
		}
		if band.Minimum == "" || band.Midpoint == "" || band.Maximum == "" {
			t.Fatalf("band %s read back with an incomplete range (%s/%s/%s)",
				bandKey, band.Minimum, band.Midpoint, band.Maximum)
		}
	}
}
