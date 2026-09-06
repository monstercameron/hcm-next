package mft

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var mftTime = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func mftManifest(t *testing.T) Manifest {
	t.Helper()
	m, err := NewManifest([]ManifestFile{
		{Path: "in/payroll-001.csv", Content: []byte("worker,amount\n1,10\n")},
		{Path: "in/payroll-002.csv", Content: []byte("worker,amount\n2,20\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func mftDefinition(m Manifest) TransferDefinition {
	return TransferDefinition{
		ID: "payroll-2026-09", Direction: DirectionPickup, EndpointRef: "endpoint:payroll",
		ScheduleRef: "schedule:monthly", FilePattern: "payroll-*.csv",
		ExpectedManifest: ManifestExpectation{Digest: m.Digest, Count: 2, MinFileSize: 1, MaxTotalSize: 1024},
		EncryptionPolicy: "PGP_REQUIRED", IntegrityPolicy: "SHA256_REQUIRED",
	}
}

func TestTodo_INTG_020(t *testing.T) {
	p := NewMemoryPort()
	manifest := mftManifest(t)
	transfer, err := p.Declare(mftDefinition(manifest))
	if err != nil {
		t.Fatal(err)
	}
	if transfer.State != StateDeclared || len(transfer.Events) != 0 {
		t.Fatalf("declaration = %+v", transfer)
	}
	if _, err = p.Stage(transfer.Definition.ID, manifest, mftTime); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Verify(transfer.Definition.ID, manifest, mftTime.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Deliver(transfer.Definition.ID, manifest.Digest, mftTime.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	ack, err := p.Acknowledge(transfer.Definition.ID, Receipt{TransferID: transfer.Definition.ID, ManifestDigest: manifest.Digest, Kind: "PARTNER_ACK", At: mftTime.Add(3 * time.Minute)}, mftTime.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if ack.State != StateAcknowledged || !ack.EventChainValid() || !ack.ReceiptChainValid() {
		t.Fatalf("invalid completed transfer: %+v", ack)
	}
}

func TestTodo_INTG_020_Golden(t *testing.T) {
	m := mftManifest(t)
	if m.Digest != "sha256:7e6cbfed93517a2962593cb4a5d1f693a0ebd38285cf0e05de5ccbb31479e5fa" {
		t.Fatalf("manifest golden digest = %s", m.Digest)
	}
	if m.ContentDigest() != m.Digest {
		t.Fatalf("manifest digest is not canonical: %s", m.Digest)
	}
}

func TestTodo_INTG_020_Integration(t *testing.T) {
	m := mftManifest(t)
	p := NewMemoryPort()
	id := mftDefinition(m)
	id.ID = "integration"
	if _, err := p.Declare(id); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Stage(id.ID, m, mftTime); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Verify(id.ID, m, mftTime); err != nil {
		t.Fatal(err)
	}
	got, err := p.Deliver(id.ID, m.Digest, mftTime)
	if err != nil || got.State != StateDelivered {
		t.Fatalf("delivery = %+v, %v", got, err)
	}
}

func TestTodo_INTG_020_Fault(t *testing.T) {
	m := mftManifest(t)
	p := NewMemoryPort()
	d := mftDefinition(m)
	d.ID = "fault"
	if _, err := p.Declare(d); err != nil {
		t.Fatal(err)
	}
	bad := m
	bad.Files = cloneFiles(m.Files)
	bad.Files[0].Content[0] ^= 1
	if _, err := p.Stage(d.ID, bad, mftTime); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Verify(d.ID, bad, mftTime); !errors.Is(err, ErrManifestMismatch) {
		t.Fatalf("verify error = %v", err)
	}
	got, err := p.Get(d.ID)
	if err != nil || got.State != StateQuarantined || len(got.Events) != 2 {
		t.Fatalf("quarantine = %+v, %v", got, err)
	}
	if _, err := p.Deliver(d.ID, m.Digest, mftTime); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("delivery after quarantine = %v", err)
	}
}

func TestTodo_INTG_020_Recovery(t *testing.T) {
	m := mftManifest(t)
	p := NewMemoryPort()
	d := mftDefinition(m)
	d.ID = "recovery"
	if _, err := p.Declare(d); err != nil {
		t.Fatal(err)
	}
	bad := m
	bad.Digest = "sha256:" + strings.Repeat("0", 64)
	if _, err := p.Stage(d.ID, bad, mftTime); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Verify(d.ID, bad, mftTime); err == nil {
		t.Fatal("bad manifest verified")
	}
	if _, err := p.Stage(d.ID, m, mftTime.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Verify(d.ID, m, mftTime.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Deliver(d.ID, m.Digest, mftTime.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func FuzzTodo_INTG_020(f *testing.F) {
	f.Add("payroll.csv")
	f.Fuzz(func(t *testing.T, name string) {
		m, err := NewManifest([]ManifestFile{{Path: "in/" + name, Content: []byte("x")}})
		if err != nil {
			return
		}
		if err := m.Validate(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestTodo_INTG_020_RedeliveryIsIdempotentByManifestDigest(t *testing.T) {
	m := mftManifest(t)
	p := NewMemoryPort()
	d := mftDefinition(m)
	d.ID = "redelivery"
	if _, err := p.Declare(d); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Stage(d.ID, m, mftTime); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Verify(d.ID, m, mftTime); err != nil {
		t.Fatal(err)
	}
	first, err := p.Deliver(d.ID, m.Digest, mftTime)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Deliver(d.ID, m.Digest, mftTime.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != len(second.Events) {
		t.Fatalf("redelivery appended an event: %d vs %d", len(first.Events), len(second.Events))
	}
}
