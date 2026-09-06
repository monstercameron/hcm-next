package database

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func validDatabasePlan() Plan {
	return Plan{
		ClusterID: "pg-cell-a", CellID: "cell-a", TLSRequired: true, HighAvailability: true, ReplicaCount: 3, FailureDomains: 3,
		PoolMin: 2, PoolMax: 20, PoolWait: 5 * time.Second, PITREnabled: true, BackupEncrypted: true, BackupRetention: 30 * 24 * time.Hour,
		MonitoringEnabled: true, RLSEnabled: true, MigrationLockName: "hcmnext-migrations", MigrationLockTimeout: 30 * time.Second,
		Roles: []Role{{Name: "app", CanRead: true, CanWrite: true}, {Name: "migrator", CanMigrate: true, CanRead: true, CanWrite: true}},
	}
}

func TestTodo_IAC_006(t *testing.T) {
	plan := validDatabasePlan()
	if err := Check(plan); err != nil {
		t.Fatal(err)
	}
	decision := AdmitMigration(plan, MigrationFence{LockName: "hcmnext-migrations", WriterID: "writer-a", Epoch: 4})
	if !decision.Allowed || decision.Code != "MIGRATION_ALLOWED" {
		t.Fatalf("migration admission = %+v", decision)
	}
}

func TestTodo_IAC_006_Race(t *testing.T) {
	plan := validDatabasePlan()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Check(plan); err != nil {
				t.Errorf("concurrent validation = %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_IAC_006_Integration(t *testing.T) {
	plan := validDatabasePlan()
	if got := AdmitMigration(plan, MigrationFence{LockName: plan.MigrationLockName, WriterID: "writer-a", Epoch: 2, CurrentOwner: "writer-a", CurrentEpoch: 2}); !got.Allowed {
		t.Fatalf("same fenced writer was denied: %+v", got)
	}
	if err := VerifyRestore(RestoreVerification{LedgerHeadsValid: true, ForeignKeysValid: true, MigrationJournalValid: true, RuntimeLeasesValid: true, RLSValidated: true}); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_IAC_006_Recovery(t *testing.T) {
	plan := validDatabasePlan()
	decision := AdmitMigration(plan, MigrationFence{LockName: plan.MigrationLockName, WriterID: "writer-b", Epoch: 3, CurrentOwner: "writer-a", CurrentEpoch: 3})
	if decision.Allowed || decision.Code != "MIGRATION_LOCK_HELD" {
		t.Fatalf("stale concurrent migration was admitted: %+v", decision)
	}
}

func TestTodo_IAC_006_Mutation(t *testing.T) {
	mutations := []func(*Plan){
		func(p *Plan) { p.PublicAccess = true },
		func(p *Plan) { p.TLSRequired = false },
		func(p *Plan) { p.PITREnabled = false },
		func(p *Plan) { p.RLSEnabled = false },
		func(p *Plan) { p.Roles[0].CanMigrate = true },
	}
	for i, mutate := range mutations {
		plan := validDatabasePlan()
		mutate(&plan)
		if err := Check(plan); err == nil {
			t.Errorf("mutation %d unexpectedly passed", i)
		}
	}
	if err := VerifyRestore(RestoreVerification{}); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete restore verification = %v", err)
	}
}
