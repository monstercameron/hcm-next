package operation_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/connectivity/operation"
	"github.com/monstercameron/hcm-next/internal/trust/custody"
	"github.com/monstercameron/hcm-next/internal/trust/lease"
)

func TestTodo_CONN_RT_003(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	queued(t, j, id)
	first, err := j.Lease(context.Background(), operation.LeaseRequest{TenantID: "tenant-promotion", OperationID: id, WorkerID: "worker-1", At: testNow, Duration: time.Minute, Revalidate: confirmed})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Lease(context.Background(), operation.LeaseRequest{TenantID: "tenant-promotion", OperationID: id, WorkerID: "worker-2", At: testNow.Add(10 * time.Second), Duration: time.Minute, Revalidate: confirmed}); !errors.Is(err, operation.ErrLeaseFenced) {
		t.Fatalf("duplicate claim = %v, want ErrLeaseFenced", err)
	}
	writer := operation.NewPayrollSync()
	if _, err := j.Dispatch(context.Background(), first, writer); err != nil {
		t.Fatal(err)
	}
	op, err := j.Get(context.Background(), "tenant-promotion", id)
	if err != nil || len(op.Attempts) != 1 || len(writer.Calls()) != 1 || op.State != operation.StateProviderAccepted {
		t.Fatalf("operation = %+v, calls=%d, err=%v", op, len(writer.Calls()), err)
	}
	entries, err := j.Journal(context.Background(), "tenant-promotion", id)
	if err != nil || len(entries) < 5 {
		t.Fatalf("journal entries = %d, err=%v", len(entries), err)
	}
	if err := j.VerifyJournal(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_CONN_RT_003_Integration(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	queued(t, j, id)
	first := leaseFor(t, j, id)
	if _, err := j.Lease(context.Background(), operation.LeaseRequest{TenantID: "tenant-promotion", OperationID: id, WorkerID: "worker-2", At: testNow.Add(2 * time.Minute), Duration: time.Minute, Revalidate: confirmed}); err != nil {
		t.Fatalf("expired lease was not reclaimed: %v", err)
	}
	if _, err := j.Dispatch(context.Background(), first, operation.NewPayrollSync()); !errors.Is(err, operation.ErrLeaseExpired) && !errors.Is(err, operation.ErrLeaseFenced) {
		t.Fatalf("stale lease dispatch = %v", err)
	}
}

func TestTodo_CONN_RT_003_Fault(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	queued(t, j, id)
	claim := leaseFor(t, j, id)
	started := make(chan struct{})
	release := make(chan struct{})
	writer := blockingWriter{started: started, release: release}
	done := make(chan error, 1)
	go func() {
		_, err := j.Dispatch(context.Background(), claim, writer)
		done <- err
	}()
	<-started
	recovered, err := j.Recover(context.Background(), testNow.Add(time.Minute))
	if err != nil || len(recovered) != 1 || recovered[0].State != operation.StateAmbiguous {
		t.Fatalf("recovery = %+v, err=%v", recovered, err)
	}
	close(release)
	if err := <-done; !errors.Is(err, operation.ErrLeaseFenced) {
		t.Fatalf("crashed send completion = %v", err)
	}
	op, _ := j.Get(context.Background(), "tenant-promotion", id)
	if op.State != operation.StateAmbiguous || len(op.Attempts) != 1 {
		t.Fatalf("recovered operation = %+v", op)
	}
}

func TestTodo_CONN_RT_003_Security(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	entries, err := j.Journal(context.Background(), "tenant-promotion", id)
	if err != nil || len(entries) != 1 {
		t.Fatalf("operation journal = %+v, err=%v", entries, err)
	}
	if entries[0].RequestDigest != "" || entries[0].CredentialLeaseID != "" {
		t.Fatal("journal unexpectedly retained sensitive dispatch material")
	}
	if _, err := j.Journal(context.Background(), "tenant-promotion", uuid.New()); !errors.Is(err, operation.ErrNotFound) {
		t.Fatalf("foreign journal read = %v", err)
	}
}

func TestTodo_CONN_RT_003_Mutation(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	entries, _ := j.Journal(context.Background(), "tenant-promotion", id)
	entries[0].Event = "TAMPERED"
	check, _ := j.Journal(context.Background(), "tenant-promotion", id)
	if check[0].Event == "TAMPERED" {
		t.Fatal("journal returned mutable backing storage")
	}
}

func FuzzTodo_CONN_RT_003(f *testing.F) {
	f.Add("worker:w-1", "resource:w-1")
	f.Fuzz(func(t *testing.T, worker, resource string) {
		_ = operation.Explain(operation.Operation{ExternalResourceKey: resource, State: operation.StateQueued})
		_ = worker
	})
}

func TestTodo_CONN_RT_003_Race(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	queued(t, j, id)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = j.Get(context.Background(), "tenant-promotion", id)
			_, _ = j.Journal(context.Background(), "tenant-promotion", id)
			_ = j.VerifyJournal(context.Background())
		}()
	}
	wg.Wait()
}

