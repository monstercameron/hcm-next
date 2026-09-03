package bootstrap_test

import (
	"context"
	"net"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/intent/app"
	"github.com/monstercameron/hcm-next/internal/intent/app/pgstore"
	kernelvalues "github.com/monstercameron/hcm-next/internal/kernel/values"
	platformexecution "github.com/monstercameron/hcm-next/internal/platform/execution"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
	"github.com/monstercameron/hcm-next/internal/transport"
	transportcell "github.com/monstercameron/hcm-next/internal/transport/cell"
	"github.com/monstercameron/hcm-next/internal/transport/edge"
	"github.com/monstercameron/hcm-next/internal/transport/envelope"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/workflow/execute"
	"github.com/monstercameron/hcm-next/internal/workflow/prototype"
)

// recordingTerminalWriter is the recording fake execute.TerminalWriter this
// suite uses in place of internal/workflow/execute/effects.LedgerTerminalWriter
// (a concurrent lane's own deliverable, and not this suite's to exercise): it
// records every call it receives and writes nothing itself, so
// TestExecuteIntentRunsThePromotionDriverUnderAuthority can assert that
// parking at the first approval WorkItem raises no governed business write at
// all.
type recordingTerminalWriter struct {
	mu    sync.Mutex
	calls []execute.TerminalWriteRequest
}

func (w *recordingTerminalWriter) Write(_ context.Context, _ dbport.Tx, req execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, req)
	return idempotency.ResultIdentity{EventRef: "fake-terminal:" + req.InstanceID.String()}, nil
}

func (w *recordingTerminalWriter) Calls() []execute.TerminalWriteRequest {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]execute.TerminalWriteRequest(nil), w.calls...)
}

// newExecutionCell composes the same P1A cell [newCell] does — the same
// pgtest schema, pgstore.Store, HMAC verifier and issued credential — plus
// the P1B execution-authority wiring: the real caller-driven promotion
// driver (internal/platform/execution.NewPromotionExecution) over this test's
// own database pool and the real internal/workflow/runtime and
// internal/humanwork/workitem stores, with terminal standing in for the
// governed business write [ExecuteIntent]'s driver raises at END.
//
// It duplicates [newCell]'s composition body (rather than composing through
// it and rewiring) because internal/intent/app.NewCell is called exactly
// once per composed cell, and this suite's whole point is proving what one
// specific composition — one with ExecutionAuthority — does; a cell
// assembled by mutating another test's already-published cell would prove
// nothing about a real composition root doing the same thing once.
func newExecutionCell(t *testing.T, terminal execute.TerminalWriter) *cell {
	t.Helper()
	c := newCell(t)

	execution, err := platformexecution.NewPromotionExecution(platformexecution.PromotionExecutionConfig{
		DB:                  c.pool,
		Terminal:            terminal,
		Clock:               func() time.Time { return baseTime },
		ApproverPrincipalID: "principal:promotion-approver",
		AuthorityDigest:     "sha256:test-p1b-authority-amendment",
		RequiredRole:        executionAuthorityTestRole,
	})
	if err != nil {
		t.Fatalf("NewPromotionExecution: %v", err)
	}

	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      testSigningKey,
		Issuer:   testIssuer,
		Audience: testAudience,
		Now:      func() time.Time { return baseTime },
	})
	if err != nil {
		t.Fatalf("NewHMACVerifier: %v", err)
	}
	token, err := verifier.Issue(trust.Claims{
		Issuer:               testIssuer,
		Audience:             testAudience,
		Subject:              testSubject,
		SubjectKind:          "human",
		Tenant:               testTenant,
		OrganizationScopeID:  testOrgScope,
		Roles:                []string{"intent_author", testRole, executionAuthorityTestRole},
		AuthorityRefs:        []string{"authority:position:vp-people"},
		Purposes:             []string{testPurpose, deniedPurpose, "workforce_analytics"},
		AuthenticationMethod: "bearer_token",
		Assurance:            "substantial",
		SessionRef:           "session-bootstrap-execution",
		IssuedAtUnix:         baseTime.Add(-time.Minute).Unix(),
		ExpiresAtUnix:        baseTime.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue execution-authority credential: %v", err)
	}

	ec := &cell{t: t, db: c.db, pool: c.pool, store: c.store, token: "Bearer " + token}
	composed, err := app.NewCell(app.CellConfig{
		Store:       c.store,
		Verifier:    verifier,
		Audience:    testAudience,
		MaxDeadline: 30 * time.Second,
		Logger:      transport.LoggerFunc(ec.appendRecord),
		Now:         func() time.Time { return baseTime },

		Executor:           execution.Executor,
		ExecutionAuthority: execution.Authority,
		ExecutionResolver:  execution.Resolver,
		ExecutionVersions:  execution.Versions,
		ExecutionCellID:    testCellID,
		TenantUUID:         func(tenant kernelvalues.TenantId) uuid.UUID { return pgstore.TenantID(string(tenant)) },
	})
	if err != nil {
		t.Fatalf("app.NewCell (execution): %v", err)
	}
	ec.app = composed

	grpcServer, err := transportcell.NewGRPCServer(composed)
	if err != nil {
		t.Fatalf("GRPCServer: %v", err)
	}
	listener := bufconn.Listen(1 << 20)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ec.grpcIntent = intentsv1.NewIntentServiceClient(conn)
	ec.grpcRegistry = registryv1.NewRegistryServiceClient(conn)

	edgeHandler, err := transportcell.NewEdgeHandler(composed)
	if err != nil {
		t.Fatalf("EdgeHandler: %v", err)
	}
	httpServer := httptest.NewServer(edgeHandler)
	t.Cleanup(httpServer.Close)
	ec.edgeIntent = edge.NewIntentClient(httpServer.Client(), httpServer.URL)
	ec.edgeRegistry = edge.NewRegistryClient(httpServer.Client(), httpServer.URL)
	ec.edgeURL = httpServer.URL
	ec.edgeClient = httpServer.Client()

	return ec
}

