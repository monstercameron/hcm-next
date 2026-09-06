package partnerapp

import (
	"errors"
	"testing"
	"time"
)

func app004Request(at time.Time, id string) CredentialIssueRequest {
	return CredentialIssueRequest{ID: id, WorkloadIdentity: "workload:app-payroll", Sender: "workload:app-payroll", Destination: "destination:payroll", Purpose: "purpose:payroll", Capabilities: []string{"payroll.read"}, IssuedAt: at, Lifetime: 10 * time.Minute}
}

func TestTodo_APP_004(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	installation := activeInstallationForCredential(t, at)
	manager := NewCredentialManager(func() time.Time { return at.Add(2 * time.Minute) })
	identity, issued, err := manager.Issue(installation, app004Request(at, "credential:app-payroll-1"))
	if err != nil {
		t.Fatal(err)
	}
	if identity.ID == "" || identity.InstallationDigest != installation.Digest || identity.ApplicationDigest != installation.VersionBinding.Digest || issued.Kind != CredentialIssued || issued.EvidenceDigest == "" {
		t.Fatalf("identity=%+v issued=%+v", identity, issued)
	}
	if identity.Capabilities == nil || identity.WorkloadIdentity == "" || identity.Sender == "" || identity.Destination == "" {
		t.Fatalf("identity is not a reference-only bounded coordinate set: %+v", identity)
	}
	checked, err := manager.Check(identity, installation, CredentialUseRequest{Sender: identity.Sender, Destination: identity.Destination, Purpose: identity.Purpose, Capabilities: []string{"payroll.read"}})
	if err != nil || checked.Outcome != "authorized" {
		t.Fatalf("check=%+v err=%v", checked, err)
	}
	rotated, rotation, err := manager.Rotate(identity, installation, app004Request(at, "credential:app-payroll-2"))
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Rotation != 2 || rotation.Kind != CredentialRotated {
		t.Fatalf("rotated=%+v evidence=%+v", rotated, rotation)
	}
	if _, err := manager.Check(identity, installation, CredentialUseRequest{Sender: identity.Sender, Destination: identity.Destination, Purpose: identity.Purpose, Capabilities: identity.Capabilities}); !errors.Is(err, ErrCredentialRevoked) {
		t.Fatalf("old identity after rotation error=%v, want ErrCredentialRevoked", err)
	}
	if err := manager.ValidateAt(rotated, installation, at.Add(3*time.Minute)); err != nil {
		t.Fatalf("rotated identity validation: %v", err)
	}
	if got := len(manager.Events()); got != 4 {
		t.Fatalf("event count=%d, want issue/check/rotate/denial", got)
	}
}

func TestTodo_APP_004_Security(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	installation := activeInstallationForCredential(t, at)
	manager := NewCredentialManager(func() time.Time { return at.Add(time.Minute) })

	tooBroad := app004Request(at, "credential:too-broad")
	tooBroad.Capabilities = []string{"payroll.read", "payroll.write"}
	if _, _, err := manager.Issue(installation, tooBroad); !errors.Is(err, ErrCredentialScope) {
		t.Fatalf("widened capability error=%v, want ErrCredentialScope", err)
	}
	badSender := app004Request(at, "credential:bad-sender")
	badSender.Sender = "workload:other"
	if _, _, err := manager.Issue(installation, badSender); !errors.Is(err, ErrCredentialSender) {
		t.Fatalf("sender error=%v, want ErrCredentialSender", err)
	}
	identity, _, err := manager.Issue(installation, app004Request(at, "credential:security"))
	if err != nil {
		t.Fatal(err)
	}
	tampered := identity
	tampered.Destination = "destination:attacker"
	if _, err := manager.Check(tampered, installation, CredentialUseRequest{Sender: identity.Sender, Destination: identity.Destination, Purpose: identity.Purpose, Capabilities: identity.Capabilities}); !errors.Is(err, ErrCredentialTampered) {
		t.Fatalf("tampered identity error=%v, want ErrCredentialTampered", err)
	}
	if _, err := manager.Check(identity, installation, CredentialUseRequest{Sender: identity.Sender, Destination: "destination:attacker", Purpose: identity.Purpose, Capabilities: identity.Capabilities}); !errors.Is(err, ErrCredentialDestination) {
		t.Fatalf("wrong destination error=%v, want ErrCredentialDestination", err)
	}
	if _, err := manager.Check(identity, installation, CredentialUseRequest{Sender: "workload:other", Destination: identity.Destination, Purpose: identity.Purpose, Capabilities: identity.Capabilities}); !errors.Is(err, ErrCredentialSender) {
		t.Fatalf("wrong sender error=%v, want ErrCredentialSender", err)
	}
	if _, _, err := installation.Revoke("operator-1", "evidence:revoke"); err != nil {
		t.Fatal(err)
	}
	revoked := installation
	revoked, _, _ = installation.Revoke("operator-1", "evidence:revoke")
	if _, err := manager.Check(identity, revoked, CredentialUseRequest{Sender: identity.Sender, Destination: identity.Destination, Purpose: identity.Purpose, Capabilities: identity.Capabilities}); !errors.Is(err, ErrCredentialBinding) {
		t.Fatalf("revoked installation error=%v, want ErrCredentialBinding", err)
	}
	mutated := installation
	mutated.Digest = "tampered"
	if _, err := manager.Check(identity, mutated, CredentialUseRequest{Sender: identity.Sender, Destination: identity.Destination, Purpose: identity.Purpose, Capabilities: identity.Capabilities}); !errors.Is(err, ErrCredentialBinding) {
		t.Fatalf("changed installation error=%v, want ErrCredentialBinding", err)
	}
	if err := manager.ValidateAt(identity, installation, at.Add(11*time.Minute)); !errors.Is(err, ErrCredentialExpired) {
		t.Fatalf("expired identity error=%v, want ErrCredentialExpired", err)
	}
}

func activeInstallationForCredential(t *testing.T, at time.Time) Installation {
	t.Helper()
	version := app003ActiveVersion(t)
	requested, _, err := RequestGrant(version, app003Request())
	if err != nil {
		t.Fatal(err)
	}
	approved, _, err := requested.Approve("approver-1", "evidence:approval-1")
	if err != nil {
		t.Fatal(err)
	}
	review := app003Review(t, version, at)
	active, _, err := approved.Activate(review, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return active
}