func TestTodo_CONN_RT_004(t *testing.T) {
	now := testNow
	fake := custody.NewInMemoryFake(func() time.Time { return now })
	handle := custody.Handle{ID: "payroll-secret", Kind: custody.Secret, Version: "v1", Tenant: "tenant-promotion", Region: "us-east-1"}
	if err := fake.Register(handle); err != nil {
		t.Fatal(err)
	}
	machine, err := lease.NewMachineManager(runtimePort{now: now}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	credential, _, err := machine.Mint(lease.MachineRequest{Handle: handle, Workload: "connector-worker", Tenant: "tenant-promotion", Purpose: "PAYROLL_SYNC", Destination: "payroll.example", Operation: custody.Encrypt, TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	queued(t, j, id)
	claim := leaseFor(t, j, id)
	writer := &credentialWriter{}
	result, err := j.DispatchWithCredential(context.Background(), operation.CredentialDispatchRequest{Lease: claim, Credential: credential, CredentialOperation: custody.Encrypt, Writer: writer}, machine)
	if err != nil || !result.ProviderCall || writer.calls != 1 || writer.lease.Lease.Lease.ID != credential.Lease.ID {
		t.Fatalf("credential dispatch = %+v, calls=%d, err=%v", result, writer.calls, err)
	}
	serialized, err := json.Marshal(writer.lease.Lease)
	if err != nil || strings.Contains(string(serialized), "raw-secret") || strings.Contains(string(serialized), "secret-value") {
		t.Fatalf("credential adapter received secret material: %s", serialized)
	}
	if !strings.Contains(string(serialized), credential.Lease.ID) {
		t.Fatalf("serialized lease omitted its reference identity: %s", serialized)
	}
	entries, _ := j.Journal(context.Background(), "tenant-promotion", id)
	found := false
	for _, entry := range entries {
		if entry.Event == "CREDENTIAL_BOUND" {
			found = entry.CredentialLeaseID == credential.Lease.ID
		}
	}
	if !found {
		t.Fatalf("credential binding missing from journal: %+v", entries)
	}
}

func TestTodo_CONN_RT_004_Integration(t *testing.T) {
	now := testNow
	fake := custody.NewInMemoryFake(func() time.Time { return now })
	handle := custody.Handle{ID: "payroll-secret", Kind: custody.Secret, Version: "v1", Tenant: "tenant-promotion", Region: "us-east-1"}
	if err := fake.Register(handle); err != nil {
		t.Fatal(err)
	}
	machine, _ := lease.NewMachineManager(runtimePort{now: now}, func() time.Time { return now })
	credential, _, _ := machine.Mint(lease.MachineRequest{Handle: handle, Workload: "connector-worker", Tenant: "tenant-promotion", Purpose: "PAYROLL_SYNC", Destination: "payroll.example", Operation: custody.Encrypt, TTL: time.Minute})
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	queued(t, j, id)
	claim := leaseFor(t, j, id)
	now = now.Add(2 * time.Minute)
	_, err := j.DispatchWithCredential(context.Background(), operation.CredentialDispatchRequest{Lease: claim, Credential: credential, CredentialOperation: custody.Encrypt, Writer: &credentialWriter{}}, machine)
	var rejected *operation.CredentialRejection
	if !errors.As(err, &rejected) || rejected.Field != "expiry" {
		t.Fatalf("expired credential = %v, rejection=%+v", err, rejected)
	}
	op, _ := j.Get(context.Background(), "tenant-promotion", id)
	if op.State != operation.StateLeased || len(op.Attempts) != 0 {
		t.Fatalf("expired credential mutated operation = %+v", op)
	}
}

func TestTodo_CONN_RT_004_Fault(t *testing.T) {
	// A provider timeout is ambiguous and remains fenced from a second send,
	// even when a fresh credential is presented.
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	queued(t, j, id)
	claim := leaseFor(t, j, id)
	writer := &credentialWriter{err: &operation.ProviderError{Result: operation.ResponseAmbiguous, MayHaveSent: true, Cause: context.DeadlineExceeded}}
	valid := testCredential(t, testNow)
	_, _ = j.DispatchWithCredential(context.Background(), operation.CredentialDispatchRequest{Lease: claim, Credential: valid.credential, CredentialOperation: custody.Encrypt, Writer: writer}, valid.manager)
	op, _ := j.Get(context.Background(), "tenant-promotion", id)
	if op.State != operation.StateAmbiguous {
		t.Fatalf("timeout state = %s", op.State)
	}
	claim2 := operation.Lease{TenantID: "tenant-promotion", OperationID: id, Token: uuid.New(), FenceToken: op.FenceToken, WorkerID: "worker-2", ExpiresAt: testNow.Add(time.Minute)}
	_, err := j.DispatchWithCredential(context.Background(), operation.CredentialDispatchRequest{Lease: claim2, Credential: valid.credential, CredentialOperation: custody.Encrypt, Writer: writer}, valid.manager)
	var rejected *operation.CredentialRejection
	if !errors.As(err, &rejected) || rejected.Field != "state" || writer.calls != 1 {
		t.Fatalf("ambiguous redelivery = %v rejection=%+v calls=%d", err, rejected, writer.calls)
	}
}

func TestTodo_CONN_RT_004_Security(t *testing.T) {
	valid := testCredential(t, testNow)
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	queued(t, j, id)
	claim := leaseFor(t, j, id)
	wrong := valid.credential
	wrong.Lease.Destination = "other.example"
	_, err := j.DispatchWithCredential(context.Background(), operation.CredentialDispatchRequest{Lease: claim, Credential: wrong, CredentialOperation: custody.Encrypt, Writer: &credentialWriter{}}, valid.manager)
	var rejected *operation.CredentialRejection
	if !errors.As(err, &rejected) || rejected.Field != "destination" {
		t.Fatalf("wrong destination = %v, rejection=%+v", err, rejected)
	}
	if op, _ := j.Get(context.Background(), "tenant-promotion", id); len(op.Attempts) != 0 {
		t.Fatal("wrong credential produced an attempt")
	}
}

func TestTodo_CONN_RT_004_Mutation(t *testing.T) {
	valid := testCredential(t, testNow)
	valid.credential.Lease.Nonce = "tampered"
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	queued(t, j, id)
	claim := leaseFor(t, j, id)
	_, err := j.DispatchWithCredential(context.Background(), operation.CredentialDispatchRequest{Lease: claim, Credential: valid.credential, CredentialOperation: custody.Encrypt, Writer: &credentialWriter{}}, valid.manager)
	var rejected *operation.CredentialRejection
	if !errors.As(err, &rejected) || rejected.Field != "lease" {
		t.Fatalf("tampered credential = %v, rejection=%+v", err, rejected)
	}
}

func FuzzTodo_CONN_RT_004(f *testing.F) {
	f.Add("destination", "operation")
	f.Fuzz(func(t *testing.T, destination, operationName string) {
		_ = operation.Explain(operation.Operation{DestinationRef: destination, SemanticOperation: operationName})
	})
}

func TestTodo_CONN_RT_004_Race(t *testing.T) {
	valid := testCredential(t, testNow)
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	queued(t, j, id)
	claim := leaseFor(t, j, id)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = j.Journal(context.Background(), "tenant-promotion", id)
			_, _ = j.Get(context.Background(), "tenant-promotion", id)
		}()
	}
	wg.Wait()
	_, _ = j.DispatchWithCredential(context.Background(), operation.CredentialDispatchRequest{Lease: claim, Credential: valid.credential, CredentialOperation: custody.Encrypt, Writer: &credentialWriter{}}, valid.manager)
}

func TestTodo_CONN_RT_004_MutationIdempotency(t *testing.T) {
	j := newJournal()
	req := request(uuid.New(), "worker:w-1", 1, operation.OrderingStrict)
	if _, err := j.Plan(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Plan(context.Background(), req); !errors.Is(err, operation.ErrDuplicate) {
		t.Fatalf("duplicate idempotency key = %v", err)
	}
}

type blockingWriter struct {
	started chan struct{}
	release chan struct{}
}

func (w blockingWriter) Write(context.Context, operation.WriteRequest) (operation.WriteResponse, error) {
	close(w.started)
	<-w.release
	return operation.WriteResponse{Result: operation.ResponseSuccess}, nil
}

type credentialWriter struct {
	lease operationLease
	calls int
	err   error
}

type operationLease struct {
	Lease lease.MachineCredentialLease
}

func (w *credentialWriter) WriteWithCredential(_ context.Context, _ operation.WriteRequest, got lease.MachineCredentialLease) (operation.WriteResponse, error) {
	w.calls++
	w.lease = operationLease{Lease: got}
	if w.err != nil {
		return operation.WriteResponse{}, w.err
	}
	return operation.WriteResponse{Result: operation.ResponseSuccess}, nil
}

type testCredentialResult struct {
	credential lease.MachineCredentialLease
	manager    *lease.MachineManager
}

func testCredential(t *testing.T, now time.Time) testCredentialResult {
	t.Helper()
	fake := custody.NewInMemoryFake(func() time.Time { return now })
	handle := custody.Handle{ID: "payroll-secret", Kind: custody.Secret, Version: "v1", Tenant: "tenant-promotion", Region: "us-east-1"}
	if err := fake.Register(handle); err != nil {
		t.Fatal(err)
	}
	manager, err := lease.NewMachineManager(runtimePort{now: now}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	credential, _, err := manager.Mint(lease.MachineRequest{Handle: handle, Workload: "connector-worker", Tenant: "tenant-promotion", Purpose: "PAYROLL_SYNC", Destination: "payroll.example", Operation: custody.Encrypt, TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return testCredentialResult{credential: credential, manager: manager}
}

type runtimePort struct{ now time.Time }

func (p runtimePort) IssueLease(_ custody.Context, handle custody.Handle, op custody.Operation, ttl time.Duration) (custody.Lease, error) {
	return custody.Lease{ID: "custody-" + handle.ID, Handle: handle, Operation: op, ExpiresAt: p.now.Add(ttl)}, nil
}
