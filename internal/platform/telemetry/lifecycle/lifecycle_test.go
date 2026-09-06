package lifecycle

import (
	"errors"
	"testing"
	"time"
)

var testNow = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

func testCopy(id, tenant string, kind Kind, role CopyRole, until time.Time) Copy {
	return Copy{ID: id, TenantToken: tenant, Kind: kind, Role: role, Backend: "backend-" + string(kind), Region: "us-east", Digest: "digest-" + id, RetainUntil: until, DeleteCapable: true, LastVerified: testNow}
}

func testStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestTelemetryLifecycleAndQueryPolicyPreventsCrossTenantOrExpiredAccess(t *testing.T) {
	store := testStore(t)
	if err := store.Register(testCopy("a-log", "tenant-a", KindLogs, RolePrimary, testNow.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(testCopy("b-log", "tenant-b", KindLogs, RolePrimary, testNow.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Query(Query{TenantToken: "tenant-a", Purpose: "incident", Kind: KindLogs, Region: "us-east", AsOf: testNow}); err != nil || len(got) != 1 || got[0].TenantToken != "tenant-a" {
		t.Fatalf("authorized query got=%v err=%v", got, err)
	}
	if got, err := store.Query(Query{TenantToken: "tenant-a", Purpose: "incident", Kind: KindLogs, Region: "us-east", AsOf: testNow.Add(2 * time.Hour)}); err != nil || len(got) != 0 {
		t.Fatalf("expired query got=%v err=%v", got, err)
	}
	if got, err := store.Query(Query{TenantToken: "tenant-b", Purpose: "incident", Kind: KindLogs, Region: "us-east", AsOf: testNow}); err != nil || len(got) != 1 || got[0].ID != "b-log" {
		t.Fatalf("tenant-b query got=%v err=%v", got, err)
	}
}

func TestTodo_OBS_021_Property(t *testing.T) {
	store := testStore(t)
	if err := store.Register(testCopy("a", "tenant-a", KindMetrics, RolePrimary, testNow.Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Query(Query{TenantToken: "tenant-a", Purpose: "slo", Kind: KindMetrics, Region: "us-east", AsOf: testNow}); err != nil || len(got) != 0 {
		t.Fatalf("expired metric got=%v err=%v", got, err)
	}
}

func TestTodo_OBS_021_Golden(t *testing.T) {
	store := testStore(t)
	if err := store.Register(testCopy("a", "tenant-a", KindTraces, RoleArchive, testNow.Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	store.MarkInventoryComplete("tenant-a")
	receipt := store.Exit("tenant-a", testNow)
	if got := receipt.Explain(); got != "telemetry exit CERTIFIABLE copies=1" {
		t.Fatalf("Explain=%q", got)
	}
}

func TestTodo_OBS_021_Integration(t *testing.T) {
	store := testStore(t)
	if err := store.Register(testCopy("primary", "tenant-a", KindLogs, RolePrimary, testNow.Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(testCopy("index", "tenant-a", KindLogs, RoleIndex, testNow.Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	store.MarkInventoryComplete("tenant-a")
	if receipt := store.Exit("tenant-a", testNow); receipt.Status != "CERTIFIABLE" {
		t.Fatalf("exit receipt=%+v", receipt)
	}
	if got, err := store.Query(Query{TenantToken: "tenant-a", Purpose: "incident", Kind: KindLogs, Region: "us-east", AsOf: testNow}); err != nil || len(got) != 0 {
		t.Fatalf("deleted telemetry was queryable got=%v err=%v", got, err)
	}
}

func TestTodo_OBS_021_Security(t *testing.T) {
	store := testStore(t)
	if _, err := store.Query(Query{TenantToken: "tenant-a", Purpose: "unknown", Kind: KindLogs, Region: "us-east", AsOf: testNow}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("unknown purpose err=%v", err)
	}
	if _, err := store.Query(Query{TenantToken: "tenant-a", Purpose: "incident", Kind: KindLogs, Region: "eu-west", AsOf: testNow}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("unauthorized residency err=%v", err)
	}
	if receipt := store.Exit("tenant-a", testNow); receipt.Status != "BLOCKED" {
		t.Fatalf("incomplete inventory receipt=%+v", receipt)
	}
}

func TestTodo_OBS_021_Recovery(t *testing.T) {
	store := testStore(t)
	copy := testCopy("a", "tenant-a", KindTraces, RoleBackup, testNow.Add(-time.Minute))
	if err := store.Register(copy); err != nil {
		t.Fatal(err)
	}
	store.MarkInventoryComplete("tenant-a")
	if receipt := store.Exit("tenant-a", testNow); receipt.Status != "CERTIFIABLE" {
		t.Fatalf("exit receipt=%+v", receipt)
	}
	restored, err := New(DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Restore(store.Snapshot()); err != nil {
		t.Fatal(err)
	}
	if got, err := restored.Query(Query{TenantToken: "tenant-a", Purpose: "incident", Kind: KindTraces, Region: "us-east", AsOf: testNow}); err != nil || len(got) != 0 {
		t.Fatalf("restore resurrected deleted copy got=%v err=%v", got, err)
	}
}

func TestTodo_OBS_021_Mutation(t *testing.T) {
	store := testStore(t)
	if err := store.Register(testCopy("a", "tenant-a", KindMetrics, RolePrimary, testNow.Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	if err := store.PlaceHold("tenant-a", "a", "incident preservation"); err != nil {
		t.Fatal(err)
	}
	store.MarkInventoryComplete("tenant-a")
	if receipt := store.Exit("tenant-a", testNow); receipt.Status != "BLOCKED" || receipt.Items[0].Outcome != "EXCEPTION" {
		t.Fatalf("hold was not preserved: %+v", receipt)
	}
}
