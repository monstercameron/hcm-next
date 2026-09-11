// Package humanwork exposes the human-work queue read surface: WorkService
// ListWorkItems and GetWorkItem (EP-WORK-001).
//
// The service is deliberately thin (ARCH-GO-023): membership, visibility
// classification and the permitted-action set are the workitem package's
// read rules; this package owns protocol, authorization, the signed stable
// queue cursor and the wire projection. The four mutating WorkService
// methods are P1B acceptance items (EP-WORK-002/003): they are registered
// and refuse with FAILED_PRECONDITION exactly as the proto contract
// specifies, rather than answering UNIMPLEMENTED. GetThresholdTable is a
// separate read todo and is left unimplemented.
package humanwork

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	humanworkv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const (
	ListWorkItemsProcedure = "/hcmnext.humanwork.v1.WorkService/ListWorkItems"
	GetWorkItemProcedure   = "/hcmnext.humanwork.v1.WorkService/GetWorkItem"

	ActionListWorkItems      = "list_work_items"
	ActionGetWorkItem        = "get_work_item"
	ActionWorkItemGovernance = "work_item_governance_view"

	defaultPageSize = 20
	maxPageSize     = 100
	cursorTTL       = 5 * time.Minute
	cursorVersion   = 1
)

var (
	ErrNotFound       = errors.New("humanwork: work item not found")
	ErrInvalidCursor  = errors.New("humanwork: queue cursor is invalid")
	ErrCursorKeyUnset = errors.New("humanwork: queue cursor key is unset")
	ErrQueueEmpty     = errors.New("humanwork: the queue reader is not configured")
)

// Reader is the deliberately small, redaction-safe port the endpoints read
// through. Tenant and principal arrive as strings because the application
// reader owns their typed forms; a read of another tenant's row, or of an
// absent row, is ErrNotFound and nothing more specific.
type Reader interface {
	// ListQueue returns every live item the principal may act on, in stable
	// deadline/identity order. The store has already applied membership; a
	// principal with no membership never appears in its output.
	ListQueue(ctx context.Context, tenant, principal string, now time.Time) ([]workitem.WorkItem, error)
	// LoadItem returns one item without applying visibility: the server
	// decides disclosure after loading, since invisible and absent items
	// project to the same NOT_FOUND.
	LoadItem(ctx context.Context, tenant, workItemID string) (workitem.WorkItem, error)
}

type Dependencies struct {
	Queue     Reader
	Authorize func(*trust.Principal, string) bool
	CursorKey []byte
	Now       func() time.Time
}

type server struct {
	humanworkv1.UnimplementedWorkServiceServer
	deps Dependencies
}

func Version() int { return 1 }

// Explain is the one-line summary surfaces and logs print for a response.
func Explain(v *humanworkv1.WorkItem) string {
	if v == nil {
		return fmt.Sprintf("human work v%d empty", Version())
	}
	return fmt.Sprintf("human work v%d item=%s status=%s version=%d actions=%v",
		Version(), v.GetWorkItemId(), v.GetStatus(), v.GetItemVersion(), v.GetPermittedActions())
}

func Register(srv *grpc.Server, deps Dependencies) {
	humanworkv1.RegisterWorkServiceServer(srv, &server{deps: deps})
}

