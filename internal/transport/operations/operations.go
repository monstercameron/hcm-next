// Package operations exposes the owner-scoped long-running operation resource.
package operations

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/common/v1"
	evidencev1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/evidence/v1"
	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/hcm-next/internal/transport"
	"github.com/monstercameron/hcm-next/internal/transport/envelope"
	"github.com/monstercameron/hcm-next/internal/transport/streaming"
	"github.com/monstercameron/hcm-next/internal/trust"
)

const (
	GetOperationProcedure    = "/hcmnext.evidence.v1.OperationsService/GetOperation"
	CancelOperationProcedure = "/hcmnext.evidence.v1.OperationsService/CancelOperation"
)

var (
	ErrNotFound       = errors.New("operations: operation not found")
	ErrNotConfigured  = errors.New("operations: store is not configured")
	ErrNotCancellable = errors.New("operations: operation is not cancellable")
	ErrNotOwner       = errors.New("operations: operation is not visible to this principal")
)

// Record is the transport-safe operation view. It deliberately contains
// references and typed results only; request payloads and provider secrets
// never cross this boundary.
type Record struct {
	OperationID string
	TenantID    string
	Owner       string
	RequestType string
	State       streaming.OperationState
	Result      *intentsv1.TypedPayload
	Error       *commonv1.ErrorDetail
	MetadataRef string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Store interface {
	Get(context.Context, string, string) (Record, error)
	Cancel(context.Context, string, string, string, string) (Record, error)
}

// MemoryStore is a deterministic conformance store for endpoint wiring. A
// production composition supplies a durable store through Store instead.
type MemoryStore struct {
	mu      sync.RWMutex
	records map[string]Record
	cancels map[string]string
	now     func() time.Time
}

func NewMemoryStore(now func() time.Time) *MemoryStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MemoryStore{records: make(map[string]Record), cancels: make(map[string]string), now: now}
}

func (s *MemoryStore) Put(record Record) error {
	if strings.TrimSpace(record.OperationID) == "" || strings.TrimSpace(record.TenantID) == "" {
		return errors.New("operations: operation id and tenant are required")
	}
	if !validState(record.State) {
		return errors.New("operations: invalid operation state")
	}
	record.TenantID = strings.TrimSpace(record.TenantID)
	record.OperationID = strings.TrimSpace(record.OperationID)
	record.Owner = strings.TrimSpace(record.Owner)
	record.RequestType = strings.TrimSpace(record.RequestType)
	if record.CreatedAt.IsZero() {
		record.CreatedAt = s.now().UTC()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	}
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.records[record.TenantID+"\x00"+record.OperationID]; exists {
		return errors.New("operations: operation already exists")
	}
	s.records[record.TenantID+"\x00"+record.OperationID] = clone(record)
	return nil
}

