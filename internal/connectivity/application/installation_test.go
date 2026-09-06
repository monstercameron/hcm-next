package application

import (
	"errors"
	"testing"
	"time"
)

func testInstallation() Installation {
	at := time.Date(2026, 9, 5, 16, 0, 0, 0, time.UTC)
	return Installation{
		ID: "install-1", TenantID: "tenant-1", CellID: "cell-a", ApplicationID: "app-1", Version: "v1", VersionDigest: "digest-v1", Revision: 1, State: Active,
		Scope:     Scope{TenantID: "tenant-1", OrganizationID: "org-a", Population: "workers", DataClasses: []string{"identity"}, Fields: []string{"worker.name"}, Purpose: "workforce", Capabilities: []string{"workers.read"}},
		TokenRefs: []string{"token:1"}, SubscriptionRefs: []string{"subscription:1"}, EffectRefs: []string{"effect:1"}, CreatedAt: at, UpdatedAt: at,
	}
}

func testManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(5*time.Minute, func() time.Time { return time.Date(2026, 9, 5, 16, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Register(testInstallation()); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestTodo_APP_005 proves exact-scope upgrades and fail-closed controls retain
// immutable history, references, and bounded propagation evidence.
func TestTodo_APP_005(t *testing.T) {
	m := testManager(t)
	at := time.Date(2026, 9, 5, 16, 1, 0, 0, time.UTC)
	upgrade, err := m.Upgrade(UpgradeRequest{InstallationID: "install-1", ApplicationID: "app-1", Version: "v2", VersionDigest: "digest-v2", Scope: testInstallation().Scope, Actor: "operator", Approver: "approver", EvidenceRef: "evidence:upgrade", Reason: "reviewed compatible version", At: at})
	if err != nil {
		t.Fatal(err)
	}
	if upgrade.Current.State != Active || upgrade.Current.Revision != 2 || upgrade.Current.PreviousDigest != upgrade.Previous.Digest || len(m.History("install-1")) != 2 {
		t.Fatalf("upgrade=%+v history=%+v", upgrade, m.History("install-1"))
	}
	controlled, err := m.Quarantine("install-1", "operator", "approver", "provider drift", "evidence:quarantine", at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if controlled.Current.State != Quarantined || controlled.Receipt.Status != "APPLIED" || !controlled.Receipt.WithinSLO {
		t.Fatalf("control=%+v", controlled)
	}
	allowed, reason := m.AuthorizeUse("install-1")
	if allowed || reason != "INSTALLATION_CONTROLLED" {
		t.Fatalf("authorize=%t reason=%s", allowed, reason)
	}
	revoked, err := m.Revoke("install-1", "operator", "approver", "confirmed compromise", "evidence:revoke", at.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Current.State != Revoked || len(revoked.Current.TokenRefs) != 1 || len(revoked.Current.SubscriptionRefs) != 1 || len(revoked.Current.EffectRefs) != 1 || len(m.Receipts("install-1")) != 2 {
		t.Fatalf("revoked=%+v", revoked)
	}
}

func TestTodo_APP_005_Fault(t *testing.T) {
	m := testManager(t)
	request := UpgradeRequest{InstallationID: "install-1", ApplicationID: "app-1", Version: "v2", VersionDigest: "digest-v2", Scope: testInstallation().Scope, Actor: "operator", Approver: "approver", EvidenceRef: "evidence:upgrade", Reason: "upgrade", At: time.Date(2026, 9, 5, 16, 1, 0, 0, time.UTC)}
	request.Scope.Capabilities = []string{"workers.read", "workers.write"}
	if _, err := m.Upgrade(request); !errors.Is(err, ErrScopeExpansion) {
		t.Fatalf("scope expansion error=%v", err)
	}
	if len(m.History("install-1")) != 1 {
		t.Fatal("failed upgrade changed history")
	}
	if _, err := m.Revoke("install-1", "operator", "approver", "reason", "evidence:revoked", request.At); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Revoke("install-1", "operator", "approver", "again", "evidence:revoked2", request.At); !errors.Is(err, ErrLifecycleTransition) {
		t.Fatalf("second revoke error=%v", err)
	}
}

func TestTodo_APP_005_Security(t *testing.T) {
	m := testManager(t)
	request := UpgradeRequest{InstallationID: "install-1", ApplicationID: "app-1", Version: "v2", VersionDigest: "digest-v2", Scope: testInstallation().Scope, Actor: "operator", Approver: "operator", EvidenceRef: "evidence:upgrade", Reason: "upgrade", At: time.Date(2026, 9, 5, 16, 1, 0, 0, time.UTC)}
	if _, err := m.Upgrade(request); !errors.Is(err, ErrUpgradeRejected) {
		t.Fatalf("self-approved upgrade error=%v", err)
	}
	if _, err := m.Quarantine("install-1", "operator", "approver", "reason", "", request.At); !errors.Is(err, ErrEvidenceRequired) {
		t.Fatalf("missing evidence error=%v", err)
	}
	if ok, reason := m.AuthorizeUse("missing"); ok || reason != "INSTALLATION_NOT_FOUND" {
		t.Fatalf("unknown installation authorize=%t reason=%s", ok, reason)
	}
}

func TestTodo_APP_005_Mutation(t *testing.T) {
	m := testManager(t)
	in, ok := m.Current("install-1")
	if !ok {
		t.Fatal("installation missing")
	}
	in.Scope.Fields[0] = "worker.secret"
	current, _ := m.Current("install-1")
	if current.Scope.Fields[0] != "worker.name" || current.Digest != in.Digest {
		t.Fatalf("defensive current copy or digest failure current=%+v mutated=%+v", current, in)
	}
	if len(m.Events("install-1")) != 1 || m.Events("install-1")[0].Digest == "" {
		t.Fatal("initial lifecycle evidence missing")
	}
}
