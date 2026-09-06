package lease_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/custody"
	"github.com/monstercameron/hcm-next/internal/trust/lease"
)

func machineManager(t *testing.T, now *time.Time) *lease.MachineManager {
	t.Helper()
	port := &machinePort{now: now}
	m, err := lease.NewMachineManager(port, func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	return m
}

type machinePort struct {
	now  *time.Time
	next int
}

func (p *machinePort) IssueLease(_ custody.Context, h custody.Handle, op custody.Operation, ttl time.Duration) (custody.Lease, error) {
	p.next++
	return custody.Lease{ID: "machine-custody-lease", Handle: h, Operation: op, ExpiresAt: p.now.Add(ttl)}, nil
}

func machineRequest() lease.MachineRequest {
	h := custody.Handle{ID: "machine-secret", Kind: custody.Secret, Version: "v1", Tenant: "tenant-a", Region: "us-east"}
	return lease.MachineRequest{Handle: h, Workload: "workload-a", Tenant: "tenant-a", Purpose: "dispatch", Destination: "service.example", Operation: custody.Encrypt, TTL: time.Minute}
}

func TestTodo_TRUST_029(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	m := machineManager(t, &now)
	ml, ev, err := m.Mint(machineRequest())
	if err != nil {
		t.Fatal(err)
	}
	if ev.Epoch != 1 || ml.RevocationEpoch != 1 || ml.WorkloadIdentity != "workload-a" {
		t.Fatalf("mint = %+v, evidence=%+v", ml, ev)
	}
	if _, err := m.Use(ml, ml.Lease.Destination, ml.Lease.Operation); err != nil {
		t.Fatal(err)
	}
	ml2, _, err := m.Mint(machineRequest())
	if err != nil {
		t.Fatal(err)
	}
	event, evidence, err := m.BumpEpoch("tenant-a", "workload compromise")
	if err != nil || event.Epoch != 2 || event.Digest == "" || evidence.EpochDigest != event.Digest {
		t.Fatalf("bump = %+v, %+v, %v", event, evidence, err)
	}
	if _, err := m.Use(ml2, ml2.Lease.Destination, ml2.Lease.Operation); !errors.Is(err, lease.ErrEpochStale) {
		t.Fatalf("old epoch use = %v, want ErrEpochStale", err)
	}
	if got := m.CurrentEpoch("tenant-a"); got != 2 {
		t.Fatalf("CurrentEpoch = %d, want 2", got)
	}
}

func TestTodo_TRUST_029_Security(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	m := machineManager(t, &now)
	ml, _, err := m.Mint(machineRequest())
	if err != nil {
		t.Fatal(err)
	}
	tampered := ml
	tampered.WorkloadIdentity = "other-workload"
	if _, err := m.Use(tampered, ml.Lease.Destination, ml.Lease.Operation); !errors.Is(err, lease.ErrTampered) {
		t.Fatalf("identity tamper = %v", err)
	}
	if _, err := m.Use(ml, "other.example", ml.Lease.Operation); !errors.Is(err, lease.ErrDestinationMismatch) {
		t.Fatalf("destination mismatch = %v", err)
	}
	if _, err := m.Use(ml, ml.Lease.Destination, custody.Decrypt); !errors.Is(err, lease.ErrOperationNotPermitted) {
		t.Fatalf("operation mismatch = %v", err)
	}
	if _, err := m.Revoke(ml.Lease.ID, "operator revoke"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Use(ml, ml.Lease.Destination, ml.Lease.Operation); !errors.Is(err, lease.ErrRevoked) {
		t.Fatalf("revoked lease use = %v, want ErrRevoked", err)
	}
}

func TestTodo_TRUST_029_Mutation(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	m := machineManager(t, &now)
	if _, _, err := m.BumpEpoch("tenant-a", "first"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.BumpEpoch("tenant-a", "second"); err != nil {
		t.Fatal(err)
	}
	events := m.EpochEvents()
	if len(events) != 2 || events[1].PreviousDigest != events[0].Digest || events[1].Epoch != events[0].Epoch+1 {
		t.Fatalf("epoch chain = %+v", events)
	}
	if _, _, err := m.BumpEpoch(" ", "bad"); !errors.Is(err, lease.ErrInvalidEpoch) {
		t.Fatalf("invalid epoch request = %v", err)
	}
}

func TestTodo_TRUST_029_Race(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	m := machineManager(t, &now)
	var wg sync.WaitGroup
	wg.Add(8)
	for i := 0; i < 8; i++ {
		go func() {
			defer wg.Done()
			_, _, _ = m.BumpEpoch("tenant-a", "concurrent")
		}()
	}
	wg.Wait()
	if m.CurrentEpoch("tenant-a") == 0 {
		t.Fatal("concurrent epoch bumps did not record an epoch")
	}
}

func FuzzTodo_TRUST_029(f *testing.F) {
	f.Add("tenant-a", "workload-a")
	f.Fuzz(func(t *testing.T, tenant, workload string) {
		now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
		m := machineManager(t, &now)
		req := machineRequest()
		req.Tenant, req.Workload = tenant, workload
		req.Handle.Tenant = tenant
		_, _, _ = m.Mint(req)
	})
}

func TestMachineLease_PublicAPIs_BoundariesAliasesAndCopies(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	if _, err := lease.NewMachineManager(nil, nil); !errors.Is(err, lease.ErrInvalidRequest) {
		t.Fatalf("nil port err=%v", err)
	}
	m := machineManager(t, &now)
	if got := m.CurrentEpoch("unknown"); got != 0 {
		t.Fatalf("unknown epoch=%d, want 0", got)
	}
	for name, pair := range map[string][2]string{
		"blank tenant": {"", "reason"}, "padded tenant": {" tenant-a", "reason"}, "blank reason": {"tenant-a", ""},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := m.BumpEpoch(pair[0], pair[1]); !errors.Is(err, lease.ErrInvalidEpoch) {
				t.Fatalf("BumpEpoch err=%v", err)
			}
		})
	}
	if event, _, err := m.BumpEpoch("tenant-a", " reason"); err != nil || event.Reason != " reason" {
		t.Fatalf("padded reason event=%+v err=%v", event, err)
	}
	var nilManager *lease.MachineManager
	if _, err := nilManager.Use(lease.MachineCredentialLease{}, "destination", custody.Encrypt); !errors.Is(err, lease.ErrMachineUnknown) {
		t.Fatalf("nil Use err=%v", err)
	}
	if _, err := nilManager.Revoke("id", "reason"); !errors.Is(err, lease.ErrMachineUnknown) {
		t.Fatalf("nil Revoke err=%v", err)
	}
	if _, err := m.Use(lease.MachineCredentialLease{Lease: lease.CredentialLease{ID: "forged"}}, "destination", custody.Encrypt); !errors.Is(err, lease.ErrMachineUnknown) {
		t.Fatalf("unknown Use err=%v", err)
	}
	badReq := machineRequest()
	badReq.Workload = " "
	if _, _, err := m.Mint(badReq); !errors.Is(err, lease.ErrInvalidRequest) {
		t.Fatalf("blank workload err=%v", err)
	}
	ml, _, err := m.Mint(machineRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.RevokeLease(ml, "alias revoke"); err != nil {
		t.Fatalf("RevokeLease: %v", err)
	}
	if _, err := m.Use(ml, ml.Lease.Destination, ml.Lease.Operation); !errors.Is(err, lease.ErrRevoked) {
		t.Fatalf("revoked alias lease err=%v", err)
	}
	if _, err := m.Revoke("missing", "reason"); !errors.Is(err, lease.ErrMachineUnknown) {
		t.Fatalf("unknown revoke err=%v", err)
	}
	if len(m.Events()) < 3 {
		t.Fatalf("machine evidence events=%d, want mint/revoke/use", len(m.Events()))
	}
	events := m.Events()
	events[0].Reason = "mutated"
	if m.Events()[0].Reason == "mutated" {
		t.Fatal("Events did not return a copy")
	}
	epoch, _, err := m.BumpEpoch("tenant-a", "rotate")
	if err != nil {
		t.Fatal(err)
	}
	epochs := m.EpochEvents()
	if len(epochs) == 0 || epochs[len(epochs)-1].Digest != epoch.Digest {
		t.Fatalf("epoch events=%+v", epochs)
	}
	epochs[0].Reason = "mutated"
	if m.EpochEvents()[0].Reason == "mutated" {
		t.Fatal("EpochEvents did not return a copy")
	}
}