func (s *MemoryStore) Get(ctx context.Context, tenant, id string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.records[tenant+"\x00"+id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return clone(record), nil
}

func (s *MemoryStore) Cancel(ctx context.Context, tenant, id, key, _ string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[tenant+"\x00"+id]
	if !ok {
		return Record{}, ErrNotFound
	}
	if key != "" {
		cancelKey := tenant + "\x00" + id + "\x00" + key
		if previous, exists := s.cancels[cancelKey]; exists {
			return clone(s.records[tenant+"\x00"+previous]), nil
		}
		s.cancels[cancelKey] = id
	}
	if record.State.Terminal() {
		return clone(record), nil
	}
	if record.State != streaming.OperationPending && record.State != streaming.OperationRunning && record.State != streaming.OperationCancellationRequested {
		return Record{}, ErrNotCancellable
	}
	record.State = streaming.OperationCancellationRequested
	record.UpdatedAt = s.now().UTC()
	s.records[tenant+"\x00"+id] = clone(record)
	return clone(record), nil
}

type server struct {
	evidencev1.UnimplementedOperationsServiceServer
	store Store
}

type Dependencies struct{ Store Store }

func Version() int { return 1 }

func Explain(record Record) string {
	return fmt.Sprintf("operation inspection v%d id=%s state=%s owner=%s", Version(), record.OperationID, record.State, record.Owner)
}

func Register(srv *grpc.Server, deps Dependencies) {
	evidencev1.RegisterOperationsServiceServer(srv, &server{store: deps.Store})
}

func NewHandler(deps Dependencies, opts ...connect.HandlerOption) http.Handler {
	s := &server{store: deps.Store}
	mux := http.NewServeMux()
	mux.Handle(GetOperationProcedure, connect.NewUnaryHandler(GetOperationProcedure, func(ctx context.Context, req *connect.Request[evidencev1.GetOperationRequest]) (*connect.Response[evidencev1.GetOperationResponse], error) {
		res, err := s.GetOperation(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(CancelOperationProcedure, connect.NewUnaryHandler(CancelOperationProcedure, func(ctx context.Context, req *connect.Request[evidencev1.CancelOperationRequest]) (*connect.Response[evidencev1.CancelOperationResponse], error) {
		res, err := s.CancelOperation(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	return mux
}

func (s *server) GetOperation(ctx context.Context, req *evidencev1.GetOperationRequest) (*evidencev1.GetOperationResponse, error) {
	p, inv, err := trustedContext(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.GetOperationId()) == "" {
		return nil, invalid(inv, "operation_id")
	}
	if s.store == nil {
		return nil, unavailable(inv, p)
	}
	record, readErr := s.store.Get(ctx, p.Tenant().String(), req.GetOperationId())
	if readErr != nil {
		return nil, projectError(readErr, inv, p)
	}
	if !visibleTo(record, p) {
		return nil, projectError(ErrNotOwner, inv, p)
	}
	return &evidencev1.GetOperationResponse{Operation: Project(record, p)}, nil
}

func (s *server) CancelOperation(ctx context.Context, req *evidencev1.CancelOperationRequest) (*evidencev1.CancelOperationResponse, error) {
	p, inv, err := trustedContext(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.GetOperationId()) == "" || strings.TrimSpace(req.GetIdempotencyKey()) == "" {
		return nil, invalid(inv, "operation_id/idempotency_key")
	}
	if s.store == nil {
		return nil, unavailable(inv, p)
	}
	// Read and authorize before invoking the cancellation port. This keeps an
	// unauthorized caller from turning a non-disclosing read into a durable
	// state transition, even when a store's Cancel method does not repeat the
	// owner check itself.
	current, readErr := s.store.Get(ctx, p.Tenant().String(), req.GetOperationId())
	if readErr != nil {
		return nil, projectError(readErr, inv, p)
	}
	if !visibleTo(current, p) {
		return nil, projectError(ErrNotOwner, inv, p)
	}
	record, cancelErr := s.store.Cancel(ctx, p.Tenant().String(), req.GetOperationId(), req.GetIdempotencyKey(), req.GetReasonRef())
	if cancelErr != nil {
		return nil, projectError(cancelErr, inv, p)
	}
	return &evidencev1.CancelOperationResponse{Operation: Project(record, p)}, nil
}

func Project(record Record, p *trust.Principal) *evidencev1.Operation {
	tenant := record.TenantID
	if p != nil {
		tenant = p.Tenant().String()
	}
	result, detail := record.Result, record.Error
	if record.State != streaming.OperationSucceeded {
		result = nil
	}
	if record.State != streaming.OperationFailed {
		detail = nil
	}
	return &evidencev1.Operation{OperationId: record.OperationID, Scope: &commonv1.ScopeContext{TenantId: tenant}, State: projectState(record.State), Result: result, Error: detail, MetadataRef: record.MetadataRef, CreatedAt: timestamp(record.CreatedAt), UpdatedAt: timestamp(record.UpdatedAt)}
}

// visibleTo applies the endpoint's owner boundary. Tenant scoping is already
// enforced by the Store call; owner scoping is kept here because the transport
// contract must not assume that a persistence adapter has performed the second
// authorization check. A record without an owner is deliberately not visible
// through an authenticated operation endpoint.
func visibleTo(record Record, p *trust.Principal) bool {
	return p != nil &&
		strings.TrimSpace(record.TenantID) == p.Tenant().String() &&
		strings.TrimSpace(record.Owner) != "" &&
		record.Owner == p.Subject()
}

func projectState(state streaming.OperationState) evidencev1.OperationState {
	switch state {
	case streaming.OperationSucceeded:
		return evidencev1.OperationState_OPERATION_STATE_SUCCEEDED
	case streaming.OperationFailed:
		return evidencev1.OperationState_OPERATION_STATE_FAILED
	case streaming.OperationCancelled:
		return evidencev1.OperationState_OPERATION_STATE_CANCELLED
	default:
		return evidencev1.OperationState_OPERATION_STATE_RUNNING
	}
}

func validState(state streaming.OperationState) bool {
	switch state {
	case streaming.OperationPending, streaming.OperationRunning, streaming.OperationSucceeded, streaming.OperationFailed, streaming.OperationCancellationRequested, streaming.OperationCancelled:
		return true
	default:
		return false
	}
}
func clone(record Record) Record {
	if record.Result != nil {
		record.Result = proto.Clone(record.Result).(*intentsv1.TypedPayload)
	}
	if record.Error != nil {
		record.Error = proto.Clone(record.Error).(*commonv1.ErrorDetail)
	}
	return record
}
func timestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t.UTC())
}

func trustedContext(ctx context.Context) (*trust.Principal, *transport.Invocation, *envelope.Error) {
	inv, ok := transport.InvocationFromContext(ctx)
	if !ok {
		return nil, nil, envelope.New(envelope.CodeUnauthenticated, "operations.no_trusted_context", "the request carries no trusted context")
	}
	p, ok := trust.FromContext(ctx)
	if !ok {
		return nil, inv, envelope.New(envelope.CodeUnauthenticated, "operations.no_principal", "the request carries no authenticated principal").WithCorrelation(inv.RequestID())
	}
	return p, inv, nil
}
func invalid(inv *transport.Invocation, field string) *envelope.Error {
	err := envelope.New(envelope.CodeInvalidArgument, "operations.invalid_request", "the request is invalid").WithViolation(field, "the field is required", "operations.request")
	if inv != nil {
		err.WithCorrelation(inv.RequestID())
	}
	return err
}
func unavailable(inv *transport.Invocation, p *trust.Principal) *envelope.Error {
	err := envelope.New(envelope.CodeUnavailable, "operations.unavailable", "operations are unavailable")
	if inv != nil {
		err.WithCorrelation(inv.RequestID())
	}
	if p != nil {
		err.WithEvidence(envelope.Evidence{ID: p.EvidenceID(), Kind: "authentication"})
	}
	return err
}
func projectError(err error, inv *transport.Invocation, p *trust.Principal) *envelope.Error {
	code, reason, message := envelope.CodeUnavailable, "operations.unavailable", "operations are unavailable"
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrNotOwner) {
		code, reason, message = envelope.CodeNotFound, "operations.not_found", "the operation does not exist or is not visible"
	}
	if errors.Is(err, ErrNotCancellable) {
		code, reason, message = envelope.CodeFailedPrecondition, "operations.not_cancellable", "the operation cannot be cancelled at its current state"
	}
	out := envelope.New(code, reason, message).WithDiagnostic(err)
	if inv != nil {
		out.WithCorrelation(inv.RequestID())
	}
	if p != nil {
		out.WithEvidence(envelope.Evidence{ID: p.EvidenceID(), Kind: "authentication"})
	}
	return out
}
