// Package humanwork exposes the human-work queue read surface (WorkService
// ListWorkItems and GetWorkItem, EP-WORK-001) and the exclusive claim/release
// write surface (ClaimWorkItem and ReleaseWorkItem, EP-WORK-002).
//
// The service is deliberately thin (ARCH-GO-023): membership, visibility
// classification, the permitted-action set and current-authority claim/lease
// logic are all the workitem package's rules, reached through [Reader] and
// [Claims]; this package owns protocol, wire-level authorization, the signed
// stable queue cursor, idempotent replay (composed from
// internal/transport/endpoint's Coordinator) and the wire projection.
// CompleteWorkItem and DecideApproval remain P1B acceptance items
// (EP-WORK-003): they are registered and refuse with FAILED_PRECONDITION
// exactly as the proto contract specifies, rather than answering
// UNIMPLEMENTED. GetThresholdTable is a separate read todo and is left
// unimplemented.
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
	"github.com/monstercameron/human-capital-management-suite/internal/transport/endpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const (
	ListWorkItemsProcedure   = "/hcmnext.humanwork.v1.WorkService/ListWorkItems"
	GetWorkItemProcedure     = "/hcmnext.humanwork.v1.WorkService/GetWorkItem"
	ClaimWorkItemProcedure   = "/hcmnext.humanwork.v1.WorkService/ClaimWorkItem"
	ReleaseWorkItemProcedure = "/hcmnext.humanwork.v1.WorkService/ReleaseWorkItem"

	ActionListWorkItems      = "list_work_items"
	ActionGetWorkItem        = "get_work_item"
	ActionWorkItemGovernance = "work_item_governance_view"
	// ActionClaimWorkItem and ActionReleaseWorkItem are EP-WORK-002: the
	// wire-level "may this principal call this method at all" gate. They are
	// deliberately separate from the per-item current-authority check
	// [workitem.Store.ClaimCurrent] and [workitem.Store.Release] perform
	// fresh against the loaded row: this gate answers "is claiming or
	// releasing work items a capability this principal has", the domain
	// answers "does this principal currently stand as this item's assignee,
	// candidate or claimant".
	ActionClaimWorkItem   = "claim_work_item"
	ActionReleaseWorkItem = "release_work_item"

	defaultPageSize = 20
	maxPageSize     = 100
	cursorTTL       = 5 * time.Minute
	cursorVersion   = 1

	// defaultClaimLease is how long a claim [server.ClaimWorkItem] mints
	// stays live before it is eligible for the expiry release every read and
	// write in [workitem.Store] already performs on touch. It is a transport
	// policy default, overridable per deployment through
	// [Dependencies.ClaimLease]; EP-WORK-002's spec calls the lease
	// "optional" but [workitem.Store.Claim] itself requires a concrete
	// expiry, so some default has to live somewhere, and here is as narrow a
	// scope as that decision gets without inventing a policy document for it.
	defaultClaimLease = 15 * time.Minute
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

// Claims is the deliberately small write port ClaimWorkItem and
// ReleaseWorkItem call through (EP-WORK-002). Like [Reader], tenant, work
// item id and principal arrive as strings: the driver behind this port owns
// their typed forms and how a refusal is classified. Each method is expected
// to perform [workitem.Store.ClaimCurrent]/[workitem.Store.Release]'s own
// current-authority check and exclusive item_version compare-and-swap in one
// durable transaction; this package supplies no authorization or CAS logic
// of its own; it only decides whether the caller may reach this port at all
// and how the port's typed refusal projects onto the wire.
type Claims interface {
	// Claim performs one current-authority, version-bound, exclusive claim.
	Claim(ctx context.Context, tenant, workItemID, principal string, expectedVersion uint64, claimExpiresAt, now time.Time, meta workitem.TransitionMeta) (workitem.WorkItem, error)
	// Release performs one current-authority release of a live claim. An
	// already-expired lease is never released as if it were current: the
	// port is expected to refuse [workitem.CodeClaimExpired] exactly as
	// [workitem.Store.Release] does, rather than complete the release.
	Release(ctx context.Context, tenant, workItemID, principal string, expectedVersion uint64, now time.Time, meta workitem.TransitionMeta) (workitem.WorkItem, error)
}

