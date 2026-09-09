package operation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
)

func TestOperation_IDLookupsRefuseCrossTenantAccess(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:tenant", 1, operation.OrderingStrict)
	if _, err := j.Get(context.Background(), "tenant-other", id); !errors.Is(err, operation.ErrNotFound) {
		t.Fatalf("cross-tenant get err = %v, want ErrNotFound", err)
	}
	if _, err := j.Queue(context.Background(), "tenant-other", id); !errors.Is(err, operation.ErrNotFound) {
		t.Fatalf("cross-tenant queue err = %v, want ErrNotFound", err)
	}
	if _, err := j.Lease(context.Background(), operation.LeaseRequest{TenantID: "tenant-other", OperationID: id, WorkerID: "worker", At: testNow, Duration: time.Minute, Revalidate: confirmed}); !errors.Is(err, operation.ErrNotFound) {
		t.Fatalf("cross-tenant lease err = %v, want ErrNotFound", err)
	}
	if _, err := j.Dispatch(context.Background(), operation.Lease{TenantID: "tenant-other", OperationID: id, Token: uuid.New(), FenceToken: 1, WorkerID: "worker", ExpiresAt: testNow.Add(time.Minute)}, operation.NewPayrollSync()); !errors.Is(err, operation.ErrLeaseFenced) {
		t.Fatalf("cross-tenant dispatch err = %v, want ErrLeaseFenced", err)
	}
	if _, err := j.RecordObservation(context.Background(), operation.Observation{TenantID: "tenant-other", ObservationID: uuid.New(), OperationID: id, ExternalResourceKey: "worker:tenant", Verdict: operation.ObservationApplied, ObservedAt: testNow}); !errors.Is(err, operation.ErrNotFound) {
		t.Fatalf("cross-tenant observation err = %v, want ErrNotFound", err)
	}
	redrive := operation.RedriveRequest{TenantID: "tenant-other", OperationID: id, ActorRef: "operator", CurrentMappingProfileVersion: "map", CurrentMappedPayloadDigest: "sha256:mapped", CurrentAuthorityPolicyFingerprint: "authz-v1", CurrentWriterFenceEpoch: 1}
	if _, err := j.PreviewRedrive(context.Background(), redrive); !errors.Is(err, operation.ErrNotFound) {
		t.Fatalf("cross-tenant preview err = %v, want ErrNotFound", err)
	}
	if _, err := j.Redrive(context.Background(), redrive); !errors.Is(err, operation.ErrNotFound) {
		t.Fatalf("cross-tenant redrive err = %v, want ErrNotFound", err)
	}
	if _, err := j.Redrives(context.Background(), "tenant-other", id); !errors.Is(err, operation.ErrNotFound) {
		t.Fatalf("cross-tenant redrives err = %v, want ErrNotFound", err)
	}
}

func TestOperation_JournalLookupIsTenantScoped(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:tenant", 1, operation.OrderingStrict)
	if _, err := j.Journal(context.Background(), "tenant-other", id); !errors.Is(err, operation.ErrNotFound) {
		t.Fatalf("cross-tenant journal err = %v, want ErrNotFound", err)
	}
	entries, err := j.Journal(context.Background(), "tenant-promotion", uuid.Nil)
	if err != nil || len(entries) != 1 || entries[0].TenantID != "tenant-promotion" {
		t.Fatalf("tenant stream = %+v, err=%v", entries, err)
	}
}
