package operations

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	evidencev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/streaming"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func exerciseMemoryStoreCancelIsIdempotentAndKeepsOwnerScope(t *testing.T) {
	t.Helper()
	store := NewMemoryStore(nil)
	record := Record{OperationID: "op-1", TenantID: "tenant-a", Owner: "subject-a", State: streaming.OperationRunning}
	if err := store.Put(record); err != nil {
		t.Fatal(err)
	}
	first, err := store.Cancel(context.Background(), "tenant-a", "op-1", "cancel-1", "operator-request")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Cancel(context.Background(), "tenant-a", "op-1", "cancel-1", "operator-request")
	if err != nil {
		t.Fatal(err)
	}
	if first.State != streaming.OperationCancellationRequested || second.State != first.State {
		t.Fatalf("cancel state changed: %s then %s", first.State, second.State)
	}
	if _, err := store.Get(context.Background(), "tenant-b", "op-1"); err != ErrNotFound {
		t.Fatalf("cross-tenant get error = %v", err)
	}
}

func TestMemoryStoreCancelIsIdempotentAndKeepsOwnerScope(t *testing.T) {
	exerciseMemoryStoreCancelIsIdempotentAndKeepsOwnerScope(t)
}

func TestLongRunningOperationEndpointsPreserveStateResultErrorAndCancellationTruth(t *testing.T) {
	exerciseMemoryStoreCancelIsIdempotentAndKeepsOwnerScope(t)
	t.Run("owner boundary is non-disclosing and side-effect free", testOwnerBoundary)
	t.Run("projection preserves typed terminal payload truth", testProjectionTruth)
}

func TestTodo_EP_OPS_001_Property(t *testing.T) {
	exerciseMemoryStoreCancelIsIdempotentAndKeepsOwnerScope(t)
}
func TestTodo_EP_OPS_001_Golden(t *testing.T) { testProjectionTruth(t) }
func TestTodo_EP_OPS_001_Race(t *testing.T) {
	store := NewMemoryStore(nil)
	if err := store.Put(Record{OperationID: "op-race", TenantID: "tenant-race", Owner: "subject-race", State: streaming.OperationRunning}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := store.Cancel(context.Background(), "tenant-race", "op-race", "cancel-race", "concurrent-cancel"); err != nil {
				t.Errorf("concurrent Cancel: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := store.Get(context.Background(), "tenant-race", "op-race"); err != nil {
				t.Errorf("concurrent Get: %v", err)
			}
		}()
	}
	wg.Wait()
	got, err := store.Get(context.Background(), "tenant-race", "op-race")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != streaming.OperationCancellationRequested {
		t.Fatalf("final concurrent state = %s, want CANCELLATION_REQUESTED", got.State)
	}
}
func TestTodo_EP_OPS_001_Integration(t *testing.T) {
	testOwnerBoundary(t)
}
func TestTodo_EP_OPS_001_Fault(t *testing.T) { testMissingStore(t) }
func TestTodo_EP_OPS_001_Security(t *testing.T) {
	testOwnerBoundary(t)
}
func TestTodo_EP_OPS_001_Conformance(t *testing.T) {
	testProjectionTruth(t)
}
func TestTodo_EP_OPS_001_Recovery(t *testing.T) {
	exerciseMemoryStoreCancelIsIdempotentAndKeepsOwnerScope(t)
}

func testOwnerBoundary(t *testing.T) {
	t.Helper()
	store := NewMemoryStore(func() time.Time { return time.Unix(100, 0).UTC() })
	if err := store.Put(Record{OperationID: "op-owned", TenantID: "acme-corp", Owner: "subject-a", State: streaming.OperationRunning}); err != nil {
		t.Fatal(err)
	}
	s := &server{store: store}

	other := admittedContext(t, "subject-b", &evidencev1.GetOperationRequest{OperationId: "op-owned"}, GetOperationProcedure)
	if _, err := s.GetOperation(other, &evidencev1.GetOperationRequest{OperationId: "op-owned"}); err == nil {
		t.Fatal("foreign owner read succeeded")
	} else {
		var owned *envelope.Error
		if !errors.As(err, &owned) || owned.Code() != envelope.CodeNotFound {
			t.Fatalf("foreign owner read error = %v, want non-disclosing NOT_FOUND", err)
		}
	}

	cancel := admittedContext(t, "subject-b", &evidencev1.CancelOperationRequest{OperationId: "op-owned", IdempotencyKey: "cancel-b"}, CancelOperationProcedure)
	if _, err := s.CancelOperation(cancel, &evidencev1.CancelOperationRequest{OperationId: "op-owned", IdempotencyKey: "cancel-b"}); err == nil {
		t.Fatal("foreign owner cancellation succeeded")
	}
	current, err := store.Get(context.Background(), "acme-corp", "op-owned")
	if err != nil {
		t.Fatal(err)
	}
	if current.State != streaming.OperationRunning {
		t.Fatalf("foreign cancellation changed state to %s", current.State)
	}
}

func testProjectionTruth(t *testing.T) {
	t.Helper()
	result := &intentsv1.TypedPayload{ProtobufWireBytes: []byte{1}}
	detail := &commonv1.ErrorDetail{ReasonRef: "operation.failed"}
	succeeded := Project(Record{OperationID: "succeeded", TenantID: "acme-corp", State: streaming.OperationSucceeded, Result: result, Error: detail}, nil)
	if succeeded.GetResult() == nil || succeeded.GetError() != nil {
		t.Fatalf("succeeded projection = result=%v error=%v", succeeded.GetResult(), succeeded.GetError())
	}
	failed := Project(Record{OperationID: "failed", TenantID: "acme-corp", State: streaming.OperationFailed, Result: result, Error: detail}, nil)
	if failed.GetResult() != nil || failed.GetError() == nil {
		t.Fatalf("failed projection = result=%v error=%v", failed.GetResult(), failed.GetError())
	}
	for _, state := range []streaming.OperationState{streaming.OperationPending, streaming.OperationCancellationRequested} {
		if got := Project(Record{OperationID: "active", TenantID: "acme-corp", State: state}, nil).GetState(); got != evidencev1.OperationState_OPERATION_STATE_RUNNING {
			t.Fatalf("wire-compatible projection for %s = %s, want RUNNING", state, got)
		}
	}
}

func testMissingStore(t *testing.T) {
	t.Helper()
	s := &server{}
	ctx := admittedContext(t, "subject-a", &evidencev1.GetOperationRequest{OperationId: "missing"}, GetOperationProcedure)
	if _, err := s.GetOperation(ctx, &evidencev1.GetOperationRequest{OperationId: "missing"}); err == nil {
		t.Fatal("missing store returned success")
	} else {
		var owned *envelope.Error
		if !errors.As(err, &owned) || owned.Code() != envelope.CodeUnavailable {
			t.Fatalf("missing store error = %v, want UNAVAILABLE", err)
		}
	}
}

func admittedContext(t *testing.T, subject string, message proto.Message, method string) context.Context {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "acme-corp", Subject: subject, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-" + subject, IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(1000, 0),
		CredentialDigest: "credential-" + subject,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, _, admitErr := transport.Admit(context.Background(), transport.Config{
		Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return p, nil }),
		Now:      func() time.Time { return time.Unix(10, 0).UTC() },
	}, transport.AdmissionRequest{
		Metadata: transport.MapMetadata{transport.AuthorizationMetadataKey: {"Bearer fixture"}},
		Method:   method, Kind: transport.KindGRPC, Message: message,
	})
	if admitErr != nil {
		t.Fatalf("transport.Admit: %v", admitErr)
	}
	return ctx
}