type Dependencies struct {
	Queue     Reader
	Claims    Claims
	Authorize func(*trust.Principal, string) bool
	CursorKey []byte
	Now       func() time.Time
	// Idempotency composes ENDPOINT-004's Coordinator: exact replay of the
	// same idempotency key and payload returns the original result without
	// re-running the claim or release effect, and a stale expected revision
	// refuses with the current revision attached rather than reaching the
	// effect at all. Required for ClaimWorkItem and ReleaseWorkItem; a nil
	// Coordinator is treated as the write surface being unavailable rather
	// than silently skipping idempotency.
	Idempotency *endpoint.Coordinator
	// ClaimLease overrides [defaultClaimLease]. Zero means the default.
	ClaimLease time.Duration
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
	mux.Handle(ClaimWorkItemProcedure, connect.NewUnaryHandler(ClaimWorkItemProcedure, func(ctx context.Context, req *connect.Request[humanworkv1.ClaimWorkItemRequest]) (*connect.Response[humanworkv1.ClaimWorkItemResponse], error) {
		res, err := s.ClaimWorkItem(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(ReleaseWorkItemProcedure, connect.NewUnaryHandler(ReleaseWorkItemProcedure, func(ctx context.Context, req *connect.Request[humanworkv1.ReleaseWorkItemRequest]) (*connect.Response[humanworkv1.ReleaseWorkItemResponse], error) {
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

// ClaimWorkItem is EP-WORK-002: an atomic, version-bound, current-authority,
// idempotent claim. The wire-level authorization gate and non-disclosing
// visibility answer are this package's own (matching GetWorkItem exactly);
// current authority, the exclusive item_version compare-and-swap and the
// append-only evidence write are entirely [Dependencies.Claims]' job, driven
// by [workitem.Store.ClaimCurrent]. Idempotent replay and the stale-revision
// precondition are composed from ENDPOINT-004's [endpoint.Coordinator]
// rather than reimplemented here.
func (s *server) ClaimWorkItem(ctx context.Context, req *humanworkv1.ClaimWorkItemRequest) (*humanworkv1.ClaimWorkItemResponse, error) {
	item, inv, p, err := s.prepareMutation(ctx, req.GetWorkItemId(), req.GetIdempotencyKey(), req.GetExpectedItemVersion(), req.GetScope(), ActionClaimWorkItem)
	if err != nil {
		return nil, err
	}
	now := s.now()
	tenant := p.Tenant().String()
	expectedRev := req.GetExpectedItemVersion()
	idemReq := endpoint.Request{
		Scope:            endpoint.Scope{Principal: p.Subject(), Tenant: tenant, Capability: "humanwork.work_item.claim"},
		MessageKey:       req.GetIdempotencyKey(),
		Payload:          []byte(fmt.Sprintf("claim|%s|%d", item.WorkItemID.String(), expectedRev)),
		ExpectedRevision: &expectedRev,
		CurrentRevision:  uint64(item.ItemVersion),
	}
	lease := s.claimLease()
	meta := workitem.TransitionMeta{ActorPrincipalID: p.Subject(), Reason: "workitem.claimed_via_endpoint", At: now}
	if _, doErr := s.deps.Idempotency.Do(ctx, idemReq, func(ctx context.Context) (endpoint.Outcome, error) {
		claimed, claimErr := s.deps.Claims.Claim(ctx, tenant, item.WorkItemID.String(), p.Subject(), expectedRev, now.Add(lease), now, meta)
		if claimErr != nil {
			return endpoint.Outcome{}, claimErr
		}
		return endpoint.Outcome{Status: "OK", ResultDigest: claimed.WorkItemID.String()}, nil
	}); doErr != nil {
		return nil, s.mutationError(inv, p, doErr)
	}
	final, loadErr := s.deps.Queue.LoadItem(ctx, tenant, item.WorkItemID.String())
	if loadErr != nil {
		return nil, unavailable(inv, p, loadErr)
	}
	governed := s.authorized(p, ActionWorkItemGovernance)
	return &humanworkv1.ClaimWorkItemResponse{WorkItem: projectItem(final, workitem.MembershipOf(final, p.Subject(), now), governed)}, nil
}

// ReleaseWorkItem is EP-WORK-002's voluntary release, built the same way as
// ClaimWorkItem: only the driver behind [Dependencies.Claims] decides
// current authority (only the item's current live claimant may release) and
// performs the transition; the transport's own job is the visibility gate,
// idempotent replay and error projection.
func (s *server) ReleaseWorkItem(ctx context.Context, req *humanworkv1.ReleaseWorkItemRequest) (*humanworkv1.ReleaseWorkItemResponse, error) {
	item, inv, p, err := s.prepareMutation(ctx, req.GetWorkItemId(), req.GetIdempotencyKey(), req.GetExpectedItemVersion(), req.GetScope(), ActionReleaseWorkItem)
	if err != nil {
		return nil, err
	}
	now := s.now()
	tenant := p.Tenant().String()
	expectedRev := req.GetExpectedItemVersion()
	reasonRef := strings.TrimSpace(req.GetReasonRef())
	idemReq := endpoint.Request{
		Scope:            endpoint.Scope{Principal: p.Subject(), Tenant: tenant, Capability: "humanwork.work_item.release"},
		MessageKey:       req.GetIdempotencyKey(),
		Payload:          []byte(fmt.Sprintf("release|%s|%d|%s", item.WorkItemID.String(), expectedRev, reasonRef)),
		ExpectedRevision: &expectedRev,
		CurrentRevision:  uint64(item.ItemVersion),
	}
	meta := workitem.TransitionMeta{ActorPrincipalID: p.Subject(), Reason: "workitem.released_via_endpoint", Detail: reasonRef, At: now}
	if _, doErr := s.deps.Idempotency.Do(ctx, idemReq, func(ctx context.Context) (endpoint.Outcome, error) {
		released, relErr := s.deps.Claims.Release(ctx, tenant, item.WorkItemID.String(), p.Subject(), expectedRev, now, meta)
		if relErr != nil {
			return endpoint.Outcome{}, relErr
		}
		return endpoint.Outcome{Status: "OK", ResultDigest: released.WorkItemID.String()}, nil
	}); doErr != nil {
		return nil, s.mutationError(inv, p, doErr)
	}
	final, loadErr := s.deps.Queue.LoadItem(ctx, tenant, item.WorkItemID.String())
	if loadErr != nil {
		return nil, unavailable(inv, p, loadErr)
	}
	governed := s.authorized(p, ActionWorkItemGovernance)
	return &humanworkv1.ReleaseWorkItemResponse{WorkItem: projectItem(final, workitem.MembershipOf(final, p.Subject(), now), governed)}, nil
}

// prepareMutation is ClaimWorkItem and ReleaseWorkItem's shared boundary:
// authenticate, validate the three fields every mutating WorkService method
// requires, authorize the wire-level capability, and load-then-apply the
// exact non-disclosing visibility rule GetWorkItem uses, so a caller who
// cannot see an item cannot learn anything about it by trying to claim or
// release it either.
func (s *server) prepareMutation(
	ctx context.Context, workItemID, idempotencyKey string, expectedVersion uint64, scope *commonv1.ScopeContext, action string,
) (workitem.WorkItem, *transport.Invocation, *trust.Principal, error) {
	p, inv, err := trustedContext(ctx)
	if err != nil {
		return workitem.WorkItem{}, nil, nil, err
	}
	workItemID = strings.TrimSpace(workItemID)
	switch {
	case workItemID == "":
		return workitem.WorkItem{}, inv, p, invalid(inv, "work_item_id")
	case strings.TrimSpace(idempotencyKey) == "":
		return workitem.WorkItem{}, inv, p, invalid(inv, "idempotency_key")
	case expectedVersion == 0:
		// A work item's own version starts at 1 and only ever increases
		// (workitem.NewWorkItem); a zero here is a Go zero value asserting
		// "current" and must be refused, never treated as a wildcard match.
		return workitem.WorkItem{}, inv, p, invalid(inv, "expected_item_version")
	}
	if !s.authorized(p, action) {
		return workitem.WorkItem{}, inv, p, denied(inv, p)
	}
	tenant := p.Tenant().String()
	if scope != nil && scope.GetTenantId() != "" && scope.GetTenantId() != tenant {
		return workitem.WorkItem{}, inv, p, notFound(inv, p)
	}
	if s.deps.Queue == nil || s.deps.Claims == nil || s.deps.Idempotency == nil {
		return workitem.WorkItem{}, inv, p, unavailable(inv, p, ErrQueueEmpty)
	}
	item, loadErr := s.deps.Queue.LoadItem(ctx, tenant, workItemID)
	if loadErr != nil {
		if errors.Is(loadErr, ErrNotFound) || workitem.CodeOf(loadErr) == workitem.CodeWorkItemNotFound {
			return workitem.WorkItem{}, inv, p, notFound(inv, p)
		}
		return workitem.WorkItem{}, inv, p, unavailable(inv, p, loadErr)
	}
	now := s.now()
	m := workitem.MembershipOf(item, p.Subject(), now)
	governed := s.authorized(p, ActionWorkItemGovernance)
	inScope := item.OrganizationScopeID != "" && item.OrganizationScopeID == p.OrganizationScopeID()
	if !workitem.Visible(item, m, inScope, governed) {
		return workitem.WorkItem{}, inv, p, notFound(inv, p)
	}
	return item, inv, p, nil
}

// mutationError projects a Claim/Release failure onto the canonical error
// model, per planning/specs/http-grpc-endpoint-contract.md's table: an
// idempotency payload mismatch or a current-state conflict (the item is no
// longer claimable at the version the caller expected -- including having
// lost a claim race) is ALREADY_EXISTS/ABORTED; a stale expected revision or
// an unmet business precondition is FAILED_PRECONDITION; an authorization
// refusal is PERMISSION_DENIED. Every branch is a safe, owned summary: none
// of them echo who currently holds the item, only that it is not currently
// claimable and, for a revision conflict, what its current version is --
// exactly the "current-safe precondition data" GREEN requires without
// crossing the claim/candidate evidence compartment [workitem.EvidenceVisible]
// already draws for reads.
func (s *server) mutationError(inv *transport.Invocation, p *trust.Principal, err error) error {
	var revConflict *endpoint.RevisionConflict
	if errors.As(err, &revConflict) {
		e := envelope.New(envelope.CodeFailedPrecondition, "humanwork.stale_revision", "the expected item version is stale")
		e.WithViolation("expected_item_version", fmt.Sprintf("current item version is %d", revConflict.Current), "humanwork.stale_revision")
		if inv != nil {
			e.WithCorrelation(inv.RequestID())
		}
		return e
	}
	var payloadConflict *endpoint.PayloadConflict
	if errors.As(err, &payloadConflict) {
		e := envelope.New(envelope.CodeAborted, "humanwork.idempotency_key_reused", "the idempotency key was already used for a different request")
		if inv != nil {
			e.WithCorrelation(inv.RequestID())
		}
		return e
	}
	if errors.Is(err, endpoint.ErrIdempotencyKeyRequired) || errors.Is(err, endpoint.ErrIdempotencyKeyMismatch) || errors.Is(err, endpoint.ErrScopeRequired) {
		return invalid(inv, "idempotency_key")
	}
	switch workitem.CodeOf(err) {
	case workitem.CodeWorkItemNotFound:
		return notFound(inv, p)
	case workitem.CodeUnauthorizedClaimant:
		e := envelope.New(envelope.CodePermissionDenied, "humanwork.unauthorized_claimant",
			"the caller does not currently hold authority over this work item")
		if inv != nil {
			e.WithCorrelation(inv.RequestID())
		}
		if p != nil {
			e.WithEvidence(envelope.Evidence{ID: p.EvidenceID(), Kind: "authentication"})
		}
		return e
	case workitem.CodeAlreadyClaimed:
		e := envelope.New(envelope.CodeAborted, "humanwork.already_claimed",
			"the work item is no longer claimable at the expected version")
		if inv != nil {
			e.WithCorrelation(inv.RequestID())
		}
		return e
	case workitem.CodeStaleItem:
		e := envelope.New(envelope.CodeFailedPrecondition, "humanwork.stale_item", "the expected item version is stale")
		if inv != nil {
			e.WithCorrelation(inv.RequestID())
		}
		return e
	case workitem.CodeClaimExpired:
		e := envelope.New(envelope.CodeFailedPrecondition, "humanwork.claim_expired",
			"the claim expired and the item returned to its policy route")
		if inv != nil {
			e.WithCorrelation(inv.RequestID())
		}
		return e
	case workitem.CodeIllegalTransition:
		e := envelope.New(envelope.CodeFailedPrecondition, "humanwork.illegal_transition",
			"the work item is not in a state that accepts this action")
		if inv != nil {
			e.WithCorrelation(inv.RequestID())
		}
		return e
	case workitem.CodeInvalidRecord:
		return invalid(inv, "work_item")
	default:
		return unavailable(inv, p, err)
	}
}

// claimLease returns the effective claim lease duration.
func (s *server) claimLease() time.Duration {
	if s.deps.ClaimLease > 0 {
		return s.deps.ClaimLease
	}
	return defaultClaimLease
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