// mustParseUUID parses s or fails the test.
func mustParseUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return id
}

// inTenantConn runs fn with a tenant-scoped Executor, exactly the way every
// row-level-security-protected workflow/work-item table requires
// (internal/data/tenancy: the app.tenant_id session setting, set for the
// life of one transaction).
func inTenantConn(t *testing.T, c *cell, tenantID uuid.UUID, fn func(workitem.Executor) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("tenant scope: %v", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("transaction body: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// executionAuthorityTestRole is the role this suite's principal holds, so an
// execution-authority cell admits the same credential [newCell] already
// issued.
const executionAuthorityTestRole = "promotion_operator"

// TestExecuteIntentIsRefusedWithoutExecutionAuthority proves that a cell
// composed with no ExecutionAuthority — every cell today, including one that
// happens to carry a wired Executor — refuses ExecuteIntent under the exact
// P1A envelope every other governed write already uses, on both transports.
func TestExecuteIntentIsRefusedWithoutExecutionAuthority(t *testing.T) {
	c := newCell(t)
	seedWorkforce(t, c)
	ctx := c.grpcContext(context.Background())

	created, err := c.grpcIntent.CreateIntent(ctx, promoteWorkerRequest(t, "execute-refused-1"))
	if err != nil {
		t.Fatalf("CreateIntent: %v", err)
	}
	intentID := created.GetIntent().GetIntentId()
	req := &intentsv1.ExecuteIntentRequest{
		IdempotencyKey: "execute-refused-1",
		IntentId:       intentID,
		Approval: &intentsv1.ProposalApproval{
			ProposalRevisionId: "revision-does-not-matter",
			Approved:           true,
			ApprovalRef:        "approval:test-1",
		},
	}

	t.Run("grpc", func(t *testing.T) {
		_, err := c.grpcIntent.ExecuteIntent(ctx, req)
		if err == nil {
			t.Fatal("expected ExecuteIntent to be refused with no ExecutionAuthority configured")
		}
		owned, ok := envelope.FromGRPC(err)
		if !ok {
			t.Fatalf("grpc: ExecuteIntent failed with an unowned error: %v", err)
		}
		assertP1AWriteRefusal(t, "grpc", owned)
	})

	t.Run("edge", func(t *testing.T) {
		_, err := c.edgeIntent.ExecuteIntent(context.Background(), edgeRequest(c, req))
		if err == nil {
			t.Fatal("expected ExecuteIntent to be refused with no ExecutionAuthority configured")
		}
		owned, ok := edge.FromConnectError(err)
		if !ok {
			t.Fatalf("edge: ExecuteIntent failed with an unowned error: %v", err)
		}
		assertP1AWriteRefusal(t, "edge", owned)
	})
}

// assertP1AWriteRefusal asserts owned is exactly the shape every other P1A
// governed-write refusal (SubmitIntent, CancelIntent, SupersedeIntent) uses:
// FAILED_PRECONDITION, citing the release.p1a_zero_effect_ceiling rule.
func assertP1AWriteRefusal(t *testing.T, surface string, owned *envelope.Error) {
	t.Helper()
	if owned.Code() != envelope.CodeFailedPrecondition {
		t.Fatalf("%s: refused with %s, want FAILED_PRECONDITION", surface, owned.Code())
	}
	ceiling := false
	for _, violation := range owned.Violations() {
		if violation.RuleRef == "release.p1a_zero_effect_ceiling" {
			ceiling = true
		}
	}
	if !ceiling {
		t.Fatalf("%s: refused without citing the P1A ceiling: %v", surface, owned.Violations())
	}
}

// TestExecuteIntentRunsThePromotionDriverUnderAuthority proves the first
// executable run end to end: a cell explicitly composed with
// ExecutionAuthority and a real caller-driven driver runs ExecuteIntent for
// an approved promote_worker proposal, parks the new instance at its first
// approval WorkItem, returns a receipt naming it, and raises no governed
// business write at all while parked.
func TestExecuteIntentRunsThePromotionDriverUnderAuthority(t *testing.T) {
	terminal := &recordingTerminalWriter{}
	c := newExecutionCell(t, terminal)
	seedWorkforce(t, c)
	ctx := c.grpcContext(context.Background())

	created, err := c.grpcIntent.CreateIntent(ctx, promoteWorkerRequest(t, "execute-authority-1"))
	if err != nil {
		t.Fatalf("CreateIntent: %v", err)
	}
	intentID := created.GetIntent().GetIntentId()

	simulated, err := c.grpcIntent.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{IntentId: intentID})
	if err != nil {
		t.Fatalf("SimulateIntent: %v", err)
	}
	revisionID := simulated.GetSimulation().GetProposalRevisionId()
	if revisionID == "" {
		t.Fatal("simulation minted no proposal revision; the fixture is not READY")
	}
	materialDigest := simulated.GetSimulation().GetMaterialProposalDigest()

	execReq := &intentsv1.ExecuteIntentRequest{
		IdempotencyKey:          "execute-authority-1",
		IntentId:                intentID,
		ExpectedInstanceVersion: created.GetIntent().GetInstanceVersion(),
		Approval: &intentsv1.ProposalApproval{
			ProposalRevisionId:     revisionID,
			MaterialProposalDigest: materialDigest,
			Approved:               true,
			ApprovalRef:            "approval:execute-authority-1",
		},
	}

	executed, err := c.grpcIntent.ExecuteIntent(ctx, execReq)
	if err != nil {
		t.Fatalf("ExecuteIntent: %v", err)
	}
	receipt := executed.GetExecution()
	if receipt.GetInstanceId() == "" {
		t.Fatal("receipt names no workflow instance")
	}
	if receipt.GetInstanceVersion() == 0 {
		t.Fatal("receipt names no instance version")
	}
	if len(receipt.GetParkedContinuations()) == 0 {
		t.Fatal("receipt names no parked continuation; the driver should have parked at the first approval")
	}
	found := false
	for _, node := range receipt.GetVisitedNodes() {
		if node == prototype.NodeApproval {
			found = true
		}
	}
	if !found {
		t.Fatalf("receipt visited nodes = %v, want %q among them", receipt.GetVisitedNodes(), prototype.NodeApproval)
	}

	// The instance parked at exactly one approval WorkItem, real and
	// durable in internal/humanwork/workitem's own store.
	tenantID := pgstore.TenantID(testTenant)
	instanceID := mustParseUUID(t, receipt.GetInstanceId())
	var items []workitem.WorkItem
	inTenantConn(t, c, tenantID, func(ex workitem.Executor) error {
		var loadErr error
		items, loadErr = workitem.Store{}.ListForInstance(context.Background(), ex, tenantID, instanceID)
		return loadErr
	})
	if len(items) != 1 {
		t.Fatalf("work items for instance %s = %d, want exactly 1", instanceID, len(items))
	}
	if items[0].WorkType != prototype.ApprovalRequirementID {
		t.Fatalf("parked work item type = %q, want %q", items[0].WorkType, prototype.ApprovalRequirementID)
	}
	if items[0].Status != workitem.StatusAssigned {
		t.Fatalf("parked work item status = %s, want %s", items[0].Status, workitem.StatusAssigned)
	}

	// Parking at APPROVAL never reaches END: the recording fake proves no
	// governed business write happened at all.
	if calls := terminal.Calls(); len(calls) != 0 {
		t.Fatalf("terminal writer calls = %d, want 0 while the instance is parked at approval", len(calls))
	}

	t.Run("edge transport agrees", func(t *testing.T) {
		created, err := c.edgeIntent.CreateIntent(context.Background(), edgeRequest(c, promoteWorkerRequest(t, "execute-authority-2")))
		if err != nil {
			t.Fatalf("CreateIntent: %v", err)
		}
		intentID := created.Msg.GetIntent().GetIntentId()
		simulated, err := c.edgeIntent.SimulateIntent(context.Background(),
			edgeRequest(c, &intentsv1.SimulateIntentRequest{IntentId: intentID}))
		if err != nil {
			t.Fatalf("SimulateIntent: %v", err)
		}
		execReq := &intentsv1.ExecuteIntentRequest{
			IdempotencyKey: "execute-authority-2",
			IntentId:       intentID,
			Approval: &intentsv1.ProposalApproval{
				ProposalRevisionId:     simulated.Msg.GetSimulation().GetProposalRevisionId(),
				MaterialProposalDigest: simulated.Msg.GetSimulation().GetMaterialProposalDigest(),
				Approved:               true,
				ApprovalRef:            "approval:execute-authority-2",
			},
		}
		executed, err := c.edgeIntent.ExecuteIntent(context.Background(), edgeRequest(c, execReq))
		if err != nil {
			t.Fatalf("ExecuteIntent (edge): %v", err)
		}
		if executed.Msg.GetExecution().GetInstanceId() == "" {
			t.Fatal("edge receipt names no workflow instance")
		}
	})
}