// NewHandler mounts every WorkService procedure over Connect so the edge can
// project the same answers; the refused methods are mounted too so their
// refusal is the typed FAILED_PRECONDITION rather than a route absence.
func NewHandler(deps Dependencies, opts ...connect.HandlerOption) http.Handler {
	s := &server{deps: deps}
	mux := http.NewServeMux()
	mux.Handle(ListWorkItemsProcedure, connect.NewUnaryHandler(ListWorkItemsProcedure, func(ctx context.Context, req *connect.Request[humanworkv1.ListWorkItemsRequest]) (*connect.Response[humanworkv1.ListWorkItemsResponse], error) {
		res, err := s.ListWorkItems(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(GetWorkItemProcedure, connect.NewUnaryHandler(GetWorkItemProcedure, func(ctx context.Context, req *connect.Request[humanworkv1.GetWorkItemRequest]) (*connect.Response[humanworkv1.GetWorkItemResponse], error) {
		res, err := s.GetWorkItem(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	// The mutating procedures are mounted to refuse, not to 404: the proto's
	// P1A disposition fixes their answer as FAILED_PRECONDITION on both
	// transports.
	mux.Handle("/hcmnext.humanwork.v1.WorkService/ClaimWorkItem", connect.NewUnaryHandler("/hcmnext.humanwork.v1.WorkService/ClaimWorkItem", func(ctx context.Context, req *connect.Request[humanworkv1.ClaimWorkItemRequest]) (*connect.Response[humanworkv1.ClaimWorkItemResponse], error) {
		res, err := s.ClaimWorkItem(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle("/hcmnext.humanwork.v1.WorkService/ReleaseWorkItem", connect.NewUnaryHandler("/hcmnext.humanwork.v1.WorkService/ReleaseWorkItem", func(ctx context.Context, req *connect.Request[humanworkv1.ReleaseWorkItemRequest]) (*connect.Response[humanworkv1.ReleaseWorkItemResponse], error) {
		res, err := s.ReleaseWorkItem(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle("/hcmnext.humanwork.v1.WorkService/CompleteWorkItem", connect.NewUnaryHandler("/hcmnext.humanwork.v1.WorkService/CompleteWorkItem", func(ctx context.Context, req *connect.Request[humanworkv1.CompleteWorkItemRequest]) (*connect.Response[humanworkv1.CompleteWorkItemResponse], error) {
		res, err := s.CompleteWorkItem(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle("/hcmnext.humanwork.v1.WorkService/DecideApproval", connect.NewUnaryHandler("/hcmnext.humanwork.v1.WorkService/DecideApproval", func(ctx context.Context, req *connect.Request[humanworkv1.DecideApprovalRequest]) (*connect.Response[humanworkv1.DecideApprovalResponse], error) {
		res, err := s.DecideApproval(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	return mux
}

func (s *server) ListWorkItems(ctx context.Context, req *humanworkv1.ListWorkItemsRequest) (*humanworkv1.ListWorkItemsResponse, error) {
	p, inv, err := trustedContext(ctx)
	if err != nil {
		return nil, err
	}
	if !s.authorized(p, ActionListWorkItems) {
		return nil, denied(inv, p)
	}
	now := s.now()
	tenant := p.Tenant().String()

	// A scope naming another tenant is a hidden resource: the non-disclosing
	// answer for a list is an empty page, not a refusal.
	if scope := req.GetScope(); scope != nil && scope.GetTenantId() != "" && scope.GetTenantId() != tenant {
		return &humanworkv1.ListWorkItemsResponse{Page: &commonv1.PageResponse{}}, nil
	}
	orgScope := scopeOrgScope(req.GetScope())
	pageSize, pageErr := pageSizeOf(req.GetPage())
	if pageErr != nil {
		return nil, invalid(inv, "page.page_size")
	}
	if s.deps.Queue == nil {
		return nil, unavailable(inv, p, ErrQueueEmpty)
	}
	items, listErr := s.deps.Queue.ListQueue(ctx, tenant, p.Subject(), now)
	if listErr != nil {
		return nil, unavailable(inv, p, listErr)
	}

	filtered := items[:0:0]
	governed := s.authorized(p, ActionWorkItemGovernance)
	for _, item := range items {
		m := workitem.MembershipOf(item, p.Subject(), now)
		inScope := orgScope != "" && item.OrganizationScopeID == orgScope ||
			orgScope == "" && item.OrganizationScopeID == p.OrganizationScopeID()
		if !workitem.Visible(item, m, inScope, governed) {
			continue
		}
		filtered = append(filtered, item)
	}

	start, cursorErr := decodeQueueCursor(req.GetPage(), s.deps.CursorKey, p, filtered, now)
	if cursorErr != nil {
		return nil, invalid(inv, "page.cursor")
	}
	if start > len(filtered) {
		return nil, invalid(inv, "page.cursor")
	}
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	res := &humanworkv1.ListWorkItemsResponse{Page: &commonv1.PageResponse{}}
	for _, item := range filtered[start:end] {
		res.WorkItems = append(res.WorkItems, projectItem(item, workitem.MembershipOf(item, p.Subject(), now), governed))
	}
	if end < len(filtered) {
		cursor, encodeErr := encodeQueueCursor(queueCursor{
			Principal: p.Subject(), Tenant: tenant, Scope: orgScope,
			Snapshot: queueDigest(filtered), Index: end, Version: cursorVersion,
			ExpiresAt: now.Add(cursorTTL).Unix(), Nonce: uuid.NewString(),
		}, s.deps.CursorKey)
		if encodeErr != nil {
			return nil, unavailable(inv, p, encodeErr)
		}
		res.Page.NextCursor = cursor
	}
	return res, nil
}

func (s *server) GetWorkItem(ctx context.Context, req *humanworkv1.GetWorkItemRequest) (*humanworkv1.GetWorkItemResponse, error) {
	p, inv, err := trustedContext(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.GetWorkItemId()) == "" {
		return nil, invalid(inv, "work_item_id")
	}
	if !s.authorized(p, ActionGetWorkItem) {
		return nil, denied(inv, p)
	}
	tenant := p.Tenant().String()
	if scope := req.GetScope(); scope != nil && scope.GetTenantId() != "" && scope.GetTenantId() != tenant {
		return nil, notFound(inv, p)
	}
	if s.deps.Queue == nil {
		return nil, unavailable(inv, p, ErrQueueEmpty)
	}
	item, loadErr := s.deps.Queue.LoadItem(ctx, tenant, req.GetWorkItemId())
	if loadErr != nil {
		if errors.Is(loadErr, ErrNotFound) || workitem.CodeOf(loadErr) == workitem.CodeWorkItemNotFound {
			return nil, notFound(inv, p)
		}
		return nil, unavailable(inv, p, loadErr)
	}
	now := s.now()
	m := workitem.MembershipOf(item, p.Subject(), now)
	governed := s.authorized(p, ActionWorkItemGovernance)
	inScope := item.OrganizationScopeID != "" && item.OrganizationScopeID == p.OrganizationScopeID()
	if !workitem.Visible(item, m, inScope, governed) {
		return nil, notFound(inv, p)
	}
	return &humanworkv1.GetWorkItemResponse{WorkItem: projectItem(item, m, governed)}, nil
}

// refused reports the contract-fixed FAILED_PRECONDITION for the mutating
// WorkService methods: work queues do not accept writes before P1B
// (humanwork_service.proto's P1A disposition), and a typed refusal is the
// documented answer rather than UNIMPLEMENTED.
func (s *server) refused(ctx context.Context, reason string) error {
	inv, ok := transport.InvocationFromContext(ctx)
	err := envelope.New(envelope.CodeFailedPrecondition, reason, "this method does not accept calls in this phase")
	if ok {
		err.WithCorrelation(inv.RequestID())
	}
	return err
}

func (s *server) ClaimWorkItem(ctx context.Context, _ *humanworkv1.ClaimWorkItemRequest) (*humanworkv1.ClaimWorkItemResponse, error) {
	return nil, s.refused(ctx, "workitem.claim_unavailable")
}

func (s *server) ReleaseWorkItem(ctx context.Context, _ *humanworkv1.ReleaseWorkItemRequest) (*humanworkv1.ReleaseWorkItemResponse, error) {
	return nil, s.refused(ctx, "workitem.release_unavailable")
}

func (s *server) CompleteWorkItem(ctx context.Context, _ *humanworkv1.CompleteWorkItemRequest) (*humanworkv1.CompleteWorkItemResponse, error) {
	return nil, s.refused(ctx, "workitem.complete_unavailable")
}

func (s *server) DecideApproval(ctx context.Context, _ *humanworkv1.DecideApprovalRequest) (*humanworkv1.DecideApprovalResponse, error) {
	return nil, s.refused(ctx, "workitem.decide_approval_unavailable")
}

func (s *server) authorized(p *trust.Principal, action string) bool {
	return s.deps.Authorize == nil || s.deps.Authorize(p, action)
}

func (s *server) now() time.Time {
	if s.deps.Now != nil {
		return s.deps.Now()
	}
	return time.Now()
}

func pageSizeOf(page *commonv1.PageRequest) (int, error) {
	if page == nil || page.GetPageSize() == 0 {
		return defaultPageSize, nil
	}
	if page.GetPageSize() < 0 || page.GetPageSize() > maxPageSize {
		return 0, ErrInvalidCursor
	}
	return int(page.GetPageSize()), nil
}

func scopeOrgScope(scope *commonv1.ScopeContext) string {
	if scope == nil {
		return ""
	}
	return strings.TrimSpace(scope.GetOrganizationScopeId())
}

// projectItem renders the wire view. The classification split is the
// workitem view rules: identity-bearing context (the resolved candidate set,
// the assigned principal) and the restricted evidence compartment are
// disclosed exactly when the view rules admit them for this caller's
// membership; a governed viewer sees context but never evidence. Subject
// refs are business context and follow the context rule.
func projectItem(item workitem.WorkItem, m workitem.Membership, governed bool) *humanworkv1.WorkItem {
	out := &humanworkv1.WorkItem{
		WorkItemId:    item.WorkItemID.String(),
		TenantId:      item.TenantID.String(),
		WorkType:      item.WorkType,
		Status:        projectStatus(item.Status),
		CorrelationId: item.CorrelationID,
		ItemVersion:   uint64(item.ItemVersion),
		CreatedAt:     timestamppb.New(item.CreatedAt),
	}
	if item.OrganizationScopeID != "" {
		out.OrganizationScope = &commonv1.ScopeContext{
			TenantId:            item.TenantID.String(),
			OrganizationScopeId: item.OrganizationScopeID,
		}
	}
	if !item.DeadlineAt.IsZero() {
		out.DueAt = timestamppb.New(item.DeadlineAt)
	}
	if item.WorkflowInstanceID != uuid.Nil {
		id := item.WorkflowInstanceID.String()
		out.WorkflowInstanceId = &id
	}
	if item.ProposalRef != "" {
		out.ProposalRef = &item.ProposalRef
	}
	// Claim and completion evidence is the restricted compartment: only the
	// acting member (or a governed viewer) sees who holds the claim.
	if workitem.EvidenceVisible(m) || governed {
		if item.ClaimedBy != "" {
			out.ClaimedBy = &item.ClaimedBy
		}
		if item.ClaimedAt != nil {
			out.ClaimedAt = timestamppb.New(*item.ClaimedAt)
		}
		if item.ClaimExpiresAt != nil {
			out.ClaimExpiresAt = timestamppb.New(*item.ClaimExpiresAt)
		}
		if item.CompletedAt != nil {
			out.CompletedAt = timestamppb.New(*item.CompletedAt)
		}
	}
	if item.OwnerKind == workitem.OwnerCandidateSet {
		ref := item.OwnerRef
		out.ResolvedQueueId = &ref
	}
	for _, a := range workitem.PermittedActions(item, m) {
		out.PermittedActions = append(out.PermittedActions, string(a))
	}
	sort.Strings(out.PermittedActions)
	if workitem.ContextVisible(m) || governed {
		for _, ref := range item.SubjectRefs {
			out.SubjectRefs = append(out.SubjectRefs, &commonv1.EntityRef{
				TenantId: item.TenantID.String(), Kind: "business_subject", Id: ref,
			})
		}
		if item.OwnerKind == workitem.OwnerPrincipal {
			ref := item.OwnerRef
			out.AssignedPrincipalId = &ref
		}
		for _, c := range item.Assignment.Resolution.Candidates {
			out.ResolvedCandidates = append(out.ResolvedCandidates, c.PrincipalID)
		}
	}
	return out
}

func projectStatus(s workitem.Status) humanworkv1.WorkItemStatus {
	switch s {
	case workitem.StatusCreated:
		return humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_CREATED
	case workitem.StatusRouted:
		return humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_ROUTED
	case workitem.StatusAvailable:
		return humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_AVAILABLE
	case workitem.StatusAssigned:
		return humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_ASSIGNED
	case workitem.StatusClaimed:
		return humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_CLAIMED
	case workitem.StatusInProgress:
		return humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_IN_PROGRESS
	case workitem.StatusEscalated:
		return humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_ESCALATED
	case workitem.StatusExpired:
		return humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_EXPIRED
	case workitem.StatusCompleted:
		return humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_COMPLETED
	case workitem.StatusReturned:
		return humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_RETURNED
	case workitem.StatusCancelled:
		return humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_CANCELLED
	default:
		return humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_UNSPECIFIED
	}
}

// queueCursor is the signed pagination token bound to the contract's tuple:
// principal, tenant, scope filter, the snapshot digest of the queue it paged
// and an expiry. A cursor replayed against a changed queue, a different
// principal or a different tenant fails closed.
type queueCursor struct {
	Principal string `json:"p"`
	Tenant    string `json:"t"`
	Scope     string `json:"s"`
	Snapshot  string `json:"w"`
	Index     int    `json:"i"`
	Version   int    `json:"v"`
	ExpiresAt int64  `json:"e"`
	Nonce     string `json:"n"`
}

func encodeQueueCursor(c queueCursor, key []byte) (string, error) {
	if len(key) == 0 {
		return "", ErrCursorKeyUnset
	}
	if c.Principal == "" || c.Tenant == "" {
		return "", ErrInvalidCursor
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

func decodeQueueCursor(page *commonv1.PageRequest, key []byte, p *trust.Principal, items []workitem.WorkItem, now time.Time) (int, error) {
	if page == nil || page.GetCursor() == "" {
		return 0, nil
	}
	if len(key) == 0 {
		return 0, ErrCursorKeyUnset
	}
	parts := strings.Split(page.GetCursor(), ".")
	if len(parts) != 2 {
		return 0, ErrInvalidCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, ErrInvalidCursor
	}
	sig, err := hex.DecodeString(parts[1])
	if err != nil {
		return 0, ErrInvalidCursor
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(raw)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return 0, ErrInvalidCursor
	}
	var c queueCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return 0, ErrInvalidCursor
	}
	if c.Version != cursorVersion || c.Index < 0 ||
		c.Principal != p.Subject() || c.Tenant != p.Tenant().String() ||
		c.Snapshot != queueDigest(items) || now.Unix() >= c.ExpiresAt {
		return 0, ErrInvalidCursor
	}
	return c.Index, nil
}

// queueDigest fingerprints the ordered queue the cursor was minted against:
// any insert, removal or membership change between pages invalidates the
// cursor, which is the "snapshot" half of the contract's cursor binding.
func queueDigest(items []workitem.WorkItem) string {
	h := sha256.New()
	for _, item := range items {
		h.Write(item.WorkItemID[:])
		var b [8]byte
		for i := range b {
			b[i] = byte(item.ItemVersion >> (8 * i))
		}
		h.Write(b[:])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func trustedContext(ctx context.Context) (*trust.Principal, *transport.Invocation, *envelope.Error) {
	inv, ok := transport.InvocationFromContext(ctx)
	if !ok {
		return nil, nil, envelope.New(envelope.CodeUnauthenticated, "humanwork.no_trusted_context", "the request carries no trusted context")
	}
	p, ok := trust.FromContext(ctx)
	if !ok {
		return nil, inv, envelope.New(envelope.CodeUnauthenticated, "humanwork.no_principal", "the request carries no authenticated principal").WithCorrelation(inv.RequestID())
	}
	return p, inv, nil
}

func invalid(inv *transport.Invocation, field string) *envelope.Error {
	err := envelope.New(envelope.CodeInvalidArgument, "humanwork.invalid_request", "the request is invalid").WithViolation(field, "the field is required or malformed", "humanwork.request")
	if inv != nil {
		err.WithCorrelation(inv.RequestID())
	}
	return err
}

func denied(inv *transport.Invocation, p *trust.Principal) *envelope.Error {
	err := envelope.New(envelope.CodePermissionDenied, "humanwork.queue_denied", "the caller is not authorized to read the work queue")
	if inv != nil {
		err.WithCorrelation(inv.RequestID())
	}
	if p != nil {
		err.WithEvidence(envelope.Evidence{ID: p.EvidenceID(), Kind: "authentication"})
	}
	return err
}

// notFound is the single non-disclosing answer for absent, invisible and
// out-of-tenant items: identical code, identical reason, identical message.
func notFound(inv *transport.Invocation, p *trust.Principal) *envelope.Error {
	err := envelope.New(envelope.CodeNotFound, "humanwork.not_found", "the work item does not exist or is not visible")
	if inv != nil {
		err.WithCorrelation(inv.RequestID())
	}
	if p != nil {
		err.WithEvidence(envelope.Evidence{ID: p.EvidenceID(), Kind: "authentication"})
	}
	return err
}

func unavailable(inv *transport.Invocation, p *trust.Principal, cause error) *envelope.Error {
	err := envelope.New(envelope.CodeUnavailable, "humanwork.queue_unavailable", "the work queue is unavailable").WithDiagnostic(cause)
	if inv != nil {
		err.WithCorrelation(inv.RequestID())
	}
	if p != nil {
		err.WithEvidence(envelope.Evidence{ID: p.EvidenceID(), Kind: "authentication"})
	}
	return err
}
