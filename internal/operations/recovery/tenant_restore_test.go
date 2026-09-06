package recovery

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func tenantRestoreRequest() TenantRestoreRequest {
	return TenantRestoreRequest{
		TenantID: "tenant-a", RecoveryPoint: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), RestoredAt: time.Date(2026, 1, 1, 0, 4, 0, 0, time.UTC),
		Rows:       []RestoredRow{{ID: "ledger-1", TenantID: "tenant-a", Plane: "ledger", Digest: "sha256:ledger"}, {ID: "worker-1", TenantID: "tenant-a", Plane: "people", Digest: "sha256:worker"}},
		DeletedIDs: []string{"deleted-1"}, HeldIDs: []string{"worker-1"}, LedgerHead: "ledger:42", MigrationJournalDigest: "sha256:migrations", RuntimeLeaseEpoch: 7,
		Conformance: RestoreConformance{LedgerHeadsValid: true, ForeignKeysValid: true, RuntimeLeasesValid: true, HoldsApplied: true, DeletionsApplied: true, HashesValid: true}, Isolated: true,
	}
}

func TestTodo_DB_022(t *testing.T) {
	receipt, err := RestoreTenant(tenantRestoreRequest())
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != RestoreReady || receipt.Fenced || receipt.RowCount != 2 || receipt.HeldCount != 1 || receipt.DeletedCount != 1 {
		t.Fatalf("restore receipt = %+v, want ready and exact counts", receipt)
	}
	if receipt.RPO != 4*time.Minute || !strings.HasPrefix(receipt.DataDigest, "sha256:") || !strings.HasPrefix(receipt.ManifestDigest, "sha256:") {
		t.Fatalf("restore evidence = %+v", receipt)
	}
}

func TestTodo_DB_022_Golden(t *testing.T) {
	first, err := RestoreTenant(tenantRestoreRequest())
	if err != nil {
		t.Fatal(err)
	}
	second, err := RestoreTenant(tenantRestoreRequest())
	if err != nil || first.DataDigest != second.DataDigest || first.ManifestDigest != second.ManifestDigest {
		t.Fatalf("restore digest is not deterministic: first=%+v second=%+v err=%v", first, second, err)
	}
}

func TestTodo_DB_022_Race(t *testing.T) {
	request := tenantRestoreRequest()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, err := RestoreTenant(request)
			if err != nil || receipt.Status != RestoreReady {
				t.Errorf("concurrent restore = %+v, err=%v", receipt, err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_DB_022_Integration(t *testing.T) {
	set, publicKey, encryptionKey := testBackup(t)
	if _, verification, err := Restore(set, publicKey, encryptionKey, VerifyOptions{Now: fixedNow()}); err != nil || !verification.Verified() {
		t.Fatalf("encrypted backup restore = %+v, err=%v", verification, err)
	}
	receipt, err := RestoreTenant(tenantRestoreRequest())
	if err != nil || receipt.LedgerHead != "ledger:42" || receipt.MigrationJournalDigest == "" {
		t.Fatalf("tenant restore did not retain recovery evidence: %+v, err=%v", receipt, err)
	}
}

func TestTodo_DB_022_Fault(t *testing.T) {
	request := tenantRestoreRequest()
	request.Conformance.HashesValid = false
	receipt, err := RestoreTenant(request)
	if err != nil || receipt.Status != RestoreFenced || !receipt.Fenced {
		t.Fatalf("failed conformance must stay fenced: %+v, err=%v", receipt, err)
	}
	request = tenantRestoreRequest()
	request.Isolated = false
	if _, err := RestoreTenant(request); !errors.Is(err, ErrInvalidTenantRestore) {
		t.Fatalf("non-isolated restore error = %v", err)
	}
}

func TestTodo_DB_022_Security(t *testing.T) {
	request := tenantRestoreRequest()
	request.Rows[0].TenantID = "tenant-b"
	if _, err := RestoreTenant(request); !errors.Is(err, ErrCrossTenantRestore) {
		t.Fatalf("foreign tenant error = %v", err)
	}
	request = tenantRestoreRequest()
	request.DeletedIDs = append(request.DeletedIDs, "ledger-1")
	if _, err := RestoreTenant(request); !errors.Is(err, ErrRestrictedRestore) {
		t.Fatalf("deleted row resurrection error = %v", err)
	}
}

func TestTodo_DB_022_Conformance(t *testing.T) {
	request := tenantRestoreRequest()
	request.Conformance = RestoreConformance{}
	receipt, err := RestoreTenant(request)
	if err != nil || receipt.Status != RestoreFenced || !receipt.Fenced {
		t.Fatalf("incomplete conformance escaped fence: %+v, err=%v", receipt, err)
	}
}

func TestTodo_DB_022_Recovery(t *testing.T) {
	request := tenantRestoreRequest()
	request.RestoredAt = request.RecoveryPoint.Add(15 * time.Minute)
	receipt, err := RestoreTenant(request)
	if err != nil || receipt.RPO != 15*time.Minute || receipt.RuntimeLeaseEpoch != 7 {
		t.Fatalf("recovery point evidence = %+v, err=%v", receipt, err)
	}
}

func TestTodo_DB_022_Mutation(t *testing.T) {
	first, err := RestoreTenant(tenantRestoreRequest())
	if err != nil {
		t.Fatal(err)
	}
	request := tenantRestoreRequest()
	request.Rows[0].Digest = "sha256:changed"
	second, err := RestoreTenant(request)
	if err != nil || first.DataDigest == second.DataDigest {
		t.Fatalf("row mutation was not reflected in digest: first=%+v second=%+v err=%v", first, second, err)
	}
}
